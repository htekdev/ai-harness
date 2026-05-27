---
name: code-review-skill
type: plugin
version: "1.0.0"
description: "Code review skill with checklist and automated checks"
author: "Harness as Code"
tags: ["code-review", "skill", "quality", "tools"]
---

# Code Review Skill

A **reusable skill** that implements a structured code review workflow with automated checks and a quality checklist.

## What This Does

1. **Analyzes** code changes for common issues
2. **Runs** automated checks (linting, tests, security)
3. **Generates** a review checklist
4. **Posts** structured review comments
5. **Blocks** PRs with critical issues

## Code Review Checklist

### ✅ Functionality
- [ ] Code does what it's supposed to do
- [ ] Edge cases are handled
- [ ] Error handling is comprehensive
- [ ] No obvious bugs or logic errors

### ✅ Security
- [ ] No hardcoded credentials or secrets
- [ ] Input validation for user data
- [ ] No SQL injection vulnerabilities
- [ ] No XSS vulnerabilities
- [ ] Proper authentication/authorization checks

### ✅ Performance
- [ ] No obvious performance issues
- [ ] Database queries are optimized
- [ ] No N+1 query problems
- [ ] Proper caching where needed

### ✅ Maintainability
- [ ] Code is readable and well-structured
- [ ] Functions are small and focused
- [ ] Variable names are descriptive
- [ ] Complex logic has comments
- [ ] No dead code or commented-out code

### ✅ Testing
- [ ] New code has tests
- [ ] Tests cover happy path and edge cases
- [ ] Test coverage hasn't decreased
- [ ] Integration tests for new features

### ✅ Documentation
- [ ] Public APIs have docstrings
- [ ] README updated if needed
- [ ] CHANGELOG updated
- [ ] Migration guide for breaking changes

## Tools

### Analyze Code Changes

```yaml
tools:
  - name: analyze_changes
    description: "Analyze code changes for issues"
    parameters:
      files: { type: array, required: true }
    script: |
      def run(args):
          files = args.get("files", [])
          issues = []
          
          for file_path in files:
              if not fs.exists(file_path):
                  continue
              
              content = fs.read(file_path)
              
              # Check for common issues
              # 1. Hardcoded secrets
              if re.match("(password|api_key|secret|token) *= *[\"'][^\"']+[\"']", content):
                  issues.append({
                      "file": file_path,
                      "severity": "critical",
                      "issue": "Potential hardcoded secret",
                      "line": "unknown"
                  })
              
              # 2. SQL injection risk
              if re.match("SELECT .* FROM .* WHERE .* \+ ", content):
                  issues.append({
                      "file": file_path,
                      "severity": "high",
                      "issue": "Potential SQL injection (string concatenation in query)",
                      "line": "unknown"
                  })
              
              # 3. Missing error handling
              if "panic(" in content or "throw new Error" in content:
                  issues.append({
                      "file": file_path,
                      "severity": "medium",
                      "issue": "Unhandled panic/throw — consider proper error handling",
                      "line": "unknown"
                  })
              
              # 4. TODO comments without issue links
              todos = re.find_all("TODO:? [^#]*$", content)
              if len(todos) > 0:
                  issues.append({
                      "file": file_path,
                      "severity": "low",
                      "issue": "TODO comment without issue link",
                      "count": len(todos)
                  })
          
          return {
              "success": True,
              "files_analyzed": len(files),
              "issues": issues,
              "critical_count": len([i for i in issues if i["severity"] == "critical"]),
              "high_count": len([i for i in issues if i["severity"] == "high"])
          }
```

### Run Automated Checks

```yaml
tools:
  - name: run_checks
    description: "Run automated checks (lint, test, build)"
    parameters:
      checks: { type: array, required: false }  # ["lint", "test", "build"]
    script: |
      def run(args):
          checks = args.get("checks", ["lint", "test"])
          results = {}
          
          # Run linter
          if "lint" in checks:
              lint_result = exec.run("golangci-lint", ["run"])
              results["lint"] = {
                  "passed": lint_result["exit_code"] == 0,
                  "output": lint_result["stdout"] + lint_result["stderr"]
              }
          
          # Run tests
          if "test" in checks:
              test_result = exec.run("go", ["test", "-v", "./..."])
              results["test"] = {
                  "passed": test_result["exit_code"] == 0,
                  "output": test_result["stdout"]
              }
          
          # Run build
          if "build" in checks:
              build_result = exec.run("go", ["build", "./..."])
              results["build"] = {
                  "passed": build_result["exit_code"] == 0,
                  "output": build_result["stderr"]
              }
          
          # Overall status
          all_passed = all(r["passed"] for r in results.values())
          
          return {
              "success": True,
              "all_passed": all_passed,
              "results": results
          }
```

