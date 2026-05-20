---
parameters:
  path: { type: string, required: true }
  start: { type: number, required: false }
  end: { type: number, required: false }
script: |
  def run(args):
      path = args["path"]
      start = args.get("start", 0)
      end = args.get("end", 0)
      if start > 0 and end > 0:
          return fs.read_lines(path, start, end)
      return fs.read(path)
---

# read_lines

Read specific lines from a file. If start/end are provided, returns only those lines (1-indexed). Otherwise reads the entire file.
