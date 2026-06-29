# Distributing a Plugin via GitHub

Ship your plugin as a standalone repository, then consume it from
`harness.md` using `artifact_sources`.

## Producer layout

In your plugin repository, place typed artifacts under a harness-style tree:

```text
plugins/
  production.md
```

Example `plugins/production.md`:

```markdown
---
name: production-baseline
type: plugin
version: 1.0.0
tools:
  - name: security_scan
    description: Security checks
    parameters: {}
    script: |
      def run(args):
          return "ok"
---
```

Tag releases (`v1.0.0`, `v1.1.0`, ...).

## Consumer config

```yaml
trusted_sources:
  - https://github.com/htekdev/harness-plugin-production

artifact_sources:
  - type: git
    url: https://github.com/htekdev/harness-plugin-production
    ref: v1.0.0
    path: plugins
```

Then fetch once:

```bash
harness pull
```

The harness caches sources under `~/.cache/ai-harness/sources/` and loads the
referenced artifacts during config resolution.

## Security model

External plugins can include Starlark hooks/tools and run with the same
privileges as local artifacts. Treat them as executable third-party code.

Recommendations:

- pin `ref` to immutable tags or commit SHAs
- optionally add `checksum: sha256:...` for reproducibility
- keep `trusted_sources` short and explicit
- use `HARNESS_OFFLINE=1` when you must guarantee cache-only operation
