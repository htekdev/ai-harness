---
parameters: {}
script: |
  def run(args):
      return {
          "runtime": "ai-harness",
          "profile": "governed-agent",
          "time": time.now(),
          "host": os.hostname(),
          "platform": os.platform(),
          "delegation_depth": ctx.get("delegation_depth", 0),
          "audit_count": metrics.get("audit.tool.pre"),
      }
---

# self_check

Returns a snapshot of the running harness — useful for verifying that the
governed-agent example is wired correctly end-to-end. Calls no external
services and bypasses no governance.
