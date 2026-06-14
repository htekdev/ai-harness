# AI Harness — Default Examples & Quickstart

This directory contains **production-ready example artifacts** to help you learn how to build AI agents with Harness as Code.

> **🚀 Start here:** [`governed-agent/`](./governed-agent/) is the flagship
> reference profile. It composes every Phase 5 governance primitive (tool
> policy, retry, network sandbox, command guard, OTel, self-augment) into a
> single `git clone → cd → harness run` example.

## Quick Start

1. **Initialize a new harness:**
   ```bash
   harness init my-agent
   cd my-agent
   ```

2. **Browse these examples** to see how artifacts work
3. **Copy examples** into your `.harness/` directory
4. **Modify** to match your needs
5. **Validate:**
   ```bash
   harness validate
   ```

6. **Run:**
   ```bash
   harness run
   ```

---

## What Are Artifacts?

**Artifacts are the building blocks of a harness.** Each artifact is a Markdown file with YAML frontmatter that defines:
- **Identity** (name, type, version)
- **Context** (prose instructions for the agent)
- **Tools** (executable functions)
- **Hooks** (pre/post event interceptors)
- **Conditions** (when this artifact is active)

### The Artifact Type Taxonomy

| Type | Priority | Purpose | Example |
|------|----------|---------|---------|
| **`override`** | 100 | Project-local overrides that supersede everything | Testing mode, private repo rules |
| **`harness`** | 80 | Root harness identity — who the agent IS | `harness.md` |
| **`builtin`** | 60 | Core capabilities shipped with the runtime | File system tools, HTTP client |
| **`plugin`** | 40 | User-authored or third-party capability bundles | Git workflows, code review skills |
| **`model`** | 20 | Provider/model configuration | OpenAI, Anthropic, Azure configs |

**Higher priority = loaded later = can override lower priority artifacts.**

---

## Example Structure

```
examples/
├── README.md              ← You are here
├── hooks/                 ← Hook examples (governance & safety)
│   ├── command-guard.md   ← Block dangerous shell commands
│   ├── secret-guard.md    ← Block accidental secret exposure
│   └── modify-tool.md     ← Modify tool arguments pre-execution
├── tools/                 ← Tool examples (capabilities)
│   ├── read-file.md       ← Simple file reader tool
│   ├── search-code.md     ← Regex search across codebase
│   └── create-task.md     ← Task creation with validation
├── skills/                ← Reusable procedures (context + tools)
│   ├── git-commit.md      ← Safe git commit workflow
│   └── code-review.md     ← Code review procedure
├── context/               ← Dynamic context examples
│   ├── pr-mode.md         ← Conditional PR review context
│   └── time-based.md      ← Business hours vs quiet mode
├── reference/             ← Reference runtime mappings
│   ├── copilot-cli.md     ← Copilot CLI concept → artifact mapping + gaps
│   └── copilot-cli/       ← Replayable reference harness artifacts
│       ├── identity.md
│       └── plugins/
│           └── copilot-runtime.md
├── policies/              ← Policy configuration examples
│   ├── model-config.md    ← Model settings artifact
│   └── delegation.md      ← Sub-agent rules
└── conditions/            ← Conditional loading examples
    ├── file-type.md       ← Load based on file extension
    └── repo-private.md    ← Load based on repo visibility
```

---

## Built-In Starlark Functions

Your tool and hook scripts have access to 50+ built-in functions organized by category:

### Filesystem (`fs.*`)
```python
fs.read(path)                    # Read file contents
fs.write(path, content)          # Write file (overwrite)
fs.append(path, content)         # Append to file
fs.exists(path)                  # Check if file exists
fs.remove(path)                  # Delete file
fs.mkdir(path)                   # Create directory
fs.list(path)                    # List directory contents
fs.stat(path)                    # Get file metadata
fs.glob(pattern)                 # Find files by pattern
fs.copy(src, dst)                # Copy file
fs.move(src, dst)                # Move file
fs.diff(path1, path2)            # Generate unified diff
```

**Security:** All `fs.*` operations are jailed to the working directory. Path traversal is blocked at the Go level.

### Editing (`fs.*`)
```python
fs.replace(path, old, new)       # Replace first occurrence
fs.replace_all(path, old, new)   # Replace all occurrences
fs.insert_at(path, line, text)   # Insert text at line number
fs.replace_lines(path, start, end, text)  # Replace line range
fs.delete_lines(path, start, end)         # Delete line range
fs.find(path, pattern)           # Find pattern in file
```

