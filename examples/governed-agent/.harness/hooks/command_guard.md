---
event: tool.pre
priority: 10
when: payload["name"] == "run_command"
script: |
  def handle(event, payload):
      cmd = payload.get("args", {}).get("command", "")
      dangerous = [
          "rm -rf /",
          "rm -rf /*",
          ":(){ :|:& };:",
          "mkfs",
          "dd if=",
          "shutdown",
          "reboot",
          "> /dev/sda",
          "chmod -R 000 /",
      ]
      for d in dangerous:
          if d in cmd:
              metrics.incr("audit.policy.deny")
              return block("dangerous command pattern blocked: '" + d + "'")
      return allow()
---

# command_guard

Hard-blocks well-known destructive shell patterns. This is intentionally a
list of literal substrings — the goal is "make obvious damage hard", not
"sandbox an adversary". For real isolation pair this with the systemd unit in
`deploy/systemd/harness.service` (read-only root, dropped capabilities).
