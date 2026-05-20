package scripting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEngine_HTTPBuiltins(t *testing.T) {
	engine := NewEngine()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get":
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if got := r.Header.Get("X-Test"); got != "from-script" {
				t.Fatalf("unexpected header: %q", got)
			}
			w.Header().Set("X-Reply", "ok")
			_, _ = w.Write([]byte("hello from get"))
		case "/post":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			defer r.Body.Close()
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if payload["message"] != "ping" {
				t.Fatalf("unexpected payload: %+v", payload)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("posted"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	runner, err := engine.CompileToolScript("http_tool", `
def run(args):
    get_resp = http.get(args["base"] + "/get", headers={"X-Test": "from-script"})
    post_resp = http.post(args["base"] + "/post", body=json.encode({"message": "ping"}), headers={"Content-Type": "application/json"})
    return str(get_resp["status"]) + "|" + get_resp["headers"]["X-Reply"] + "|" + get_resp["body"] + "|" + str(post_resp["status"]) + "|" + post_resp["body"]
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{"base": "`+server.URL+`"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "200|ok|hello from get|201|posted"; result != want {
		t.Fatalf("got %q, want %q", result, want)
	}
}

func TestEngine_RegexBuiltins(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("regex_tool", `
def run(args):
    match = re.match("(?P<prefix>[A-Z]+)-(\\d+)", "BUG-123 extra")
    refs = re.find_all("#\\d+", "Fixes #12 and relates to #34")
    replaced = re.replace("\\s+", "-", "hello   regex world")
    return json.encode({
        "matched": match["matched"],
        "prefix": match["named_groups"]["prefix"],
        "number": match["groups"][1],
        "refs": refs,
        "replaced": replaced,
    })
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload struct {
		Matched  bool     `json:"matched"`
		Prefix   string   `json:"prefix"`
		Number   string   `json:"number"`
		Refs     []string `json:"refs"`
		Replaced string   `json:"replaced"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !payload.Matched || payload.Prefix != "BUG" || payload.Number != "123" || payload.Replaced != "hello-regex-world" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if len(payload.Refs) != 2 || payload.Refs[0] != "#12" || payload.Refs[1] != "#34" {
		t.Fatalf("unexpected refs: %+v", payload.Refs)
	}
}

func TestEngine_HashBuiltins(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("hash_tool", `
def run(args):
    return hash.sha256("abc") + "|" + hash.md5("abc")
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad|900150983cd24fb0d6963f7d28e17f72"; result != want {
		t.Fatalf("got %q, want %q", result, want)
	}
}

func TestEngine_CacheBuiltins(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("cache_tool", `
def run(args):
    count = cache.get("count", 0)
    cache.set("count", count + 1)
    deleted = False
    if cache.has("transient"):
        deleted = cache.delete("transient")
    else:
        cache.set("transient", "value")
    return str(cache.get("count")) + "|" + str(cache.has("count")) + "|" + str(deleted)
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	first, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first != "1|True|False" {
		t.Fatalf("first run = %q", first)
	}

	second, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second != "2|True|True" {
		t.Fatalf("second run = %q", second)
	}

	clearRunner, err := engine.CompileToolScript("cache_clear_tool", `
def run(args):
    cache.clear()
    return str(cache.has("count"))
`)
	if err != nil {
		t.Fatalf("compile clear: %v", err)
	}

	cleared, err := clearRunner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("clear run: %v", err)
	}
	if cleared != "False" {
		t.Fatalf("expected cache to be cleared, got %q", cleared)
	}
}
