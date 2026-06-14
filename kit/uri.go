package kit

import (
	"net/url"
	"sort"
	"strings"

	"github.com/tamnd/any-cli/kit/errs"
)

// URI is a parsed resource URI (8000_uri §5.2): a name for a record, not a
// location. The scheme is a site slug ("goodreads") or a cross-site kind
// ("host", "pages", "feed", "search", "data"); for a site scheme the authority
// is the record type and the path is the id, while for host/pages the authority
// is a real hostname.
type URI struct {
	Scheme    string
	Authority string
	Path      []string
	Query     map[string]string
	Fragment  string
}

// reservedKinds are the cross-site schemes a registered domain may not claim, so
// the universal fallbacks (8000_uri §2.2) can never be shadowed.
var reservedKinds = map[string]bool{
	"host":   true,
	"pages":  true,
	"feed":   true,
	"search": true,
	"data":   true,
}

// IsReservedKind reports whether scheme is one of the cross-site kind schemes.
func IsReservedKind(scheme string) bool { return reservedKinds[scheme] }

// ParseURI parses a canonical-shaped resource URI. It is deliberately strict:
// it does not normalize messy https URLs or bare ids (that is Host.Resolve's
// job, which reuses a domain's parser); it only reads an already-URI-shaped
// string into its parts. The scheme and authority are lower-cased; path segments
// keep their case, since some domains have case-sensitive ids.
func ParseURI(s string) (URI, error) {
	s = strings.TrimSpace(s)
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok || scheme == "" {
		return URI{}, errs.Usage("not a resource URI: %q (want scheme://authority/id)", s)
	}
	scheme = strings.ToLower(scheme)
	if !schemeOK(scheme) {
		return URI{}, errs.Usage("bad URI scheme: %q", scheme)
	}
	// http(s) is a location, never a resource-URI name; reject it so Host.Resolve
	// routes a pasted link to the domain that owns its host instead of mistaking
	// it for a URI.
	if scheme == "http" || scheme == "https" {
		return URI{}, errs.Usage("%q is a URL, not a resource URI", s)
	}

	var u URI
	u.Scheme = scheme

	// Peel the fragment, then the query, from the tail.
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		u.Fragment = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		u.Query = parseQuery(rest[i+1:])
		rest = rest[:i]
	}

	// authority is up to the first slash; the remainder is the id path. We keep
	// the path tail verbatim before splitting so an embedded URL id (archive's
	// web/<ts>/<url>) survives, then split on "/" for ordinary ids.
	authority, pathTail, _ := strings.Cut(rest, "/")
	u.Authority = strings.ToLower(authority)
	if u.Authority == "" {
		return URI{}, errs.Usage("URI has no authority: %q", s)
	}
	for seg := range strings.SplitSeq(pathTail, "/") {
		if seg == "" {
			continue
		}
		if dec, err := url.PathUnescape(seg); err == nil {
			seg = dec
		}
		u.Path = append(u.Path, seg)
	}
	return u, nil
}

// String renders the canonical serialization, the inverse of ParseURI. The query
// keys are sorted so equal URIs serialize byte-for-byte equal (8000_uri §3.2).
func (u URI) String() string {
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	b.WriteString(u.Authority)
	for _, seg := range u.Path {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(seg))
	}
	if len(u.Query) > 0 {
		keys := make([]string, 0, len(u.Query))
		for k := range u.Query {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('?')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(u.Query[k]))
		}
	}
	if u.Fragment != "" {
		b.WriteByte('#')
		b.WriteString(u.Fragment)
	}
	return b.String()
}

// ID is the path joined with "/", the record's id (with any sub-ids). It is the
// arg a resolver op receives.
func (u URI) ID() string { return strings.Join(u.Path, "/") }

// DataPath is the on-disk path for this URI under the data root, without an
// extension: <scheme>/<authority>/<id...>. It is the single rule of 8000_uri
// §6.1 — the file path is the URI — so a tree of these is self-describing.
func (u URI) DataPath() string {
	parts := append([]string{u.Scheme, u.Authority}, u.Path...)
	for i, p := range parts {
		parts[i] = safeSegment(p)
	}
	return strings.Join(parts, "/")
}

// safeSegment makes a URI segment safe to use as a single path component: it
// percent-encodes a slash so an embedded-URL id does not explode into extra
// directories, leaving other characters legible.
func safeSegment(s string) string {
	return strings.ReplaceAll(s, "/", "%2F")
}

// schemeOK validates a scheme token: lowercase letter then letters/digits.
func schemeOK(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func parseQuery(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := map[string]string{}
	for pair := range strings.SplitSeq(s, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		if dk, err := url.QueryUnescape(k); err == nil {
			k = dk
		}
		if dv, err := url.QueryUnescape(v); err == nil {
			v = dv
		}
		out[k] = v
	}
	return out
}
