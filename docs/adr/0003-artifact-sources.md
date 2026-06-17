# ADR 0002 — External artifact sources and trust boundary

**Status:** Accepted
**Date:** 2026-06-17
**Phase:** 6.x / 7.x
**Decider:** Copilot Coding Agent

## Context

The harness historically loaded artifacts only from local `.harness/`
subdirectories. This blocked plugin distribution as standalone repositories and
forced users to manually copy files or wire submodules.

We need a one-line config path to consume plugin packs from external git repos,
with cache support, offline operation, and explicit trust boundaries.

## Options considered

### Option A — Keep local-only loading

- Pros: simplest implementation and risk profile.
- Cons: no first-class plugin distribution story; manual sync burden.

### Option B — Add `artifact_sources` with pluggable fetchers + trust allowlist

- Pros: direct config-driven plugin consumption; cache/offline support;
  explicit trust declarations.
- Cons: larger loader surface; source-fetch security considerations.

## Decision

**We will add `artifact_sources` and `trusted_sources` to config, with `local`
and `git` sources resolved into cache-backed local roots before standard
artifact loading.**

Rationale:

1. Keeps the existing artifact parser/composer intact by resolving sources to
   filesystem paths first.
2. Allows gradual fetcher expansion (`http`, `oci`) without reworking runtime
   composition.
3. Makes trust explicit: external executable artifacts must come from
   allowlisted origins.

## Consequences

- Plugin repositories can be consumed directly via pinned refs.
- `harness pull` can prefetch/update source cache for CI and offline use.
- Operators must treat external plugins as executable third-party code and
  manage source allowlists accordingly.

## Revisit triggers

Revisit if any of these occur:

- We need signed provenance verification beyond URL trust + optional checksum.
- We need source types that cannot be materialized as local directories.
- Branch-based mutable refs become a required workflow.
