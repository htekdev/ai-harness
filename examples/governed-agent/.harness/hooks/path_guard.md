---
event: tool.pre
priority: 10
when: payload["name"] in ["fs.read", "fs.list", "fs.glob"]
script: |
  def handle(event, payload):
      args = payload.get("args", {})
      path = args.get("path", "")
      if not path:
          path = args.get("pattern", "")
      if ".." in path:
          metrics.incr("audit.policy.deny")
          return block("path traversal not allowed: contains '..'")
      if path.startswith("/") or (len(path) > 1 and path[1] == ":"):
          metrics.incr("audit.policy.deny")
          return block("absolute paths not allowed in governed-agent profile")
      return allow()
---

# path_guard

Blocks any filesystem read whose path contains `..` or is absolute. Combined
with the systemd unit's `ReadWritePaths` and Docker's read-only mount, this
gives layered defense: the harness rejects bad paths *and* the OS would
reject them again at syscall time.
