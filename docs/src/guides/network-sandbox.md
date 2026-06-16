# Network Sandboxing

> **Audience:** anyone shipping a harness whose tools, hooks, or scripted
> contexts may make outbound HTTP. **Goal:** lock the outbound surface to
> an explicit allowlist so an off-the-rails model cannot reach hosts the
> operator never sanctioned.

The network sandbox is **layer 4** of the
[governance stack](../concepts/governance.md): the layer that doesn't
trust the harness. Every Starlark call that opens a socket — `http.get`,
`http.post`, and any subprocess launched through `exec.run` that
inherits the same enforcement — passes through it before the
TCP connection is established. A reject is a `SandboxError` raised
before the request leaves the process, with the denied hostname in the
error message and `network.policy=denied` on the surrounding span.

This guide covers the shipped behavior on `v0.6.0`:

- The `network` block in `harness.md`
- Default-allow back-compat vs. deny-by-default once you opt in
- How `allowed_domains` matches hostnames
- The `*` literal escape hatch and what it does (and does not) relax
- Diagnosing rejections in development
- Pairing the sandbox with OS-level isolation

For the field-level reference (defaults, types, schema), see
[`harness.md` Frontmatter → `network`](../reference/harness-md.md#network).

---

## 1. The shape of the policy

The sandbox is configured in a single top-level `network` block in
`harness.md`:

```yaml
network:
  allowed_domains:
    - api.github.com
    - "*.example.com"
```

That's the whole surface. There is no separate "enable" flag, no
per-tool override, no priority field. The reason is deliberate:
**network reach is a property of the entire harness, not of an
individual artifact.** A network policy that any artifact could relax
would not be a policy.

The policy is read once at load time, baked into the Starlark runtime's
HTTP client, and re-evaluated on every outbound call. It cannot be
mutated at runtime — not by a tool, not by a hook, not by `meta`.

---

## 2. The two postures

The sandbox has exactly two postures, and the switch between them is
the **presence or absence of entries in `allowed_domains`**.

### A. Default-allow (back-compat)

If `network` is omitted entirely, **or** `allowed_domains` is empty,
scripts may reach any host. This is the pre-5.5 behavior and exists so
that upgrading the binary does not silently break harnesses written
before the sandbox shipped.

```yaml
# harness.md
---
model: { provider: copilot, name: gpt-4o }
# no `network:` block → outbound is unrestricted
---
```

This posture is fine for **L1 / L2 deployments**: prototypes,
single-author repos, dev workstations. Use it knowing it is a
non-policy: the only thing standing between the model and the open
internet is whatever your tools choose to call.

### B. Deny-by-default (the moment you opt in)

The instant `allowed_domains` is non-empty, the policy flips to
**default-deny**. There is no implicit "everything else is fine."

```yaml
network:
  allowed_domains:
    - api.github.com
```

After this change:

- `http.get("https://api.github.com/zen")` succeeds.
- `http.get("https://example.com/")` raises `SandboxError: host
  example.com is not in allowed_domains`.
- `http.get("ftp://files.example.com/")` raises — non-`http(s)` schemes
  are rejected unconditionally, regardless of host.

**This is the recommended posture for L3 (Governed Autonomy) and
above.** If you have written a `tools_policy: allowlist` or a
`tool.pre` hook stack, you almost certainly also want a
`network.allowed_domains`.

---

## 3. How matching works

`allowed_domains` is matched against the **hostname** of the request
URL (not the path, not the query string, not headers).

| Pattern              | Matches                                              | Does **not** match                |
|----------------------|------------------------------------------------------|-----------------------------------|
| `api.github.com`     | `api.github.com`                                     | `gist.github.com`, `github.com`   |
| `*.example.com`      | `api.example.com`, `foo.bar.example.com`             | `example.com` (no leading label)  |
| `example.com`        | `example.com`, `api.example.com`, `*.example.com`    | `notexample.com`                  |
| `*` (literal star)   | any host (host filter disabled — see below)          | non-`http(s)` schemes still reject |

A bare hostname (`example.com`) matches the host **and its
sub-domains**. A leading-`*` wildcard (`*.example.com`) matches
sub-domains but **not** the apex. If you want both, list the apex
explicitly or use the bare form.

The match is case-insensitive and does not consider port. There is no
support for path-prefix matching, IP ranges, or CIDR blocks today —
those have come up in design discussion and are tracked as roadmap
items, not shipped behavior.

### The `"*"` escape hatch

Listing the literal entry `"*"` disables hostname filtering while
keeping the rest of the sandbox active:

```yaml
network:
  allowed_domains:
    - "*"
```

This still rejects non-`http(s)` schemes (no `ftp://`, no `file://`,
no `gopher://`). It is the right choice when you genuinely cannot
enumerate hosts up front — for example, a research agent that must
fetch arbitrary URLs from the open web — but you still want
scheme-level discipline and the `network.policy` span attribute for
observability.

Use it sparingly. `*` is **not** the same as omitting the block: an
explicit `*` is an opt-in to "any HTTP host," which is a very
different posture from "we never thought about it."

---

## 4. Wiring it for the governed-agent example

The repository's flagship [governed-agent example](../examples/governed-agent.md)
demonstrates the sandbox with a real `web_fetch` tool. Two surfaces
converge:

1. **`harness.md` declares the policy** in the `network` block.
2. **The `harness` CLI accepts an `--allowed-domain` flag** (repeatable)
   that *adds* to whatever the file specifies. This is convenient for
   per-environment overrides — e.g., a smoke test that needs to reach a
   staging host.

```bash
# Use what's in harness.md
harness run "fetch https://api.github.com/zen"

# Override / extend at the CLI
harness run \
  --allowed-domain api.github.com \
  --allowed-domain '*.example.com' \
  "fetch https://api.example.com/health"
```

The CLI flag does **not** invert the posture. If `harness.md` has an
empty `allowed_domains`, passing `--allowed-domain api.github.com`
flips you into deny-by-default with that single host allowed — same as
adding it to the file.

---

## 5. Diagnosing rejections

When a request is denied, Starlark raises an error of the shape:

```
SandboxError: host gist.github.com is not in allowed_domains
```

The denied hostname is part of the message verbatim, which is the
quickest way to spot a missing entry during development. Three things
to know:

- **Failures don't crash the turn.** Tool authors should structure their
  Starlark to return `{"error": ...}` on caller-visible failures rather
  than letting the `SandboxError` propagate. The
  [Starlark built-ins reference](../reference/starlark-builtins.md#http)
  shows the recommended `try`-style flow.
- **Spans carry `network.policy`.** Every outbound attempt records
  `network.policy = allowed | denied` on the surrounding `tool.exec`
  span, alongside `network.host`. When you wire OTel
  ([Observability with OpenTelemetry](./observability.md)), this is
  the cleanest signal that the sandbox is doing work — and the cleanest
  alert source for a sustained spike of denials.
- **DNS, TLS, and timeouts are separate.** A `SandboxError` is the
  policy layer rejecting the request *before* the socket opens. DNS
  failures, TLS errors, and 30-second default timeouts surface as
  different Starlark errors — don't conflate them.

---

## 6. Pair it with OS-level isolation

The sandbox is **defense in depth**, not a substitute for OS
boundaries. Even with `allowed_domains` set, an L3+ deployment should
still:

- Run the harness as a **non-privileged user** (no `root`, no
  `Administrators`).
- Mount the artifact tree **read-only** from the supervisor's
  perspective.
- Use a **systemd network namespace** (`PrivateNetwork=` is too strict
  for most agents; `RestrictAddressFamilies=AF_INET AF_INET6` is the
  usual middle ground) or a **non-privileged container**.
- Pair the sandbox with a `command_guard` hook for `exec.run` and a
  `path_guard` hook for `fs.write`. Network policy is one risk axis;
  it is not the only one.

The reference [`deploy/systemd/harness.service`](https://github.com/htekdev/ai-harness/tree/main/deploy/systemd/harness.service)
unit and the [`deploy/docker/`](https://github.com/htekdev/ai-harness/tree/main/deploy/docker)
recipes show what these layers look like wired together.

---

## 7. Migration notes

If you are adopting the sandbox on an existing harness:

1. **Run with `network.allowed_domains: ["*"]` first.** This switches
   you into the "explicit posture" world without breaking any tool that
   was reaching arbitrary hosts. Every outbound call now records
   `network.policy=allowed`, which gives you a clean audit log.
2. **Watch the `network.host` attribute over a few representative
   runs.** Build the real allowlist from what your harness actually
   touches, not from what you think it touches. Models are very good
   at finding hosts you didn't predict.
3. **Replace `"*"` with the enumerated list.** Any host that was
   previously implicit now becomes a deliberate, reviewed entry in
   `harness.md` — exactly the property [Harness as
   Code](../concepts/harness-as-code.md) is built around.

A future `harness audit network` subcommand to summarize observed
hostnames over a span of turns is on the roadmap. Until it ships, the
OTel-driven workflow above is the recommended path.

---

## 8. What's intentionally **not** here

A few capabilities that often come up but **are not** part of the
shipped sandbox in `v0.6.0`:

- **Per-artifact policies.** The sandbox is harness-wide; an individual
  tool cannot opt itself into a wider policy. This is by design — see
  §1.
- **Path / query / header filtering.** Only the hostname is matched.
  If you need URL-shape policy, layer a `tool.pre` hook on the
  affected tool.
- **IP / CIDR matching.** `allowed_domains` is hostname-based;
  resolved IPs are not consulted.
- **Outbound proxy enforcement.** The sandbox does not currently force
  traffic through an HTTP proxy. If your environment requires one, set
  `HTTPS_PROXY` at the OS level and let the Go HTTP client pick it up.
- **Inbound restrictions.** This sandbox is purely outbound.
  `harness serve` listeners (e.g., the Telegram input source) are
  governed by the `serve` block and the supervisor, not by `network`.

If any of these are a hard requirement for your deployment, file an
issue against [`htekdev/ai-harness`](https://github.com/htekdev/ai-harness/issues)
with the use case — the artifact model has room for them, but they
need a deliberate design pass rather than implicit behavior.

---

## See also

- [Governance & Policy](../concepts/governance.md) — where the network
  sandbox sits in the four-layer governance stack.
- [`harness.md` Frontmatter → `network`](../reference/harness-md.md#network)
  — field-level schema and defaults.
- [Starlark Built-ins → `http`](../reference/starlark-builtins.md#http)
  — the call surface that the sandbox enforces.
- [Production Deployment](./deployment.md) — supervisor, secrets, and
  OS-level isolation that complement the sandbox.
- [Observability with OpenTelemetry](./observability.md) — wiring
  `network.policy` and `network.host` into your traces.
