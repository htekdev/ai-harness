---
name: git-ops
type: plugin
version: "0.2.0"
description: Git operations plugin for version control workflows
author: htekdev
tags: [git, vcs, development]
depends_on: [core-tools]
tools:
  - name: git-status
    description: Show the working tree status
    parameters:
      path:
        type: string
        required: false
        description: Repository path (defaults to cwd)
  - name: git-diff
    description: Show changes between commits or working tree
    parameters:
      ref:
        type: string
        required: false
        description: Git ref to diff against
hooks:
  - event: onPostToolUse
    handler: advisory
    when: "tool.name == 'exec' and 'git push' in tool.args.command"
    reason: Consider using git-ops tools instead of raw git commands
---

# Git Operations Plugin

Provides safe, governed git operations that integrate with the harness
hook system for audit and policy enforcement.
