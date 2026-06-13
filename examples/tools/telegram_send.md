---
type: tool
name: telegram_send
description: |
  Send a Telegram message to a chat via the Bot API. Reads TELEGRAM_BOT_TOKEN
  from the environment. Use this from any agent to notify Hector or any other
  allowlisted chat. Pair with `harness serve --source telegram` for the full
  bidirectional loop where replies are injected back into the session.
parameters:
  chat_id:
    type: string
    required: true
    description: Telegram chat ID to send to.
  text:
    type: string
    required: true
    description: Message text.
  speak:
    type: string
    required: false
    description: |
      Optional short TTS string for Tasker integration. When provided,
      `SPEAK: <speak>` is prepended so the notification preview reads aloud.
script: |
  def run(args):
      token = env("TELEGRAM_BOT_TOKEN")
      if not token:
          return {"error": "TELEGRAM_BOT_TOKEN not set"}
      text = args["text"]
      speak = args.get("speak", "")
      if speak:
          text = "SPEAK: " + speak + "\n\n" + text
      url = "https://api.telegram.org/bot" + token + "/sendMessage"
      resp = http.post(url, {"chat_id": args["chat_id"], "text": text})
      return {"ok": True, "response": resp}
---

# telegram_send

Outbound Telegram tool. Drop this in `.harness/tools/` and any session can send
Telegram messages governed by your existing `tool.pre` / `tool.post` hooks.

## Bidirectional loop

This tool handles the **outbound** half. For inbound (Telegram replies injected
as user turns), run:

```
TELEGRAM_BOT_TOKEN=xxx harness serve --source stdin --source telegram --telegram-chat 7729308746
```

`harness serve` long-polls `getUpdates`, filters by the allowlist, and routes
each message into a per-`chat_id` session. Responses go back via the same
`sendMessage` endpoint that this tool uses, so the conversation completes the
round-trip without any additional plumbing.

## Configuration

Required env: `TELEGRAM_BOT_TOKEN`

Hooks that may want to govern this tool:

- `tool.pre` on `telegram_send` — validate `chat_id` against an allowlist before
  send (defense in depth — `harness serve` already enforces an inbound allowlist).
- `tool.post` on `telegram_send` — observability / audit log.

## Reference

- Spec: `data/specs/ai-harness-telegram-integration-v1.md` (rocha-family repo)
- Issue: htekdev/ai-harness#73
