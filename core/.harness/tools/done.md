---
parameters:
  summary:
    type: string
    required: false
    description: "1-3 sentence summary of what was accomplished"
  claims:
    type: array
    required: false
    description: "Structured claims that downstream verification hooks can validate"
timeout_ms: 1000
script: |
  def run(args):
      summary = args.get("summary", "")
      claims = args.get("claims", [])
      return ctx.agent.set_done_flag(summary=summary, claims=claims)
---

Signal that work is complete for this turn. This tool only sets a completion flag; harness exit still depends on the configured `exit_policy` and any `agent.stop` hooks.
