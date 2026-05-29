package artifact

import (
	"testing"
)

func TestParseType(t *testing.T) {
	tests := []struct {
		input   string
		want    Type
		wantErr bool
	}{
		{"override", TypeOverride, false},
		{"harness", TypeHarness, false},
		{"compaction", TypeCompaction, false},
		{"builtin", TypeBuiltin, false},
		{"plugin", TypePlugin, false},
		{"model", TypeModel, false},
		{"OVERRIDE", TypeOverride, false},
		{"Plugin", TypePlugin, false},
		{"  model  ", TypeModel, false},
		{"unknown", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTypePriority(t *testing.T) {
	// Verify priority ordering: override > harness > compaction > builtin > plugin > model
	if TypeOverride.Priority() <= TypeHarness.Priority() {
		t.Error("override should have higher priority than harness")
	}
	if TypeHarness.Priority() <= TypeCompaction.Priority() {
		t.Error("harness should have higher priority than compaction")
	}
	if TypeCompaction.Priority() <= TypeBuiltin.Priority() {
		t.Error("compaction should have higher priority than builtin")
	}
	if TypeBuiltin.Priority() <= TypePlugin.Priority() {
		t.Error("builtin should have higher priority than plugin")
	}
	if TypePlugin.Priority() <= TypeModel.Priority() {
		t.Error("plugin should have higher priority than model")
	}
}

func TestTypeValid(t *testing.T) {
	for _, typ := range AllTypes() {
		if !typ.Valid() {
			t.Errorf("AllTypes() contains invalid type %q", typ)
		}
	}
	if Type("invalid").Valid() {
		t.Error("Type('invalid').Valid() should be false")
	}
}

func TestEffectivePriority(t *testing.T) {
	a := &Artifact{Metadata: Metadata{Type: TypePlugin}}
	if a.EffectivePriority() != 40 {
		t.Errorf("expected 40, got %d", a.EffectivePriority())
	}

	a.PriorityOverride = 55
	if a.EffectivePriority() != 55 {
		t.Errorf("expected 55 with override, got %d", a.EffectivePriority())
	}
}
