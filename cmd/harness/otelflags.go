package main

import (
	"fmt"
	"strings"
)

// extractOtelFlags scans args for the global --otel-endpoint / --otel-sample /
// --otel-service flags (also accepting `-otel-*`, `--otel-*=value`, and the
// short `--otel-* value` form) and returns:
//   - args with those flags removed
//   - endpoint   (empty if unspecified)
//   - sample     (empty if unspecified)
//   - service    (empty if unspecified)
//   - an error if a flag is provided without a value
//
// Mirrors extractLogFlags so the OTel knobs sit alongside --log-* without any
// subcommand needing to declare them.
func extractOtelFlags(args []string) (out []string, endpoint, sample, service string, err error) {
	out = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, hasValue := splitOtelFlag(a)
		switch name {
		case "--otel-endpoint", "-otel-endpoint":
			if !hasValue {
				if i+1 >= len(args) {
					return nil, "", "", "", fmt.Errorf("--otel-endpoint requires a value")
				}
				i++
				value = args[i]
			}
			endpoint = value
		case "--otel-sample", "-otel-sample":
			if !hasValue {
				if i+1 >= len(args) {
					return nil, "", "", "", fmt.Errorf("--otel-sample requires a value")
				}
				i++
				value = args[i]
			}
			sample = value
		case "--otel-service", "-otel-service":
			if !hasValue {
				if i+1 >= len(args) {
					return nil, "", "", "", fmt.Errorf("--otel-service requires a value")
				}
				i++
				value = args[i]
			}
			service = value
		default:
			out = append(out, a)
		}
	}
	return out, endpoint, sample, service, nil
}

func splitOtelFlag(arg string) (name, value string, hasValue bool) {
	if !strings.HasPrefix(arg, "-") {
		return arg, "", false
	}
	if i := strings.Index(arg, "="); i >= 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}
