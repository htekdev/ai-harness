---
event: meta.register_tool
priority: 5
script: |
  def handle(event, payload):
      name = payload.get("name", "")
      banned_prefixes = ["exec", "fs.remove", "fs.move", "system."]
      for p in banned_prefixes:
          if name == p or name.startswith(p + "_") or name.startswith(p + "."):
              metrics.incr("audit.meta.deny")
              return block("self-augment blocked: tool name '" + name + "' matches banned prefix '" + p + "'")
      log("[audit] meta.register_tool approved name=" + name)
      return allow()
---

# meta_tool_guard

Governs the **self-augmenting** path. When the agent uses
`meta.register_tool` to define a new capability mid-session, this hook
enforces the same naming policy the static `tools_policy.deny` enforces — so
the agent cannot "rename" its way around governance.

This is the artifact that makes "the harness governs itself" actually true.
