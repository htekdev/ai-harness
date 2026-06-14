# Writing a Tool

> A hands-on tutorial. By the end of this guide you'll have written,
> validated, run, and governed your own tool — a `word_count` tool that
> reads a file, counts words, and gates dangerous paths through a hook.

This guide assumes you finished the [Quickstart](../getting-started.md)
and have `harness` on your `PATH`. If not, do that first — it gets you
to a working binary and a provider token in five minutes.

We'll build a small but real tool that exercises every part of the
artifact contract:

- typed parameters with validation,
- a Starlark `run(args)` that uses filesystem and string built-ins,
- a structured return value,
- a `tool.pre` hook that vetoes calls to sensitive paths,
- and a `tool.post` hook that logs every invocation for audit.

When you're done, you'll know how to write any tool the harness needs.

## 1. Set up a workspace

Create an empty directory and scaffold a harness inside it:

```bash
mkdir -p my-agent && cd my-agent
harness init .
```

You should see a populated tree:

```
my-agent/
├── harness.md
└── .harness/
    ├── tools/
    │   ├── read_file.md
    │   ├── write_file.md
    │   ├── list_files.md
    │   └── get_current_folder.md
    └── hooks/
        ├── block_dangerous_commands.md
        └── detect_secrets.md
```

The four scaffolded tools are good reference reading — every tool you
write follows the same shape.

## 2. Write the tool

Create `.harness/tools/word_count.md`:

```markdown
---
parameters:
  path:
    type: string
    required: true
    description: "Path to a text file to count words in"
  ignore_blank_lines:
    type: boolean
    required: false
    description: "Skip empty lines in the count (default false)"
timeout_ms: 5000
script: |
  def run(args):
      path = args.get("path", "")
      ignore_blank = args.get("ignore_blank_lines", False)

      if not path:
          return {"error": "path is required"}
      if not fs.exists(path):
          return {"error": "file not found: " + path}

      content = fs.read(path)
      lines = content.split("\n")

      word_total = 0
      line_total = 0
      for line in lines:
          if ignore_blank and line.strip() == "":
              continue
          line_total += 1
          word_total += len(line.split())

      return {
          "success": True,
          "path": path,
          "lines": line_total,
          "words": word_total,
          "bytes": len(content),
      }
---

# word_count

Count the lines, words, and bytes in a text file. Use this when the user
asks how long a document is, how many words they wrote, or wants a rough
size estimate before processing a file.

When `ignore_blank_lines` is true, empty lines are skipped from both the
line count and the word count. Defaults to false to match `wc -l`
behavior.
```

Three things worth noticing about that file:

- **The frontmatter is the contract.** Everything between the `---`
  delimiters is YAML the harness parses. The body after the closing
  `---` is markdown the *model* reads as part of its system prompt —
  use it to explain *when* to reach for the tool, not how it works
  internally.
- **`script` is a YAML literal block, not a fenced code block.** The
  `|` after `script:` and the indentation are what make it Starlark.
  Fenced ``` ```starlark ``` ``` blocks in the body are *not* extracted
  — they're just docs.
- **The function is named `run(args)`.** Always. The harness will not
  find any other entry point.

## 3. Validate before running

The validator catches schema typos, parameter shape errors, and Starlark
compile errors offline — no model calls, no token spend.

```bash
harness validate
```

Expected output:

```
✅ harness.md valid
   5 tools, 2 hooks, 0 agents (3 ms)
```

If you mistyped a parameter name or forgot to define `run`, you'll get a
specific error pointing at the file and line. Fix and re-run until
green.

You can also list what the harness now knows about:

```bash
harness tools
```

`word_count` should appear with its description and parameter schema.

## 4. Run one turn against a model

Point the agent at the tool with a natural-language prompt:

```bash
harness run "How many words are in README.md?"
```

You should see the model call `word_count` with `path: "README.md"`,
get a structured result back, and report the count to you in plain
English.

If you want to watch every tool call as it happens, add `--stream`:

```bash
harness run --stream "How many words are in README.md?"
```

The streaming output makes the lifecycle visible: parameter coercion,
the call, the structured return, the model's interpretation. This is
the same trace a `tool.pre` / `tool.post` hook sees.

## 5. Add a `tool.pre` hook to gate sensitive paths

