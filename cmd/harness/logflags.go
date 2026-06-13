package main

import (
	"fmt"
	"strings"
)

// extractLogFlags scans args for the global --log-level / --log-format
// flags (also accepting `-log-level`, `--log-level=value`, and the short
// `--log-format value` form) and returns:
//   - args with those flags removed
//   - the chosen format (empty if unspecified)
//   - the chosen level  (empty if unspecified)
//   - an error if a flag is provided without a value
//
// Keeping this here (rather than in a subcommand FlagSet) lets every
// subcommand inherit the same logging knobs without each one declaring
// the flag itself.
func extractLogFlags(args []string) (out []string, format, level string, err error) {
	out = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, hasValue := splitLogFlag(a)
		switch name {
		case "--log-level", "-log-level":
			if !hasValue {
				if i+1 >= len(args) {
					return nil, "", "", fmt.Errorf("--log-level requires a value")
				}
				i++
				value = args[i]
			}
			level = value
		case "--log-format", "-log-format":
			if !hasValue {
				if i+1 >= len(args) {
					return nil, "", "", fmt.Errorf("--log-format requires a value")
				}
				i++
				value = args[i]
			}
			format = value
		default:
			out = append(out, a)
		}
	}
	return out, format, level, nil
}

// splitLogFlag separates "--log-level=info" into ("--log-level", "info", true)
// and leaves a bare "--log-level" as ("--log-level", "", false). For non-log
// flags it returns the original token in name and hasValue=false so callers
// pass them through unmodified.
func splitLogFlag(arg string) (name, value string, hasValue bool) {
	if !strings.HasPrefix(arg, "-") {
		return arg, "", false
	}
	if i := strings.Index(arg, "="); i >= 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}
