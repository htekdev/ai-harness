# Bidirectional Telegram Integration (`harness serve`)

> A hands-on tutorial. By the end of this guide you'll have a running
> `harness serve` process that accepts messages from Telegram, routes
> each one through your harness session, and sends the model's reply
> back — all without anyone sitting at a terminal.

This guide assumes you've finished the [Quickstart](../getting-started.md)
and have a working `harness.md` + `.harness/` layout. Everything here builds
on top of that foundation.

## Background

`harness run` is a stdin REPL — one user, one session, one terminal. When you
need the harness to run continuously and respond to messages arriving from
external sources (Telegram, peer agents, etc.), use `harness serve` instead.

`harness serve` replaces the blocking `scanner.Scan()` loop with a
multi-source select loop over registered **input sources**. Each source is a
Go `input.Source` that produces `Event` values. The serve loop routes each
event to a per-`SessionKey` worker goroutine, calls `Harness.Run`, and
optionally routes the response back via the same source (if it implements
`input.Replier`).

```
Telegram ──▶ TelegramSource ──▶┐
                                ├─▶ serve loop ──▶ Harness.Run ──▶ TelegramSource.Reply ──▶ Telegram
stdin    ──▶ StdinSource    ──▶┘
```

---

## Prerequisites

- A Telegram bot token. Create one with [@BotFather](https://t.me/BotFather);
  copy the `123456:ABC...` token.
- Your own Telegram chat ID. Send `/start` to your bot, then call
  `https://api.telegram.org/bot<TOKEN>/getUpdates` to find `message.chat.id`.
- `OPENAI_API_KEY` (or your provider's env var) — same as for `harness run`.

---

## 1. Add the `telegram_send` tool

The serve loop handles **inbound** routing automatically. For **outbound**
Telegram messages triggered by tool calls (e.g. notifications, proactive
updates), add the `telegram_send` tool to your project.

Copy `examples/tools/telegram_send.md` from this repo into your project's
`.harness/tools/` directory:

```bash
cp examples/tools/telegram_send.md .harness/tools/telegram_send.md
```

The tool script calls `http.post` with your `TELEGRAM_BOT_TOKEN` to
`api.telegram.org/bot{token}/sendMessage`. No Go code needed — it is a
pure Starlark tool artifact.

> **Network sandbox note:** `api.telegram.org` must be in your harness's
> `network.allowed_domains` list. Add it to `harness.md`:
>
> ```yaml
> network:
>   allowed_domains:
>     - api.telegram.org
> ```

---

## 2. Configure `serve:` in `harness.md`

Add a `serve:` block to your `harness.md` frontmatter. Secrets are **never**
embedded — each source references an environment variable by name.

```yaml
---
model:
  provider: openai
  name: gpt-4o

network:
  allowed_domains:
    - api.telegram.org

serve:
  sources:
    - type: stdin                   # keep the REPL available alongside Telegram

    - type: telegram
      token_env: TELEGRAM_BOT_TOKEN # env var holding the bot token
      chat_allowlist: [7729308746]  # replace with your real chat ID(s)
      poll_timeout_seconds: 25      # long-poll window; max 50
      offset_path: ./.harness/state/telegram-offset.json  # optional: survive restarts
---

You are a helpful assistant.
```

`chat_allowlist` is required and must be non-empty — there is no wildcard
mode in v1. This prevents your bot from responding to arbitrary users.

---

## 3. Start `harness serve`

```bash
export TELEGRAM_BOT_TOKEN=123456:ABC...
export OPENAI_API_KEY=sk-...

harness serve --config harness.md
```

You should see:

```
🤖 AI Harness — Serve Mode
   config:  harness.md
   sources: stdin, telegram
   (Ctrl-C to stop)
---
```

Send a message to your bot in Telegram. The serve process will:

1. Long-poll `getUpdates` (every 25 s window).
2. Receive your message, check the `chat_id` against the allowlist.
3. Route the message to a per-`chat_id` session worker.
4. Call `Harness.Run` with the message text.
5. Reply to your Telegram chat with the model's response.

---

## 4. CLI flags vs. `serve:` config block

The `serve:` YAML block is the recommended approach for production. For
quick experiments, you can pass everything on the command line instead —
CLI flags take precedence over the config block:

```bash
# CLI-only (no serve: block in harness.md needed)
TELEGRAM_BOT_TOKEN=... harness serve \
  --source stdin \
  --source telegram \
  --telegram-chat 7729308746 \
  --telegram-poll 25
```

Both forms are equivalent; the config block avoids repeating flags on
every invocation.

---

## 5. Multi-chat routing

Each unique `chat_id` gets its own isolated session. Two users messaging your
bot concurrently will each see a coherent conversation history without
interfering with each other.

Turns from the same `chat_id` are **serialized** — the second message waits
for the first `Harness.Run` to finish before starting. Turns from different
`chat_id` values run **concurrently**.

To allow multiple users add all their chat IDs to the allowlist:

```yaml
serve:
  sources:
    - type: telegram
      token_env: TELEGRAM_BOT_TOKEN
      chat_allowlist: [7729308746, 9988776655]
      poll_timeout_seconds: 25
```

---

## 6. Offset persistence (survive restarts)

By default the update offset lives only in memory. If the serve process
restarts, Telegram's server-side buffer (∼24 h) may redeliver old messages.
To avoid that, set `offset_path`:

```yaml
    - type: telegram
      token_env: TELEGRAM_BOT_TOKEN
      chat_allowlist: [7729308746]
      offset_path: ./.harness/state/telegram-offset.json
```

The directory is created automatically. The file stores the last
acknowledged `update_id` as a single JSON number. Deleting the file
resets the offset to 0 (re-processes the Telegram buffer).

---

## 7. `harness serve --source stdin` — drop-in for `harness run`

`harness serve --source stdin` (or a `serve:` block that lists only `stdin`)
is functionally equivalent to `harness run`. The same REPL experience,
same commands (`/tools`, `/hooks`, `/help`, `quit`), same exit behaviour.

```bash
harness serve --source stdin   # identical to: harness run
```

This lets you swap `harness run` for `harness serve` in scripts without
changing any other behaviour — then add more sources later without
touching the script.

---

## 8. Production tips

- **systemd / Docker:** reference recipes are in
  [`deploy/systemd/`](https://github.com/htekdev/ai-harness/tree/main/deploy/systemd)
  and
  [`deploy/docker/`](https://github.com/htekdev/ai-harness/tree/main/deploy/docker).
  Set `TELEGRAM_BOT_TOKEN` as a systemd `EnvironmentFile` or a Docker
  secret — never bake it into the image.
- **Hook governance:** add a `tool.pre` hook on `telegram_send` to
  validate `chat_id` against your own allowlist as defense-in-depth. The
  inbound `chat_allowlist` already gates who can talk to the bot; the
  outbound hook gates what the model can say to whom.
- **Observability:** `harness serve` emits `source.pump` OpenTelemetry
  spans wrapping each `agent.turn` span. See the
  [Observability guide](./observability.md) for the full OTel setup.
- **Error routing:** if `Harness.Run` returns an error, the serve loop
  automatically sends `"error: <message>"` back to the originating chat
  so the user sees something concrete instead of silence.

---

## Reference

| Topic | Where |
|-------|-------|
| `harness serve` CLI flags | [CLI Reference — `serve`](../reference/cli.md#serve) |
| `serve:` frontmatter schema | [harness.md — `serve`](../reference/harness-md.md#serve) |
| Starlark `http.post` / `http.get` | [Starlark Built-ins — `http`](../reference/starlark-builtins.md#http) |
| `telegram_send` tool artifact | [`examples/tools/telegram_send.md`](https://github.com/htekdev/ai-harness/blob/main/examples/tools/telegram_send.md) |
| Network sandbox | [harness.md — `network`](../reference/harness-md.md#network) |
| Production deployment | [Production Deployment](./deployment.md) |
