# Typed Artifact Registry Schema (Design)

Status: Draft (pre-implementation)
Schema version: `harness_artifact/v1alpha1`

## Purpose

This document defines the **base typed artifact schema model** for AI Harness so runtime loading and composition can rely on explicit, validated artifact contracts.

## 1) Base taxonomy

The base taxonomy is intentionally minimal and versioned:

- `harness_artifact` (abstract base schema; not directly instantiated)
- `harness`
- `builtin`
- `plugin`
- `override`

`harness_artifact` defines shared identity and metadata rules. Concrete artifact kinds extend the base schema with type-specific constraints.

## 2) Base schema: `harness_artifact`

Required fields:

- `schema_version` (string): must equal `harness_artifact/v1alpha1`
- `kind` (string): one of `harness`, `builtin`, `plugin`, `override`
- `name` (string): unique per kind, lowercase alphanumeric + hyphen, min length 2

Optional fields:

- `version` (string): canonical format is semver `X.Y.Z`; `X.Y` is accepted only for backward compatibility with current runtime behavior.
- `description` (string)
- `author` (string)
- `tags` ([]string)
- `depends_on` ([]string): dependency edges by artifact name
- `condition` (string): Starlark expression evaluated per turn
- `priority` (int): explicit override of default kind priority
- `context` (markdown body)
- `tools` ([]tool definitions)
- `hooks` ([]hook definitions)

## 3) Type-specific validation rules

In addition to base validation:

### `harness`

- Must include non-empty `context`.
- Represents root identity/policy artifact (one per project by convention).

### `builtin`

- Must define at least one tool or hook.
- Should declare `version`.

### `plugin`

- Must include non-empty `description`.

### `override`

- Must define at least one of: `tools`, `hooks`, or non-empty `context`.

### Cross-cutting validation

- Duplicate tool names in one artifact are invalid.
- Duplicate hook `(event, handler)` pairs in one artifact are invalid.
- `kind` must be valid.
- `name` must match naming rules.
- `version`, when present, must match accepted semver-like format.

## 4) Runtime loading and composition mapping

### Loading

- Parse Markdown frontmatter + body into typed artifact metadata + context.
- Directory conventions map to expected kinds (`identity.md`, `builtins/`, `plugins/`, `overrides/`) but declared `kind` remains authoritative. Current runtime behavior is to accept mismatches without error; follow-up work should add optional warnings. Example: a file under `plugins/` declaring `kind: override` is currently accepted, but should be treated as suspicious in validation UX.
- Validate each artifact before registry registration.
- Registry rejects invalid entries and enforces dependency validation.

### Composition

- Compose active artifacts by effective priority (default kind priority unless overridden). Any positive integer override is allowed; the kind values below are defaults, not reserved bands. Recommended convention: stay within 1-200 and use increments of 10 or 20 for readability.
- Default kind priority order:
  - `override` (100)
  - `harness` (80)
  - `builtin` (60)
  - `plugin` (40)
- Conditions are evaluated per turn; only active artifacts participate unless explicitly including inactive entries.
- Identity context is sourced from `harness`; additional context blocks come from active non-harness artifacts.

## 5) Follow-up implementation tasks

1. Introduce explicit `schema_version` + `kind` aliases in parser while preserving backward compatibility with existing `type` field.
2. Add migration/normalization path from legacy artifact fields to `harness_artifact/v1alpha1`.
3. Encode base schema contract as table-driven parser/validator tests.
4. Add CLI output to show schema version and effective kind for each artifact.
5. Add docs/examples showing valid `harness_artifact/v1alpha1` frontmatter.
6. Define compatibility policy for future schema versions (`v1beta1`, `v1`).

## 6) Compatibility notes

- Current runtime includes additional typed artifacts (for example model onboarding artifacts).
- This document defines the **base registry contract** requested for first-class typed artifacts.
- Additional domain-specific artifact kinds can compose on top of this base contract in later schema revisions.
