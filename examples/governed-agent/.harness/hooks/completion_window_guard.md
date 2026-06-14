---
event: completion.pre
priority: 50
script: |
  def handle(event, payload):
      messages = payload.get("messages", [])
      if len(messages) > 200:
          metrics.incr("audit.policy.deny")
          return block("conversation history too long for governed-agent profile (max 200 messages)")
      return allow()
---

# completion_window_guard

Caps the conversation window before it goes to the provider. The hard limit
in `context.max_history` already trims older turns; this hook is the
last-line defense against pathological inputs (e.g. a tool returning 5000
messages in one shot).