### Execution (`exec.*`)
```python
exec.run(cmd, args=[], timeout_ms=30000, dir=".")
# Returns: {"stdout": "...", "stderr": "...", "exit_code": 0}

# Example:
result = exec.run("git", ["log", "--oneline", "-5"])
if result["exit_code"] == 0:
    print(result["stdout"])
```

### Network (`http.*`, `url.*`)
```python
http.get(url, headers={}, timeout_ms=30000)
http.post(url, body, headers={}, timeout_ms=30000)
# Returns: {"status": 200, "body": "...", "headers": {...}}

url.parse(url_string)
# Returns: {"scheme": "https", "host": "example.com", ...}

url.encode(params_dict)
# Returns: "key1=value1&key2=value2"
```

### Crypto (`hash.*`, `base64.*`, `crypto.*`)
```python
hash.sha256(data)                # SHA-256 hash (hex string)
hash.md5(data)                   # MD5 hash (hex string)
base64.encode(data)              # Base64 encode
base64.decode(encoded)           # Base64 decode
crypto.hmac_sha256(key, data)    # HMAC-SHA256 (hex string)
```

### State (`cache.*`, `ctx.*`)
```python
# Cache: persistent across turns
cache.set(key, value)
cache.get(key)
cache.has(key)
cache.delete(key)
cache.clear()

# Context: ephemeral state per turn
ctx.set(key, value)
ctx.get(key)
ctx.has(key)
ctx.delete(key)
ctx.clear()
ctx.snapshot()  # Returns dict of all ctx keys
```

### Metrics (`metrics.*`)
```python
metrics.incr(name, delta=1)      # Increment counter
metrics.get(name)                # Get counter value
metrics.reset(name)              # Reset counter to 0
metrics.snapshot()               # Returns dict of all metrics
```

### Strings (`string.*`)
```python
string.upper(s)
string.lower(s)
string.trim(s)
string.split(s, sep)
string.join(items, sep)
string.truncate(s, max_len, suffix="...")
string.pad_left(s, width, char=" ")
string.pad_right(s, width, char=" ")
```

### Template (`template.*`)
```python
template.render(tmpl, vars)
# Example:
tmpl = "Hello {{name}}, you have {{count}} messages"
output = template.render(tmpl, {"name": "Alice", "count": 5})
# Returns: "Hello Alice, you have 5 messages"
```

### Validation (`validate.*`)
```python
validate.email(email_string)     # Returns True/False
validate.url(url_string)         # Returns True/False
validate.json(json_string)       # Returns True/False
```

### Sets (`set.*`)
```python
s = set.new(["a", "b", "c"])
set.contains(s, "a")             # Returns True
set.union(s1, s2)                # Union of two sets
set.intersect(s1, s2)            # Intersection
set.diff(s1, s2)                 # Difference (s1 - s2)
set.values(s)                    # Returns list of values
set.size(s)                      # Returns count
```

### Regex (`re.*`)
```python
re.match(pattern, string)        # Returns True if match
re.find_all(pattern, string)     # Returns list of matches
re.replace(pattern, repl, string)# Replace matches
```

### Core Utilities
```python
time.now()                       # Current Unix timestamp
env(key)                         # Get environment variable
json.encode(obj)                 # JSON stringify
json.decode(string)              # JSON parse
log(msg)                         # Write to harness log
uuid.v4()                        # Generate UUID
random(min, max)                 # Random integer
sleep(ms)                        # Sleep for milliseconds
assert(condition, msg="")        # Assertion
emit(event_name, payload)        # Fire custom hook event
```

---

## Hook Actions

Inside hook scripts, you can control execution flow:

```python
def handle(event, payload):
    # Allow the operation to proceed
    return allow()
    
    # Block the operation with a reason
    return block("Reason why this is blocked")
    
    # Modify the payload before continuing
    return modify({"arguments": {"new": "value"}})
```

---

## Hook Events

| Event | Fires When | Blockable | Payload Keys |
|-------|------------|-----------|--------------|
| `session.start` | Agent session begins | No | - |
| `session.end` | Agent session ends | No | - |
| `tool.pre` | Before any tool executes | **Yes** | `tool_name`, `arguments` |
| `tool.post` | After tool returns | Modify | `tool_name`, `arguments`, `result` |
| `completion.pre` | Before LLM call | **Yes** | `messages`, `model` |
| `completion.post` | After LLM returns | Modify | `messages`, `response` |
| `delegation.pre` | Before spawning sub-agent | **Yes** | `agent_name`, `prompt` |
| `delegation.post` | After sub-agent returns | Modify | `agent_name`, `result` |
| `meta.register_tool` | Runtime tool registration | **Yes** | `name`, `description` |
| `meta.register_hook` | Runtime hook registration | **Yes** | `event`, `handler` |
| `meta.register_agent` | Runtime agent registration | **Yes** | `name` |
| `meta.call_tool` | Programmatic tool invocation | **Yes** | `tool_name`, `arguments` |
| `error` | Any error occurs | No | `error`, `source` |

