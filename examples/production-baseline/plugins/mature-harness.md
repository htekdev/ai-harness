---
name: mature-harness-production-baseline
type: plugin
version: "0.1.0"
description: "Showcase plugin: production-ready governance, memory tiers, and context discipline"
author: "AI Harness"
tags: ["showcase", "production", "governance", "memory", "delegation"]
condition: 'not ctx.has("mode") or ctx.get("mode") in ["production", "governed"]'
tools:
  - name: safe_content_write
    description: "Safely write file content with guardrails against oversized writes and /tmp paths"
    parameters:
      path:
        type: string
        required: true
        description: "Relative path inside the harness workspace"
      content:
        type: string
        required: true
        description: "File content to write"
      max_bytes:
        type: number
        required: false
        description: "Maximum allowed bytes for content (default 16384)"
    script: |
      def run(args):
          path = args.get("path", "")
          content = args.get("content", "")
          max_bytes = int(args.get("max_bytes", 16384))

          if not path:
              return {"error": "path is required"}
          if path == "/tmp" or path.startswith("/tmp/") or path == "tmp" or path.startswith("tmp/"):
              return {"error": "refusing writes to /tmp in production baseline profile"}
          if len(content) > max_bytes:
              return {"error": "content exceeds max_bytes=" + str(max_bytes)}

          fs.write(path, content)
          emit("custom.safe_write", {"path": path, "bytes": len(content)})
          return {"success": True, "path": path, "bytes": len(content)}

  - name: emit_structured_event
    description: "Emit a structured event and append it to the in-memory events log tier"
    parameters:
      event_name:
        type: string
        required: true
        description: "Event name to emit"
      payload:
        type: object
        required: false
        description: "Optional structured payload"
    script: |
      def run(args):
          event_name = args.get("event_name", "")
          payload = args.get("payload", {})
          if not event_name:
              return {"error": "event_name is required"}

          record = {
              "event": event_name,
              "timestamp": time.now(),
              "payload": payload,
          }
          emit(event_name, payload)

          events = []
          if cache.has("memory.events"):
              events = json.decode(cache.get("memory.events"))
          events.append(record)
          cache.set("memory.events", json.encode(events))

          log("structured_event " + event_name)
          return {"success": True, "record": record}

  - name: memory_tier_put
    description: "Write key/value data into a memory tier (core, working, long_term, events)"
    parameters:
      tier:
        type: string
        required: true
        description: "Memory tier: core | working | long_term | events"
      key:
        type: string
        required: true
        description: "Key to set within the memory tier"
      value:
        type: object
        required: true
        description: "Value to store"
    script: |
      def run(args):
          tier = args.get("tier", "")
          key = args.get("key", "")
          value = args.get("value", None)
          allowed = ["core", "working", "long_term", "events"]

          if tier not in allowed:
              return {"error": "tier must be one of core|working|long_term|events"}
          if not key:
              return {"error": "key is required"}

          cache_key = "memory." + tier
          data = {}
          if cache.has(cache_key):
              data = json.decode(cache.get(cache_key))
          data[key] = value
          cache.set(cache_key, json.encode(data))

          return {"success": True, "tier": tier, "key": key}

hooks:
  - event: tool.pre
    handler: prefer_named_tool_policy
    priority: 5
    script: |
      def handle(event, payload):
          name = payload.get("name", "")
          if name == "exec":
              return block("raw 'exec' blocked by production baseline policy; use a named wrapper tool")
          return allow()

  - event: tool.pre
    handler: file_write_guard
    priority: 8
    script: |
      def handle(event, payload):
          name = payload.get("name", "")
          args = payload.get("args", {})

          if name == "safe_content_write":
              path = args.get("path", "")
              content = args.get("content", "")
              if path == "/tmp" or path.startswith("/tmp/") or path == "tmp" or path.startswith("tmp/"):
                  return block("refusing writes to /tmp in production baseline profile")
              if len(content) > 16384:
                  return block("content write exceeds production baseline max_bytes=16384")

          if name in ["exec", "bash", "shell"]:
              command = args.get("command", args.get("cmd", ""))
              if "> /tmp" in command or ">/tmp" in command or "tee /tmp" in command:
                  return block("refusing command-based write to /tmp in production baseline profile")
              if "cat <<" in command and ">" in command and len(command) > 4096:
                  return block("large heredoc-to-file command blocked; prefer safe_content_write or edit-style operations")

          return allow()

  - event: delegation.pre
    handler: delegation_depth_and_allowlist_guard
    priority: 10
    script: |
      def handle(event, payload):
          depth = int(payload.get("depth", 1))
          agent = payload.get("agent", payload.get("agent_name", ""))
          allowlist = ["explore", "task", "research", "code-review", "general-purpose"]

          if depth > 3:
              return block("delegation depth exceeds production baseline max_depth=3")
          if agent and agent not in allowlist:
              return block("delegation target not allowlisted: " + agent)
          return allow()

  - event: completion.pre
    handler: completion_message_window_guard
    priority: 30
    script: |
      def handle(event, payload):
          messages = payload.get("messages", [])
          if len(messages) > 200:
              return block("completion message window exceeds production baseline max=200")
          return allow()

  - event: turn.start
    handler: hydrate_context_memory_tiers
    priority: 20
    script: |
      def handle(event, payload):
          if cache.has("memory.core"):
              ctx.set("memory_core", json.decode(cache.get("memory.core")))
          else:
              ctx.set("memory_core", {})

          if cache.has("memory.working"):
              ctx.set("memory_working", json.decode(cache.get("memory.working")))
          else:
              ctx.set("memory_working", {})

          return allow()

  - event: turn.end
    handler: persist_events_and_working_memory
    priority: 80
    script: |
      def handle(event, payload):
          text = payload.get("text", "")
          usage = payload.get("usage", {})
          snapshot = {
              "timestamp": time.now(),
              "text": text[:600],
              "usage": usage,
          }

          # events.log append (JSONL)
          fs.mkdir(".harness/memory")
          fs.append(".harness/memory/events.log", json.encode(snapshot) + "\n")

          # working-memory update
          working = {}
          if cache.has("memory.working"):
              working = json.decode(cache.get("memory.working"))
          working["last_turn_summary"] = snapshot
          cache.set("memory.working", json.encode(working))

          return allow()
---

# Mature Harness Production Baseline (Showcase Plugin)

This showcase plugin is a production-grade baseline that demonstrates:

- Governance-first operation (`tool.pre`, `delegation.pre`, `completion.pre`)
- Context discipline (`turn.start` hydration + `turn.end` persistence)
- Safe content-write patterns over raw shell heredocs
- Memory tiering (`core`, `working`, `long_term`, `events`)

## Condensed Constitution

1. **Use named, governed tools first.** Raw execution is a last resort and can
   be blocked by policy.
2. **Prefer explicit, bounded actions.** Keep delegation depth and completion
   windows constrained.
3. **Treat memory as a first-class system.** Core identity, working state,
   long-term facts, and event traces are separate tiers.
4. **No assumption-by-default.** Surface uncertainty and ask for clarification
   when intent or constraints are ambiguous.
5. **Communicate operationally.** Keep decisions observable via structured
   event emission and turn-end logging.

## Builtins and events used in this profile

- Builtins: `fs.*`, `cache.*`, `ctx.*`, `json.*`, `time.now()`, `emit()`, `log()`
- Hook events: `tool.pre`, `delegation.pre`, `completion.pre`, `turn.start`, `turn.end`
