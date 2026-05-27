---
parameters:
  path:
    type: string
    required: true
    description: "Path to the file to write"
  content:
    type: string
    required: true
    description: "Content to write to the file"
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
---

Write content to a file at the specified path. Creates the file if it doesn't exist, or overwrites it if it does.
