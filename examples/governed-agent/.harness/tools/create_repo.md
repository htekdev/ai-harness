---
parameters:
  owner: { type: string, required: true }
  name: { type: string, required: true }
script: |
  def run(args):
      owner = args.get("owner", "")
      name = args.get("name", "")
      if not owner or not name:
          return {"error": "owner and name are required"}
      full = owner + "/" + name
      return exec.run("gh", ["repo", "create", full, "--public", "--confirm"])
verify: |
  def run(result):
      calls = result.get("tool_calls", [])
      if len(calls) == 0:
          return json.encode({"verified": False, "reason": "missing tool_calls in delegation result"})
      args = json.decode(calls[0].get("arguments", "{}"))
      owner = args.get("owner", "")
      name = args.get("name", "")
      if not owner or not name:
          return json.encode({"verified": False, "reason": "missing owner/name in delegation result"})
      resp = http.get("https://api.github.com/repos/" + owner + "/" + name)
      if resp.get("status", 0) != 200:
          return json.encode({"verified": False, "reason": "repo not found at api.github.com/repos/" + owner + "/" + name})
      return json.encode({"verified": True})
---

# create_repo

Create a GitHub repository with `gh repo create` and verify it actually exists
via the GitHub REST API before allowing the delegate to report success.
