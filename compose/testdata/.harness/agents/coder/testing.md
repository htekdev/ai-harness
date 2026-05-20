---
name: testing
description: Agent-specific testing guidance
tools:
  - name: go_test
    description: Run Go package tests
    parameters:
      target:
        type: string
        required: true
---

Run focused tests after each change.

