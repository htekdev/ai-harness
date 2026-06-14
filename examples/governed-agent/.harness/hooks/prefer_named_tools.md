---
event: tool.pre
priority: 5
when: payload["name"] == "exec"
script: |
  def handle(event, payload):
      # The agent should always prefer named wrappers (run_command) over the
      # raw exec built-in. tools_policy already denies "exec", but this hook
      # produces a clearer error for the model: "use run_command instead".
      metrics.incr("audit.policy.deny")
      return block("raw 'exec' is disabled in this profile — use the named 'run_command' tool instead")
---

# prefer_named_tools

Blocks raw `exec` calls and tells the model exactly which named tool to use.
This is the "soft" governance layer that makes denied calls *informative*
rather than just "tool not found".

Pairs with `tools_policy.deny: ["exec"]` in `harness.md` — the policy is the
hard enforcement, this hook is the human-readable explanation.
