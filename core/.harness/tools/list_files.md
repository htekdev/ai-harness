---
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
---

List all files and directories in the specified directory. Defaults to the current working directory if no path is provided.
