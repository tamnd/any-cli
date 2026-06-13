package kit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/any-cli/kit/errs"
)

// serveCommand adds the `serve` subcommand, which exposes every read operation
// as an HTTP endpoint. A GET to /v1/<verb> runs the operation; positional args
// come from the path tail, flags from the query string. Records stream back as
// NDJSON (one JSON object per line) so a long crawl is consumable as it runs.
// Write operations are reachable only with --allow-writes.
func (a *App) serveCommand(g *globalFlags) *cobra.Command {
	var (
		addr        string
		allowWrites bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the operations over HTTP (NDJSON)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := FromContext(cmd.Context())
			if st == nil {
				var err error
				st, err = a.newState(cmd.Context(), g)
				if err != nil {
					return err
				}
			}
			srv := a.httpServer(st, allowWrites)
			httpd := &http.Server{Addr: addr, Handler: srv}
			fmt.Fprintf(cmd.OutOrStdout(), "%s serving on %s\n", a.id.Binary, addr)
			ctx := cmd.Context()
			go func() {
				<-ctx.Done()
				_ = httpd.Close()
			}()
			if err := httpd.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	cmd.Flags().BoolVar(&allowWrites, "allow-writes", false, "expose write operations")
	return cmd
}

func (a *App) httpServer(st *State, allowWrites bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "binary": a.id.Binary})
	})
	mux.HandleFunc("/v1/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, a.openAPI())
	})
	for _, op := range a.ops {
		m := op.Meta()
		if m.Write && !allowWrites {
			continue
		}
		opCopy := op
		route := "/v1/" + m.routePath()
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			a.handleHTTP(w, r, opCopy, st)
		})
		// Allow trailing positional args after the verb: /v1/get/<id>.
		mux.HandleFunc(route+"/", func(w http.ResponseWriter, r *http.Request) {
			a.handleHTTP(w, r, opCopy, st)
		})
	}
	return mux
}

func (a *App) handleHTTP(w http.ResponseWriter, r *http.Request, op Operation, st *State) {
	ctx := r.Context()
	m := op.Meta()

	tail := strings.TrimPrefix(r.URL.Path, "/v1/"+m.routePath())
	tail = strings.Trim(tail, "/")
	var pathArgs []string
	if tail != "" {
		pathArgs = strings.Split(tail, "/")
	}

	flags := map[string]any{}
	for k, vs := range r.URL.Query() {
		if len(vs) == 1 {
			flags[k] = vs[0]
		} else {
			flags[k] = vs
		}
	}
	limit := st.Globals.Limit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}

	client, err := st.Client(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	in := Input{Args: pathArgs, Flags: flags, Globals: Globals{Limit: limit}}
	sink := &ndjsonSink{w: w}
	rt := RunContext{Client: client, Store: st.store, Limit: limit}
	if err := op.Invoke(ctx, in, rt, sink); err != nil && errs.KindOf(err) != errs.KindNoResults && !sink.wrote {
		writeErr(w, err)
		return
	}
	sink.Flush()
}

// ndjsonSink streams one JSON object per line. Once a record is written the
// status is already 200, so a later error can only be logged in a trailer line.
type ndjsonSink struct {
	w     http.ResponseWriter
	enc   *json.Encoder
	wrote bool
}

func (s *ndjsonSink) Emit(rec any) error {
	if s.enc == nil {
		s.enc = json.NewEncoder(s.w)
	}
	s.wrote = true
	if err := s.enc.Encode(rec); err != nil {
		return err
	}
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func (s *ndjsonSink) Flush() error { return nil }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	writeJSON(w, errs.HTTPStatus(err), map[string]any{
		"error": err.Error(),
		"kind":  int(errs.KindOf(err)),
	})
}

// openAPI builds a minimal OpenAPI 3.1 document describing the read operations,
// enough for client generation and discovery.
func (a *App) openAPI() map[string]any {
	paths := map[string]any{}
	for _, op := range a.ops {
		m := op.Meta()
		params := []map[string]any{}
		for _, p := range op.Params() {
			loc := "query"
			required := false
			if p.Kind == KindArg {
				continue // path args are described in the path template below
			}
			params = append(params, map[string]any{
				"name":     p.Name,
				"in":       loc,
				"required": required,
				"schema":   schemaForParam(p),
			})
		}
		paths["/v1/"+m.routePath()] = map[string]any{
			"get": map[string]any{
				"summary":     m.Summary,
				"description": m.Long,
				"operationId": m.toolName(),
				"parameters":  params,
				"responses": map[string]any{
					"200": map[string]any{
						"description": "NDJSON stream of records",
						"content": map[string]any{
							"application/x-ndjson": map[string]any{
								"schema": op.OutputSchema(),
							},
						},
					},
				},
			},
		}
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   a.id.Binary,
			"version": a.id.Version,
			"summary": a.id.Short,
		},
		"paths": paths,
	}
}
