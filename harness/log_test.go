package harness

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewLogger_DefaultIsTextInfo(t *testing.T) {
	var buf bytes.Buffer
	l, err := NewLogger("", "", &buf)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	l.Info("hello", "k", "v")
	l.Debug("should-not-appear")

	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("expected text output to contain 'hello'; got %q", out)
	}
	if !strings.Contains(out, "k=v") {
		t.Errorf("expected text output to contain 'k=v'; got %q", out)
	}
	if strings.Contains(out, "should-not-appear") {
		t.Errorf("expected debug message to be filtered at info level; got %q", out)
	}
}

func TestNewLogger_JSONHandler(t *testing.T) {
	var buf bytes.Buffer
	l, err := NewLogger("json", "debug", &buf)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	l.Debug("d1", "tool", "search")

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("output is not JSON: %v (raw=%q)", err, buf.String())
	}
	if rec["msg"] != "d1" {
		t.Errorf("expected msg=d1; got %v", rec["msg"])
	}
	if rec["tool"] != "search" {
		t.Errorf("expected tool=search; got %v", rec["tool"])
	}
	if rec["level"] != "DEBUG" {
		t.Errorf("expected level=DEBUG; got %v", rec["level"])
	}
}

func TestNewLogger_LevelFiltering(t *testing.T) {
	cases := []struct {
		level     string
		emit      string // method to call (info|warn|error|debug)
		wantInOut bool
	}{
		{"warn", "info", false},
		{"warn", "warn", true},
		{"warn", "error", true},
		{"error", "warn", false},
		{"info", "debug", false},
		{"debug", "debug", true},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		l, err := NewLogger("text", tc.level, &buf)
		if err != nil {
			t.Fatalf("level=%s: NewLogger: %v", tc.level, err)
		}
		switch tc.emit {
		case "debug":
			l.Debug("msg-x")
		case "info":
			l.Info("msg-x")
		case "warn":
			l.Warn("msg-x")
		case "error":
			l.Error("msg-x")
		}
		got := strings.Contains(buf.String(), "msg-x")
		if got != tc.wantInOut {
			t.Errorf("level=%s emit=%s: got contains=%v want=%v (out=%q)",
				tc.level, tc.emit, got, tc.wantInOut, buf.String())
		}
	}
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	if _, err := NewLogger("text", "spammy", nil); err == nil {
		t.Fatal("expected error for invalid level")
	}
}

func TestNewLogger_InvalidFormat(t *testing.T) {
	if _, err := NewLogger("xml", "info", nil); err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestSetLoggerAndAccessor(t *testing.T) {
	orig := Logger()
	t.Cleanup(func() { SetLogger(orig) })

	var buf bytes.Buffer
	custom, err := NewLogger("json", "info", &buf)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	SetLogger(custom)

	Logger().Info("ping", "n", 1)

	if !strings.Contains(buf.String(), `"msg":"ping"`) {
		t.Errorf("expected SetLogger to install JSON logger; got %q", buf.String())
	}

	SetLogger(nil)
	if Logger() == nil {
		t.Fatal("Logger() should never return nil after reset")
	}
}

func TestConfigureLoggerFromFlags(t *testing.T) {
	orig := Logger()
	t.Cleanup(func() { SetLogger(orig) })

	if err := ConfigureLoggerFromFlags("json", "warn"); err != nil {
		t.Fatalf("ConfigureLoggerFromFlags: %v", err)
	}
	if Logger() == nil {
		t.Fatal("expected installed logger after ConfigureLoggerFromFlags")
	}

	if err := ConfigureLoggerFromFlags("bogus", "info"); err == nil {
		t.Fatal("expected error for invalid format flag")
	}
}