### Post Review Comment

```yaml
tools:
  - name: post_review
    description: "Post a structured review comment"
    parameters:
      analysis: { type: object, required: true }
      checks: { type: object, required: true }
      decision: { type: string, required: true }  # approve, comment, request_changes
    script: |
      def run(args):
          analysis = args.get("analysis", {})
          checks = args.get("checks", {})
          decision = args.get("decision", "comment")
          
          # Build review summary
          summary = "## Code Review Summary\n\n"
          
          # Issues found
          issues = analysis.get("issues", [])
          if len(issues) > 0:
              summary = summary + "### Issues Found (" + str(len(issues)) + ")\n\n"
              
              critical = [i for i in issues if i["severity"] == "critical"]
              if len(critical) > 0:
                  summary = summary + "**🚨 Critical:**\n"
                  for issue in critical:
                      summary = summary + "- " + issue["file"] + ": " + issue["issue"] + "\n"
                  summary = summary + "\n"
              
              high = [i for i in issues if i["severity"] == "high"]
              if len(high) > 0:
                  summary = summary + "**⚠️ High:**\n"
                  for issue in high:
                      summary = summary + "- " + issue["file"] + ": " + issue["issue"] + "\n"
                  summary = summary + "\n"
          
          # Check results
          summary = summary + "### Automated Checks\n\n"
          for check_name, result in checks.get("results", {}).items():
              status = "✅" if result["passed"] else "❌"
              summary = summary + "- " + status + " " + check_name + "\n"
          
          # Decision
          if decision == "approve":
              summary = summary + "\n✅ **Approved** — Looks good to merge!\n"
          elif decision == "request_changes":
              summary = summary + "\n🔴 **Changes Requested** — Please address critical issues before merging.\n"
          
          return {"success": True, "summary": summary, "decision": decision}
```

## Example Usage

**Full review workflow:**
```
1. analyze_changes with files: ["src/auth.go", "src/api.go"]
2. run_checks with checks: ["lint", "test", "build"]
3. post_review with analysis and checks results
```

## Blocking Critical Issues Hook

```yaml
hooks:
  - event: tool.post
    handler: block_critical_issues
    priority: 20
    when: 'tool_name == "post_review"'
    script: |
      def handle(event, payload):
          result = payload.get("result", {})
          
          # Parse result (assuming JSON string)
          if isinstance(result, str):
              result = json.decode(result)
          
          decision = result.get("decision", "")
          
          # Block merge if changes requested
          if decision == "request_changes":
              return block("Cannot approve PR — changes requested. Address critical issues first.")
          
          return allow()
```

## Customization

### Add Language-Specific Checks

```python
# Python: check for print statements
if file_path.endswith(".py") and "print(" in content:
    issues.append({"issue": "Remove debug print statements"})

# JavaScript: check for console.log
if file_path.endswith(".js") and "console.log(" in content:
    issues.append({"issue": "Remove console.log statements"})
```

### Add Performance Checks

```python
# Check for N+1 query patterns
if "for " in content and "SELECT" in content:
    issues.append({"issue": "Potential N+1 query in loop"})
```

### Add Dependency Audit

```python
# Run security audit on dependencies
audit_result = exec.run("npm", ["audit"])
if audit_result["exit_code"] != 0:
    issues.append({"severity": "high", "issue": "Dependency vulnerabilities found"})
```

## Related Skills

- See `examples/skills/git-commit.md` for commit workflows
- See `examples/tools/search-code.md` for code search
- See `examples/hooks/secret-guard.md` for secret detection

## Learn More

- String operations: `examples/README.md#strings-string`
- Regex matching: `examples/README.md#regex-re`
- Execution: `examples/README.md#execution-exec`
