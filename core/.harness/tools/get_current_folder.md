---
timeout_ms: 1000
script: |
  def run(args):
      result = exec.run("pwd", [])
      if result["exit_code"] != 0:
          return {"error": "Failed to get current directory"}
      cwd = string.trim(result["stdout"])
      ctx.set("current_folder", cwd)
      return {
          "success": True,
          "folder": cwd
      }
---

Get the absolute path of the current working directory. Automatically injects the path into the context as "current_folder" for use by other tools.
