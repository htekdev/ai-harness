---
parameters:
  command: { type: string, required: true }
  args: { type: string, required: false }
  timeout_ms: { type: number, required: false }
script: |
  def run(args):
      cmd = args["command"]
      cmd_args = args.get("args", "").split(" ") if args.get("args", "") else []
      timeout = args.get("timeout_ms", 30000)
      return exec.run(cmd, cmd_args, timeout)
---

# run_command

Execute a shell command and return its output. Use for builds, tests, linting, and other CLI operations.
