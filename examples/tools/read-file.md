---
name: read-file
type: plugin
version: "1.0.0"
description: "Simple file reader tool with error handling"
author: "Harness as Code"
tags: ["filesystem", "io", "tools"]
---

# Read File Tool

A simple tool that **reads file contents** from the workspace with proper error handling.

## What This Does

- Reads a file from the workspace
- Returns file contents as a string
- Handles errors gracefully (file not found, permission denied)
- Respects filesystem jail (can't read outside working directory)

## Use Cases

- Read configuration files
- Load templates
- Read code files for analysis
- Load data files

## How to Use

1. Copy this file to `.harness/tools/read-file.md`
2. Run `harness validate` to confirm it loads
3. Run `harness tools --verbose` to see it in the registry
4. Call it from your agent: "Read the file README.md"

## Tools

```yaml
- name: read_file
  description: "Read the contents of a file from the workspace"
  parameters:
    path:
      type: string
      required: true
      description: "Path to the file to read (relative or absolute)"
  timeout_ms: 5000
  script: |
    def run(args):
        # Get the file path
        path = args.get("path", "")
        
        # Validation
        if not path:
            return {"error": "Path parameter is required"}
        
        # Check if file exists
        if not fs.exists(path):
            return {"error": "File not found: " + path}
        
        # Read file contents
        try:
            content = fs.read(path)
            return {
                "success": True,
                "path": path,
                "content": content,
                "length": len(content)
            }
        except Exception as e:
            return {"error": "Failed to read file: " + str(e)}
```

## Example Usage

**Agent prompt:**
```
Read the file config.yaml
```

**Tool call:**
```json
{
  "tool": "read_file",
  "arguments": {
    "path": "config.yaml"
  }
}
```

**Tool response:**
```json
{
  "success": true,
  "path": "config.yaml",
  "content": "model:\n  name: gpt-4o\n  temperature: 0.7\n",
  "length": 45
}
```

## Error Handling

**File not found:**
```json
{
  "error": "File not found: missing.txt"
}
```

**Permission denied:**
```json
{
  "error": "Failed to read file: permission denied"
}
```

## Security

- **Filesystem jail:** All `fs.*` operations are jailed to the working directory
- **Path traversal blocked:** Can't read `/etc/passwd` or `../../secrets.txt`
- **Timeout:** 5 second timeout prevents hanging on large files
- **Error messages:** Don't expose sensitive path information

## Customization

### Add File Type Validation

```python
# Only allow reading specific file types
allowed_extensions = [".md", ".txt", ".yaml", ".json"]
ext = path[path.rfind("."):]
if ext not in allowed_extensions:
    return {"error": "File type not allowed: " + ext}
```

### Add Size Limit

```python
# Check file size before reading
stat = fs.stat(path)
max_size = 1024 * 1024  # 1 MB
if stat["size"] > max_size:
    return {"error": "File too large: " + str(stat["size"]) + " bytes"}
```

### Add Caching

```python
# Cache file contents
cache_key = "file:" + path
if cache.has(cache_key):
    return {"success": True, "content": cache.get(cache_key), "cached": True}

content = fs.read(path)
cache.set(cache_key, content)
return {"success": True, "content": content, "cached": False}
```

## Related Tools

- See `examples/tools/search-code.md` for searching across files
- See `examples/tools/create-task.md` for task creation
- See `examples/README.md#filesystem-fs` for all `fs.*` functions

## Learn More

- Parameter types: `examples/README.md#tool-parameter-types`
- Built-in functions: `examples/README.md#filesystem-fs`
- Error handling patterns: `examples/README.md#common-patterns`
