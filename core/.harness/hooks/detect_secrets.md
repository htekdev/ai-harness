---
event: tool.pre
priority: 20
when: 'payload["name"] == "write_file"'
script: |
  def handle(event, payload):
      args = payload.get("arguments", {})
      path = args.get("path", "")
      content = args.get("content", "")
      lower = content.lower()

      markers = [
          "begin rsa private key",
          "begin private key",
          "begin openssh private key",
          "aws_secret_access_key",
          "api_key",
          "apikey",
          "password=",
          "secret=",
      ]
      for marker in markers:
          if marker in lower:
              log("BLOCKED write_file: secret-like marker '" + marker +
                  "' detected in content for path " + path)
              return {
                  "action": "block",
                  "reason": "refusing to write apparent secret (" + marker +
                            ") to " + path + "; redact before retrying",
              }
      return {"action": "allow"}
---

# detect_secrets

A `tool.pre` governance hook that scans `write_file` content for common
secret patterns (private keys, API keys, AWS credentials, inline
passwords) and blocks the write before the file is created. The model
sees the block reason and can recover by redacting and retrying.

## Why `tool.pre` instead of `tool.post`?

Once the file is on disk, the secret has already escaped the harness.
Inspecting at `tool.pre` lets the harness refuse the action — keeping the
governance contract architectural rather than after-the-fact.

## Hook contract reminders

- Function name is **`handle(event, payload)`** — not `run`.
- For `tool.pre`, `payload` is **flat**: `{"id", "name", "arguments"}`.
- Returns must be one of `{"action": "allow"}`, `{"action": "block",
  "reason": "..."}`, or `{"action": "modify", "payload": {...}}`. Other
  shapes are silently treated as **allow**.

Tune the `markers` list for your environment (regex-grade patterns can
live in a follow-up hook that uses `re.search`). See
[`docs/src/guides/writing-a-hook.md`](../../../docs/src/guides/writing-a-hook.md)
for the full tutorial.
