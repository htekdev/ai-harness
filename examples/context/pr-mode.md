---
name: pr-mode
type: plugin
version: "1.0.0"
description: "Conditional context for pull request review mode"
author: "Harness as Code"
tags: ["context", "pr", "conditional", "review"]
condition: 'ctx.get("mode") == "pull_request"'
---

# PR Review Mode Context

**Conditional context** that loads automatically when the agent is in pull request review mode.

## What This Does

- Loads **only when** `ctx.get("mode") == "pull_request"` is true
- Provides PR-specific instructions and rules
- Activated/deactivated automatically each turn based on runtime state
- Keeps the agent focused on PR review best practices

## How Conditions Work

The `condition` field in frontmatter contains a **Starlark expression** that's evaluated every turn:

```yaml
condition: 'ctx.get("mode") == "pull_request"'
```

- If the condition returns `True` → artifact is **active** this turn
- If the condition returns `False` → artifact is **inactive** this turn
- Conditions are evaluated **per-turn** — artifacts can activate/deactivate dynamically

## PR Review Rules

When this context is active, the agent follows these rules:

### Code Quality

- **Review all changed files** before approving
- **Check for common issues:**
  - Hardcoded credentials or secrets
  - SQL injection vulnerabilities
  - XSS vulnerabilities
  - Missing error handling
  - Hardcoded URLs or magic numbers
  - TODO/FIXME comments without issues

### CI/CD

- **Check CI status** before suggesting merge
- **Wait for all checks** to pass
- **Review test coverage** — no decrease allowed
- **Check build artifacts** for size/performance

### Best Practices

- **Never force-push** to shared branches
- **Add reviewers** based on CODEOWNERS
- **Suggest atomic commits** — one concern per commit
- **Check commit messages** — follow conventional commits
- **Verify branch naming** — `feat/`, `fix/`, `chore/` prefixes

### Review Comments

- **Be specific** — quote code and explain the issue
- **Be actionable** — suggest a fix, not just a problem
- **Be respectful** — no sarcasm or judgment
- **Link to docs** — provide context for suggestions

## Tools Available in PR Mode

```yaml
tools:
  - name: review_pr
    description: "Submit a PR review with comments"
    parameters:
      pr_number: { type: number, required: true }
      status: { type: string, required: true }  # approve, comment, request_changes
      comments: { type: array, required: false }
    script: |
      def run(args):
          # Implementation here
          return {"success": True}
```

## Example: Testing the Condition

**Set the context:**
```python
ctx.set("mode", "pull_request")
```

**Verify it's active:**
```bash
$ harness context --verbose
✅ pr-mode (plugin, priority 40, ACTIVE)
   Condition: ctx.get("mode") == "pull_request" → True
```

**Change the mode:**
```python
ctx.set("mode", "interactive")
```

**Verify it's inactive:**
```bash
$ harness context --verbose
⚪ pr-mode (plugin, priority 40, INACTIVE)
   Condition: ctx.get("mode") == "pull_request" → False
```

## Setting Context Variables

Context variables are set by:
1. **The harness runtime** (e.g., `mode`, `repo_visibility`, `branch`)
2. **Your code** via `ctx.set(key, value)`
3. **Tool scripts** that modify runtime state
4. **External triggers** (webhooks, CI events)

## Common Condition Patterns

### Load Only in Private Repos

```yaml
condition: 'ctx.get("repo_visibility") == "private"'
```

### Load Only for Python Files

```yaml
condition: 'ctx.get("file_ext") == ".py"'
```

### Load Only During Business Hours

```yaml
condition: '8 <= time.now() % 86400 / 3600 < 18'
```

### Combine Multiple Conditions

```yaml
condition: 'ctx.get("mode") == "review" and ctx.get("lang") == "go"'
```

## Related Examples

- See `examples/context/time-based.md` for time-based conditions
- See `examples/conditions/file-type.md` for file type detection
- See `examples/conditions/repo-private.md` for repo visibility

## Learn More

- Conditional loading: `examples/README.md#conditional-loading-starlark-expressions`
- Context functions: `examples/README.md#state-cache-ctx`
- Common patterns: `examples/README.md#common-patterns`
