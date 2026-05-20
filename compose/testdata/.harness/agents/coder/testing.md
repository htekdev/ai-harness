---
name: testing
description: Agent-specific testing guidance
---

Run focused tests after each change.

## Tools
```yaml
- name: go_test
  description: Run Go package tests
  parameters:
    target:
      type: string
      required: true
```
