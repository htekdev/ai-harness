---
name: production-baseline-showcase-docs
type: plugin
version: "0.1.0"
description: "Documentation companion artifact for the production baseline showcase"
author: "AI Harness"
tags: ["showcase", "docs"]
condition: "False"
---

# Production Baseline Showcase Plugin

This example ships a **mature harness profile** as a small, drop-in bundle:

- `/examples/production-baseline/identity.md`
- `/examples/production-baseline/plugins/mature-harness.md`

Use it as both:

- A production baseline starter profile
- A concrete "Harness-as-Code" showcase for governance + memory discipline

## What this showcase includes

### Tools

- `safe_content_write`  
  Safe file writes with `/tmp` and size guardrails.
- `emit_structured_event`  
  Structured event emission + event-tier append.
- `memory_tier_put`  
  Helper for memory tiers: `core`, `working`, `long_term`, `events`.

### Governance hooks

- `tool.pre` → `prefer_named_tool_policy`  
  Blocks raw `exec` in favor of named wrappers.
- `tool.pre` → `file_write_guard`  
  Blocks `/tmp` writes and large heredoc-style shell write patterns.
- `delegation.pre` → `delegation_depth_and_allowlist_guard`  
  Enforces depth ≤ 3 and a sub-agent allowlist.
- `completion.pre` → `completion_message_window_guard`  
  Blocks oversized message windows.
- `turn.start` → `hydrate_context_memory_tiers`  
  Hydrates core + working memory into `ctx`.
- `turn.end` → `persist_events_and_working_memory`  
  Appends JSONL snapshots to `.harness/memory/events.log` and updates working memory.

## Why this is "mature"

It demonstrates a practical baseline for:

- Safe content mutation over raw shell-based file writes
- Bounded autonomy and explicit delegation policy
- Deterministic lifecycle hooks and auditable event trails
- Memory tier separation that scales beyond toy demos
