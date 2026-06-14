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

	t := table.New().Rows(r.gridRows...)
	if !r.o.NoHeader {
		head := r.gridHead
		if !md {
			head = upper(head)
		}
		t = t.Headers(head...)
	}

	if md {
		// A GitHub-flavored pipe table: no outer top/bottom rule, never colored,
		// so it pastes cleanly into docs, issues, and READMEs.
		t = t.Border(lipgloss.MarkdownBorder()).
			BorderTop(false).
			BorderBottom(false).
			StyleFunc(markdownStyle)
	} else {
		t = t.Border(lipgloss.RoundedBorder()).
			BorderStyle(r.borderStyle()).
			StyleFunc(r.tableStyle())
	}

	_, err := io.WriteString(r.w, t.String()+"\n")
	return err
}

// padded is the shared cell style: one space of breathing room on each side.
var padded = lipgloss.NewStyle().Padding(0, 1)

// markdownStyle keeps every cell plain and left-aligned so the rendered pipes
// stay valid markdown.
func markdownStyle(int, int) lipgloss.Style { return padded.Align(lipgloss.Left) }

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
	ansiKey   = "\x1b[36m" // cyan
	ansiStr   = "\x1b[32m" // green
	ansiNum   = "\x1b[33m" // yellow
	ansiLit   = "\x1b[35m" // magenta: true/false/null
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
