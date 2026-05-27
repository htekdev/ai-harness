---
name: delegation-policy
type: plugin
version: "1.0.0"
description: "Sub-agent delegation policy and configuration"
author: "Harness as Code"
tags: ["delegation", "sub-agents", "policy"]
---

# Delegation Policy

Policy configuration for **sub-agent delegation** — when the agent spawns child agents to handle subtasks.

## What This Does

- Defines delegation limits (max depth, max concurrent)
- Sets iteration limits per depth level
- Configures sub-agent timeouts
- Controls delegation permissions

## Delegation Policy (YAML)

These settings are typically in `harness.yaml`, but can also be defined in artifacts:

```yaml
## Policy

```yaml
delegation:
  # Maximum delegation depth (how many levels of sub-agents)
  max_depth: 3
  
  # Maximum concurrent sub-agents
  max_concurrent: 5
  
  # Iteration limits per depth level
  # [depth_0, depth_1, depth_2, depth_3]
  iterations_per_depth: [20, 10, 5, 3]
  
  # Timeout per sub-agent (milliseconds)
  timeout_ms: 300000  # 5 minutes
  
  # Allow sub-agents to delegate further?
  allow_recursive: true
```
\```

## Delegation Depth Explained

```
Parent Agent (depth 0, max 20 iterations)
  ├─ Sub-Agent A (depth 1, max 10 iterations)
  │   ├─ Sub-Agent A1 (depth 2, max 5 iterations)
  │   └─ Sub-Agent A2 (depth 2, max 5 iterations)
  └─ Sub-Agent B (depth 1, max 10 iterations)
      └─ Sub-Agent B1 (depth 2, max 5 iterations)
```

**Why decreasing iterations?**
- Prevents infinite recursion
- Keeps sub-agents focused
- Limits token/cost explosion

## Delegation Hooks

### Block Dangerous Delegations

```yaml
hooks:
  - event: delegation.pre
    handler: block_dangerous_delegation
    priority: 10
    script: |
      def handle(event, payload):
          agent_name = payload.get("agent_name", "")
          prompt = payload.get("prompt", "")
          
          # Block delegation to delete/destroy agents
          if "delete" in agent_name or "destroy" in agent_name:
              return block("Delegation to delete agents not allowed")
          
          # Block if prompt contains secrets
          if "password=" in prompt or "api_key=" in prompt:
              return block("Cannot delegate prompts containing secrets")
          
          return allow()
```

### Log All Delegations

```yaml
hooks:
  - event: delegation.pre
    handler: log_delegation
    priority: 100
    script: |
      def handle(event, payload):
          agent_name = payload.get("agent_name", "")
          depth = payload.get("depth", 0)
          
          log("🔀 Delegating to: " + agent_name + " (depth " + str(depth) + ")")
          
          # Increment delegation counter
          metrics.incr("delegations_total")
          metrics.incr("delegations_" + agent_name)
          
          return allow()
```

### Modify Sub-Agent Prompt

```yaml
hooks:
  - event: delegation.pre
    handler: inject_delegation_context
    priority: 50
    script: |
      def handle(event, payload):
          prompt = payload.get("prompt", "")
          depth = payload.get("depth", 0)
          
          # Inject context into sub-agent prompt
          enhanced_prompt = """
          You are a sub-agent at depth level """ + str(depth) + """.
          Your parent agent has delegated this task to you.
          Be concise and focused on the specific task.
          
          Task: """ + prompt
          
          return modify({"prompt": enhanced_prompt})
```

## Delegation Patterns

### Sequential Delegation (Chain)

```
Research Agent → Writing Agent → Review Agent
```

Each agent completes before the next starts.

### Parallel Delegation (Fan-Out)

```
Parent Agent
  ├─ Agent A (parallel)
  ├─ Agent B (parallel)
  └─ Agent C (parallel)
```

All agents run simultaneously.

### Recursive Delegation (Tree)

```
Problem Decomposer
  ├─ Subtask 1
  │   ├─ Subtask 1.1
  │   └─ Subtask 1.2
  └─ Subtask 2
```

Each sub-agent can spawn more sub-agents.

## Delegation Tool Example

```yaml
tools:
  - name: delegate
    description: "Delegate a task to a specialized sub-agent"
    parameters:
      agent_name: { type: string, required: true }
      task: { type: string, required: true }
    script: |
      def run(args):
          agent_name = args.get("agent_name", "")
          task = args.get("task", "")
          
          # Validate
          if not agent_name or not task:
              return {"error": "agent_name and task are required"}
          
          # Check delegation depth
          current_depth = ctx.get("delegation_depth", 0)
          max_depth = ctx.get("max_delegation_depth", 3)
          
          if current_depth >= max_depth:
              return {"error": "Maximum delegation depth reached (" + str(max_depth) + ")"}
          
          # Emit delegation event (triggers delegation.pre hooks)
          emit("delegation.pre", {
              "agent_name": agent_name,
              "prompt": task,
              "depth": current_depth + 1
          })
          
          # Delegate (actual implementation would call harness runtime)
          log("Delegating to: " + agent_name)
          
          # For this example, return mock result
          result = "Sub-agent " + agent_name + " completed task"
          
          # Emit delegation.post event
          emit("delegation.post", {
              "agent_name": agent_name,
              "result": result,
              "depth": current_depth + 1
          })
          
          return {"success": True, "result": result}
```

## Conditional Delegation

### Only Delegate During Business Hours

```yaml
condition: '8 <= time.now() % 86400 / 3600 < 18'
```

### Only Delegate for Complex Tasks

```python
complexity = ctx.get("task_complexity", "simple")
if complexity == "complex":
    # Delegate
    pass
else:
    # Handle directly
    pass
```

## Cost Control

### Limit Delegation in Dev Mode

```yaml
# In dev mode, limit to 1 level
delegation:
  max_depth: 1
  max_concurrent: 2
  condition: 'ctx.get("environment") == "development"'
```

### Track Delegation Costs

```python
# In delegation.post hook
metrics.incr("total_delegations")
metrics.incr("delegation_cost", estimated_tokens * cost_per_token)
```

## Related Examples

- See `examples/policies/model-config.md` for model configuration
- See `examples/hooks/command-guard.md` for governance hooks
- See `examples/README.md#hook-events` for delegation events

## Learn More

- Delegation architecture: See product spec section on delegation
- Hook events: `examples/README.md#hook-events`
- Metrics tracking: `examples/README.md#metrics-metrics`
