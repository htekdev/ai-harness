---
name: git-commit-workflow
type: plugin
version: "1.0.0"
description: "Safe git commit workflow with validation and conventions"
author: "Harness as Code"
tags: ["git", "workflow", "skill", "tools"]
---

# Git Commit Workflow Skill

A **reusable skill** that implements a safe git commit workflow with validation, conventional commits, and co-author support.

## What This Is

A **skill** is a reusable procedure that combines:
- **Context** (instructions and rules)
- **Tools** (executable functions)
- **Hooks** (governance and safety)

Skills are **composable** — you can import multiple skills into one harness.

## What This Skill Does

1. **Validates** changes before committing
2. **Enforces** conventional commit messages
3. **Adds** co-author trailers automatically
4. **Checks** for secrets before committing
5. **Confirms** before pushing

## Workflow Steps

### 1. Stage Changes

```yaml
tools:
  - name: git_stage
    description: "Stage files for commit"
    parameters:
      paths: { type: array, required: true, description: "File paths to stage" }
    script: |
      def run(args):
          paths = args.get("paths", [])
          
          if len(paths) == 0:
              return {"error": "No files specified"}
          
          # Stage each file
          for path in paths:
              if not fs.exists(path):
                  return {"error": "File not found: " + path}
              
              result = exec.run("git", ["add", path])
              if result["exit_code"] != 0:
                  return {"error": "Failed to stage " + path + ": " + result["stderr"]}
          
          return {"success": True, "staged": paths}
```

### 2. Validate Commit Message

```yaml
tools:
  - name: git_commit
    description: "Commit staged changes with validation"
    parameters:
      message: { type: string, required: true }
      co_authors: { type: array, required: false }
    script: |
      def run(args):
          message = args.get("message", "")
          co_authors = args.get("co_authors", [])
          
          # Validate conventional commit format
          if not re.match("^(feat|fix|docs|style|refactor|test|chore)(\([a-z]+\))?:", message):
              return {"error": "Commit message must follow conventional commits: type(scope): description"}
          
          # Check message length
          if len(message) < 10:
              return {"error": "Commit message too short (min 10 characters)"}
          
          if len(message) > 72:
              return {"error": "Commit message too long (max 72 characters for first line)"}
          
          # Build full commit message with co-authors
          full_message = message
          for author in co_authors:
              full_message = full_message + "\n\nCo-authored-by: " + author
          
          # Commit
          result = exec.run("git", ["commit", "-m", full_message])
          if result["exit_code"] != 0:
              return {"error": "Commit failed: " + result["stderr"]}
          
          return {"success": True, "message": message}
```

### 3. Pre-Commit Validation Hook

```yaml
hooks:
  - event: tool.pre
    handler: git_commit_validation
    priority: 10
    when: 'tool_name == "git_commit"'
    script: |
      def handle(event, payload):
          # Check for staged secrets
          result = exec.run("git", ["diff", "--cached"])
          diff = result["stdout"]
          
          # Secret patterns
          secret_patterns = ["api_key=", "password=", "secret=", "token="]
          
          for pattern in secret_patterns:
              if pattern in diff:
                  return block("Staged changes contain potential secret: " + pattern)
          
          return allow()
```

## Example Usage

**Stage files:**
```
Use git_stage to stage: ["src/main.go", "README.md"]
```

**Commit:**
```
Use git_commit with message: "feat(api): add user authentication"
```

**With co-authors:**
```
Use git_commit with message "feat: add search" and co-authors ["Alice <alice@example.com>", "Bob <bob@example.com>"]
```

## Conventional Commit Types

| Type | Use When |
|------|----------|
| `feat:` | Adding a new feature |
| `fix:` | Fixing a bug |
| `docs:` | Documentation changes only |
| `style:` | Code style changes (formatting, missing semi-colons) |
| `refactor:` | Code refactoring (no feature/bug changes) |
| `test:` | Adding or updating tests |
| `chore:` | Maintenance tasks (deps, config, build) |

**Examples:**
- `feat(auth): add JWT token validation`
- `fix(api): handle null response from database`
- `docs: update installation instructions`
- `chore(deps): upgrade to Go 1.22`

## Git Push Tool

```yaml
tools:
  - name: git_push
    description: "Push commits to remote"
    parameters:
      force: { type: boolean, required: false, description: "Force push (dangerous)" }
    script: |
      def run(args):
          force = args.get("force", False)
          
          # Block force push to main/master
          branch_result = exec.run("git", ["branch", "--show-current"])
          branch = string.trim(branch_result["stdout"])
          
          if force and branch in ["main", "master"]:
              return {"error": "Force push to " + branch + " is not allowed"}
          
          # Push
          push_args = ["push"]
          if force:
              push_args.append("--force")
          
          result = exec.run("git", push_args)
          if result["exit_code"] != 0:
              return {"error": "Push failed: " + result["stderr"]}
          
          return {"success": True, "branch": branch}
```

## Related Skills

- See `examples/skills/code-review.md` for review workflows
- See `examples/hooks/command-guard.md` for command safety
- See `examples/tools/search-code.md` for code search

## Customization

### Add Branch Name Validation

```python
# Enforce branch naming convention
if not re.match("^(feat|fix|chore)/[a-z0-9-]+$", branch):
    return {"error": "Branch name must match: type/description (e.g., feat/add-auth)"}
```

### Add Commit Signing

```python
# GPG sign commits
result = exec.run("git", ["commit", "-S", "-m", full_message])
```

### Add Pre-Push Checks

```python
# Run tests before pushing
test_result = exec.run("go", ["test", "./..."])
if test_result["exit_code"] != 0:
    return {"error": "Tests failed. Fix before pushing."}
```

## Learn More

- Execution: `examples/README.md#execution-exec`
- Validation: `examples/README.md#validation-validate`
- Hooks: `examples/README.md#hook-events`
