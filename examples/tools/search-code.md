---
name: search-code
type: plugin
version: "1.0.0"
description: "Search for patterns across codebase files using regex"
author: "Harness as Code"
tags: ["search", "regex", "tools", "filesystem"]
---

# Search Code Tool

A tool that **searches for regex patterns across codebase files** — like `grep` but with structured output.

## What This Does

- Searches all files in a directory for a regex pattern
- Returns structured results with file paths and line numbers
- Supports file extension filtering
- Handles large codebases efficiently

## Use Cases

- Find function definitions across a codebase
- Search for TODO/FIXME comments
- Find all imports of a module
- Locate hardcoded strings or magic numbers
- Identify security patterns (SQL injection, eval usage)

## How to Use

1. Copy this file to `.harness/tools/search-code.md`
2. Run `harness validate` to confirm it loads
3. Run `harness tools --verbose` to see it in the registry
4. Call it: "Search for 'function main' in all Go files"

## Tools

```yaml
- name: search_code
  description: "Search for a regex pattern across codebase files"
  parameters:
    pattern:
      type: string
      required: true
      description: "Regex pattern to search for"
    path:
      type: string
      required: false
      description: "Directory to search (defaults to current directory)"
    file_ext:
      type: string
      required: false
      description: "Filter by file extension (e.g., '.go', '.py', '.js')"
    max_results:
      type: number
      required: false
      description: "Maximum number of results to return (default: 100)"
  timeout_ms: 30000
  script: |
    def run(args):
        # Extract arguments
        pattern = args.get("pattern", "")
        search_path = args.get("path", ".")
        file_ext = args.get("file_ext", "")
        max_results = args.get("max_results", 100)
        
        # Validation
        if not pattern:
            return {"error": "Pattern parameter is required"}
        
        # Find all files matching the extension
        if file_ext:
            glob_pattern = "**/*" + file_ext
        else:
            glob_pattern = "**/*"
        
        files = fs.glob(glob_pattern)
        
        # Search each file
        results = []
        for file_path in files:
            # Skip directories
            if not fs.exists(file_path):
                continue
            
            # Read file contents
            try:
                content = fs.read(file_path)
            except Exception:
                continue  # Skip files we can't read
            
            # Search for pattern in file
            matches = re.find_all(pattern, content)
            
            if len(matches) > 0:
                # Find line numbers for each match
                lines = content.split("\n")
                for match in matches:
                    for i, line in enumerate(lines):
                        if match in line:
                            results.append({
                                "file": file_path,
                                "line": i + 1,
                                "match": match,
                                "context": string.trim(line)
                            })
                            
                            # Check max results
                            if len(results) >= max_results:
                                return {
                                    "success": True,
                                    "pattern": pattern,
                                    "results": results,
                                    "truncated": True,
                                    "message": "Reached max results (" + str(max_results) + ")"
                                }
        
        return {
            "success": True,
            "pattern": pattern,
            "results": results,
            "count": len(results),
            "truncated": False
        }
```

## Example Usage

**Agent prompt:**
```
Search for 'TODO' comments in all Python files
```

**Tool call:**
```json
{
  "tool": "search_code",
  "arguments": {
    "pattern": "TODO:",
    "file_ext": ".py",
    "max_results": 50
  }
}
```

**Tool response:**
```json
{
  "success": true,
  "pattern": "TODO:",
  "results": [
    {
      "file": "src/main.py",
      "line": 42,
      "match": "TODO: Add error handling",
      "context": "    # TODO: Add error handling here"
    },
    {
      "file": "src/utils.py",
      "line": 15,
      "match": "TODO: Optimize this function",
      "context": "    # TODO: Optimize this function"
    }
  ],
  "count": 2,
  "truncated": false
}
```

## Common Search Patterns

### Find Function Definitions (Python)

```python
pattern = "def [a-zA-Z_][a-zA-Z0-9_]*\("
file_ext = ".py"
```

### Find SQL Queries

```python
pattern = "SELECT .* FROM"
```

### Find Hardcoded IPs

```python
pattern = "[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}"
```

### Find Security Issues

```python
pattern = "(eval\(|exec\(|os\.system\(|subprocess\.call\()"
```

## Performance Tips

1. **Use file extension filtering** to reduce search scope
2. **Set `max_results`** to avoid returning huge result sets
3. **Use specific patterns** — overly broad patterns are slow
4. **Increase timeout** for large codebases

## Customization

### Add Ignore Patterns

```python
# Skip certain directories
ignore_dirs = ["node_modules", ".git", "dist", "build"]
for ignore in ignore_dirs:
    if ignore in file_path:
        continue
```

### Case-Insensitive Search

```python
# Convert to lowercase for comparison
content_lower = string.lower(content)
pattern_lower = string.lower(pattern)
matches = re.find_all(pattern_lower, content_lower)
```

### Return Full Context Lines

```python
# Return 2 lines before and after match
context_lines = []
start = max(0, i - 2)
end = min(len(lines), i + 3)
for j in range(start, end):
    context_lines.append(lines[j])
```

## Related Tools

- See `examples/tools/read-file.md` for reading single files
- See `examples/tools/create-task.md` for task creation
- See `examples/README.md#regex-re` for regex functions

## Learn More

- Built-in functions: `examples/README.md#regex-re`
- Filesystem operations: `examples/README.md#filesystem-fs`
- Common patterns: `examples/README.md#common-patterns`
