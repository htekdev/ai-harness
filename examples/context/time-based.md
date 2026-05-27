---
name: time-based-context
type: plugin
version: "1.0.0"
description: "Conditional context based on time of day (business hours vs quiet mode)"
author: "Harness as Code"
tags: ["context", "time", "conditional", "business-hours"]
---

# Time-Based Context

**Conditional context** that changes agent behavior based on the time of day.

## What This Does

- Loads different context based on time of day
- **Business hours** (8 AM - 6 PM): Full capabilities, proactive
- **Quiet mode** (6 PM - 8 AM): Limited notifications, conservative
- Evaluated every turn based on current time

## Business Hours Context

**Condition:** `8 <= time.now() % 86400 / 3600 < 18`

**Explanation:**
- `time.now()` returns current Unix timestamp (seconds since epoch)
- `% 86400` gets seconds since midnight (modulo 24 hours)
- `/ 3600` converts to hours (0-24)
- `8 <= hour < 18` checks if between 8 AM and 6 PM

### Business Hours Rules

When active during business hours:

- **Be proactive** — suggest improvements and optimizations
- **Send notifications** — alerts, reminders, updates
- **Create tasks** — auto-generate action items
- **Run automation** — deploy, test, build
- **Make decisions** — approve PRs, merge branches

### Quiet Mode Rules

When active during quiet hours (6 PM - 8 AM):

- **Be conservative** — only critical operations
- **Minimal notifications** — errors and critical alerts only
- **No automation** — defer deploys and builds
- **No decisions** — wait for human approval
- **Batch updates** — collect and send in morning

## Artifact Examples

### Business Hours Artifact

```markdown
---
name: business-hours
type: plugin
condition: '8 <= time.now() % 86400 / 3600 < 18'
---

# Business Hours Mode

You're operating during business hours (8 AM - 6 PM).

## Rules

- Be proactive and suggest improvements
- Send notifications for important events
- Create tasks automatically
- Run automation freely
- Make autonomous decisions within autonomy level
```

### Quiet Mode Artifact

```markdown
---
name: quiet-mode
type: plugin
condition: 'time.now() % 86400 / 3600 < 8 or time.now() % 86400 / 3600 >= 18'
---

# Quiet Mode

You're operating during quiet hours (6 PM - 8 AM).

## Rules

- Be conservative — only critical operations
- Minimal notifications — errors and critical alerts only
- No deployments or automation
- Defer decisions until business hours
- Batch non-urgent updates
```

## Example: Testing Time Conditions

**Check current time:**
```python
hour = time.now() % 86400 / 3600
log("Current hour: " + str(hour))
```

**Verify which artifact is active:**
```bash
$ harness context --verbose
✅ business-hours (plugin, priority 40, ACTIVE)
   Condition: 8 <= time.now() % 86400 / 3600 < 18 → True
⚪ quiet-mode (plugin, priority 40, INACTIVE)
   Condition: time.now() % 86400 / 3600 < 8 or ... → False
```

## Time-Based Tool Example

### Send Notification (Time-Aware)

```yaml
tools:
  - name: send_notification
    description: "Send a notification (respects quiet mode)"
    parameters:
      message: { type: string, required: true }
      priority: { type: string, required: false }  # low, medium, high, critical
    script: |
      def run(args):
          message = args.get("message", "")
          priority = args.get("priority", "medium")
          
          # Get current hour
          hour = time.now() % 86400 / 3600
          is_quiet_mode = hour < 8 or hour >= 18
          
          # During quiet mode, only send critical notifications
          if is_quiet_mode and priority != "critical":
              log("Deferred notification to business hours: " + message)
              cache.set("deferred:" + uuid.v4(), json.encode({
                  "message": message,
                  "priority": priority,
                  "deferred_at": time.now()
              }))
              return {"deferred": True, "message": "Notification queued for business hours"}
          
          # Send immediately
          log("Sending notification: " + message)
          return {"sent": True, "message": message}
```

## Common Time Patterns

### Weekend Detection

```yaml
# Load only on weekends (Saturday = 6, Sunday = 0)
# Note: This requires a more complex calculation with date libraries
# For now, this is a placeholder pattern
condition: 'ctx.get("is_weekend") == True'
```

### Night Owl Mode (10 PM - 6 AM)

```yaml
condition: 'time.now() % 86400 / 3600 >= 22 or time.now() % 86400 / 3600 < 6'
```

### Morning Briefing Window (6 AM - 9 AM)

```yaml
condition: '6 <= time.now() % 86400 / 3600 < 9'
```

### Lunch Break (12 PM - 1 PM)

```yaml
condition: '12 <= time.now() % 86400 / 3600 < 13'
```

## Timezone Considerations

**Important:** `time.now()` returns UTC time. To use local time:

1. **Set timezone offset in context:**
   ```python
   ctx.set("timezone_offset_hours", -6)  # Central Time (UTC-6)
   ```

2. **Adjust in conditions:**
   ```yaml
   condition: '8 <= (time.now() + ctx.get("timezone_offset_hours") * 3600) % 86400 / 3600 < 18'
   ```

## Customization Ideas

### Send Daily Summary at 5 PM

```python
hour = time.now() % 86400 / 3600
if hour >= 17 and hour < 17.1:  # 5:00-5:06 PM window
    # Generate and send daily summary
    pass
```

### Rate Limit Based on Time

```python
# Higher rate limit during business hours
hour = time.now() % 86400 / 3600
if 8 <= hour < 18:
    max_requests = 100
else:
    max_requests = 10
```

## Related Examples

- See `examples/context/pr-mode.md` for mode-based conditions
- See `examples/conditions/file-type.md` for file detection
- See `examples/README.md#conditional-loading-starlark-expressions`

## Learn More

- Time functions: `examples/README.md#core-utilities`
- Conditional loading: `examples/README.md#conditional-loading-starlark-expressions`
- Common patterns: `examples/README.md#common-patterns`
