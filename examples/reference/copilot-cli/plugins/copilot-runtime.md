---
name: copilot-cli-runtime-mapping
type: plugin
version: "0.1.0"
description: "Reference plugin expressing Copilot CLI runtime patterns as harness artifacts"
author: "AI Harness"
tags: ["reference", "copilot-cli", "tools", "hooks", "delegation", "background"]
condition: 'not ctx.has("runtime") or ctx.get("runtime") == "copilot-cli"'
tools:
  - name: bash
    description: "Named shell tool wrapper (preferred over raw exec)"
    parameters:
      command:
        type: string
        required: true
        description: "Shell command to execute"
      timeout_ms:
        type: number
        required: false
        description: "Optional timeout override"
    script: |
      def run(args):
          command = args.get("command", "")
          timeout = args.get("timeout_ms", 30000)
          if not command:
              return {"error": "command is required"}
          # exec.run returns {"stdout": "...", "stderr": "...", "exit_code": N}
          return exec.run("bash", ["-lc", command], timeout)

  - name: load_context_bundle
    description: "Load runtime context defaults and mode hints"
    parameters:
      mode:
        type: string
        required: false
        description: "Runtime mode (chat, review, planning)"
    script: |
      def run(args):
          mode = args.get("mode", "chat")
          ctx.set("runtime", "copilot-cli")
          ctx.set("mode", mode)
          ctx.set("named_tool_policy", "prefer_named")
          return {
              "success": True,
              "runtime": ctx.get("runtime"),
              "mode": ctx.get("mode"),
              "named_tool_policy": ctx.get("named_tool_policy"),
          }

  - name: delegate_task
    description: "Delegate work to a specialized sub-agent"
    parameters:
      agent_name:
        type: string
        required: true
        description: "Specialized sub-agent name"
      task:
        type: string
        required: true
        description: "Task prompt for the sub-agent"
    script: |
      def run(args):
          agent_name = args.get("agent_name", "")
          task = args.get("task", "")
          if not agent_name or not task:
              return {"error": "agent_name and task are required"}

          depth = ctx.get("delegation_depth", 0)
          emit("delegation.pre", {
              "agent_name": agent_name,
              "prompt": task,
              "depth": depth + 1,
          })

          # Reference behavior: runtime would route to real sub-agent execution.
          result = {
              "agent_name": agent_name,
              "status": "delegated",
              "task": task,
          }

          emit("delegation.post", {
              "agent_name": agent_name,
              "result": result,
              "depth": depth + 1,
          })
          return {"success": True, "result": result}

  - name: background_start
    description: "Start cooperative background work and return a task id"
    parameters:
      job_type:
        type: string
        required: true
        description: "Background job category"
      payload:
        type: object
        required: false
        description: "Optional job payload"
    script: |
      def run(args):
          bg_prefix = "bg:"
          # uuid.v4 and cache.* are harness built-ins used for cooperative task state.
          task_id = uuid.v4()
          record = {
              "id": task_id,
              "job_type": args.get("job_type", "generic"),
              "status": "queued",
              "payload": args.get("payload", {}),
          }
          cache.set(bg_prefix + task_id, json.encode(record))
          emit("custom.background.start", record)
          return {"success": True, "task_id": task_id, "status": "queued"}

  - name: background_status
    description: "Get cooperative background task status"
    parameters:
      task_id:
        type: string
        required: true
        description: "Background task id"
    script: |
      def run(args):
          bg_prefix = "bg:"
          task_id = args.get("task_id", "")
          if not task_id:
              return {"error": "task_id is required"}
          key = bg_prefix + task_id
          if not cache.has(key):
              return {"error": "task not found", "task_id": task_id}
          return json.decode(cache.get(key))

hooks:
  - event: turn.start
    handler: init_reference_runtime
    priority: 20
    script: |
      def handle(event, payload):
          if not ctx.has("runtime"):
              ctx.set("runtime", "copilot-cli")
          if not ctx.has("delegation_depth"):
              ctx.set("delegation_depth", 0)
          return allow()

  - event: tool.pre
    handler: enforce_named_tool_policy
    priority: 5
    script: |
      def handle(event, payload):
          tool_name = payload.get("tool_name", "")
          if tool_name == "exec" and ctx.get("named_tool_policy") == "prefer_named":
              return block("Use named tool 'bash' instead of raw 'exec'")
          return allow()

  - event: completion.pre
    handler: copilot_runtime_prompt_guard
    priority: 30
    script: |
      def handle(event, payload):
          messages = payload.get("messages", [])
          if len(messages) > 200:
              return block("message window too large for reference runtime policy")
          return allow()

  - event: delegation.pre
    handler: delegation_depth_guard
    priority: 10
    script: |
      def handle(event, payload):
          max_depth = 3
          depth = payload.get("depth", 1)
          if depth > max_depth:
              return block("delegation depth exceeds reference policy max_depth=" + str(max_depth))
          return allow()

  - event: delegation.post
    handler: delegation_metrics
    priority: 80
    script: |
      def handle(event, payload):
          metrics.incr("delegations_total")
          return allow()

  - event: custom.background.start
    handler: background_activation
    priority: 15
    script: |
      def handle(event, payload):
          bg_prefix = "bg:"
          key = bg_prefix + payload.get("id", "")
          if cache.has(key):
              current = json.decode(cache.get(key))
              current["status"] = "running"
              cache.set(key, json.encode(current))
          return allow()

  - event: meta.register_tool
    handler: guard_runtime_tool_registration
    priority: 5
    script: |
      def handle(event, payload):
          name = payload.get("name", "")
          if name == "exec":
              return block("direct runtime registration of 'exec' is disallowed in this reference profile")
          return allow()
---

# Copilot CLI Runtime Mapping (Artifact)

This plugin intentionally combines tool definitions, hooks, context loading, delegation, and cooperative background behavior to serve as a single inspectable reference profile.

For concept mapping and gap analysis, see:
`examples/reference/copilot-cli.md`
