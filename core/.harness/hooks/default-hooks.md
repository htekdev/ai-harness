---
name: default-hooks
type: plugin
version: "1.0.0"
description: Default safety hooks for command and secret protection
author: AI Harness
tags: [security, safety, governance]
---

# Default Hooks

Safety hooks that protect against dangerous operations.

## Hooks

```yaml
- event: tool.pre
  handler: block_dangerous_commands
  priority: 10
  when: 'tool_name == "exec"'
  script: |
    def handle(event, payload):
        args = payload.get("arguments", {})
        cmd = args.get("cmd", "")
        
        # Dangerous patterns
        dangerous = [
            "rm -rf",
            "dd if=",
            "> /dev/",
            "mkfs",
            "format c:",
        ]
        
        for pattern in dangerous:
            if pattern in cmd:
                return block("Refusing dangerous command: " + cmd)
        
        return allow()

- event: tool.post
  handler: detect_secrets
  priority: 5
  script: |
    def handle(event, payload):
        result = payload.get("result", "")
        if not isinstance(result, str):
            result = json.encode(result)
        
        # Secret patterns
        if "-----BEGIN PRIVATE KEY-----" in result:
            return block("Output contains private key")
        if "api_key=" in result or "password=" in result:
            return block("Output may contain secrets")
        
        return allow()
```
