package main

import (
	"reflect"
	"testing"
)

func TestExtractLogFlags(t *testing.T) {
	cases := []struct {
		name   string
		in     []string
		out    []string
		format string
		level  string
		err    bool
	}{
		{
			name: "no log flags",
			in:   []string{"run", "--config", "harness.md"},
			out:  []string{"run", "--config", "harness.md"},
		},
		{
			name:   "equals form",
			in:     []string{"--log-level=debug", "serve", "--source", "stdin"},
			out:    []string{"serve", "--source", "stdin"},
			level:  "debug",
		},
		{
			name:   "space form",
			in:     []string{"--log-format", "json", "deploy", "--input", "hi"},
			out:    []string{"deploy", "--input", "hi"},
			format: "json",
		},
		{
			name:   "both flags interspersed",
			in:     []string{"run", "--log-level", "warn", "--config", "h.md", "--log-format=text"},
			out:    []string{"run", "--config", "h.md"},
			level:  "warn",
			format: "text",
		},
		{
			name: "missing value triggers error",
			in:   []string{"run", "--log-level"},
			err:  true,
		},
		{
			name:   "single-dash variants",
			in:     []string{"-log-level=info", "-log-format", "json", "validate"},
			out:    []string{"validate"},
			level:  "info",
			format: "json",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, format, level, err := extractLogFlags(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error; got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(out, tc.out) {
				t.Errorf("out mismatch: got %v want %v", out, tc.out)
			}
			if format != tc.format {
				t.Errorf("format mismatch: got %q want %q", format, tc.format)
			}
			if level != tc.level {
				t.Errorf("level mismatch: got %q want %q", level, tc.level)
			}
		})
	}
}
