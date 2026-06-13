// Package errs defines the typed error taxonomy shared by every kit-based CLI
// and its one true mapping to CLI exit codes, HTTP status codes, and MCP error
// objects. A domain returns these so the surfaces classify failures the same
// way everywhere, replacing the bespoke error-to-exit-code map each repo used
// to hand-roll.
package errs

import (
	"errors"
	"fmt"
)

// Kind is the classification of a failure. The zero value (KindOK) means no
// error. Each kind has a fixed CLI exit code and HTTP status (see Code/Status).
type Kind int

const (
	KindOK          Kind = iota // success
	KindGeneric                 // generic failure (exit 1)
	KindUsage                   // bad flags/args (exit 2)
	KindNoResults               // the query yielded nothing (exit 3)
	KindNeedAuth                // credentials required or invalid (exit 4)
	KindRateLimited             // upstream exhausted after retries (exit 5)
	KindNotFound                // entity/id does not exist (exit 6)
	KindUnsupported             // capability not available for this site (exit 7)
	KindNetwork                 // transport failure (exit 8)
)

// Error is a classified error carrying a Kind plus the underlying cause.
type Error struct {
	Kind Kind
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	switch {
	case e.Msg != "" && e.Err != nil:
		return e.Msg + ": " + e.Err.Error()
	case e.Msg != "":
		return e.Msg
	case e.Err != nil:
		return e.Err.Error()
	default:
		return kindName(e.Kind)
	}
}

func (e *Error) Unwrap() error { return e.Err }

// New builds a classified error from a kind and a message (printf-style).
func New(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// Wrap classifies an existing error under a kind, keeping it unwrappable.
func Wrap(kind Kind, err error, format string, args ...any) *Error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...), Err: err}
}

// Convenience constructors for the common kinds.
func Usage(format string, args ...any) *Error       { return New(KindUsage, format, args...) }
func NoResults(format string, args ...any) *Error   { return New(KindNoResults, format, args...) }
func NeedAuth(format string, args ...any) *Error    { return New(KindNeedAuth, format, args...) }
func RateLimited(format string, args ...any) *Error { return New(KindRateLimited, format, args...) }
func NotFound(format string, args ...any) *Error    { return New(KindNotFound, format, args...) }
func Unsupported(format string, args ...any) *Error { return New(KindUnsupported, format, args...) }
func Network(format string, args ...any) *Error     { return New(KindNetwork, format, args...) }

// KindOf returns the Kind of any error: an *Error's kind, or KindGeneric for a
// non-nil unclassified error, or KindOK for nil.
func KindOf(err error) Kind {
	if err == nil {
		return KindOK
	}
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Kind
	}
	return KindGeneric
}

// ExitCode maps an error to the stable CLI exit code.
func ExitCode(err error) int {
	switch KindOf(err) {
	case KindOK:
		return 0
	case KindUsage:
		return 2
	case KindNoResults:
		return 3
	case KindNeedAuth:
		return 4
	case KindRateLimited:
		return 5
	case KindNotFound:
		return 6
	case KindUnsupported:
		return 7
	case KindNetwork:
		return 8
	default:
		return 1
	}
}

// HTTPStatus maps an error to the HTTP status the API surface returns.
func HTTPStatus(err error) int {
	switch KindOf(err) {
	case KindOK:
		return 200
	case KindUsage:
		return 400
	case KindNoResults, KindNotFound:
		return 404
	case KindNeedAuth:
		return 401
	case KindRateLimited:
		return 429
	case KindUnsupported:
		return 422
	case KindNetwork:
		return 502
	default:
		return 500
	}
}

func kindName(k Kind) string {
	switch k {
	case KindOK:
		return "ok"
	case KindUsage:
		return "usage error"
	case KindNoResults:
		return "no results"
	case KindNeedAuth:
		return "authentication required"
	case KindRateLimited:
		return "rate limited"
	case KindNotFound:
		return "not found"
	case KindUnsupported:
		return "unsupported"
	case KindNetwork:
		return "network error"
	default:
		return "error"
	}
}