---

## Tool Parameter Types

When defining tools, use these parameter types:

| Type | Description | Example |
|------|-------------|---------|
| `string` | Text value | `"hello"` |
| `number` | Integer or float | `42`, `3.14` |
| `boolean` | True/false | `true`, `false` |
| `array` | List of values | `["a", "b", "c"]` |
| `object` | Dictionary/map | `{"key": "value"}` |

---

## Conditional Loading (Starlark Expressions)

Use the `condition` field in frontmatter to control when an artifact is active:

```yaml
# Load only in private repositories
condition: 'ctx.get("repo_visibility") == "private"'

# Load only during business hours (8 AM - 6 PM)
condition: '8 <= time.now() % 86400 / 3600 < 18'

# Load only for Python files
condition: 'ctx.get("file_ext") == ".py"'

# Load only if a feature flag is enabled
condition: 'ctx.get("feature_code_review") == True'

# Combine conditions
condition: 'ctx.get("mode") == "review" and ctx.get("lang") == "go"'
```

**Conditions are evaluated every turn.** Artifacts become active/inactive dynamically based on runtime state.

---

## Common Patterns

### 1. Create a Governance Hook
```markdown
---
name: command-guard
type: plugin
---

# Command Guard

Blocks dangerous shell commands.

## Hooks

```yaml
- event: tool.pre
  handler: command_guard
  priority: 10
  when: 'tool_name == "exec"'
  script: |
    def handle(event, payload):
        cmd = payload.get("arguments", {}).get("cmd", "")
        dangerous = ["rm -rf", "dd if=", "> /dev/"]
        for pattern in dangerous:
            if pattern in cmd:
                return block("Refusing destructive command: " + cmd)
        return allow()
```
\```

### 2. Create a Tool with Validation
```markdown
---
name: create-task
type: plugin
---

# Task Creator

Creates validated task entries.

## Tools

```yaml
- name: create_task
  description: "Create a new task with validation"
  parameters:
    title:
      type: string
      required: true
      description: "Task title"
    priority:
      type: string
      required: false
      description: "Priority: low, medium, high"
  script: |
    def run(args):
        title = args.get("title", "")
        priority = args.get("priority", "medium")
        
        # Validation
        if len(title) < 3:
            return {"error": "Title must be at least 3 characters"}
        
        if priority not in ["low", "medium", "high"]:
            return {"error": "Invalid priority"}
        
        # Create task
        task_id = uuid.v4()
        task = {"id": task_id, "title": title, "priority": priority}
        
        # Save to cache
        cache.set("task:" + task_id, json.encode(task))
        
        return {"success": True, "task_id": task_id}
```
\```

### 3. Create Conditional Context
```markdown
---
name: pr-mode
type: plugin
condition: 'ctx.get("mode") == "pull_request"'
---

# PR Review Mode

Activated automatically when reviewing pull requests.

## Rules

- Always use `create_pr` tool for pull request operations
- Review all changed files before approving
- Check CI status before suggesting merge
- Never force-push to shared branches
- Add reviewers based on CODEOWNERS

## Tools

```yaml
- name: review_pr
  description: "Review a pull request"
  parameters:
    pr_number: { type: number, required: true }
  script: |
    def run(args):
        # Implementation here
        return {"status": "reviewed"}
```
\```

---

## Next Steps

1. **Copy an example** into your `.harness/` directory
2. **Modify** the frontmatter and script to match your needs
3. **Run `harness validate`** to check for errors
4. **Run `harness run`** to test your agent
5. **Use `harness context`** to inspect what's loaded
6. **Use `harness tools`** to list available tools
7. **Use `harness hooks`** to see active hooks

---

## Learn More

- **Product Spec:** See `data/specs/ai-harness-product-spec.md` for architecture details
- **Roadmap:** See `data/specs/ai-harness-roadmap.md` for planned features
- **Evals:** Run `make eval` to test against real models
- **Issues:** https://github.com/htekdev/ai-harness/issues

---

## Philosophy

> "Keep the core tiny. Make the edges powerful."

AI Harness is **not** a mega-framework. It's a minimal runtime that:
- **Runs** your tools and hooks deterministically
- **Composes** artifacts in priority order
- **Evaluates** conditions every turn
- **Observes** context for debugging
- **Tests** agent behavior in CI

**You own the edges.** Define your tools, hooks, and policies as code. The harness executes them safely.

Happy harnessing! 🚀
