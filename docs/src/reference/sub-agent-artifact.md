# Sub-Agent Artifact Schema

A **sub-agent artifact** is a single Markdown file under `.harness/agents/`
that declares a delegate the parent agent can spawn via the built-in
`delegate` tool. This page is the exhaustive reference for the artifact
format: every supported frontmatter field, the inline-vs-reference
semantics for tools and hooks, the loading and registration rules, and
the runtime contract the parent sees.

For the conceptual overview — *what* delegation is and *why* sub-agents
are files — see [Concepts → Delegation](../concepts/delegation.md). For a
step-by-step walkthrough, see the
[Writing a Sub-Agent](../guides/writing-a-sub-agent.md) guide.

> **Versioning note.** Every field documented on this page is part of
> the stable artifact configuration surface under SemVer. New optional
> fields may be added in minor releases without breaking existing files.

## File shape

```markdown
---
model: gpt-4o-mini
description: Researches topics via HTTP and summarizes findings concisely

tools:
  - name: fetch_url
    parameters:
      url: { type: string, required: true }
    script: |
      def run(args):
          return http.get(args["url"], {}, 30)
  - search_text      # ← string reference to a tool in .harness/tools/

hooks:
  - researcher_guard # ← string reference to a hook in .harness/hooks/
---

# Researcher

You are a research agent. Gather information from URLs, extract
relevant data, and summarize findings clearly and concisely.

## Guidelines

- Always cite your sources (include URLs)
- Summarize findings in structured format
```

Rules enforced by the loader (`config.ParseAgentMarkdown` in
`config/markdown.go`):

1. The file **must** start with a `---` delimiter line. Files without
   frontmatter are rejected by the parser.
2. The frontmatter **must** be closed by a second `---` on its own
   line.
3. The **filename is the sub-agent name.** A file at
   `.harness/agents/researcher.md` registers a sub-agent whose `Name`
   is `researcher`. There is no `name:` field in frontmatter — the
   filename is canonical.
4. The Markdown **body** after the closing delimiter is the child's
   **system prompt**. The harness passes it verbatim as the child's
   system message at delegation time. Unlike hooks (where the body is
   prose-only), the body of a sub-agent artifact is *the* model-facing
   contract — treat every line as production prompt.
5. Frontmatter is parsed as YAML. Unknown top-level keys are **ignored
   silently** — typos in field names produce no error. Use
   `harness validate` to confirm the runtime sees the schema you expect.
6. Fenced code blocks inside the body are **not** extracted as tools or
   hooks. Capabilities are declared in the frontmatter `tools:` and
   `hooks:` lists; the body is system prompt only.

## Top-level fields

| Field         | Type                          | Default | Required |
|---------------|-------------------------------|---------|----------|
| `description` | string                        | _empty_ | recommended |
| `model`       | string (provider model ID)    | inherits parent model | no |
| `tools`       | list of `AgentTool` (string or inline) | `[]` | no |
| `hooks`       | list of `AgentHook` (string or inline) | `[]` | no |

### `description`

A short, single-sentence summary the parent's planner sees when it
chooses among delegates. Surfaces in the tool catalog as the
`delegate(agent=<name>)` entry's description.

Recommended even though not validated. An empty description forces the
parent to guess from the agent name alone.

### `model`

Overrides the parent's model for this child only. Any provider/model ID
your harness has a configured provider for is valid (e.g.
`gpt-4o-mini`, `gpt-4o`, `claude-sonnet-4.5`).

When omitted, the child inherits the parent's model. Use this field to
deliberately route cheaper or faster work — e.g. a `researcher` running
on `gpt-4o-mini` while the parent runs on `gpt-4o`.

### `tools`

Each entry is an `AgentTool`, which is **either**:

- a **string reference** — the name of a tool already on disk under
  `.harness/tools/<name>.md` (or registered via a plugin/builtin), e.g.
  `- fetch_url`; or
- an **inline tool definition** — the full
  [tool artifact schema](./tool-artifact.md) `{ name, parameters,
  script, ... }` declared directly in the agent file.

