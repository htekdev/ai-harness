---
name: core-tools
type: builtin
version: "1.0.0"
description: Core execution and file system tools
author: htekdev
tags: [core, execution]
tools:
  - name: exec
    description: Execute shell commands
    parameters:
      command:
        type: string
        required: true
        description: The command to execute
    timeout_ms: 30000
  - name: read-file
    description: Read a file from disk
    parameters:
      path:
        type: string
        required: true
        description: Path to read
hooks:
  - event: onPreToolUse
    handler: log
    priority: 1
---

# Core Tools

Provides fundamental execution and file system capabilities.
These tools are always available and cannot be overridden by plugins.
