# PR Review Mode — Context Source

This context is automatically injected when the agent is running in
`pull_request` mode (`ctx.get("mode") == "pull_request"`).

## PR Review Guidelines

- Always read the full diff before commenting.
- Focus on correctness, security, and maintainability — not style.
- Reference the specific file and line number when raising concerns.
- Distinguish between **blocking** issues (must fix before merge) and
  **non-blocking** suggestions (nice-to-have improvements).
- Check that new code is covered by tests.
- Verify that public API changes are reflected in documentation.

## Common PR Anti-Patterns

- Overly large PRs — recommend splitting into smaller reviewable units.
- Missing error handling in critical paths.
- Hardcoded secrets or credentials.
- Untested edge cases (empty input, nil pointers, concurrent access).

## Review Checklist

1. Does the change do what the description says?
2. Are there security implications?
3. Is the code readable and well-named?
4. Are tests adequate?
5. Is the PR description accurate and complete?
