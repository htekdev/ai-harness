---
name: openai-model-catalog
type: model
version: "1.0.0"
description: "Example model artifact for provider/model onboarding"
author: "Harness as Code"
tags: ["model", "openai", "catalog"]
models:
  - name: gpt-4o
    provider: openai
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
    temperature: 0.2
    max_tokens: 16384
    capabilities:
      streaming: true
      tool_calling: true
      vision: true
      json_mode: true
  - name: gpt-4o-mini
    provider: openai
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
    temperature: 0.4
    max_tokens: 8192
    capabilities:
      streaming: true
      tool_calling: true
      json_mode: true
---

# Model Configuration Artifact

Use `type: model` artifacts to onboard new providers and models without rewriting the core harness.

## What belongs in a model artifact

- Provider wiring (`provider`, `api_key_env`, `base_url`)
- Model defaults (`temperature`)
- Model limits (`max_tokens`)
- Capability flags (`capabilities.*`)
- Optional markdown body explaining when this catalog should be active

Model artifacts may include standard artifact metadata like `condition`, `tags`, `depends_on`, and `priority`, but they should not define tools or hooks.

## Recommended onboarding flow

1. Create a file in `.harness/models/`.
2. Add one or more entries under `models:`.
3. Use `condition:` when a catalog should only apply in specific environments.
4. Run `harness artifacts -v` to verify discovery and inspect the registered model names.
5. Only change Go code if the provider needs brand-new transport/auth/runtime behavior.

## Conditional catalog example

To switch catalogs by environment, make separate model artifacts and gate them at the artifact level:

```markdown
---
name: prod-openai-models
type: model
condition: 'ctx.get("environment") == "production"'
models:
  - name: gpt-4o
    provider: openai
    api_key_env: OPENAI_API_KEY
    temperature: 0.2
    max_tokens: 16384
---
```

```markdown
---
name: dev-openai-models
type: model
condition: 'ctx.get("environment") == "development"'
models:
  - name: gpt-4o-mini
    provider: openai
    api_key_env: OPENAI_API_KEY
    temperature: 0.4
    max_tokens: 8192
---
```

## Interaction with other artifact types

- `harness` artifacts define identity and base system prompt.
- `builtin` / `plugin` artifacts define tools and hooks.
- `override` artifacts can change context or activation behavior around models, but they do not define `models:`.
- Multiple active model artifacts compose together, so keep model names distinct and use conditions for environment-specific catalogs.

## Related examples

- See `examples/context/pr-mode.md` for conditional loading
- See `examples/README.md#conditional-loading-starlark-expressions`
