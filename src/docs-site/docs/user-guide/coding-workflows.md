---
sidebar_position: 2
title: Coding Workflows
description: Guide your AI assistant through multi-step coding tasks with intelligent workflows and mid-execution modifications.
---

# Coding Workflows

AI Studio includes intelligent coding workflows that break down complex tasks into structured steps. These workflows understand your intent and guide the AI through debugging, testing, implementation, and more—all while staying responsive to your feedback.

## What are Coding Workflows?

When you ask AI Studio to perform a coding task, it automatically detects the type of work and creates a multi-step workflow tailored to that task:

- **Implementation**: Design → Generate Code → Review → Test
- **Debugging**: Reproduce Issue → Analyze → Fix → Verify
- **Testing**: Write Tests → Run Tests → Fix Failures → Verify Coverage
- **Refactoring**: Analyze Structure → Plan Changes → Refactor → Validate
- **Documentation**: Analyze Code → Generate Docs → Review → Update
- **Architecture**: Research Patterns → Design → Document → Review

Each workflow automatically handles the progression through steps, retries when needed, and ensures quality at each stage.

## Making Quick Adjustments During Execution

As a workflow executes, you can guide the AI with **simple additive modifications** by typing and sending a message while the workflow is running.

### When to Use Mid-Execution Modifications

✅ **Good use cases for interrupting:**
- **Adding considerations**: "Also add error handling"
- **Including extra details**: "But make sure to include input validation"
- **Remembering forgotten items**: "Don't forget to update the tests"
- **Making small adjustments**: "Include documentation comments"
- **Adding best practices**: "Consider edge cases for null values"

These interruptions work best when you want to **enhance or refine** what the AI is already doing without changing the fundamental direction.

### How It Works

1. **AI Studio starts executing** a workflow (e.g., implementing a login form)
2. **You realize you want to add something** (e.g., "Also add error handling")
3. **Start typing in the input field** while the workflow is running
   - The button icon switches from ❌ (stop) to → (send) as you type
   - The input border turns orange to indicate interrupt mode
4. **Press Enter or click the → button** to send your modification
   - Your message appears with a ↪️ badge in the chat history
   - The current step receives your feedback automatically
5. **The workflow adapts** to include your modification in the current or next step

### Example: Adding Error Handling

```
You: Generate a login form component

AI Studio: [Starts Implementation Workflow]
          Step 1: Designing component structure...

You: [types while running] "Also add error handling for network failures"
     [Icon changes from ❌ to →]

AI Studio: [Receives modification]
          ↪️ Modification received: "Also add error handling for network failures"
          Step 2: Generating code with error handling included...
```

The AI automatically incorporates your feedback into its current work without restarting from scratch.

## When to Stop and Start Fresh

For **major direction changes**, it's better to stop the current workflow and start a new one:

❌ **Not ideal for mid-execution modification:**
- **Changing the task type**: "No, write tests first instead"
- **Switching directions**: "Actually, let's refactor the existing code instead"
- **Major scope changes**: "Wait, let's redesign the entire authentication system"
- **Different approach**: "Stop, let's use a different framework"

### How to Stop and Redirect

1. **Click the ❌ button** (when no text is typed) to stop the current workflow
2. **Send a fresh prompt** with your new direction
3. **AI Studio classifies the new task** and starts the appropriate workflow

This gives you clean separation between different tasks and ensures each workflow can focus on a single clear objective.

### Example: Changing Task Type

```
You: Implement user authentication

AI Studio: [Starts Implementation Workflow]
          Step 1: Designing authentication flow...

You: [Clicks ❌ to stop]
     [Workflow stops cleanly]

You: Actually, let's write comprehensive tests for the existing auth code first

AI Studio: [Classifies as Testing task]
          [Starts Testing Workflow]
          Step 1: Analyzing existing code...
```

This approach keeps workflows focused and prevents confusion between different objectives.

## Visual Indicators

AI Studio provides clear visual feedback for interrupt mode:

| State | Button Icon | Input Border | Behavior |
|-------|-------------|--------------|----------|
| **Executing (no text)** | ❌ | Normal | Click to stop workflow |
| **Executing + typing** | → | Orange | Send to add modification |
| **Idle** | → | Normal | Send new message |

The ↪️ badge in chat history marks messages that modified an active workflow.

## Best Practices

### ✅ Do:
- Use interruptions for **additive refinements** ("also", "but", "include")
- Keep modifications **focused and specific**
- Let workflows complete when possible for best results
- Use stop + new prompt for **major changes**

