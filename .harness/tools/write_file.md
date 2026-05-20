---
parameters:
  path: { type: string, required: true }
  content: { type: string, required: true }
script: |
  def run(args):
      fs.write(args["path"], args["content"])
      return "written: " + args["path"]
---

# write_file

Write content to a file. Creates the file and any parent directories if they don't exist. Overwrites existing content.
