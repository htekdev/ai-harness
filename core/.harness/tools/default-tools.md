---
name: default-tools
type: plugin
version: "1.0.0"
description: Default file system tools for working with files and directories
author: AI Harness
tags: [filesystem, core]
---

# Default Tools

Core file system tools that work out of the box.

## Tools

```yaml
- name: read_file
  description: "Read the contents of a file"
  parameters:
    path:
      type: string
      required: true
      description: "Path to the file to read"
  timeout_ms: 5000
  script: |
    def run(args):
        path = args.get("path", "")
        if not path:
            return {"error": "Path is required"}
        if not fs.exists(path):
            return {"error": "File not found: " + path}
        content = fs.read(path)
        return {
            "success": True,
            "path": path,
            "content": content
        }

- name: write_file
  description: "Write content to a file"
  parameters:
    path:
      type: string
      required: true
      description: "Path to the file to write"
    content:
      type: string
      required: true
      description: "Content to write"
  timeout_ms: 5000
  script: |
    def run(args):
        path = args.get("path", "")
        content = args.get("content", "")
        if not path:
            return {"error": "Path is required"}
        fs.write(path, content)
        return {
            "success": True,
            "path": path,
            "bytes_written": len(content)
        }

- name: list_files
  description: "List files in a directory"
  parameters:
    path:
      type: string
      required: false
      description: "Directory path (defaults to current directory)"
  timeout_ms: 5000
  script: |
    def run(args):
        path = args.get("path", ".")
        if not fs.exists(path):
            return {"error": "Directory not found: " + path}
        files = fs.list(path)
        return {
            "success": True,
            "path": path,
            "files": files
        }

- name: get_current_folder
  description: "Get the absolute path of the current working directory"
  timeout_ms: 1000
  script: |
    def run(args):
        result = exec.run("pwd", [])
        if result["exit_code"] != 0:
            return {"error": "Failed to get current directory"}
        cwd = string.trim(result["stdout"])
        ctx.set("current_folder", cwd)
        return {
            "success": True,
            "folder": cwd
        }
```
