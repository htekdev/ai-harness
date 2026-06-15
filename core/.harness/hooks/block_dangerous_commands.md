---
event: tool.pre
priority: 10
when: 'payload["name"] == "write_file"'
script: |
  def handle(event, payload):
      args = payload.get("arguments", {})
      path = args.get("path", "")
      protected = ["/etc/", "/root/", "/var/lib/", "/sys/", "/proc/", "/boot/"]
      for prefix in protected:
          if path.startswith(prefix):
              log("BLOCKED write_file to protected path: " + path)
              return {
                  "action": "block",
                  "reason": "path " + path + " is in a protected system directory; refusing write",
              }
      return {"action": "allow"}
---

# block_dangerous_commands

A `tool.pre` safety hook that vetoes `write_file` calls targeting protected
system directories (`/etc/`, `/root/`, `/var/lib/`, `/sys/`, `/proc/`,
`/boot/`). The agent never gets to mutate the host filesystem in these
locations — the harness short-circuits the call before the tool runs and
returns the `reason` to the model so it can explain the refusal.

## Customising this hook

Adapt the `protected` list for your environment, or extend the `when:`
clause to also fire on other tools (e.g. an `exec` tool that wraps shell
commands):

```
when: 'payload["name"] in ("write_file", "exec")'
```

When you add an `exec`-style tool, also inspect
`payload["arguments"]["command"]` for substrings like `rm -rf`, `dd if=`,
or `mkfs` and return a `block` action.

## Hook contract reminders

- Function name is **`handle(event, payload)`** — not `run`.
- For `tool.pre`, `payload` is **flat**: `{"id", "name", "arguments"}`.
  There is no `payload["tool"]` wrapper.
- Returns must be one of `{"action": "allow"}`, `{"action": "block",
  "reason": "..."}`, or `{"action": "modify", "payload": {...}}`. Any
  other shape (e.g. `{"block": true}`) is silently treated as **allow**.

See [`docs/src/guides/writing-a-hook.md`](../../../docs/src/guides/writing-a-hook.md)
for the full tutorial.
