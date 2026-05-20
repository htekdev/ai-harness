---
name: git-workflow
description: Base git workflow guidance
---

Use the approved git workflow for all repository changes.

## Tools
```yaml
- name: run_tests
  description: Run the repository test suite
  parameters:
    package:
      type: string
      required: false
  timeout_ms: 10000
```

## Hooks
```yaml
- event: tool.pre
  handler: guard_git
  priority: 10
  script: |
    def handle(event, payload):
      return {"ok": True}
```
