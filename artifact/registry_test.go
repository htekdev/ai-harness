package artifact

import (
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	a := &Artifact{
		Metadata: Metadata{
			Name:        "test-plugin",
			Type:        TypePlugin,
			Description: "a test plugin",
		},
		Tools: []ToolDef{{Name: "my-tool", Description: "does stuff"}},
	}

	if err := reg.Register(a); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := reg.Get("test-plugin")
	if !ok {
		t.Fatal("Get returned false for registered artifact")
	}
	if got.Metadata.Name != "test-plugin" {
		t.Errorf("expected name 'test-plugin', got %q", got.Metadata.Name)
	}
	if reg.Count() != 1 {
		t.Errorf("expected count 1, got %d", reg.Count())
	}
}

func TestRegistryDuplicateNameRejected(t *testing.T) {
	reg := NewRegistry()

	a := &Artifact{
		Metadata: Metadata{
			Name:        "my-plugin",
			Type:        TypePlugin,
			Description: "first",
		},
		Tools: []ToolDef{{Name: "t1", Description: "test"}},
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}

	b := &Artifact{
		Metadata: Metadata{
			Name:        "my-plugin",
			Type:        TypePlugin,
			Description: "second",
		},
		Tools: []ToolDef{{Name: "t2", Description: "test"}},
	}
	err := reg.Register(b)
	if err == nil {
		t.Error("expected error for duplicate name")
	}
}

func TestRegistrySingletonHarness(t *testing.T) {
	reg := NewRegistry()

	h1 := &Artifact{
		Metadata: Metadata{Name: "harness-one", Type: TypeHarness},
		Context:  "Identity one",
	}
	if err := reg.Register(h1); err != nil {
		t.Fatal(err)
	}

	h2 := &Artifact{
		Metadata: Metadata{Name: "harness-two", Type: TypeHarness},
		Context:  "Identity two",
	}
	err := reg.Register(h2)
	if err == nil {
		t.Error("expected error for second harness artifact")
	}
}

func TestRegistryOrdering(t *testing.T) {
	reg := NewRegistry()

	// Register in random order
	artifacts := []*Artifact{
		{Metadata: Metadata{Name: "my-override", Type: TypeOverride}, Context: "override context"},
		{Metadata: Metadata{Name: "my-plugin", Type: TypePlugin, Description: "test plugin"}, Tools: []ToolDef{{Name: "t1", Description: "test"}}},
		{Metadata: Metadata{Name: "my-harness", Type: TypeHarness}, Context: "identity"},
		{Metadata: Metadata{Name: "my-builtin", Type: TypeBuiltin, Version: "1.0.0"}, Tools: []ToolDef{{Name: "t2", Description: "test"}}},
		{Metadata: Metadata{Name: "my-model", Type: TypeModel}, Models: []ModelDef{{Name: "gpt-4o", Provider: "openai", MaxTokens: 4096}}},
	}

	for _, a := range artifacts {
		if err := reg.Register(a); err != nil {
			t.Fatalf("Register %s: %v", a.Metadata.Name, err)
		}
	}

	all := reg.All()
	if len(all) != 5 {
		t.Fatalf("expected 5, got %d", len(all))
	}

	// Should be ordered: model(20) < plugin(40) < builtin(60) < harness(80) < override(100)
	expectedOrder := []string{"my-model", "my-plugin", "my-builtin", "my-harness", "my-override"}
	for i, a := range all {
		if a.Metadata.Name != expectedOrder[i] {
			t.Errorf("position %d: expected %q, got %q", i, expectedOrder[i], a.Metadata.Name)
		}
	}
}

func TestRegistryByType(t *testing.T) {
	reg := NewRegistry()

	p1 := &Artifact{Metadata: Metadata{Name: "plugin-a", Type: TypePlugin, Description: "a"}, Tools: []ToolDef{{Name: "t1", Description: "test"}}}
	p2 := &Artifact{Metadata: Metadata{Name: "plugin-b", Type: TypePlugin, Description: "b"}, Tools: []ToolDef{{Name: "t2", Description: "test"}}}
	b1 := &Artifact{Metadata: Metadata{Name: "core-tools", Type: TypeBuiltin, Version: "1.0.0"}, Tools: []ToolDef{{Name: "t3", Description: "test"}}}

	for _, a := range []*Artifact{p1, p2, b1} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	plugins := reg.ByType(TypePlugin)
	if len(plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(plugins))
	}

	builtins := reg.ByType(TypeBuiltin)
	if len(builtins) != 1 {
		t.Errorf("expected 1 builtin, got %d", len(builtins))
	}
}

func TestRegistryByTag(t *testing.T) {
	reg := NewRegistry()

	a := &Artifact{
		Metadata: Metadata{
			Name:        "tagged-plugin",
			Type:        TypePlugin,
			Description: "tagged",
			Tags:        []string{"security", "auth"},
		},
		Tools: []ToolDef{{Name: "t1", Description: "test"}},
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}

	found := reg.ByTag("security")
	if len(found) != 1 {
		t.Errorf("expected 1, got %d", len(found))
	}

	found = reg.ByTag("nonexistent")
	if len(found) != 0 {
		t.Errorf("expected 0, got %d", len(found))
	}
}

func TestRegistryRemove(t *testing.T) {
	reg := NewRegistry()
	a := &Artifact{
		Metadata: Metadata{Name: "removable", Type: TypePlugin, Description: "test"},
		Tools:    []ToolDef{{Name: "t1", Description: "test"}},
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}

	if !reg.Remove("removable") {
		t.Error("Remove should return true")
	}
	if reg.Count() != 0 {
		t.Error("registry should be empty after remove")
	}
	if reg.Remove("nonexistent") {
		t.Error("Remove of nonexistent should return false")
	}
}

func TestRegistryValidateDependencies(t *testing.T) {
	reg := NewRegistry()

	a := &Artifact{
		Metadata: Metadata{
			Name:        "depends-on-missing",
			Type:        TypePlugin,
			Description: "has dep",
			DependsOn:   []string{"missing-artifact"},
		},
		Tools: []ToolDef{{Name: "t1", Description: "test"}},
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}

	err := reg.ValidateDependencies()
	if err == nil {
		t.Error("expected dependency validation error")
	}
}

func TestRegistryResolveWithConditions(t *testing.T) {
	reg := NewRegistry()

	always := &Artifact{
		Metadata: Metadata{Name: "always-on", Type: TypePlugin, Description: "always"},
		Tools:    []ToolDef{{Name: "t1", Description: "test"}},
	}
	conditional := &Artifact{
		Metadata:  Metadata{Name: "conditional", Type: TypePlugin, Description: "sometimes"},
		Condition: "ctx.get('env') == 'production'",
		Tools:     []ToolDef{{Name: "t2", Description: "test"}},
	}

	for _, a := range []*Artifact{always, conditional} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	// Resolve with a function that rejects the condition
	active, err := reg.Resolve(func(cond string) (bool, error) {
		return false, nil // reject all conditions
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(active) != 1 {
		t.Errorf("expected 1 active (unconditional), got %d", len(active))
	}
	if active[0].Metadata.Name != "always-on" {
		t.Errorf("expected always-on, got %q", active[0].Metadata.Name)
	}

	// Resolve with a function that accepts
	active, err = reg.Resolve(func(cond string) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Errorf("expected 2 active, got %d", len(active))
	}
}
