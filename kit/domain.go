package kit

import (
	"fmt"
	"slices"
	"sync"
)

// Domain is the driver for one site's data and its URI scheme. A site's library
// package registers an implementation from its init() with Register, so a blank
// import is enough to enable the domain — exactly as a database/sql program
// enables a driver with `import _ "github.com/lib/pq"`. The same Domain also
// builds the site's single binary (App), so the binary and a multi-domain host
// (ant) share one source of truth per site.
type Domain interface {
	// Info returns the domain's identity and URI-scheme metadata.
	Info() DomainInfo
	// Register installs this domain's operations, client factory, and any
	// domain-global flags onto app. It must be deterministic and do no I/O.
	Register(app *App)
}

// DomainInfo is the static descriptor of a domain.
type DomainInfo struct {
	Scheme   string   // canonical URI scheme and binary slug, e.g. "goodreads"
	Aliases  []string // alternate schemes that canonicalize to Scheme, e.g. {"ytb"}
	Hosts    []string // hostnames this domain owns, e.g. {"goodreads.com"}; used to resolve a pasted https URL to this domain
	Identity Identity // reused by the single-site main to seed help/version
}

// Resolver is the optional capability a Domain implements to support `ant
// resolve` and `ant url`. It is the home of the domain's existing idOrURL parser
// (Classify) and XxxURL helper (Locate); both are pure string functions, so the
// URI-native verbs touch no network. A Domain that omits Resolver is still fully
// addressable for inputs that are already canonical URIs.
type Resolver interface {
	// Classify turns any accepted input — a bare id, an @handle, a messy https
	// URL — into the canonical (uriType, id). A bare id with no type hint
	// resolves to the domain's default type.
	Classify(input string) (uriType, id string, err error)
	// Locate is the inverse for one resource: the live https location for a
	// (uriType, id).
	Locate(uriType, id string) (url string, err error)
}

var (
	domainsMu sync.RWMutex
	domains   = map[string]Domain{} // canonical scheme -> domain
	aliases   = map[string]string{} // alias or canonical scheme -> canonical scheme
)

// Register makes a Domain available to every host in the process. It is meant to
// be called from a domain package's init(). It panics — like sql.Register — if
// dom is nil, if its scheme is empty or malformed, if the scheme shadows a
// reserved kind (host/pages/feed/search/data), or if the scheme or any alias
// collides with an already-registered domain.
func Register(dom Domain) {
	if dom == nil {
		panic("kit: Register(nil)")
	}
	info := dom.Info()
	scheme := info.Scheme
	if !schemeOK(scheme) {
		panic("kit: Register: bad domain scheme: " + scheme)
	}
	if IsReservedKind(scheme) {
		panic("kit: Register: scheme shadows a reserved kind: " + scheme)
	}
	domainsMu.Lock()
	defer domainsMu.Unlock()
	if _, dup := domains[scheme]; dup {
		panic("kit: Register: domain registered twice: " + scheme)
	}
	if owner, taken := aliases[scheme]; taken {
		panic(fmt.Sprintf("kit: Register: scheme %q already used by %q", scheme, owner))
	}
	domains[scheme] = dom
	aliases[scheme] = scheme
	for _, al := range info.Aliases {
		if !schemeOK(al) {
			panic("kit: Register: bad alias: " + al)
		}
		if IsReservedKind(al) {
			panic("kit: Register: alias shadows a reserved kind: " + al)
		}
		if owner, taken := aliases[al]; taken {
			panic(fmt.Sprintf("kit: Register: alias %q already used by %q", al, owner))
		}
		aliases[al] = scheme
	}
}

// Domains returns the canonical schemes of all registered domains, sorted. It is
// the analogue of sql.Drivers().
func Domains() []string {
	domainsMu.RLock()
	defer domainsMu.RUnlock()
	out := make([]string, 0, len(domains))
	for s := range domains {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// Lookup returns the registered domain for a scheme or one of its aliases,
// resolving the alias to the canonical domain. ok is false if no domain claims
// the scheme.
func Lookup(scheme string) (Domain, bool) {
	domainsMu.RLock()
	defer domainsMu.RUnlock()
	canon, ok := aliases[scheme]
	if !ok {
		return nil, false
	}
	return domains[canon], true
}

// canonScheme maps an alias to its canonical scheme, or returns the input
// unchanged when it is not a known alias (e.g. a reserved kind).
func canonScheme(scheme string) string {
	domainsMu.RLock()
	defer domainsMu.RUnlock()
	if canon, ok := aliases[scheme]; ok {
		return canon
	}
	return scheme
}

// resetDomainsForTest clears the registry. Tests that register a fake domain
// call it to stay isolated; production code never does.
func resetDomainsForTest() {
	domainsMu.Lock()
	defer domainsMu.Unlock()
	domains = map[string]Domain{}
	aliases = map[string]string{}
}
