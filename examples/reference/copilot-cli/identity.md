---
name: copilot-cli-reference-runtime
type: harness
version: "0.1.0"
description: "Reference harness identity modeling Copilot CLI runtime behavior"
author: "AI Harness"
tags: ["reference", "copilot-cli", "runtime"]
---

# Copilot CLI Reference Runtime

This harness identity models a Copilot CLI-style runtime using AI Harness artifacts.

## Intent

- Prefer named tools over direct low-level built-ins
- Apply governance hooks before execution
- Load context dynamically by runtime mode
- Delegate specialized tasks to sub-agents
- Support cooperative long-running/background work patterns