Inline tools are private to the sub-agent — they are not added to the
parent's tool catalog and cannot be referenced from other artifacts.
Use inline tools for capabilities tightly scoped to one delegate; use
string references when the same tool is shared across the parent and
multiple children.

The decision is per-entry: a single `tools:` list can mix inline
definitions and string references freely.

### `hooks`

Each entry is an `AgentHook`, which is **either**:

- a **string reference** — the name of a hook already on disk under
  `.harness/hooks/<name>.md`, e.g. `- researcher_guard`; or
- an **inline hook definition** — the full
  [hook artifact schema](./hook-artifact.md) `{ event, script, when,
  priority }` declared directly in the agent file.

Inline hooks declared here run **only** when this sub-agent is the
active delegate. The parent's global hook chain still runs around the
delegation boundary (`delegation.pre` / `delegation.post` fire from the
parent's perspective regardless of which agent is targeted).

When `hooks:` is omitted or empty, the child still inherits every
`tool.pre` / `tool.post` policy registered on the parent. That is the
default: a sub-agent does not get a smaller harness, it gets the
parent's harness one level deeper.

## Loading and registration

The artifact loader walks `.harness/agents/` (and any additional
artifact roots configured in `harness.md`) and registers every `*.md`
file it finds. There is no manifest, no central registration step,
and no order dependency:

```
.harness/
├── harness.md
├── tools/
│   ├── fetch_url.md
│   └── search_text.md
├── hooks/
│   └── researcher_guard.md
└── agents/
    ├── researcher.md     ← registered as "researcher"
    └── code-reviewer.md  ← registered as "code-reviewer"
```

String references in `tools:` / `hooks:` are resolved **after** all
artifact files are loaded, so the order in which files are discovered
on disk does not matter. A sub-agent can reference a tool defined in
the same directory, in `.harness/tools/`, or in any loaded plugin or
builtin bundle.

`harness validate` lists every registered sub-agent under **agents**
alongside tools and hooks, and reports unresolved references (e.g. a
sub-agent referencing a tool name that no artifact defines).

## Runtime contract

When the parent calls the built-in `delegate` tool with
`{ "agent": "<name>", "task": "..." }`:

1. The runtime resolves `<name>` against the registered sub-agent
   table.
2. It spawns a child runtime at `depth = parent.depth + 1`, subject
   to the per-depth iteration budget (default `[20, 10, 5, 3]`).
3. The child runs with its declared `model` (or the parent's, if
   unset), its inline + referenced tools, the parent's tool catalog
   minus anything the parent's `tool.pre` hooks block at this depth,
   and the parent's hook chain plus this sub-agent's inline hooks.
4. The child's final structured result is returned to the parent's
   `delegate` tool result. The parent never sees the child's
   intermediate tool calls in its own context window.

`delegation.pre` fires after argument validation and before the
child runs; `delegation.post` fires after the child returns and
before the parent sees the result. Both events traverse the parent's
hook chain — see the [Hook Artifact Schema](./hook-artifact.md) for
the payload shapes and decision contract.

## Inline-vs-on-disk equivalence

A sub-agent that uses only string references is exactly equivalent to
an agent block declared inline under `harness.md`'s top-level
`agents:` list. The schema is identical in both surfaces — the only
difference is that on-disk artifacts are discovered by filename and
inline blocks are discovered by their position in `harness.md`.

For most teams, **on-disk artifacts are the preferred surface**:
they version-control cleanly, diff cleanly, and can be reviewed
file-by-file. Inline `agents:` blocks in `harness.md` are useful for
small, single-file demos or when an entire harness fits in one file.

## See also

- [Concepts → Delegation](../concepts/delegation.md)
- [Guides → Writing a Sub-Agent](../guides/writing-a-sub-agent.md)
- [Tool Artifact Schema](./tool-artifact.md)
- [Hook Artifact Schema](./hook-artifact.md)
- [`harness.md` Frontmatter](./harness-md.md)
