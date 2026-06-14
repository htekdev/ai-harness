package scripting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNetworkSandbox_NilOrEmptyIsUnrestricted(t *testing.T) {
	t.Parallel()
	var s *NetworkSandbox
	if err := s.Allow("https://example.com/foo"); err != nil {
		t.Fatalf("nil sandbox should allow all: got %v", err)
	}

	empty := NewNetworkSandbox(nil)
	if err := empty.Allow("https://example.com/foo"); err != nil {
		t.Fatalf("empty sandbox should allow all: got %v", err)
	}

	whitespaceOnly := NewNetworkSandbox([]string{"", "   ", "\t"})
	if err := whitespaceOnly.Allow("https://example.com/foo"); err != nil {
		t.Fatalf("whitespace-only allowlist should be empty/unrestricted: got %v", err)
	}
}

func TestNetworkSandbox_ApexAndSubdomainMatching(t *testing.T) {
	t.Parallel()
	s := NewNetworkSandbox([]string{"example.com"})

	allowCases := []string{
		"https://example.com/",
		"http://EXAMPLE.com/path",
		"https://api.example.com/v1",
		"https://a.b.example.com/",
		"https://example.com:8443/",
		"https://example.com./", // trailing dot
	}
	for _, u := range allowCases {
		if err := s.Allow(u); err != nil {
			t.Errorf("expected %q to be allowed, got %v", u, err)
		}
	}

	denyCases := []string{
		"https://evil.com/",
		"https://notexample.com/",
		"https://example.com.attacker.com/", // suffix-trick guard
	}
	for _, u := range denyCases {
		if err := s.Allow(u); err == nil {
			t.Errorf("expected %q to be denied", u)
		}
	}
}

func TestNetworkSandbox_WildcardEntryRequiresSubdomain(t *testing.T) {
	t.Parallel()
	s := NewNetworkSandbox([]string{"*.example.com"})

	if err := s.Allow("https://api.example.com/"); err != nil {
		t.Errorf("subdomain should be allowed: %v", err)
	}
	if err := s.Allow("https://example.com/"); err == nil {
		t.Errorf("apex should NOT be allowed under *.example.com")
	}
}

func TestNetworkSandbox_StarMatchesAll(t *testing.T) {
	t.Parallel()
	s := NewNetworkSandbox([]string{"*"})
	for _, u := range []string{"https://anything.com/", "http://192.168.1.1/", "https://x.y.z/"} {
		if err := s.Allow(u); err != nil {
			t.Errorf("%q should be allowed under *: %v", u, err)
		}
	}
	// non-http(s) still rejected
	if err := s.Allow("file:///etc/passwd"); err == nil {
		t.Errorf("file:// must be rejected even with *")
	}
}

func TestNetworkSandbox_RejectsNonHTTPSchemes(t *testing.T) {
	t.Parallel()
	s := NewNetworkSandbox([]string{"example.com"})
	for _, u := range []string{
		"file:///etc/passwd",
		"gopher://example.com/",
		"ftp://example.com/",
	} {
		err := s.Allow(u)
		if err == nil {
			t.Errorf("expected %q to be rejected by scheme check", u)
			continue
		}
		var se *SandboxError
		if !errors.As(err, &se) {
			t.Errorf("expected *SandboxError for %q, got %T", u, err)
		}
	}
}

func TestNetworkSandbox_RejectsIPLiteralsByDefault(t *testing.T) {
	t.Parallel()
	s := NewNetworkSandbox([]string{"example.com"})
	for _, u := range []string{
		"http://127.0.0.1/",
		"http://10.0.0.5:8080/",
		"http://[::1]/",
	} {
		if err := s.Allow(u); err == nil {
			t.Errorf("expected IP literal %q to be denied", u)
		}
	}
}

func TestNetworkSandbox_AllowedDomainsRoundTrip(t *testing.T) {
	t.Parallel()
	s := NewNetworkSandbox([]string{"Example.com", "*.api.com", "*", " ", "*.bad.com"})
	got := s.AllowedDomains()
	want := []string{"example.com", "*.api.com", "*", "*.bad.com"}
	if len(got) != len(want) {
		t.Fatalf("AllowedDomains len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestEngine_SetNetworkSandbox_BlocksHTTPGet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should never be reached when sandbox blocks the request")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	engine := NewEngine()
	// Sandbox that disallows everything (the test server runs on 127.0.0.1).
	engine.SetNetworkSandbox(NewNetworkSandbox([]string{"only-allowed.example"}))

	runner, err := engine.CompileToolScript("fetch", `
def run(args):
    resp = http.get(args["url"])
    return str(resp["status"])
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, runErr := runner.Run(context.Background(), json.RawMessage(`{"url": "`+srv.URL+`"}`))
	if runErr == nil {
		t.Fatalf("expected sandbox to block request to %s", srv.URL)
	}
	if !strings.Contains(runErr.Error(), "network sandbox") {
		t.Fatalf("expected sandbox error, got: %v", runErr)
	}
}

func TestEngine_NoSandbox_AllowsHTTPGet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	engine := NewEngine()
	if engine.NetworkSandbox() != nil {
		t.Fatalf("expected no default sandbox")
	}

	runner, err := engine.CompileToolScript("fetch", `
def run(args):
    resp = http.get(args["url"])
    return str(resp["status"])
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out, err := runner.Run(context.Background(), json.RawMessage(`{"url": "`+srv.URL+`"}`))
	if err != nil {
		t.Fatalf("expected no-sandbox engine to succeed, got %v", err)
	}
	if out != "200" {
		t.Fatalf("unexpected status: %q", out)
	}
}

func TestEngine_SandboxAllowsListedHost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	engine := NewEngine()
	// "*" allows everything (still rejects non-http schemes).
	engine.SetNetworkSandbox(NewNetworkSandbox([]string{"*"}))

	runner, err := engine.CompileToolScript("fetch", `
def run(args):
    resp = http.get(args["url"])
    return str(resp["status"])
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := runner.Run(context.Background(), json.RawMessage(`{"url": "`+srv.URL+`"}`)); err != nil {
		t.Fatalf("expected * sandbox to allow %s, got %v", srv.URL, err)
	}
}

func TestEngine_SandboxBlocksHTTPPost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be reached")
	}))
	defer srv.Close()

	engine := NewEngine()
	engine.SetNetworkSandbox(NewNetworkSandbox([]string{"only-allowed.example"}))

	runner, err := engine.CompileToolScript("send", `
def run(args):
    resp = http.post(args["url"], body="x")
    return str(resp["status"])
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, runErr := runner.Run(context.Background(), json.RawMessage(`{"url": "`+srv.URL+`"}`))
	if runErr == nil || !strings.Contains(runErr.Error(), "network sandbox") {
		t.Fatalf("expected sandbox error from http.post, got %v", runErr)
	}
}
