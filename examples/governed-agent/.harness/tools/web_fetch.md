---
parameters:
  url: { type: string, required: true }
  timeout_ms: { type: number, required: false }
script: |
  def run(args):
      url = args.get("url", "")
      timeout = args.get("timeout_ms", 10000)
      if not url:
          return {"error": "url is required"}
      # http.get is sandboxed by scripting.NetworkSandbox when the engine is
      # configured with allowed_domains. URLs outside the allowlist surface
      # as a SandboxError to the caller.
      resp = http.get(url, {}, timeout)
      return {
          "status": resp.get("status", 0),
          "body": string.truncate(resp.get("body", ""), 4000),
          "url": url,
      }
---

# web_fetch

Fetch an HTTP(S) URL through the harness network sandbox. Only hosts listed
in the engine's `allowed_domains` will succeed; everything else is blocked
before the request leaves the process.

Use this instead of asking the user to paste content.
