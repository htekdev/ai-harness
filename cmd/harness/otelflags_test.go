package main

import (
	"reflect"
	"testing"
)

func TestExtractOtelFlags(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		out      []string
		endpoint string
		sample   string
		service  string
		err      bool
	}{
		{
			name: "no otel flags",
			in:   []string{"run", "--config", "harness.md"},
			out:  []string{"run", "--config", "harness.md"},
		},
		{
			name:     "endpoint equals form",
			in:       []string{"--otel-endpoint=http://localhost:4318", "serve", "--source", "stdin"},
			out:      []string{"serve", "--source", "stdin"},
			endpoint: "http://localhost:4318",
		},
		{
			name:   "sample space form",
			in:     []string{"--otel-sample", "0.25", "deploy", "--input", "hi"},
			out:    []string{"deploy", "--input", "hi"},
			sample: "0.25",
		},
		{
			name:     "all three interspersed",
			in:       []string{"run", "--otel-endpoint", "http://h:4318", "--config", "h.md", "--otel-sample=0.5", "--otel-service=svc"},
			out:      []string{"run", "--config", "h.md"},
			endpoint: "http://h:4318",
			sample:   "0.5",
			service:  "svc",
		},
		{
			name: "missing endpoint value triggers error",
			in:   []string{"run", "--otel-endpoint"},
			err:  true,
		},
		{
			name: "missing sample value triggers error",
			in:   []string{"run", "--otel-sample"},
			err:  true,
		},
		{
			name: "missing service value triggers error",
			in:   []string{"run", "--otel-service"},
			err:  true,
		},
		{
			name:     "single-dash variants",
			in:       []string{"-otel-endpoint=http://h", "-otel-sample", "1.0", "validate"},
			out:      []string{"validate"},
			endpoint: "http://h",
			sample:   "1.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, endpoint, sample, service, err := extractOtelFlags(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error, got out=%v", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(out, tc.out) {
				t.Errorf("out: got %v want %v", out, tc.out)
			}
			if endpoint != tc.endpoint {
				t.Errorf("endpoint: got %q want %q", endpoint, tc.endpoint)
			}
			if sample != tc.sample {
				t.Errorf("sample: got %q want %q", sample, tc.sample)
			}
			if service != tc.service {
				t.Errorf("service: got %q want %q", service, tc.service)
			}
		})
	}
}
