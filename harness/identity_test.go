package harness

import "testing"

func TestCoreIdentityLevels(t *testing.T) {
	if got := CoreIdentity("disabled"); got != "" {
		t.Fatalf("expected disabled core identity to be empty, got %q", got)
	}
	if got := CoreIdentity("minimal"); got == "" {
		t.Fatal("expected minimal core identity content")
	}
	if got := CoreIdentity("enabled"); got == "" {
		t.Fatal("expected enabled core identity content")
	}
	if got := CoreIdentity("unknown-level"); got != CoreIdentity("enabled") {
		t.Fatalf("expected unknown level to fallback to enabled identity, got %q", got)
	}
}

func TestComposeSystemPrompt(t *testing.T) {
	const user = "You are helpful."
	if got := ComposeSystemPrompt(user, "disabled"); got != user {
		t.Fatalf("disabled should preserve user prompt, got %q", got)
	}
	if got := ComposeSystemPrompt("", "enabled"); got == "" {
		t.Fatal("enabled with empty user prompt should still include baseline")
	}
	if got := ComposeSystemPrompt(user, "minimal"); got == user {
		t.Fatal("minimal should prepend baseline prompt")
	}
}
