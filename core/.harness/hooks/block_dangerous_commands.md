---
event: tool.pre
priority: 100
when: "tool.name == 'exec' and ('rm -rf' in tool.args.command or 'dd if=' in tool.args.command or 'mkfs' in tool.args.command)"
script: |
  def run(event, payload):
      command = payload.get("tool", {}).get("args", {}).get("command", "")
      log("BLOCKED: Dangerous command detected: " + command)
      return {
          "block": True,
          "reason": "Command contains potentially dangerous operations (rm -rf, dd, mkfs). Please review before executing."
      }
---

Safety hook that blocks potentially dangerous shell commands before execution. Prevents destructive operations like recursive deletion, disk wiping, and filesystem formatting.
