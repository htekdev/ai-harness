package errs

import (
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestKindString(t *testing.T) {
	cases := map[Kind]string{
		KindUnknown:     "unknown",
		KindConfig:      "config",
		KindTool:        "tool",
		KindCompletion:  "completion",
		KindDelegation:  "delegation",
		KindSource:      "source",
		KindPersistence: "persistence",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestNewfFormatsMessage(t *testing.T) {
	e := Newf(KindConfig, "config.load", "missing key %q", "model")
	if e.Kind != KindConfig {
		t.Fatalf("kind = %v, want KindConfig", e.Kind)
	}
	if e.Op != "config.load" {
		t.Fatalf("op = %q", e.Op)
	}
	want := `config.load: missing key "model"`
	if got := e.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestWrapNilReturnsNil(t *testing.T) {
	if err := Wrap(KindTool, "tools.execute", nil, "x"); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
	if err := Retriable(KindCompletion, "completion.call", nil, "x"); err != nil {
		t.Fatalf("Retriable(nil) = %v, want nil", err)
	}
}

func TestWrapPreservesCause(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	err := Wrap(KindCompletion, "completion.call", cause, "provider %s failed", "openai")
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("errors.Is should find wrapped cause; got chain %v", err)
	}
	want := "completion.call: provider openai failed: unexpected EOF"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestKindOfWalksChain(t *testing.T) {
	inner := Newf(KindTool, "tools.execute", "boom")
	outer := fmt.Errorf("wrapping: %w", inner)
	if k := KindOf(outer); k != KindTool {
		t.Fatalf("KindOf(outer) = %v, want KindTool", k)
	}
	if k := KindOf(nil); k != KindUnknown {
		t.Fatalf("KindOf(nil) = %v, want KindUnknown", k)
	}
	if k := KindOf(io.EOF); k != KindUnknown {
		t.Fatalf("KindOf(stdlib) = %v, want KindUnknown", k)
	}
}

func TestIsRetriable(t *testing.T) {
	transient := Retriable(KindCompletion, "completion.call", io.ErrUnexpectedEOF, "transient")
	if !IsRetriable(transient) {
		t.Fatalf("IsRetriable(retriable) = false")
	}
	wrapped := fmt.Errorf("ctx: %w", transient)
	if !IsRetriable(wrapped) {
		t.Fatalf("IsRetriable should walk chain")
	}
	hard := Wrap(KindConfig, "config.load", io.EOF, "fatal")
	if IsRetriable(hard) {
		t.Fatalf("IsRetriable(non-retriable) = true")
	}
	if IsRetriable(nil) {
		t.Fatalf("IsRetriable(nil) = true")
	}
}

func TestErrorIsByKind(t *testing.T) {
	e := Newf(KindSource, "source.read", "boom")
	if !errors.Is(e, &Error{Kind: KindSource}) {
		t.Fatalf("errors.Is by Kind=Source should match")
	}
	if errors.Is(e, &Error{Kind: KindTool}) {
		t.Fatalf("errors.Is by Kind=Tool should not match a source error")
	}
	// Sentinel Kind=Unknown matches any *Error, useful for "is this typed?" tests.
	if !errors.Is(e, &Error{}) {
		t.Fatalf("errors.Is with empty target should match any *Error")
	}
}

func TestErrorAs(t *testing.T) {
	e := Wrap(KindDelegation, "delegation.execute", io.EOF, "boom")
	wrapped := fmt.Errorf("ctx: %w", e)
	var target *Error
	if !errors.As(wrapped, &target) {
		t.Fatalf("errors.As should find *Error in chain")
	}
	if target.Kind != KindDelegation {
		t.Fatalf("target.Kind = %v, want KindDelegation", target.Kind)
	}
}
