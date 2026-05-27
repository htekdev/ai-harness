---
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
---

Read the contents of a file at the specified path. Returns the file content on success or an error if the file does not exist.
