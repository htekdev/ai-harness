---
name: repo-visibility-context
type: plugin
version: "1.0.0"
description: "Conditional context based on repository visibility (private vs public)"
author: "Harness as Code"
tags: ["conditional", "repo", "security", "context"]
---

# Repository Visibility Conditional Loading

**Conditional artifacts** that load based on whether the repository is **private** or **public**.

## What This Does

- Loads different rules for private vs public repos
- **Private repos:** More permissive (can commit secrets, use real data)
- **Public repos:** More restrictive (block secrets, sanitize data)
- Activates automatically based on `ctx.get("repo_visibility")`

## How to Set Repository Visibility

The harness runtime should detect repository visibility at session start. You can also set it manually:

```python
# At session start or in a tool
ctx.set("repo_visibility", "private")
ctx.set("repo_visibility", "public")
```

## Private Repository Context

```markdown
---
name: private-repo-context
type: plugin
condition: 'ctx.get("repo_visibility") == "private"'
---

# Private Repository Context

You're working in a **private repository**.

## Permissions

✅ Allowed in private repos:
- Commit configuration with API keys (still discourage, but not blocked)
- Use real customer data for testing
- Write internal-only documentation
- Reference internal systems by name
- Use company-specific terminology

⚠️ Still required:
- Review code for security issues
- Follow coding standards
- Write tests
- Document complex logic

## Rules

- **Secrets:** Can commit `.env` files (but warn about it)
- **Data:** Can use real data for development
- **Naming:** Can reference internal systems
- **Docs:** Can include internal links and references
\```

## Public Repository Context

```markdown
---
name: public-repo-context
type: plugin
condition: 'ctx.get("repo_visibility") == "public"'
---

# Public Repository Context

You're working in a **public repository**. Extra security rules apply.

## Critical Rules

🚫 **NEVER** commit:
- API keys or secrets
- Real customer data
- Internal system names or URLs
- Proprietary algorithms
- Company-specific terminology

✅ **ALWAYS:**
- Use placeholder data
- Sanitize all examples
- Use generic terminology
- Link to public documentation only
- Assume the world can see this code

## Hooks Active

- Secret detection (blocks commits with secrets)
- Data sanitization (warns about real data)
- Link checker (blocks internal URLs)
- License checker (requires LICENSE file)
\```

## Secret Guard Hook (Public Repos Only)

```yaml
hooks:
  - event: tool.pre
    handler: block_secrets_in_public_repo
    priority: 5
    when: 'tool_name in ["git_commit", "write_file"] and ctx.get("repo_visibility") == "public"'
    script: |
      def handle(event, payload):
          args = payload.get("arguments", {})
          
          # Check commit message or file content
          content = args.get("message", "") + args.get("content", "")
          
          # Secret patterns
          secret_patterns = [
              "api_key=",
              "password=",
              "secret=",
              "token=",
              "AKIA",  # AWS key prefix
              "ghp_",  # GitHub token
          ]
          
          for pattern in secret_patterns:
              if pattern in content:
                  return block("🚫 Cannot commit potential secret in public repo: " + pattern)
          
          return allow()
```

## Data Sanitization Hook (Public Repos Only)

```yaml
hooks:
  - event: tool.pre
    handler: warn_about_real_data
    priority: 20
    when: 'tool_name == "write_file" and ctx.get("repo_visibility") == "public"'
    script: |
      def handle(event, payload):
          args = payload.get("arguments", {})
          content = args.get("content", "")
          
          # Check for real-looking data
          real_data_patterns = [
              "@gmail.com",
              "@yahoo.com",
              "@hotmail.com",
              # Phone numbers
              "[0-9]{3}-[0-9]{3}-[0-9]{4}",
              # Credit cards (simplified)
              "[0-9]{4} [0-9]{4} [0-9]{4} [0-9]{4}",
          ]
          
          for pattern in real_data_patterns:
              if re.match(pattern, content):
                  log("⚠️ WARNING: File may contain real data. Use placeholders in public repos.")
                  break
          
          return allow()
