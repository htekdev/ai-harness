---
name: git-workflow
description: Base git workflow guidance
tools:
  - name: run_tests
    description: Run the repository test suite
    parameters:
      package:
        type: string
        required: false
    timeout_ms: 10000
hooks:
  - event: tool.pre
    handler: guard_git
    priority: 10
    script: |
      def handle(event, payload):
        return {"ok": True}
---

Use the approved git workflow for all repository changes.

