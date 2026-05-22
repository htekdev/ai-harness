---
name: strict-mode
type: override
description: Project-local override for strict development mode
condition: "env('HARNESS_STRICT') != ''"
tools:
  - name: exec
    description: Execute shell commands (strict mode - 10s timeout)
    parameters:
      command:
        type: string
        required: true
        description: The command to execute
    timeout_ms: 10000
hooks:
  - event: onPreToolUse
    handler: deny
    when: "tool.name == 'exec' and 'rm -rf' in tool.args.command"
    reason: Destructive commands are blocked in strict mode
---

# Strict Mode Override

When `HARNESS_STRICT` is set, this override:
- Reduces exec timeout to 10s
- Blocks destructive rm -rf commands
