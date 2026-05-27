---
name: modify-tool-args
type: plugin
version: "1.0.0"
description: "Modifies tool arguments before execution (example: auto-add file extensions)"
author: "Harness as Code"
tags: ["tools", "modify", "automation"]
---

# Modify Tool Arguments Hook

A governance hook that **modifies tool arguments before execution** — useful for adding defaults, normalizing inputs, or enforcing conventions.

## What This Does

- Intercepts tool calls via the `tool.pre` event
- Modifies arguments before the tool executes
- Example: Auto-append `.md` extension to filenames without one
- Example: Normalize file paths to absolute paths
- Example: Add default timeout values

## Use Cases

- **Auto-complete file extensions** — `.md`, `.py`, `.go`
- **Normalize paths** — convert relative to absolute
- **Add defaults** — timeout, retry count, headers
- **Enforce conventions** — lowercase filenames, no spaces
- **Sanitize inputs** — strip dangerous characters

## How to Use

1. Copy this file to `.harness/hooks/modify-tool.md`
2. Customize the `handle()` function for your use case
3. Run `harness validate` to confirm it loads
4. Test by calling a tool and observing the modification

## Hooks

```yaml
- event: tool.pre
  handler: modify_tool_args
  priority: 50
  when: 'tool_name == "read_file"'
  script: |
    def handle(event, payload):
        # Extract arguments
        args = payload.get("arguments", {})
        path = args.get("path", "")
        
        # Modification 1: Auto-append .md extension if missing
        if path and not path.endswith((".md", ".txt", ".json", ".yaml", ".yml")):
            path = path + ".md"
            log("📝 Auto-appended .md extension: " + path)
        
        # Modification 2: Normalize to absolute path (if not already)
        if path and not path.startswith("/"):
            # Get working directory
            cwd = ctx.get("working_dir", ".")
            path = cwd + "/" + path
            log("📂 Normalized to absolute path: " + path)
        
        # Return modified arguments
        modified_args = {"path": path}
        return modify({"arguments": modified_args})

- event: tool.pre
  handler: add_default_timeout
  priority: 60
  when: 'tool_name == "http_get"'
  script: |
    def handle(event, payload):
        # Extract arguments
        args = payload.get("arguments", {})
        
        # Add default timeout if not specified
        if "timeout_ms" not in args:
            args["timeout_ms"] = 30000  # 30 seconds
            log("⏱️ Added default timeout: 30000ms")
        
        # Return modified arguments
        return modify({"arguments": args})
```

## Example: Testing the Hook

**Before modification:**
```bash
$ harness run
> Read file: notes
Tool arguments: {"path": "notes"}
```

**After modification:**
```bash
$ harness run
> Read file: notes
📝 Auto-appended .md extension: notes.md
📂 Normalized to absolute path: /workspace/notes.md
Tool arguments: {"path": "/workspace/notes.md"}
```

## Customization Ideas

### 1. Enforce Lowercase Filenames

```python
def handle(event, payload):
    args = payload.get("arguments", {})
    path = args.get("path", "")
    path = path.lower()
    return modify({"arguments": {"path": path}})
```

### 2. Strip Dangerous Characters

```python
def handle(event, payload):
    args = payload.get("arguments", {})
    cmd = args.get("cmd", "")
    
    # Remove shell operators
    cmd = cmd.replace("&&", "").replace("||", "").replace(";", "")
    
    return modify({"arguments": {"cmd": cmd}})
```

### 3. Add Default Headers to HTTP Calls

```python
def handle(event, payload):
    args = payload.get("arguments", {})
    headers = args.get("headers", {})
    
    # Add User-Agent if not present
    if "User-Agent" not in headers:
        headers["User-Agent"] = "AI-Harness/1.0"
    
    args["headers"] = headers
    return modify({"arguments": args})
```

## Priority

**Priority 50-60** = Application-level hooks. Run after governance hooks (1-49) but before user hooks (100+).

## Important: Modify vs Block vs Allow

```python
return allow()              # Let it proceed unchanged
return block(reason)        # Stop execution, return error
return modify(payload)      # Change arguments/result and continue
```

## Learn More

- See `examples/hooks/command-guard.md` for blocking patterns
- See `examples/tools/read-file.md` for tool definitions
- Hook actions reference: `examples/README.md#hook-actions`