### ❌ Avoid:
- Using interruptions to **completely change direction**
- Sending **multiple rapid modifications** (let each one be incorporated first)
- Interrupting for **questions** (these work better as separate messages after completion)
- Overriding **core workflow logic** (trust the workflow's structure)

## Common Modification Keywords

AI Studio detects these keywords to understand your intent:

**Additive modifications:**
- "Also..." → Add something extra
- "But..." → Add a constraint or consideration  
- "Include..." → Make sure to add
- "Remember..." → Don't forget to add
- "Consider..." → Take into account
- "Make sure..." → Ensure something is included

These keywords help the AI understand you're enhancing the current work, not redirecting it.

## Workflow Quality and Memory

Each workflow:
- **Maintains context** across all steps
- **Remembers modifications** you've made
- **Accumulates feedback** to improve subsequent steps
- **Validates results** at key checkpoints
- **Preserves your preferences** for future tasks

Your interruptions become part of the workflow's memory, ensuring consistent behavior throughout the task.

## FAQ

**Q: What happens if I interrupt multiple times?**  
A: Each modification is accumulated and injected into subsequent steps. The workflow keeps track of all your feedback.

**Q: Can I undo an interruption?**  
A: Not directly. If you need to change direction significantly, stop the workflow and start fresh with a new prompt.

**Q: Will interruptions slow down the workflow?**  
A: Minimal impact (< 200ms). The detection and injection happen automatically without extra LLM calls.

**Q: What if my keyword isn't detected?**  
A: The system looks for common modification patterns. If unsure, be explicit: "Also add..." or "Include..."

**Q: Can I interrupt during any step?**  
A: Yes. Modifications are queued and incorporated at the next available step transition.

---

## Workflow Undo System 🚀

AI Studio includes an **industry-leading workflow undo system** that treats each workflow run as a transaction you can intelligently search and restore.

### Why This Matters

Unlike traditional undo (Ctrl+Z) or git revert, the Workflow Undo System:

- ✅ **Understands your codebase**: Search by application component ("auth middleware", "database layer")
- ✅ **Combines time + relevance**: Recent changes rank higher, but old relevant changes still surface
- ✅ **Preserves workflow context**: Know *why* changes were made, not just *what* changed
- ✅ **No git pollution**: Experimental workflows don't clutter your commit history
- ✅ **Multi-day history**: 30+ days of searchable workflow history

### Natural Language Search

Find past workflows using plain English:

```bash
# Semantic search with time ranking
$ sop workflow find "authentication middleware"

Found 5 matching workflows (ranked by recency + relevance):

1. [Jul 27, 14:30] ⭐ Add JWT validation          (score: 0.95)
   Component: Authentication System → Middleware
   Files: auth/middleware.go, api/handler.go
   Changes: +45 -12 lines
   
2. [Jul 26, 10:15] ⭐ Fix session timeout bug     (score: 0.88)
   Component: Authentication System → Middleware
   Files: auth/middleware.go
   Changes: +8 -3 lines

Restore to which point? (1-2, or 'browse'): _
```

### How It Works

**Automatic Transaction Recording**:
1. Each workflow run is captured as a transaction
2. Files are backed up before modification
3. Code artifacts (functions, classes) are extracted via AST analysis
4. Transaction is indexed in multiple dimensions:
   - **Date** (exact filtering)
   - **Code artifacts** (semantic search)
   - **Workflow metadata** (type, tags, description)

**Intelligent Ranking**:
- Combines **semantic relevance** (how well it matches your query)
- With **time recency** (recent changes rank higher)
- Default: 70% relevance + 30% recency (configurable)

### Common Workflows

**Find recent changes to a component:**
```bash
$ sop workflow find "parser engine" --date today
```

**Restore to a specific point:**
```bash
$ sop workflow restore txn_20260727_143022
```

**Preview before restoring:**
```bash
$ sop workflow restore txn_20260727_143022 --dry-run
```

**Selective file restore:**
```bash
$ sop workflow restore txn_20260727_143022 --files-only auth/middleware.go
```

**Browse by component:**
```bash
$ sop workflow list --component "Authentication System"
```

### Time-Ranked Results

Results are automatically ranked to show the most relevant recent changes first:

- **Today's changes**: Get maximum boost (recency = 1.0)
- **This week**: Moderate boost (recency = 0.5)
- **Older changes**: Minimal boost, but still surface if highly relevant

**Example**: Searching "auth middleware" returns today's JWT changes first, but also surfaces a highly relevant authentication refactor from last month if it's semantically similar.

### Multi-Dimensional Index

Each transaction is accessible via multiple paths:

```
Date Path:        2026-07-27 → "Add JWT validation"
Component Path:   Authentication System → Middleware → "Add JWT validation"
Type Path:        security_enhancement → "Add JWT validation"
```

This enables both:
- Fast date lookups: "What changed today?"
- Semantic searches: "Authentication middleware changes"

### Benefits for Developers

**Time Savings**:
- 10-30 minutes per undo operation (vs. manual git bisect)
- 2-5 hours per week for frequent experimenters
- 100+ hours per year

**Confidence Boost**:
- Experiment freely—undo is one semantic query away
- No fear of breaking working code
- Try multiple approaches in parallel

**Context Preservation**:
- Understand why changes were made
- See full workflow context
- Learn from past attempts

### Storage & Retention

- Default: 30 days of history
- Automatic cleanup with milestone protection
- ~48 KB per typical transaction
- Compressed backups for older changes

### Learn More

For complete technical details, see the [Workflow Undo System documentation](../../../../docs/WORKFLOW_UNDO_SYSTEM.md).

---

## Summary

Coding workflows in AI Studio provide structured, intelligent task execution with the flexibility to make **simple additive modifications** on the fly. Use interruptions for quick refinements and enhancements, but stop and restart for major direction changes. This balance keeps workflows focused while staying responsive to your needs.
