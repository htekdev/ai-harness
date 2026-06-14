---
parameters:
  command: { type: string, required: true }
  timeout_ms: { type: number, required: false }
script: |
  def run(args):
      command = args.get("command", "")
      timeout = args.get("timeout_ms", 15000)
      if not command:
          return {"error": "command is required"}
      # Named wrapper around exec.run so the prefer_named_tools hook can
      # distinguish "agent asked for the named tool" (allowed) from "agent
      # tried to call raw exec" (blocked).
      result = exec.run("bash", ["-lc", command], timeout)
      return {
          "stdout": string.truncate(result.get("stdout", ""), 4000),
          "stderr": string.truncate(result.get("stderr", ""), 2000),
          "exit_code": result.get("exit_code", 0),
      }
---

# run_command

Run a shell command through a named wrapper. The `command_guard` hook blocks
known-dangerous patterns (`rm -rf /`, `mkfs`, `dd if=`, etc.) before the
command ever reaches the OS.
