---
name: secret-guard
type: plugin
version: "1.0.0"
description: "Blocks tool output containing secrets or API keys"
author: "Harness as Code"
tags: ["security", "secrets", "governance"]
---

# Secret Guard Hook

A governance hook that **scans tool output for secrets** and blocks them before they reach the agent.

## What This Does

- Intercepts all tool outputs via the `tool.post` event
- Scans output for common secret patterns
- Blocks output containing secrets and returns a sanitized error
- Logs secret detection events for security audit

## Secret Patterns Detected

- API keys (e.g., `AKIAIOSFODNN7EXAMPLE`)
- JWT tokens (e.g., `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...`)
- Private keys (`-----BEGIN PRIVATE KEY-----`)
- GitHub tokens (`ghp_`, `gho_`, `ghs_`)
- Passwords in output (`password=`, `pwd=`)
- Database connection strings
- AWS credentials

## How to Use

1. Copy this file to `.harness/hooks/secret-guard.md`
2. Run `harness validate` to confirm it loads
3. Run `harness hooks --verbose` to see it in the registry
4. Test by running a tool that might output secrets

## Hooks

```yaml
- event: tool.post
  handler: secret_guard
  priority: 5
  script: |
    def handle(event, payload):
        # Extract tool result
        result = payload.get("result", "")
        tool_name = payload.get("tool_name", "unknown")
        
        # Convert to string if needed
        if not isinstance(result, str):
            result = json.encode(result)
        
        # Secret patterns (regex-like checks)
        secret_indicators = [
            "-----BEGIN PRIVATE KEY-----",
            "-----BEGIN RSA PRIVATE KEY-----",
            "ghp_",  # GitHub Personal Access Token
            "gho_",  # GitHub OAuth token
            "ghs_",  # GitHub Server-to-Server token
            "AKIA",  # AWS Access Key prefix
            "password=",
            "api_key=",
            "secret=",
            "token=",
        ]
        
        # Check for JWT tokens (simple pattern)
        jwt_parts = result.split(".")
        if len(jwt_parts) == 3 and all(len(part) > 10 for part in jwt_parts):
            log("🚫 SECRET DETECTED: JWT token in " + tool_name + " output")
            return block("Output contains a JWT token. Cannot display for security reasons.")
        
        # Check for secret indicators
        for indicator in secret_indicators:
            if indicator in result:
                log("🚫 SECRET DETECTED: " + indicator + " in " + tool_name + " output")
                return block("Output contains potential secret (" + indicator + "). Cannot display for security reasons.")
        
        # Check for AWS-style keys (pattern: 20-char alphanum + 40-char base64)
        if re.match("AKIA[0-9A-Z]{16}", result):
            log("🚫 SECRET DETECTED: AWS key pattern in " + tool_name + " output")
            return block("Output contains potential AWS credentials. Cannot display for security reasons.")
        
        # Allow safe output
        return allow()
```

## Example: Testing the Hook

**Blocked output:**
```bash
$ harness run
> Read the file `.env`
❌ Error: Output contains potential secret (api_key=). Cannot display for security reasons.
```

**Allowed output:**
```bash
$ harness run
> Read the file `README.md`
✅ Success: [file contents]
```

## Customization

Want to add more patterns? Edit the `secret_indicators` list:

```python
secret_indicators = [
    "ghp_",
    "YOUR_CUSTOM_PATTERN",
]
```

Or add regex patterns using `re.match()`:

```python
if re.match("YOUR_REGEX_PATTERN", result):
    return block("Secret detected")
```

## Priority

**Priority 5** = Highest-priority hook. Runs **first** on all tool outputs to catch secrets before any other processing.

## Learn More

- See `examples/hooks/command-guard.md` for input validation
- See `examples/hooks/modify-tool.md` for output sanitization
- Built-in functions: `examples/README.md#built-in-starlark-functions`
