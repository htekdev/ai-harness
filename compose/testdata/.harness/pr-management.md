---
name: pr-management
description: Pull request workflow
condition: ctx.get("mode") == "pull_request"
---

Apply pull request review and CI expectations.

## Tools
```yaml
- name: create_pr
  description: Create a pull request
  parameters:
    title:
      type: string
      required: true
```

## Hooks
```yaml
- event: tool.post
  handler: ensure_ci
  priority: 20
  when: ctx.get("mode") == "pull_request"
```
