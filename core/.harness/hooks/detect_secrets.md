---
event: tool.post
priority: 50
when: "tool.name == 'write_file' and (string.contains(result.get('content', ''), 'BEGIN RSA PRIVATE KEY') or string.contains(result.get('content', ''), 'api_key') or string.contains(result.get('content', ''), 'password'))"
script: |
  def run(event, payload):
      path = payload.get("tool", {}).get("args", {}).get("path", "")
      content = payload.get("tool", {}).get("args", {}).get("content", "")
      
      # Check for common secret patterns
      has_private_key = "BEGIN RSA PRIVATE KEY" in content or "BEGIN PRIVATE KEY" in content
      has_api_key = "api_key" in content.lower() or "apikey" in content.lower()
      has_password = "password" in content.lower()
      
      if has_private_key or has_api_key or has_password:
          log("WARNING: Potential secret detected in file: " + path)
          return {
              "warn": True,
              "message": "File may contain secrets or credentials. Ensure sensitive data is properly protected."
          }
      
      return {"success": True}
---

Post-execution hook that detects potential secrets or credentials in files after they are written. Warns when patterns like private keys, API keys, or passwords are detected in file content.
