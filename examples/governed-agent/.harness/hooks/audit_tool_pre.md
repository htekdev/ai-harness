---
event: tool.pre
priority: 1
script: |
  def handle(event, payload):
      # Audit-everything hook: increment a counter for every tool call,
      # including ones that will subsequently be blocked.
      metrics.incr("audit.tool.pre")
      log("[audit] tool.pre name=" + payload.get("name", "?"))
      return allow()
---

# audit_tool_pre

Audit hook that runs FIRST on every `tool.pre` event. Increments
`audit.tool.pre` (visible via `metrics.snapshot()`) and emits a log line per
tool call. Pairs with `audit_tool_post` for full request/response auditing.

Priority `1` ensures it runs before policy hooks, so even denied calls show up
in the audit count.
