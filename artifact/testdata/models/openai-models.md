---
name: openai-models
type: model
version: "1.0.0"
description: OpenAI model configurations
author: htekdev
tags: [openai, gpt]
models:
  - name: gpt-4o
    provider: openai
    max_tokens: 128000
    temperature: 0.7
    api_key_env: OPENAI_API_KEY
  - name: gpt-4o-mini
    provider: openai
    max_tokens: 16000
    temperature: 0.5
    api_key_env: OPENAI_API_KEY
---

# OpenAI Models

Configuration for OpenAI GPT models. Set `OPENAI_API_KEY` environment
variable to authenticate.
