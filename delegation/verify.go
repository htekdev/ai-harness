package delegation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/htekdev/ai-harness/scripting"
)

// VerifyOutcome is the structured result of a single verification attempt.
//
// Issue #103: deterministic, declarative claims verification at the
// delegation boundary. A verifier asserts that a sub-agent's claimed work
// actually happened (file exists, repo resolves, commit is reachable, …)
// and returns a Verified flag plus a human Reason on failure. The
// Delegator's Ralph loop consumes this to decide whether to accept the
// delegation result or re-invoke the delegate with the failure context.
type VerifyOutcome struct {
	// Verified is true if the verifier accepts the delegation result.
	Verified bool `json:"verified"`
	// Reason is a human-readable explanation; required when Verified is
	// false (it gets re-injected into the delegate on retry) and
	// optional but useful when Verified is true (audit trail).
	Reason string `json:"reason,omitempty"`
}

// runVerifyScript executes a Starlark verify script against the delegation
// result and returns the structured outcome.
//
// Contract: the script must define `run(result)`. The argument is a dict
// shaped like the JSON encoding of *Result (response/tool_calls/tool_results).
// The script must return a JSON-encoded object (use `json.encode(...)`)
// with at least `verified` (bool); `reason` is optional. Any compile/runtime
// error is surfaced as an error from this function — *not* converted into
// "verified: false". Callers (the Ralph loop) decide whether to fail the
// whole delegation or treat it as a hard stop.
//
// Example verifier script:
//
//	def run(result):
//	    # http and fs builtins are available
//	    resp = http.get("https://api.github.com/repos/htekdev/ai-harness")
//	    return json.encode({
//	        "verified": resp["status"] == 200,
//	        "reason": "" if resp["status"] == 200 else "repo not found",
//	    })
func runVerifyScript(ctx context.Context, engine *scripting.Engine, script string, result *Result) (VerifyOutcome, error) {
	if engine == nil {
		return VerifyOutcome{}, fmt.Errorf("verify: scripting engine not configured")
	}
	if script == "" {
		return VerifyOutcome{Verified: true}, nil
	}

	runner, err := engine.CompileToolScript("delegation.verify", script)
	if err != nil {
		return VerifyOutcome{}, fmt.Errorf("verify: compile script: %w", err)
	}

	// Marshal the delegation result so the script sees a clean JSON dict.
	payload, err := json.Marshal(result)
	if err != nil {
		return VerifyOutcome{}, fmt.Errorf("verify: marshal result: %w", err)
	}

	out, err := runner.Run(ctx, payload)
	if err != nil {
		return VerifyOutcome{}, fmt.Errorf("verify: script error: %w", err)
	}

	// The runner stringifies the returned value; we expect either a JSON
	// object or a Starlark dict that stringified to one.
	var outcome VerifyOutcome
	if err := json.Unmarshal([]byte(out), &outcome); err != nil {
		// Tolerate the common case where the script returns a bare bool
		// or a string. A bare `True` becomes `verified: true`; a bare
		// string is treated as a failure reason.
		if out == "True" || out == "true" {
			return VerifyOutcome{Verified: true}, nil
		}
		if out == "False" || out == "false" {
			return VerifyOutcome{Verified: false, Reason: "verifier returned bare false"}, nil
		}
		return VerifyOutcome{}, fmt.Errorf("verify: parse outcome %q: %w (must be a JSON object with 'verified' and optional 'reason')", out, err)
	}

	return outcome, nil
}
