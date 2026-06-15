---
event: agent.stop
priority: 10
script: |
  def handle(event, payload):
      if not ctx.agent.done_flag():
          return block("Call the `done` tool when the objective is complete.")

      verification = ctx.agent.run_verification_chain()
      if not verification.get("ok", False):
          return block("verification failed: " + verification.get("reason", "unknown reason"))

      return allow()
---

Autopilot stop gate: requires an explicit done signal and a successful
verification chain before the harness allows loop exit.
