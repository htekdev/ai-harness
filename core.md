# AI Harness — Core Positioning

This document captures the canonical positioning thesis for AI Harness, including the vendor bias table and the Harness as Code differentiator.

---

## The Fragmentation Problem

The AI coding agent landscape has accelerated into deep vendor fragmentation. Every major platform now ships its own agent — each optimized for its own model stack, ecosystem, and toolchain:

- **WWDC 2026:** Apple shipped Xcode 27 with a dual-engine agent (Apple Foundation Models + Core AI), locked to Swift/Apple platforms.
- **GitHub Copilot:** Deeply embedded in VS Code, GitHub Actions, and the Microsoft ecosystem.
- **Claude Code:** Tied to Anthropic's API and model roadmap.
- **Codex:** OpenAI frontier model dependency, CLI-first but API-bound.
- **Pi:** Minimal terminal harness, TypeScript-native.

Each agent delivers real value — within its ecosystem. The problem compounds over time: behavior locked into one agent's harness doesn't transfer. Governance rules, tool definitions, and system prompts written for Copilot don't run in Claude Code. Workflows built for Xcode 27 don't port to VS Code.

**This is the exact problem Harness as Code solves.**

---

## Vendor Bias Table

Every AI coding agent carries an inherent bias shaped by its vendor, model, and ecosystem. AI Harness is explicitly biased toward extensibility and portability as its founding constraints.

| Harness | Primary Bias |
|---------|-------------|
| GitHub Copilot | GitHub ecosystem, VS Code, Actions |
| Claude Code | Anthropic models, Anthropic API |
| Codex | OpenAI frontier models |
| Pi | Minimal terminal coding, TypeScript |
| Xcode 27 Agent | Apple Foundation Models, Core AI, Swift/Apple ecosystem |
| **AI Harness** | **Extensibility — Harness as Code** |

### Why Xcode 27 Validates This Thesis

Apple's dual-engine architecture (running two AI models simultaneously) is a direct validation of two AI Harness principles:

1. **Multi-model composition** — AI Harness treats models as typed artifacts (`type: model`), enabling provider onboarding without core rewrites. Xcode 27 proves the industry needs exactly this: multiple models, coordinated without monolithic rewrites.
2. **Harness lock-in compounds** — Xcode 27 is purpose-built for Swift and Apple platforms. Any governance rules, tool definitions, or workflows built inside the Xcode agent stay there. AI Harness keeps those artifacts in version-controlled Markdown — portable, reviewable, and model-agnostic.

---

## Positioning Statement

> **"Every major vendor now ships an AI coding agent biased toward their own stack. AI Harness is biased toward portability — behavior as code that works wherever your team works."**

---

## Content Angle: "Xcode 27 Proves Why You Need Harness as Code"

Proposed content piece for htek.dev (tracking: content-management issue for Xcode 27 positioning):

- **Hook:** WWDC 2026 / Xcode 27 as the news peg
- **Walk-through:** Full vendor bias table — 6 players, 6 locked stacks
- **Core argument:** Harness lock-in compounds — governance rules, tool definitions, and system prompts are the new infrastructure, and today they're trapped in vendor silos
- **Resolution:** Harness as Code — artifacts in Git, portable across providers, governed by default
- **CTA:** AI Harness as the portable, governance-forward alternative

---

## References

- [README — What Makes It Different](README.md#what-makes-it-different)
- [README — Pi Benchmark](README.md#pi-benchmark-shared-philosophy-different-depth)
- [plan.md — Phase 6 launch content](plan.md)
- Apple WWDC 2026 / Xcode 27 announcement
