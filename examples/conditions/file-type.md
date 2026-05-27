---
name: file-type-context
type: plugin
version: "1.0.0"
description: "Conditional context based on file type being edited"
author: "Harness as Code"
tags: ["conditional", "file-type", "context"]
---

# File Type Conditional Loading

**Conditional artifacts** that load based on the **file extension** being worked on.

## What This Does

- Loads language-specific context when working on certain file types
- Activates/deactivates automatically based on `ctx.get("file_ext")`
- Provides file-type-specific tools and rules

## How to Set File Extension Context

The harness runtime should set `ctx.set("file_ext", ".py")` when editing a Python file. You can also set it manually:

```python
# In a tool or at session start
ctx.set("file_ext", ".go")
ctx.set("file_ext", ".py")
ctx.set("file_ext", ".js")
```

## Example: Python Context

```markdown
---
name: python-context
type: plugin
condition: 'ctx.get("file_ext") == ".py"'
---

# Python Development Context

You're working on Python code.

## Rules

- Follow PEP 8 style guide
- Use type hints for function signatures
- Prefer f-strings over % or .format()
- Use `pathlib` instead of `os.path`
- Write docstrings for public functions (Google style)

## Common Patterns

```python
# Type hints
def process_data(items: list[str]) -> dict[str, int]:
    pass

# F-strings
name = "World"
print(f"Hello, {name}!")

# Pathlib
from pathlib import Path
config_file = Path("config.yaml")
```

## Tools

```yaml
- name: run_python
  description: "Run Python script"
  script: |
    def run(args):
        result = exec.run("python3", [args.get("file")])
        return result
```
\```

## Example: Go Context

```markdown
---
name: go-context
type: plugin
condition: 'ctx.get("file_ext") == ".go"'
---

# Go Development Context

You're working on Go code.

## Rules

- Follow Go idioms and conventions
- Use `gofmt` for formatting
- Handle errors explicitly (no ignored errors)
- Use short variable names in small scopes
- Prefer interfaces for abstractions

## Common Patterns

```go
// Error handling
if err != nil {
    return fmt.Errorf("failed to process: %w", err)
}

// Interface definition
type Reader interface {
    Read(p []byte) (n int, err error)
}

// Struct with methods
type User struct {
    Name string
    Age  int
}

func (u *User) IsAdult() bool {
    return u.Age >= 18
}
```

## Tools

```yaml
- name: go_test
  description: "Run Go tests"
  script: |
    def run(args):
        result = exec.run("go", ["test", "-v", "./..."])
        return result

- name: go_fmt
  description: "Format Go code"
  script: |
    def run(args):
        result = exec.run("gofmt", ["-w", "."])
        return result
```
\```

## Example: JavaScript/TypeScript Context

```markdown
---
name: javascript-context
type: plugin
condition: 'ctx.get("file_ext") in [".js", ".ts", ".jsx", ".tsx"]'
---

# JavaScript/TypeScript Development Context

You're working on JavaScript or TypeScript code.

## Rules

- Use ES6+ features (arrow functions, destructuring, async/await)
- Prefer `const` over `let`, never use `var`
- Use TypeScript types when available
- Handle promises with async/await, not .then()
- Use strict equality (`===`) not loose equality (`==`)

## Common Patterns

```javascript
// Arrow functions
const add = (a, b) => a + b;

// Destructuring
const {name, age} = user;
const [first, ...rest] = items;

// Async/await
async function fetchData() {
    try {
        const response = await fetch(url);
        const data = await response.json();
        return data;
    } catch (error) {
        console.error('Failed to fetch:', error);
    }
}
```

## Tools

```yaml
- name: npm_test
  description: "Run npm tests"
  script: |
    def run(args):
        result = exec.run("npm", ["test"])
        return result

- name: eslint
  description: "Run ESLint"
  script: |
    def run(args):
        result = exec.run("npx", ["eslint", "."])
        return result
```
\```

## Multi-Extension Conditions

### Any Web File (HTML, CSS, JS)

```yaml
condition: 'ctx.get("file_ext") in [".html", ".css", ".js", ".jsx"]'
```

### Any Config File (YAML, JSON, TOML)

```yaml
condition: 'ctx.get("file_ext") in [".yaml", ".yml", ".json", ".toml"]'
```

### Any Script File

```yaml
condition: 'ctx.get("file_ext") in [".sh", ".bash", ".zsh", ".fish"]'
```

## Setting File Extension Automatically

### In Session Start Hook

```yaml
hooks:
  - event: session.start
    handler: detect_file_type
    script: |
      def handle(event, payload):
          # Get current file from context
          current_file = ctx.get("current_file", "")
          
          if current_file:
              # Extract extension
              if "." in current_file:
                  ext = current_file[current_file.rfind("."):]
                  ctx.set("file_ext", ext)
                  log("Detected file type: " + ext)
          
          return allow()
```

### In Tool Pre-Hook

```yaml
hooks:
  - event: tool.pre
    handler: detect_file_from_args
    when: 'tool_name == "edit_file" or tool_name == "read_file"'
    script: |
      def handle(event, payload):
          args = payload.get("arguments", {})
          path = args.get("path", "")
          
          if "." in path:
              ext = path[path.rfind("."):]
              ctx.set("file_ext", ext)
          
          return allow()
```

## File Type Tool Example

```yaml
tools:
  - name: detect_file_type
    description: "Detect and set file type from path"
    parameters:
      path: { type: string, required: true }
    script: |
      def run(args):
          path = args.get("path", "")
          
          if not "." in path:
              return {"error": "No extension found in path"}
          
          ext = path[path.rfind("."):]
          ctx.set("file_ext", ext)
          
          # Map extension to language
          lang_map = {
              ".py": "Python",
              ".go": "Go",
              ".js": "JavaScript",
              ".ts": "TypeScript",
              ".rs": "Rust",
              ".java": "Java",
              ".cpp": "C++",
              ".rb": "Ruby",
          }
          
          lang = lang_map.get(ext, "Unknown")
          
          return {
              "success": True,
              "extension": ext,
              "language": lang
          }
```

## Related Examples

- See `examples/context/pr-mode.md` for mode-based conditions
- See `examples/context/time-based.md` for time-based conditions
- See `examples/conditions/repo-private.md` for repo visibility

## Learn More

- Conditional loading: `examples/README.md#conditional-loading-starlark-expressions`
- Context variables: `examples/README.md#state-cache-ctx`
- String operations: `examples/README.md#strings-string`
