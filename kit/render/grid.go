package render

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

// flushGrid draws the buffered table or markdown grid. Both share one lipgloss
// table; the format chooses the border, whether the header is uppercased, and
// whether the accent colors are emitted.
func (r *Renderer) flushGrid() error {
	if !r.gridSeen {
		return nil // no records: emit nothing, like an empty stream
	}
	md := r.o.Format == Markdown

	head := r.gridHead
	rows := r.gridRows
	if md {
		// A literal pipe or newline in a cell would break the column boundaries
		// of a GitHub pipe table, so escape them before lipgloss lays it out.
		head = escapeMarkdownRow(head)
		rows = make([][]string, len(r.gridRows))
		for i, row := range r.gridRows {
			rows[i] = escapeMarkdownRow(row)
		}
	} else {
		head = upper(head)
	}

	t := table.New().Rows(rows...)
	if !r.o.NoHeader {
		t = t.Headers(head...)
	}

	if md {
		// No outer top/bottom rule, never colored, full-width content so the
		// table pastes cleanly into docs, issues, and READMEs.
		t = t.Border(lipgloss.MarkdownBorder()).
			BorderTop(false).
			BorderBottom(false).
			StyleFunc(markdownStyle)
		_, err := io.WriteString(r.w, t.String()+"\n")
		return err
	}

	t = t.Border(lipgloss.RoundedBorder()).
		BorderStyle(r.borderStyle()).
		StyleFunc(r.tableStyle())
	out := t.String()
	// Shrink a too-wide table to the terminal so it never wraps at the edge;
	// lipgloss redistributes the slack to the widest columns. Only ever shrink:
	// constraining a table that already fits would stretch it to fill the width.
	if r.o.Width > 0 && lipgloss.Width(out) > r.o.Width {
		out = t.Width(r.o.Width).String()
	}
	_, err := io.WriteString(r.w, out+"\n")
	return err
}

// padded is the shared cell style: one space of breathing room on each side.
var padded = lipgloss.NewStyle().Padding(0, 1)

// markdownStyle keeps every cell plain and left-aligned so the rendered pipes
// stay valid markdown.
func markdownStyle(int, int) lipgloss.Style { return padded.Align(lipgloss.Left) }

// escapeMarkdownRow makes a row's cells safe inside a GitHub pipe table: a
// literal pipe becomes \|, and any newline collapses to a space so the cell
// stays on one table line.
func escapeMarkdownRow(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		c = strings.ReplaceAll(c, "\n", " ")
		c = strings.ReplaceAll(c, "|", "\\|")
		out[i] = c
	}
	return out
}

// borderStyle dims the grid lines when color is on, else leaves them plain.
func (r *Renderer) borderStyle() lipgloss.Style {
	if r.o.Color {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	}
	return lipgloss.NewStyle()
}

// tableStyle styles the terminal grid: a bold accented header and plain padded
// body cells. Color is gated on Options.Color so piped output stays plain text.
func (r *Renderer) tableStyle() table.StyleFunc {
	color := r.o.Color
	return func(row, _ int) lipgloss.Style {
		if row == table.HeaderRow && color {
			// Bold is itself an ANSI attribute, so it is gated on color too:
			// a piped or --color=never table stays pure text.
			return padded.Bold(true).Foreground(lipgloss.Color("212"))
		}
		return padded
	}
}

// ---- JSON syntax highlighting ----

const (
	ansiReset = "\x1b[0m"
	ansiKey   = "\x1b[36m"         // cyan
	ansiStr   = "\x1b[32m"         // green
	ansiNum   = "\x1b[33m"         // yellow
	ansiLit   = "\x1b[35m"         // magenta: true/false/null
	ansiHead  = "\x1b[1;38;5;212m" // bold pink: the list heading, matching the table header accent (color 212)
)

// colorJSON highlights marshaled JSON when color is enabled, and returns the
// bytes untouched otherwise so piped output is always machine-parseable.
func (r *Renderer) colorJSON(b []byte) string {
	if !r.o.Color {
		return string(b)
	}
	return colorizeJSON(string(b))
}

// colorizeJSON wraps the tokens of already-valid JSON in ANSI colors. It treats
// a string immediately before a colon as a key, and leaves all structural
// punctuation and whitespace untouched.
func colorizeJSON(s string) string {
	var out strings.Builder
	out.Grow(len(s) + len(s)/4)
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				j++
			}
			k := j
			for k < len(s) && (s[k] == ' ' || s[k] == '\n' || s[k] == '\t') {
				k++
			}
			if k < len(s) && s[k] == ':' {
				out.WriteString(ansiKey + s[i:j] + ansiReset)
			} else {
				out.WriteString(ansiStr + s[i:j] + ansiReset)
			}
			i = j
		case c == '-' || (c >= '0' && c <= '9'):
			j := i
			for j < len(s) {
				d := s[j]
				if d == '-' || d == '+' || d == '.' || d == 'e' || d == 'E' || (d >= '0' && d <= '9') {
					j++
					continue
				}
				break
			}
			out.WriteString(ansiNum + s[i:j] + ansiReset)
			i = j
		case strings.HasPrefix(s[i:], "true"):
			out.WriteString(ansiLit + "true" + ansiReset)
			i += len("true")
		case strings.HasPrefix(s[i:], "false"):
			out.WriteString(ansiLit + "false" + ansiReset)
			i += len("false")
		case strings.HasPrefix(s[i:], "null"):
			out.WriteString(ansiLit + "null" + ansiReset)
			i += len("null")
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}
