package artifact

import (
	"testing"
)

func TestValidateCommon_NameRequired(t *testing.T) {
	a := &Artifact{
		Metadata: Metadata{Name: "", Type: TypePlugin},
	}
	err := Validate(a)
	if err == nil {
		t.Fatal("expected validation error for empty name")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatal("expected *ValidationError")
	}
	if len(ve.Issues) == 0 {
		t.Fatal("expected at least one issue")
	}
}

func TestValidateCommon_NamePattern(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"my-artifact", true},
		{"fs", true},
		{"tool-v2", true},
		{"a1", true},
		{"MyPlugin", false},    // uppercase
		{"my artifact", false}, // spaces
		{"-bad", false},        // starts with hyphen
		{"bad-", false},        // ends with hyphen
		{"a", false},           // too short
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Artifact{
				Metadata: Metadata{
					Name:        tt.name,
					Type:        TypePlugin,
					Description: "test plugin",
				},
				Tools: []ToolDef{{Name: "test-tool", Description: "test"}},
			}
			err := Validate(a)
			if tt.valid && err != nil {
				t.Errorf("name %q should be valid, got: %v", tt.name, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("name %q should be invalid", tt.name)
			}
		})
	}
}

func TestValidateVersion(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		{"1.0.0", true},
		{"0.1.0", true},
		{"2.10.3", true},
		{"1.0.0-beta", true},
		{"1.0", true},
		{"abc", false},
		{"1.0.0.0", false},
		{"", true}, // optional
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			a := &Artifact{
				Metadata: Metadata{
					Name:        "test-artifact",
					Type:        TypePlugin,
					Version:     tt.version,
					Description: "test",
				},
				Tools: []ToolDef{{Name: "t1", Description: "test"}},
			}
			err := Validate(a)
			if tt.valid && err != nil {
				t.Errorf("version %q should be valid, got: %v", tt.version, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("version %q should be invalid", tt.version)
			}
		})
	}
}

func TestValidateBuiltin(t *testing.T) {
	// Builtin must have tools or hooks and a version
	a := &Artifact{
		Metadata: Metadata{
			Name:    "core-tools",
			Type:    TypeBuiltin,
			Version: "1.0.0",
		},
		Tools: []ToolDef{{Name: "exec", Description: "execute commands"}},
	}
	if err := Validate(a); err != nil {
		t.Errorf("valid builtin should pass: %v", err)
	}

	// Missing tools and hooks
	a.Tools = nil
	a.Hooks = nil
	err := Validate(a)
	if err == nil {
		t.Error("builtin without tools/hooks should fail")
	}
}

func TestValidateModel(t *testing.T) {
	a := &Artifact{
		Metadata: Metadata{
			Name: "openai-models",
			Type: TypeModel,
		},
		Models: []ModelDef{
			{Name: "gpt-4o", Provider: "openai", MaxTokens: 4096},
		},
	}
	if err := Validate(a); err != nil {
		t.Errorf("valid model artifact should pass: %v", err)
	}

	// Model with tools should fail
	a.Tools = []ToolDef{{Name: "bad", Description: "should not be here"}}
	err := Validate(a)
	if err == nil {
		t.Error("model artifact with tools should fail")
	}
}

func TestValidateNonModelRejectsModels(t *testing.T) {
	a := &Artifact{
		Metadata: Metadata{
			Name:        "plugin-with-models",
			Type:        TypePlugin,
			Description: "invalid plugin",
		},
		Models: []ModelDef{
			{Name: "gpt-4o", Provider: "openai", MaxTokens: 4096},
		},
	}

	if err := Validate(a); err == nil {
		t.Error("non-model artifact with models should fail")
	}
}

func TestValidateHarness(t *testing.T) {
	a := &Artifact{
		Metadata: Metadata{
			Name: "my-harness",
			Type: TypeHarness,
		},
		Context: "You are a helpful assistant.",
	}
	if err := Validate(a); err != nil {
		t.Errorf("valid harness should pass: %v", err)
	}

	// Harness without context should fail
	a.Context = ""
	err := Validate(a)
	if err == nil {
		t.Error("harness without identity context should fail")
	}
}

func TestValidateOverride(t *testing.T) {
	a := &Artifact{
		Metadata: Metadata{
			Name: "my-override",
			Type: TypeOverride,
		},
		Context: "Additional instructions for this project.",
	}
	if err := Validate(a); err != nil {
		t.Errorf("valid override should pass: %v", err)
	}

	// Override with nothing should fail
	a.Context = ""
	a.Tools = nil
	a.Hooks = nil
	err := Validate(a)
	if err == nil {
		t.Error("override with no content should fail")
	}
}

func TestValidateDuplicateTools(t *testing.T) {
	a := &Artifact{
		Metadata: Metadata{
			Name:        "dupe-tools",
			Type:        TypePlugin,
			Description: "test",
		},
		Tools: []ToolDef{
			{Name: "my-tool", Description: "first"},
			{Name: "my-tool", Description: "second"},
		},
	}
	err := Validate(a)
	if err == nil {
		t.Error("duplicate tool names should fail validation")
	}
}
