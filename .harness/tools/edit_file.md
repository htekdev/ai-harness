---
parameters:
  path: { type: string, required: true }
  old: { type: string, required: true }
  new: { type: string, required: true }
script: |
  def run(args):
      fs.replace(args["path"], args["old"], args["new"])
      return "replaced in: " + args["path"]
---

# edit_file

Make a surgical find-and-replace edit in a file. The `old` string must match exactly one occurrence in the file.
