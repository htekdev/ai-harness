---
name: command-guard
type: plugin
version: "1.0.0"
description: "Blocks dangerous shell commands before execution"
author: "Harness as Code"
tags: ["security", "governance", "exec"]
---

# Command Guard Hook

A governance hook that **blocks dangerous shell commands** before they execute.

## What This Does

- Intercepts all `exec` tool calls via the `tool.pre` event
- Scans command strings for dangerous patterns
- Blocks execution and returns a clear error message
- Logs blocked attempts for security audit

## Security Patterns Blocked

- `rm -rf` — recursive force delete
- `dd if=` — direct disk write
- `> /dev/` — device file writes
- `mkfs` — filesystem formatting
- `chmod 777` — permission escalation
- `curl | bash` — pipe-to-shell execution

## How to Use

1. Copy this file to `.harness/hooks/command-guard.md`
2. Run `harness validate` to confirm it loads
3. Run `harness hooks --verbose` to see it in the registry
4. Test it by trying a blocked command

## Hooks

```yaml
- event: tool.pre
  handler: command_guard
  priority: 10
  when: 'tool_name == "exec"'
  script: |
    def handle(event, payload):
        # Extract the command from tool arguments
        args = payload.get("arguments", {})
        cmd = args.get("cmd", "")
        
        # Dangerous patterns that should never execute
        dangerous_patterns = [
            "rm -rf",
            "dd if=",
            "> /dev/",
            "mkfs",
            "chmod 777",
            "curl | bash",
            "wget | sh",
            ":(){ :|:& };:",  # Fork bomb
        ]
        
        # Check for dangerous patterns
        for pattern in dangerous_patterns:
            if pattern in cmd:
                log("🚫 BLOCKED: " + cmd)
                return block("Refusing dangerous command: " + cmd + " (matched pattern: " + pattern + ")")
        
        # Allow safe commands
        return allow()
```

## Example: Testing the Hook

**Blocked command:**
```bash
$ harness run
> Use the exec tool to run: rm -rf /tmp/test
❌ Error: Refusing dangerous command: rm -rf /tmp/test (matched pattern: rm -rf)
```

**Allowed command:**
```bash
$ harness run
> Use the exec tool to run: ls -la
✅ Success: [output of ls -la]
```

## Customization

Want to add more patterns? Edit the `dangerous_patterns` list:

```python
dangerous_patterns = [
    "rm -rf",
    "dd if=",
    "YOUR_CUSTOM_PATTERN",
]
```

## Priority

**Priority 10** = High-priority governance hook. Runs **before** most other hooks so dangerous commands are blocked early.

## Learn More

- See `examples/hooks/secret-guard.md` for secret detection
- See `examples/hooks/modify-tool.md` for argument modification
- Hook event reference: `examples/README.md#hook-events`