```

## Detecting Repository Visibility

### From Git Remote URL

```yaml
tools:
  - name: detect_repo_visibility
    description: "Detect if repository is private or public"
    script: |
      def run(args):
          # Get git remote URL
          result = exec.run("git", ["remote", "get-url", "origin"])
          
          if result["exit_code"] != 0:
              return {"error": "Not a git repository"}
          
          remote_url = string.trim(result["stdout"])
          
          # Check for private indicators
          # (This is simplified - real detection would use GitHub API)
          is_private = "private" in remote_url or "internal" in remote_url
          
          visibility = "private" if is_private else "public"
          ctx.set("repo_visibility", visibility)
          
          return {
              "success": True,
              "visibility": visibility,
              "remote_url": remote_url
          }
```

### From GitHub API

```yaml
tools:
  - name: check_github_visibility
    description: "Check repository visibility via GitHub API"
    parameters:
      repo: { type: string, required: true }  # Format: "owner/repo"
    script: |
      def run(args):
          repo = args.get("repo", "")
          
          # Call GitHub API
          api_url = "https://api.github.com/repos/" + repo
          result = http.get(api_url, headers={
              "Accept": "application/vnd.github.v3+json",
              "Authorization": "token " + env("GITHUB_TOKEN")
          })
          
          if result["status"] != 200:
              return {"error": "Failed to fetch repo info"}
          
          # Parse response
          data = json.decode(result["body"])
          is_private = data.get("private", False)
          
          visibility = "private" if is_private else "public"
          ctx.set("repo_visibility", visibility)
          
          return {
              "success": True,
              "visibility": visibility,
              "repo": repo
          }
```

## Session Start Hook (Auto-Detect)

```yaml
hooks:
  - event: session.start
    handler: auto_detect_repo_visibility
    priority: 5
    script: |
      def handle(event, payload):
          # Try to detect from git remote
          result = exec.run("git", ["remote", "get-url", "origin"])
          
          if result["exit_code"] == 0:
              remote_url = string.trim(result["stdout"])
              
              # Simple heuristic: check for "github.com" and no "private" in path
              # Real implementation would use GitHub API
              if "github.com" in remote_url:
                  # Default to public for safety
                  ctx.set("repo_visibility", "public")
                  log("🌍 Detected public repository — strict security rules active")
              else:
                  ctx.set("repo_visibility", "private")
                  log("🔒 Detected private repository")
          else:
              # Not a git repo, default to public (safe)
              ctx.set("repo_visibility", "public")
          
          return allow()
```

## Testing Visibility Detection

```bash
# Test private repo context
$ harness run
> Set repo visibility to private
$ harness context --verbose
✅ private-repo-context (plugin, priority 40, ACTIVE)

# Test public repo context
$ harness run
> Set repo visibility to public
$ harness context --verbose
✅ public-repo-context (plugin, priority 40, ACTIVE)
```

## Customization

### Add Internal Domain Detection

```python
# Detect internal repos by domain
if "internal.company.com" in remote_url:
    ctx.set("repo_visibility", "private")
    ctx.set("repo_type", "internal")
```

### Add License Check for Public Repos

```python
# In public repos, require LICENSE file
if ctx.get("repo_visibility") == "public":
    if not fs.exists("LICENSE"):
        log("⚠️ Public repository missing LICENSE file")
```

## Related Examples

- See `examples/hooks/secret-guard.md` for secret detection
- See `examples/context/pr-mode.md` for mode-based conditions
- See `examples/conditions/file-type.md` for file type detection

## Learn More

- Conditional loading: `examples/README.md#conditional-loading-starlark-expressions`
- Context variables: `examples/README.md#state-cache-ctx`
- Network requests: `examples/README.md#network-http-url`
