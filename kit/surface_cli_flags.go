package kit

import (
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagRef holds a pointer to the parsed value of one operation flag plus enough
// metadata to read it back after cobra parses the command line.
type flagRef struct {
	spec ParamSpec
	str  *string
	i    *int
	b    *bool
	f    *float64
	sl   *[]string
}

// registerFlag adds one typed cobra flag for an operation parameter and returns
// a ref the RunE closure reads after parsing.
func registerFlag(cmd *cobra.Command, p ParamSpec) *flagRef {
	fs := cmd.Flags()
	ref := &flagRef{spec: p}
	switch p.Type {
	case TypeBool:
		ref.b = new(bool)
		def := p.Default == "true"
		bindBool(fs, ref.b, p, def)
	case TypeInt:
		ref.i = new(int)
		bindInt(fs, ref.i, p, atoiDefault(p.Default))
	case TypeFloat:
		ref.f = new(float64)
		bindFloat(fs, ref.f, p, atofDefault(p.Default))
	case TypeStringSlice:
		ref.sl = new([]string)
		bindStringSlice(fs, ref.sl, p, splitList(p.Default))
	default: // string and duration both arrive as text on the CLI
		ref.str = new(string)
		bindString(fs, ref.str, p, p.Default)
	}
	return ref
}

func bindBool(fs *pflag.FlagSet, p *bool, spec ParamSpec, def bool) {
	if spec.Short != "" {
		fs.BoolVarP(p, spec.Name, spec.Short, def, spec.Help)
	} else {
		fs.BoolVar(p, spec.Name, def, spec.Help)
	}
}

func bindInt(fs *pflag.FlagSet, p *int, spec ParamSpec, def int) {
	if spec.Short != "" {
		fs.IntVarP(p, spec.Name, spec.Short, def, spec.Help)
	} else {
		fs.IntVar(p, spec.Name, def, spec.Help)
	}
}

func bindFloat(fs *pflag.FlagSet, p *float64, spec ParamSpec, def float64) {
	if spec.Short != "" {
		fs.Float64VarP(p, spec.Name, spec.Short, def, spec.Help)
	} else {
		fs.Float64Var(p, spec.Name, def, spec.Help)
	}
}

func bindString(fs *pflag.FlagSet, p *string, spec ParamSpec, def string) {
	if spec.Short != "" {
		fs.StringVarP(p, spec.Name, spec.Short, def, spec.Help)
	} else {
		fs.StringVar(p, spec.Name, def, spec.Help)
	}
}

func bindStringSlice(fs *pflag.FlagSet, p *[]string, spec ParamSpec, def []string) {
	if spec.Short != "" {
		fs.StringSliceVarP(p, spec.Name, spec.Short, def, spec.Help)
	} else {
		fs.StringSliceVar(p, spec.Name, def, spec.Help)
	}
}

// collectFlags reads back the parsed value of every operation flag that the user
// actually set (or that has a non-empty default), keyed by parameter name, ready
// for Input.Flags. Unset flags with no default are omitted so the handler sees
// its own zero value.
func collectFlags(cmd *cobra.Command, refs map[string]*flagRef) map[string]any {
	out := map[string]any{}
	for name, ref := range refs {
		changed := cmd.Flags().Changed(name)
		switch {
		case ref.b != nil:
			if changed || *ref.b {
				out[name] = *ref.b
			}
		case ref.i != nil:
			if changed || *ref.i != 0 {
				out[name] = *ref.i
			}
		case ref.f != nil:
			if changed || *ref.f != 0 {
				out[name] = *ref.f
			}
		case ref.sl != nil:
			if changed || len(*ref.sl) > 0 {
				out[name] = *ref.sl
			}
		case ref.str != nil:
			if changed || *ref.str != "" {
				out[name] = *ref.str
			}
		}
	}
	return out
}

func atoiDefault(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func atofDefault(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
