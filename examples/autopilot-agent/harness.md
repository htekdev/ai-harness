---
model:
  provider: copilot
  name: gpt-4o
  api_key_env: GH_TOKEN

context:
  max_history: 50
  max_tokens: 128000

exit_policy:
  mode: hybrid
  max_iterations: 50
  on_max_iterations: error
---

# Autopilot Agent (Exit Policy Example)

This example keeps the loop running until both:

1. The `done` tool is called, and
2. `agent.stop` verification hooks allow exit.

Use this profile when you want verified-completion behavior instead of
single-pass natural exits.
