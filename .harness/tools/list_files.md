---
parameters:
  pattern: { type: string, required: false }
  path: { type: string, required: false }
script: |
  def run(args):
      pattern = args.get("pattern", "*")
      path = args.get("path", ".")
      return fs.glob(pattern)
---

# list_files

List files matching a glob pattern. Defaults to listing all files in the current directory.
