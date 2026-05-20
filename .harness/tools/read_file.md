---
parameters:
  path: { type: string, required: true }
script: |
  def run(args):
      return fs.read(args["path"])
---

# read_file

Read a file from the workspace and return its full contents. The path must be relative to the current working directory.
