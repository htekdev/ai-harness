---
name: model-configuration
type: model
version: "1.0.0"
description: "Model configuration artifact for OpenAI GPT-4"
author: "Harness as Code"
tags: ["model", "openai", "config"]
---

# Model Configuration Artifact

A **model artifact** defines provider and model configuration for your harness.

## What This Does

- Defines model provider (OpenAI, Anthropic, Azure, etc.)
- Sets model parameters (temperature, max_tokens, etc.)
- Configures API authentication
- Allows runtime model switching

## Model Artifacts vs Policy YAML

**Two ways to configure models:**

1. **Policy YAML** (`harness.yaml`) — Simple, singleton config
2. **Model Artifacts** — Composable, conditional, multiple models

**Use model artifacts when:**
- You need multiple models
- Models should load conditionally
- You want to version-control model configs
- You want to override models per-environment

## Models

```yaml
models:
  - name: gpt-4o
    provider: openai
    max_tokens: 4096
    temperature: 0.7
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
  
  - name: gpt-4o-mini
    provider: openai
    max_tokens: 16384
    temperature: 0.5
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
  
  - name: gpt-3.5-turbo
    provider: openai
    max_tokens: 4096
    temperature: 0.9
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
```

## Parameter Reference

| Parameter | Type | Description |
|-----------|------|-------------|
| `name` | string | Model identifier (e.g., "gpt-4o") |
| `provider` | string | Provider name (openai, anthropic, azure, copilot) |
| `max_tokens` | number | Maximum tokens in context window |
| `temperature` | number | Randomness (0.0 = deterministic, 1.0 = creative) |
| `api_key_env` | string | Environment variable name for API key |
| `base_url` | string | API base URL (optional, provider-specific) |

## Temperature Guide

| Temperature | Use Case | Example |
|-------------|----------|---------|
| 0.0 - 0.3 | Deterministic, factual | Code generation, data extraction |
| 0.4 - 0.7 | Balanced | General assistant, Q&A |
| 0.8 - 1.0 | Creative | Writing, brainstorming |
| 1.0+ | Very creative | Fiction, poetry |

## Conditional Model Loading

Load different models based on context:

### Example: Use GPT-4 for Production, GPT-3.5 for Dev

```markdown
---
name: prod-model
type: model
condition: 'ctx.get("environment") == "production"'
---

```yaml
models:
  - name: gpt-4o
    provider: openai
    temperature: 0.3
    api_key_env: OPENAI_API_KEY
```
\```

```markdown
---
name: dev-model
type: model
condition: 'ctx.get("environment") == "development"'
---

```yaml
models:
  - name: gpt-3.5-turbo
    provider: openai
    temperature: 0.7
    api_key_env: OPENAI_API_KEY
```
\```

## Provider-Specific Configurations

### Anthropic Claude

```yaml
models:
  - name: claude-3-opus
    provider: anthropic
    max_tokens: 200000
    temperature: 0.7
    api_key_env: ANTHROPIC_API_KEY
    base_url: https://api.anthropic.com/v1
```

### Azure OpenAI

```yaml
models:
  - name: gpt-4-azure
    provider: azure
    max_tokens: 8192
    temperature: 0.7
    api_key_env: AZURE_OPENAI_KEY
    base_url: https://YOUR-RESOURCE.openai.azure.com/
```

### GitHub Copilot

```yaml
models:
  - name: gpt-4o
    provider: copilot
    max_tokens: 4096
    temperature: 0.7
    api_key_env: GH_TOKEN
```

## Runtime Model Switching

Switch models at runtime using context:

```python
# In a tool or hook
ctx.set("active_model", "gpt-4o")
```

Then in your model artifact:

```yaml
condition: 'ctx.get("active_model") == "gpt-4o"'
```

## Multiple Models for Different Tasks

### Example: Different Models for Code vs Writing

```yaml
# Code-focused model
- name: code-model
  provider: openai
  max_tokens: 8192
  temperature: 0.2
  condition: 'ctx.get("task_type") == "code"'

# Writing-focused model
- name: writing-model
  provider: openai
  max_tokens: 4096
  temperature: 0.8
  condition: 'ctx.get("task_type") == "writing"'
```

## Model Override via Environment

```bash
# Override model via environment variable
export HARNESS_MODEL=gpt-4o-mini
harness run
```

In your artifact:

```yaml
condition: 'env("HARNESS_MODEL") == "gpt-4o-mini"'
```

## Cost Optimization

### Use Cheaper Models for Simple Tasks

```python
# In a tool that sets task complexity
if complexity == "simple":
    ctx.set("active_model", "gpt-3.5-turbo")  # Cheaper
elif complexity == "complex":
    ctx.set("active_model", "gpt-4o")  # More capable
```

## Related Examples

- See `examples/policies/delegation.md` for sub-agent configs
- See `examples/context/pr-mode.md` for conditional loading
- See `examples/README.md#conditional-loading-starlark-expressions`

## Learn More

- Model configuration: See product spec section on models
- Context variables: `examples/README.md#state-cache-ctx`
- Environment variables: `examples/README.md#core-utilities`
