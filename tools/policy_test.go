package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func okHandler(_ context.Context, _ json.RawMessage) (string, error) {
	return "ok", nil
}

func defOf(name string) Definition {
	return Definition{Name: name, Description: name}
}

func mustRegister(t *testing.T, r *Registry, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := r.Register(defOf(n), okHandler); err != nil {
			t.Fatalf("register %q: %v", n, err)
		}
	}
}

// --- Policy.Allows ---------------------------------------------------------

func TestPolicy_NilAllowsEverything(t *testing.T) {
	var p *Policy
	if !p.Allows("anything") {
		t.Fatalf("nil policy must allow")
	}
	if !(&Policy{}).Allows("anything") {
		t.Fatalf("empty policy must allow")
	}
}

func TestPolicy_DenyWinsOverAllow(t *testing.T) {
	p := &Policy{Allow: []string{"fs.*"}, Deny: []string{"fs.write"}}
	if !p.Allows("fs.read") {
		t.Fatalf("fs.read should be allowed by allowlist")
	}
	if p.Allows("fs.write") {
		t.Fatalf("fs.write must be denied even though it matches allow")
	}
}

func TestPolicy_AllowlistInferred(t *testing.T) {
	p := &Policy{Allow: []string{"safe"}}
	if !p.Allows("safe") {
		t.Fatalf("safe should be allowed")
	}
	if p.Allows("dangerous") {
		t.Fatalf("dangerous should be implicitly denied in allowlist mode")
	}
}

func TestPolicy_DenylistInferred(t *testing.T) {
	p := &Policy{Deny: []string{"exec"}}
	if !p.Allows("anything") {
		t.Fatalf("denylist mode should allow by default")
	}
	if p.Allows("exec") {
		t.Fatalf("exec should be denied")
	}
}

func TestPolicy_GlobPatterns(t *testing.T) {
	p := &Policy{Mode: PolicyModeAllowlist, Allow: []string{"fs.*", "net_*"}}
	cases := map[string]bool{
		"fs.read":   true,
		"fs.write":  true,
		"net_get":   true,
		"net_post":  true,
		"exec":      false,
		"shell":     false,
		"fs":        false, // glob requires the dot
		"fs.deep.x": true,  // filepath.Match * matches anything not /
	}
	for name, want := range cases {
		if got := p.Allows(name); got != want {
			t.Errorf("Allows(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPolicy_ExplicitModeOverridesInference(t *testing.T) {
	// Allow non-empty but mode pinned to denylist ⇒ Allow ignored.
	p := &Policy{Mode: PolicyModeDenylist, Allow: []string{"safe"}, Deny: []string{"exec"}}
	if !p.Allows("anything") {
		t.Fatalf("denylist mode should default-allow")
	}
	if !p.Allows("safe") {
		t.Fatalf("safe should still be allowed")
	}
	if p.Allows("exec") {
		t.Fatalf("exec should be denied")
	}
}

func TestPolicy_Validate(t *testing.T) {
	if err := (&Policy{}).Validate(); err != nil {
		t.Fatalf("empty policy must validate: %v", err)
	}
	if err := (&Policy{Mode: "weird"}).Validate(); err == nil {
		t.Fatalf("invalid mode must error")
	}
	if err := (&Policy{Allow: []string{""}}).Validate(); err == nil {
		t.Fatalf("empty allow entry must error")
	}
	if err := (&Policy{Deny: []string{"  "}}).Validate(); err == nil {
		t.Fatalf("whitespace-only deny entry must error")
	}
	if err := (&Policy{Allow: []string{"["}}).Validate(); err == nil {
		t.Fatalf("malformed glob must error")
	}
	if err := (&Policy{Allow: []string{"fs.*"}, Deny: []string{"fs.write"}}).Validate(); err != nil {
		t.Fatalf("valid policy errored: %v", err)
	}
}

// --- Registry integration --------------------------------------------------

func TestRegistry_NoPolicyAllowsAll(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "a", "b")
	if got := len(r.List()); got != 2 {
		t.Fatalf("expected 2 listed tools, got %d", got)
	}
	if !r.Has("a") {
		t.Fatalf("a should be visible")
	}
	res := r.Execute(context.Background(), Call{ID: "1", Name: "a"})
	if res.IsError {
		t.Fatalf("a should execute: %s", res.Content)
	}
}

func TestRegistry_SetPolicy_RejectsInvalid(t *testing.T) {
	r := NewRegistry()
	if err := r.SetPolicy(&Policy{Allow: []string{"["}}); err == nil {
		t.Fatalf("invalid policy must be rejected")
	}
	if r.Policy() != nil {
		t.Fatalf("rejected policy must not be installed")
	}
	if err := r.SetPolicy(nil); err != nil {
		t.Fatalf("nil clear must succeed: %v", err)
	}
}

func TestRegistry_AllowlistFiltersListAndExecute(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "fs.read", "fs.write", "exec")
	if err := r.SetPolicy(&Policy{Allow: []string{"fs.read"}}); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	got := r.List()
	if len(got) != 1 || got[0].Name != "fs.read" {
		t.Fatalf("List() should expose only fs.read, got %+v", got)
	}
	if r.Has("exec") {
		t.Fatalf("Has(exec) must be false under policy")
	}
	if !r.HasRegistered("exec") {
		t.Fatalf("HasRegistered(exec) must remain true (raw registry presence)")
	}

	// Execute on denied tool returns IsError + 'denied by policy'.
	res := r.Execute(context.Background(), Call{ID: "1", Name: "fs.write"})
	if !res.IsError || !strings.Contains(res.Content, "denied by policy") {
		t.Fatalf("expected policy-deny error, got %+v", res)
	}
	// Execute on permitted tool succeeds.
	res = r.Execute(context.Background(), Call{ID: "2", Name: "fs.read"})
	if res.IsError {
		t.Fatalf("fs.read should execute: %s", res.Content)
	}
}

func TestRegistry_DenylistFiltersListAndExecute(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "fs.read", "fs.write", "exec")
	if err := r.SetPolicy(&Policy{Deny: []string{"exec", "fs.write"}}); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	got := r.List()
	if len(got) != 1 || got[0].Name != "fs.read" {
		t.Fatalf("List() should expose only fs.read, got %+v", got)
	}
	res := r.Execute(context.Background(), Call{ID: "1", Name: "exec"})
	if !res.IsError {
		t.Fatalf("exec should be denied")
	}
}

