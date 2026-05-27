---
name: create-task
type: plugin
version: "1.0.0"
description: "Create validated task entries with metadata"
author: "Harness as Code"
tags: ["tasks", "validation", "tools", "state"]
---

# Create Task Tool

A tool that **creates validated task entries** with metadata, due dates, and persistence.

## What This Does

- Creates task entries with validation
- Stores tasks in the cache (persistent across turns)
- Auto-generates task IDs
- Validates priority, assignee, and due date fields
- Returns structured task object

## Use Cases

- Task management systems
- TODO tracking
- Issue creation
- Action item capture
- Workflow automation

## How to Use

1. Copy this file to `.harness/tools/create-task.md`
2. Run `harness validate` to confirm it loads
3. Run `harness tools --verbose` to see it in the registry
4. Call it: "Create a task to review the PR with high priority"

## Tools

```yaml
- name: create_task
  description: "Create a new task with validation and metadata"
  parameters:
    title:
      type: string
      required: true
      description: "Task title (required, min 3 characters)"
    description:
      type: string
      required: false
      description: "Task description or details"
    priority:
      type: string
      required: false
      description: "Priority: low, medium, high, critical (default: medium)"
    assignee:
      type: string
      required: false
      description: "Person assigned to this task"
    due_date:
      type: string
      required: false
      description: "Due date in YYYY-MM-DD format"
    tags:
      type: array
      required: false
      description: "Tags for categorization"
  timeout_ms: 5000
  script: |
    def run(args):
        # Extract arguments
        title = args.get("title", "")
        description = args.get("description", "")
        priority = args.get("priority", "medium")
        assignee = args.get("assignee", "")
        due_date = args.get("due_date", "")
        tags = args.get("tags", [])
        
        # Validation: Title
        if not title:
            return {"error": "Title is required"}
        
        if len(title) < 3:
            return {"error": "Title must be at least 3 characters"}
        
        # Validation: Priority
        valid_priorities = ["low", "medium", "high", "critical"]
        if priority not in valid_priorities:
            return {"error": "Invalid priority. Must be one of: " + string.join(valid_priorities, ", ")}
        
        # Validation: Due Date (basic format check)
        if due_date and not re.match("[0-9]{4}-[0-9]{2}-[0-9]{2}", due_date):
            return {"error": "Invalid due_date format. Use YYYY-MM-DD"}
        
        # Generate task ID
        task_id = uuid.v4()
        
        # Create task object
        task = {
            "id": task_id,
            "title": title,
            "description": description,
            "priority": priority,
            "assignee": assignee,
            "due_date": due_date,
            "tags": tags,
            "status": "open",
            "created_at": time.now()
        }
        
        # Save to cache (persistent across turns)
        cache_key = "task:" + task_id
        cache.set(cache_key, json.encode(task))
        
        # Also save to task index for querying
        task_list = []
        if cache.has("task_index"):
            task_list = json.decode(cache.get("task_index"))
        
        task_list.append(task_id)
        cache.set("task_index", json.encode(task_list))
        
        # Increment task counter metric
        metrics.incr("tasks_created")
        
        return {
            "success": True,
            "task": task,
            "message": "Task created successfully"
        }
```

## Example Usage

**Agent prompt:**
```
Create a task to review the authentication module with high priority, due next Friday
```

**Tool call:**
```json
{
  "tool": "create_task",
  "arguments": {
    "title": "Review authentication module",
    "description": "Security review of auth flow and token handling",
    "priority": "high",
    "due_date": "2026-06-05",
    "tags": ["security", "review", "auth"]
  }
}
```

**Tool response:**
```json
{
  "success": true,
  "task": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Review authentication module",
    "description": "Security review of auth flow and token handling",
    "priority": "high",
    "assignee": "",
    "due_date": "2026-06-05",
    "tags": ["security", "review", "auth"],
    "status": "open",
    "created_at": 1717200000
  },
  "message": "Task created successfully"
}
```

## Error Examples

**Missing title:**
```json
{
  "error": "Title is required"
}
```

**Invalid priority:**
```json
{
  "error": "Invalid priority. Must be one of: low, medium, high, critical"
}
```

**Invalid date format:**
```json
{
  "error": "Invalid due_date format. Use YYYY-MM-DD"
}
```

## Companion Tools

You'll probably want to create these companion tools:

### List Tasks

```python
def run(args):
    if not cache.has("task_index"):
        return {"tasks": []}
    
    task_ids = json.decode(cache.get("task_index"))
    tasks = []
    
    for task_id in task_ids:
        cache_key = "task:" + task_id
        if cache.has(cache_key):
            task = json.decode(cache.get(cache_key))
            tasks.append(task)
    
    return {"tasks": tasks, "count": len(tasks)}
```

### Complete Task

```python
def run(args):
    task_id = args.get("task_id", "")
    cache_key = "task:" + task_id
    
    if not cache.has(cache_key):
        return {"error": "Task not found"}
    
    task = json.decode(cache.get(cache_key))
    task["status"] = "completed"
    task["completed_at"] = time.now()
    
    cache.set(cache_key, json.encode(task))
    metrics.incr("tasks_completed")
    
    return {"success": True, "task": task}
```

### Delete Task

```python
def run(args):
    task_id = args.get("task_id", "")
    cache_key = "task:" + task_id
    
    if not cache.has(cache_key):
        return {"error": "Task not found"}
    
    # Remove from cache
    cache.delete(cache_key)
    
    # Remove from index
    task_ids = json.decode(cache.get("task_index"))
    task_ids = [id for id in task_ids if id != task_id]
    cache.set("task_index", json.encode(task_ids))
    
    return {"success": True, "message": "Task deleted"}
```

## Customization

### Add Auto-Assignee

```python
# Auto-assign based on tags
if "security" in tags and not assignee:
    assignee = "security-team"
```

### Add Status Transitions

```python
valid_statuses = ["open", "in_progress", "blocked", "completed"]
```

### Add Task Templates

```python
templates = {
    "bug": {"priority": "high", "tags": ["bug"]},
    "feature": {"priority": "medium", "tags": ["feature"]},
}

template_name = args.get("template", "")
if template_name in templates:
    template = templates[template_name]
    priority = template["priority"]
    tags = template["tags"]
```

## Related Tools

- See `examples/tools/read-file.md` for file operations
- See `examples/tools/search-code.md` for code search
- See `examples/README.md#state-cache-ctx` for cache/state functions

## Learn More

- State management: `examples/README.md#state-cache-ctx`
- Validation patterns: `examples/README.md#validation-validate`
- Common patterns: `examples/README.md#common-patterns`
