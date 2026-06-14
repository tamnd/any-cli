package kit

import (
	"reflect"
	"time"
)

// Envelope is the self-describing form a record takes when it is written to the
// data surface (8000_uri §6): the record's own fields under Data, wrapped with
// the URI that names it (@id), its type (@type), when it was fetched (@fetched),
// and its outbound graph edges as URIs (@links). A tree of these is walkable
// without the domain code that produced them, because every name is a URI.
type Envelope struct {
	ID      string              `json:"@id"`
	Type    string              `json:"@type"`
	Fetched string              `json:"@fetched,omitempty"`
	Links   map[string][]string `json:"@links,omitempty"`
	Data    any                 `json:"data"`
}

// Wrap builds the Envelope for a record: it mints the record's URI, reads its
// link fields as URIs, and stamps the fetch time (zero time omits @fetched).
// Links are grouped by the record field they came from, so a consumer can tell a
// book's author edge from its similar-books edges.
func (h *Host) Wrap(rec any, fetched time.Time) (Envelope, error) {
	u, err := h.Mint(rec)
	if err != nil {
		return Envelope{}, err
	}
	env := Envelope{
		ID:   u.String(),
		Type: u.Scheme + "/" + u.Authority,
		Data: rec,
	}
	if !fetched.IsZero() {
		env.Fetched = fetched.UTC().Format(time.RFC3339)
	}
	if links := h.linksByField(rec); len(links) > 0 {
		env.Links = links
	}
	return env, nil
}

// linksByField groups a record's outbound link URIs by the source field's json
// name, the shape Envelope.Links wants.
func (h *Host) linksByField(rec any) map[string][]string {
	t := derefType(reflect.TypeOf(rec))
	if t == nil {
		return nil
	}
	out := map[string][]string{}
	for _, lf := range linkFields(t) {
		scheme := canonScheme(lf.scheme)
		for _, id := range fieldStrings(rec, lf.index) {
			u := URI{Scheme: scheme, Authority: lf.authority, Path: splitID(id)}
			out[lf.jsonName] = append(out[lf.jsonName], u.String())
		}
	}
	return out
}
