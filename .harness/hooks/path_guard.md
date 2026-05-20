---
event: tool.pre
priority: 1
when: payload["name"] in ["read_file", "write_file", "edit_file", "read_lines"]
script: |
  def handle(event, payload):
      path = payload.get("args", {}).get("path", "")
      if ".." in path:
          return block("path traversal not allowed: contains '..'")
      if path.startswith("/") or (len(path) > 1 and path[1] == ":"):
          return block("absolute paths not allowed")
      return allow()
---

# path_guard

Blocks any file operation that attempts path traversal (`..`) or uses absolute paths. This prevents agents from accessing files outside the workspace.
