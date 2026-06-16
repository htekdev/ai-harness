# Security Policy

Thanks for helping keep AI Harness and its users safe. This document explains
which versions receive security fixes and how to report a vulnerability.

## Supported Versions

AI Harness is pre-1.0 software under active development. Security fixes land on
the latest minor release line on `main`. Older minor lines are not patched.

| Version | Supported          |
|---------|--------------------|
| 0.6.x   | :white_check_mark: |
| < 0.6   | :x:                |

When a new minor (`0.7.x`, etc.) ships, the previous minor stops receiving
security fixes. Users are expected to upgrade to the latest minor to stay
patched.

## Reporting a Vulnerability

Please **do not** open a public GitHub issue, discussion, or pull request for
suspected security vulnerabilities.

Use GitHub's private vulnerability reporting:

1. Go to https://github.com/htekdev/ai-harness/security/advisories/new
2. Fill in a clear description, reproduction steps, and impact assessment
3. Submit the advisory — only repo maintainers will see it

If GitHub private reporting is unavailable, you can email the maintainer at
`security@htek.dev` with the same information.

### What to include

A good report typically contains:

- Affected version(s) (e.g. `v0.6.0`, `main` at commit SHA)
- A minimal reproduction (artifact snippet, command line, expected vs. actual)
- Impact: what an attacker can do (e.g. tool sandbox escape, policy bypass,
  prompt-injection-driven file exfiltration)
- Any suggested mitigation, if known

### What to expect

- **Acknowledgement:** within 3 business days
- **Initial assessment:** within 7 business days
- **Fix or mitigation timeline:** communicated after assessment, prioritized by
  severity
- **Coordinated disclosure:** we'll work with you on a disclosure window before
  any public advisory is published

## Scope

In-scope examples:

- Tool sandbox escapes (Starlark or shell tools breaking governance)
- Policy / hook bypasses that allow disallowed tool calls
- Sub-agent delegation flaws that escalate permissions
- Secrets handling defects (env exposure, log leakage)
- Supply-chain issues in published artifacts (binaries, modules)

Out-of-scope examples:

- Vulnerabilities requiring an attacker who already controls your local machine
  or harness configuration
- Issues only reproducible against forked or heavily modified harness configs
- Behavior of upstream LLM providers (report those to the provider directly)
- Denial-of-service via clearly abusive harness configs that wouldn't pass
  review (e.g. unbounded recursion the operator wrote themselves)

## Safe Harbor

We will not pursue legal action against researchers who:

- Make a good-faith effort to follow this policy
- Avoid privacy violations, data destruction, and service disruption
- Report findings privately through the channels above before any public
  disclosure

Thank you for practicing coordinated disclosure.
