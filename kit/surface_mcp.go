package kit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"github.com/tamnd/any-cli/kit/errs"
)

// mcpCommand adds the `mcp` subcommand, which serves the operations as MCP tools
// over stdio JSON-RPC. Each registered operation becomes one tool named
// "<verb>"; its inputSchema is the operation's parameter schema. Escape-hatch
// commands are not exposed.
func (a *App) mcpCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "mcp",
		Short:  "Run as an MCP server over stdio",
		Hidden: false,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.serveMCP(cmd.Context(), os.Stdin, os.Stdout)
		},
	}
}

// JSON-RPC 2.0 envelopes.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (a *App) serveMCP(ctx context.Context, in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(bufio.NewReader(in))
	enc := json.NewEncoder(out)
	var wmu sync.Mutex
	write := func(resp rpcResponse) {
		wmu.Lock()
		defer wmu.Unlock()
		resp.JSONRPC = "2.0"
		_ = enc.Encode(resp)
	}

	for {
		var req rpcRequest
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		// Notifications carry no id and need no response.
		switch req.Method {
		case "initialize":
			write(rpcResponse{ID: req.ID, Result: a.mcpInitResult()})
		case "tools/list":
			write(rpcResponse{ID: req.ID, Result: map[string]any{"tools": a.mcpTools()}})
		case "tools/call":
			write(a.mcpCall(ctx, req))
		case "ping":
			write(rpcResponse{ID: req.ID, Result: map[string]any{}})
		case "notifications/initialized":
			// no response
		default:
			if len(req.ID) > 0 {
				write(rpcResponse{ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
			}
		}
	}
}

func (a *App) mcpInitResult() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]any{
			"name":    a.id.Binary,
			"version": a.id.Version,
		},
		"instructions": a.id.Short,
	}
}

func (a *App) mcpTools() []map[string]any {
	tools := make([]map[string]any, 0, len(a.ops))
	for _, op := range a.ops {
		m := op.Meta()
		desc := m.Summary
		if m.Write {
			desc += " (writes state)"
		}
		tools = append(tools, map[string]any{
			"name":        m.toolName(),
			"description": desc,
			"inputSchema": op.InputSchema(),
		})
	}
	return tools
}

func (a *App) mcpCall(ctx context.Context, req rpcRequest) rpcResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}}
	}
	op, ok := a.byName[params.Name]
	if !ok {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: "unknown tool: " + params.Name}}
	}

	st := FromContext(ctx)
	if st == nil {
		return rpcResponse{ID: req.ID, Result: toolError(errs.New(errs.KindGeneric, "no run state"))}
	}
	client, err := st.Client(ctx)
	if err != nil {
		return rpcResponse{ID: req.ID, Result: toolError(err)}
	}

	in := mcpInput(op, params.Arguments, st.Globals.Limit)
	sink := &collectSink{}
	rt := RunContext{Client: client, Store: st.store, Limit: st.Globals.Limit}
	if err := op.Invoke(ctx, in, rt, sink); err != nil && errs.KindOf(err) != errs.KindNoResults {
		return rpcResponse{ID: req.ID, Result: toolError(err)}
	}
	return rpcResponse{ID: req.ID, Result: sink.toolResult()}
}

// mcpInput splits the tool arguments object into positional args (by the
// operation's declared arg names) and named flags.
func mcpInput(op Operation, args map[string]any, limit int) Input {
	m := op.Meta()
	flags := maps.Clone(args)
	if flags == nil {
		flags = map[string]any{}
	}
	var positional []string
	for _, arg := range m.Args {
		if v, ok := args[arg.Name]; ok {
			delete(flags, arg.Name)
			if arg.Variadic {
				positional = append(positional, toStringSlice(v)...)
				continue
			}
			positional = append(positional, fmt.Sprint(v))
		}
	}
	return Input{Args: positional, Flags: flags, Globals: Globals{Limit: limit}}
}

func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, len(s))
		for i, e := range s {
			out[i] = fmt.Sprint(e)
		}
		return out
	case string:
		return splitList(s)
	default:
		return []string{fmt.Sprint(v)}
	}
}

// collectSink gathers emitted records for an MCP tool result: structured content
// as the records array, plus a text rendering for clients that show text.
type collectSink struct {
	recs []any
}

func (s *collectSink) Emit(rec any) error {
	s.recs = append(s.recs, rec)
	return nil
}

func (s *collectSink) Flush() error { return nil }

func (s *collectSink) toolResult() map[string]any {
	text, _ := json.MarshalIndent(s.recs, "", "  ")
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(text)},
		},
		"structuredContent": map[string]any{"records": s.recs},
	}
}

func toolError(err error) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{
			{"type": "text", "text": err.Error()},
		},
	}
}
