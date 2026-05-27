# Plan for Adding Asynchronous Functionality

This document outlines the implementation plan for extending async capabilities for the repository. Asynchronous functionality ensures performance scalability, enabling efficient task execution and improved user workflows.

---

## Overview of Current State

1. **Existing Async Support**:
   - The `delegate_async` mechanism partially supports asynchronous delegation but needs further context within broader execution models.
   - Starlark scripting is used for tool logic, which can run asynchronously using the existing runtime.

2. **Primary Areas of Improvement**:
   - Delegation workflows (`delegation`): Enhance async task stores to handle more extensive and concurrent delegations across sessions.
   - Tool integrations (`tools`): Refactor heavily I/O-bound tools like file or HTTP utilities to operate asynchronously.
   - Hooks/lifecycle governance (`hooks`): Support async hooks to enable non-linear behavior monitoring between sessions.

---

## Objectives

1. **Expand Async Handling Across the Framework**:
   - Implement mechanisms for queueing, awaiting, and managing long-running tool tasks.
   - Integrate models that streamline communication across async sub-agents.

2. **Core Changes**
   - Refactor the `agent` turn loop to discriminate between blocking sync and non-blocking async processes dynamically.

3. **Backward Compatibility**:
   - Retain all synchronous versions of tools and hooks for seamless fallback modes.

---

## Implementation Plan

### Step 1: Analysis of Existing Codebase
- Enumerate and audit tools like `fs.read`, `http.get/post`, `delegate`, and hooks.
- Mark areas prone to blocking (e.g., external API clients).
- Investigate current delegation limitations for async scaling.

### Step 2: Async Delegation Storage
- Add asynchronous messaging/queue for long-running delegations (`delegate_async`). Tasks will return immediately while status handlers like `await` and `result` enable lifecycles.

---

### Step 3: Hook System Enhancements
- Modify hooks to support async/non-blocking scripts to allow Starlark filtering of concurrent events.

---

### Step 6: Refactor Tool Logic

#### Tool Audit
- Identify known tools like HTTP endpoint interaction (e.g., `http.get`) or disk-bound tasks (`fs.read`) that can default to async versions.

#### Starlark Extension
- Develop alternate async-compatible runtime/retry instrumentation models for executing complex tool retry bounds while calling async third-party libraries.

---

### Deliverables

- **Enhancements/asynchronous APIs for tasks.**
- Agent turn-polling framing coordination intended to prehandle.