func TestRegistry_DenyWinsOverAllow(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "fs.read", "fs.write")
	if err := r.SetPolicy(&Policy{Allow: []string{"fs.*"}, Deny: []string{"fs.write"}}); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	if !r.Has("fs.read") {
		t.Fatalf("fs.read should be visible")
	}
	if r.Has("fs.write") {
		t.Fatalf("fs.write must be hidden — deny wins")
	}
}

func TestRegistry_ToOpenAIFormatRespectsPolicy(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "fs.read", "exec")
	if err := r.SetPolicy(&Policy{Deny: []string{"exec"}}); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	tools := r.ToOpenAIFormat()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool exposed to model, got %d", len(tools))
	}
	fn := tools[0]["function"].(map[string]any)
	if fn["name"].(string) != "fs.read" {
		t.Fatalf("expected fs.read, got %v", fn["name"])
	}
}

func TestRegistry_GetRespectsPolicy(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "fs.read", "exec")
	_ = r.SetPolicy(&Policy{Allow: []string{"fs.*"}})
	if _, ok := r.Get("fs.read"); !ok {
		t.Fatalf("fs.read should be retrievable")
	}
	if _, ok := r.Get("exec"); ok {
		t.Fatalf("exec should be hidden by policy")
	}
}

func TestRegistry_ListAllIgnoresPolicy(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "fs.read", "exec")
	_ = r.SetPolicy(&Policy{Allow: []string{"fs.*"}})
	if got := len(r.ListAll()); got != 2 {
		t.Fatalf("ListAll must expose every registered tool, got %d", got)
	}
	if got := len(r.List()); got != 1 {
		t.Fatalf("List must filter, got %d", got)
	}
}

func TestRegistry_PolicyClearRestoresAccess(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "exec")
	_ = r.SetPolicy(&Policy{Deny: []string{"exec"}})
	if !r.Execute(context.Background(), Call{ID: "1", Name: "exec"}).IsError {
		t.Fatalf("exec should be denied")
	}
	if err := r.SetPolicy(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if r.Execute(context.Background(), Call{ID: "2", Name: "exec"}).IsError {
		t.Fatalf("exec should succeed after clear")
	}
}

func TestRegistry_PolicyConcurrentSafety(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, "a", "b", "c")
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			_ = r.SetPolicy(&Policy{Allow: []string{"a"}})
			_ = r.SetPolicy(&Policy{Deny: []string{"a"}})
			_ = r.SetPolicy(nil)
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		_ = r.List()
		_ = r.Execute(context.Background(), Call{ID: "x", Name: "a"})
	}
	<-done
}