The tool happily counts words in `/etc/shadow` if the agent asks. We
don't want that. Add `.harness/hooks/word_count_path_guard.md`:

```markdown
---
event: tool.pre
priority: 10
script: |
  def handle(event, payload):
      if payload.get("name") != "word_count":
          return {"action": "allow"}
      path = payload.get("args", {}).get("path", "")
      forbidden = ["/etc/", "/root/", "/var/lib/"]
      for prefix in forbidden:
          if path.startswith(prefix):
              return {
                  "action": "block",
                  "reason": "path " + path + " is in a protected directory",
              }
      return {"action": "allow"}
---

# word_count path guard

Prevents `word_count` from reading files under system-sensitive
directories. Blocks at the `tool.pre` boundary so the call never
reaches the Starlark sandbox.
```

Two things to notice:

- **The hook narrows by tool name first.** A `tool.pre` hook sees
  *every* tool call. Returning `{"action": "allow"}` early when the
  call isn't for `word_count` keeps the hook cheap and scoped.
- **The verdict is `allow / block / modify`.** That's the same ternary
  every hook in the harness uses. `block` short-circuits the call
  with the supplied reason; the model sees a structured error in its
  next turn.

Run `harness validate` again to confirm the hook compiles, then ask
the agent something it should refuse:

```bash
harness run "Count the words in /etc/passwd."
```

The model will receive a blocked tool result with the reason from your
hook and explain to the user that the path is protected. The Starlark
`run` function never executed.

## 6. Add a `tool.post` hook for audit

Even allowed calls should leave a trail. Add
`.harness/hooks/word_count_audit.md`:

```markdown
---
event: tool.post
priority: 50
script: |
  def handle(event, payload):
      if payload.get("name") != "word_count":
          return {"action": "allow"}
      result = payload.get("result", {})
      log.info(
          "word_count.audit",
          path=payload.get("args", {}).get("path", ""),
          words=result.get("words", 0),
          lines=result.get("lines", 0),
          success=result.get("success", False),
      )
      return {"action": "allow"}
---

# word_count audit log

Emits a structured log line after every successful or failed
`word_count` call. Pair with the OpenTelemetry exporter to ship audit
events to Jaeger or any OTel collector.
```

Run a counted call:

```bash
harness run --stream "How many words in CHANGELOG.md?"
```

You should see a `word_count.audit path=CHANGELOG.md words=… lines=…
success=true` line in the logs. The hook fires *after* the tool
returns, sees the actual result, and emits a structured event the
observability stack can index. No change to the tool itself was
required.

## 7. Iterate

A tool is done when:

1. **Validate passes.** No schema or compile errors.
2. **Happy path returns structured data**, not a string. Always return a
   dict with named fields — that's what makes downstream tools and
   hooks composable.
3. **Errors return `{"error": "..."}`.** Don't `return None` or raise.
   The model handles a structured error gracefully; an empty return
   confuses it.
4. **Hooks govern it.** A real production tool has at least a `tool.pre`
   guard for the inputs you don't want and a `tool.post` audit for the
   ones you do.
5. **The body explains when to use it.** That markdown is part of the
   system prompt the model sees. Treat it as the tool's user manual.

## What you've learned

You've now built every layer of a governed tool:

- **Artifact format** — frontmatter for the contract, YAML literal
  `script:` for the Starlark, body for model-visible documentation.
- **Starlark built-ins** — `fs.read`, `fs.exists`, string operations,
  structured returns.
- **`tool.pre` hook** — vetoing input before the sandbox runs.
- **`tool.post` hook** — auditing output without changing the tool.
- **The validate → run loop** — fast offline iteration before any
  token spend.

That's the whole tool authoring surface. Every more advanced tool —
network calls, exec wrappers, sub-agent delegation — composes the same
five pieces.

## What to read next

- **[Writing a Hook](./writing-a-hook.md)** — the symmetric tutorial
  for hooks, going deeper on `allow / block / modify` and event
  payloads.
- **[Tools (concept)](../concepts/tools.md)** — the design rationale
  for the artifact format and the tool lifecycle.
- **[Governance & Policy](../concepts/governance.md)** — how the four
  governance layers compose around the tools you write.
- **Reference:** the [Tool Artifact Schema](../reference/tool-artifact.md)
  documents every supported frontmatter field, including the ones not
  used in this tutorial (per-tool retry, custom timeouts, tags).
