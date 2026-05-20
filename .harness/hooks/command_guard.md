---
event: tool.pre
priority: 2
when: payload["name"] == "run_command"
script: |
  def handle(event, payload):
      cmd = payload.get("args", {}).get("command", "")
      dangerous = ["rm -rf /", "del /s /q C:", "format", "mkfs", "dd if="]
      for d in dangerous:
          if d in cmd:
              return block("dangerous command blocked: " + d)
      return allow()
---

# command_guard

Blocks dangerous shell commands that could cause system damage (recursive deletes, disk formatting, etc).
