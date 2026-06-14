---
event: tool.post
priority: 99
script: |
  def handle(event, payload):
      metrics.incr("audit.tool.post")
      err = payload.get("error", "")
      if err:
          metrics.incr("audit.tool.error")
          log("[audit] tool.post name=" + payload.get("name", "?") + " error=" + err)
      return allow()
---

# audit_tool_post

Counts every `tool.post` and bumps `audit.tool.error` whenever a tool call
returned an error. Combined with `audit_tool_pre`, the metrics snapshot tells
you tool-call volume, error rate, and (via the policy hook below) deny rate.
