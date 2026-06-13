package kit

// Sink is where an operation's records go. Each surface supplies its own: the
// CLI sink renders to the terminal, the API sink streams an HTTP response, the
// MCP sink accumulates structured tool content. A handler only ever calls
// emit(record); swapping the sink swaps the surface with no change to domain
// code.
type Sink interface {
	Emit(rec any) error
	Flush() error
}

// errStop unwinds an emit loop once the row limit is reached. It is generated
// and swallowed inside Invoke and never surfaces to a handler or a user.
type stopError struct{}

func (stopError) Error() string { return "stop" }

var errStop = stopError{}
