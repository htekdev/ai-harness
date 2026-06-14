# Network sandbox (Phase 5.5)

When a Starlark tool or hook script reaches outbound networks via the
`http.*` built-ins, AI Harness can enforce a **domain allowlist** so that
production deployments fail closed by default.

## Why

Pre-5.5, any script could call `http.get` / `http.post` against any host
the harness process could reach. That is fine for local development, but
risky for unattended `harness serve` deployments — a misbehaving script
(or a prompt-injected one) could exfiltrate data or pivot internally.

Phase 5.5 adds a **default-deny** sandbox keyed off a top-level
`network:` block in your harness config. When the block is present, only
listed domains (and their sub-domains) are reachable; everything else
errors out with a typed `network sandbox` error.

When the block is **omitted**, behaviour is unchanged: scripts may reach
any host, exactly as before. Sandboxing is opt-in per harness.

## Config

```yaml
model:
  name: gpt-4o
  api_key_env: GITHUB_TOKEN

network:
  allowed_domains:
    - api.openai.com
    - "*.api.githubcopilot.com"
    - example.com           # apex + sub-domains (api.example.com etc.)
    - "*.internal"          # sub-domains only — apex "internal" denied
```

### Matching rules

| Entry              | Allows                                    | Denies                       |
|--------------------|-------------------------------------------|------------------------------|
| `example.com`      | `example.com`, `api.example.com`, `a.b.example.com` | `evil.com`, `notexample.com` |
| `*.example.com`    | `api.example.com`, `a.b.example.com`      | `example.com` (apex only)    |
| `*`                | every host                                | non-http(s) schemes still rejected |

Additional guarantees, regardless of allowlist contents:

- Schemes other than `http` / `https` are always rejected when the
  sandbox is active (`file://`, `gopher://`, `ftp://`, …).
- IP literals (`127.0.0.1`, `[::1]`, …) are rejected unless the
  allowlist contains the explicit `*` escape hatch.
- Port suffixes are ignored (`example.com:8443` matches `example.com`).
- Trailing dots are normalized (`example.com.` matches `example.com`).
- Comparison is case-insensitive.

### What scripts see when blocked

```text
http.get: network sandbox: host "evil.com" is not in the allowlist (url=https://evil.com/)
```

The error is surfaced through Starlark like any other built-in failure,
so existing `try` / `except` patterns work unchanged.

## Programmatic access

If you embed the harness as a library, you can attach a sandbox directly
to a `scripting.Engine`:

```go
engine := scripting.NewEngine()
engine.SetNetworkSandbox(scripting.NewNetworkSandbox([]string{
    "api.openai.com",
    "*.api.githubcopilot.com",
}))
```

`engine.NetworkSandbox()` returns the active sandbox (or `nil`), and
`sb.AllowedDomains()` returns the normalized entries — useful for
`harness context` style observability surfaces.

## Recommended defaults

For `harness serve` in production:

```yaml
network:
  allowed_domains:
    - api.openai.com
    - api.githubcopilot.com
    - "*.api.githubcopilot.com"
```

This lets the completion client reach the LLM provider while every other
outbound network call from scripts is denied. Add additional entries
only for the specific hosts your tools genuinely need.
