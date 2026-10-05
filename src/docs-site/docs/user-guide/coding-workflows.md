---
sidebar_position: 2
title: Intelligent Coding
description: Northstar AI's Dynamic Planner and Phase system for intelligent multi-step coding tasks.
---

# Intelligent Coding with Northstar AI

Northstar AI uses the **LLM as Orchestrator** paradigm—the AI chooses how to approach your task. Through a **Dynamic Planner** and **phase-based execution system**, it analyzes your request, creates a plan, and adapts as the work develops.

### LLM-Guided Work

Northstar AI's LLM-guided approach provides:

- 🤖 **AI analyzes your task** and determines the best approach
- 🎯 **Dynamic planning** tailored to your specific situation
- 🔄 **Adaptive execution** that adjusts based on discoveries
- 💬 **Natural language control** throughout the process

You describe what you want, and the AI orchestrates everything—from task classification to tool selection to execution flow.

## Works Across Native and Remote Tool Surfaces

Northstar AI can work with both its native tools and connected editor tools.

In practice, that means Northstar AI can:

- read a remote agent's supported phases
- understand which tools are available in each phase
- align those remote phases to Northstar AI's own workflow model
- delegate a bounded step remotely
- keep orchestration, memory, and handoff inside Northstar AI

This lets the daemon-managed Cline SDK backend execute a bounded coding task while Northstar AI Ask keeps the request connected to its phases, memory, and workstream.

In the AI Studio chat window, use the **Preferred LLM** selector in the header to change the primary model for subsequent messages. Choose **Pool config** at the bottom of that selector to manage provider keys. The selected model remains the preferred primary while Northstar AI can still use eligible pool fallbacks for availability.

:::tip For Developers
Want to understand the technical architecture? See the SDK documentation:
- **[Workflow Architecture](../sdk/workflow-architecture)** - Dynamic Planner, Ask pipeline, participant pattern, conversational feedback
- **[Undo and Restore](../sdk/undo-system)** - Session-aware workstream history, semantic search, and recovery
:::

## How It Works: Dynamic Planner + Phase System

### Dynamic Planning 🤖

The **Dynamic Planner** is Northstar AI's intelligent task analyzer that:

- **Classifies your task** - Determines task type (debugging, implementation, testing, refactoring)
- **Generates a custom plan** - Creates phase sequence tailored to your specific request
- **Adapts to complexity** - Simple tasks get streamlined plans, complex tasks get comprehensive multi-phase approaches
- **Adjusts on the fly** - Modifies the plan based on your feedback and discoveries during execution

**How LLM Orchestration works:**
1. **You describe your coding task** in natural language
2. **LLM analyzes and classifies** - Task type, complexity, required capabilities
3. **Dynamic Planner generates custom plan** - Phase sequence tailored to your situation
4. **LLM orchestrates execution** - Selects tools, controls flow, adapts to findings
5. **Continuous reasoning** - LLM adjusts plan based on your feedback and discoveries

Every decision—from which phase to enter, which tools to use, and when to pivot—is guided by the LLM reasoning about your specific context.

The same reasoning guides recovery. If execution needs more information, Northstar AI can pause for clarification, stop when your attention is needed, or move back to an earlier phase when that is the best way forward.

**Example:**
```
You: Debug the authentication timeout issue

Northstar AI: Analyzing task...
          Task Type: Debugging
          Complexity: Medium
          
          Generated Plan:
          Phase 1: Investigation - Analyze session middleware
          Phase 2: Planning - Identify timeout configuration issues  
          Phase 3: Execution - Generate and apply fix
          Phase 4: Review - Verify with tests
          
          Starting Phase 1 (Investigation)...
```

### Phase-Based Execution

Northstar AI executes through **structured phases** that organize the work into clear stages:

**Standard Phase Sequence** (adapted per task):

1. **Investigation** — Inspect repository, files, tests, and gather context
   - Tools: `search_codebase`, `read_file`, `analyze_structure`
   - Focus: Understanding the problem and current state

2. **Planning** — Design the solution approach
   - Tools: `plan_design`, `analyze_dependencies`
   - Focus: Creating a concrete implementation strategy

3. **Execution** — Apply code changes
   - Tools: `generate_code`, `apply_patch`, `build_project`
   - Focus: Implementing the planned changes

4. **Review** — Validate changes
   - Tools: `run_tests`, `verify_build`
   - Focus: Ensuring changes work correctly

5. **Delivery** — Summarize and handoff
   - Tools: `document_changes`, `create_summary`
   - Focus: Providing clear description of what changed

**Phase Characteristics:**
- **Adaptive**: Not all tasks need all phases (simple fix might skip Planning)
- **Resumable**: Can pause and resume between phases
- **Revisitable**: Can return to earlier phase if new information emerges
- **Self-correcting**: Can move backward when the task is in the wrong phase or local execution is looping
- **Clarification-aware**: Can pause and wait for one missing fact instead of guessing
- **Stop-capable**: Can stop cleanly when the environment or task state requires user control
- **Tool-aware**: Each phase has access to relevant coding tools
- **Integration-aware**: A phase can use local tools, remote tools, or a mix when external systems describe their capabilities in metadata

### Recovery Within the Same Task

The phase system keeps the task connected while the work changes:

- if a task needs more evidence, the workflow can move back to Investigation
- if the design needs revision, it can move back to Planning
- if one missing fact blocks progress, it can pause for clarification
- if the situation is truly blocked, it can stop cleanly instead of continuing blindly

This keeps the conversation, artifacts, and decisions together as the task develops.

### Why This Is Also More Token-Efficient

The same phase system helps conserve tokens.

Northstar AI keeps each turn focused on the current phase and active domain, so the model can spend more of its prompt budget on the work that matters right now.

It also reduces unnecessary work:

- fewer wrong-tool attempts
- fewer blind retries in the wrong phase
- less wasted continuation when clarification is needed
- less need to repeat context when the task changes direction

The result is better output per token spent.

For the customer-facing comparison and positioning, see [Why Northstar AI Uses Fewer Tokens](../token-efficiency-advantage).

### Knowledge Base Templates (Optional)

For common, well-defined tasks, you can optionally use **team-defined templates** stored in Knowledge Bases:

- **Test Fixing**: Analyze failures → Isolate → Apply fix → Validate → Re-test
- **Refactoring**: Analyze structure → Plan changes → Apply → Verify
- **Feature Implementation**: Design → Generate code → Apply → Test

These are **pre-defined phase sequences** that encode your team's best practices. The Dynamic Planner can use these as starting points and adapt them to your specific situation.

## Multi-Language Support

Northstar AI workflows provide **comprehensive support for multiple programming languages**, automatically adapting behavior based on your project type.

### Supported Languages

The shared coding workflow stack currently supports the following languages for reading, analysis, and workflow/script generation:

| Language | Workflow support |
|----------|------------------|
| **Go** | ✅ Reader/analyzer + generation + test-aware workflow integration |
| **Python** | ✅ Reader/analyzer + generation + test-aware workflow integration |
| **JavaScript / TypeScript** | ✅ Reader/analyzer + generation + JSX/TSX-aware parsing and generation |
| **Java** | ✅ Reader/analyzer + generation + build/test-aware context |
| **Rust** | ✅ Reader/analyzer + generation + test-aware context |
| **C#** | ✅ Reader/analyzer + generation + AST parsing and symbol extraction |
| **HTML / CSS** | ✅ Reader/analyzer + generation for markup and styling workflows |
| **React** | ✅ Reader/analyzer + generation for component-oriented workflows |
| **Angular** | ✅ Reader/analyzer + generation for component/template workflows |

> In practice, the shared coding workflow stack exposes first-class support for these languages across the reader/analyzer path and the workflow/script generator layer.

### Automatic Language Detection

When you open a project, Northstar AI:

1. **Detects your project type** by scanning for marker files (e.g., `go.mod`, `package.json`, `Cargo.toml`)
2. **Configures language-specific tooling** for tests, code analysis, and context gathering
3. **Adapts workflow behavior** to use the appropriate commands and patterns

You don't need to configure anything—workflows automatically use the right tools for your language.

### Language-Specific Features

> Current status: the shared AST layer now supports C# parsing and symbol/import extraction for `.cs` files, and workflow code generation now supports Go, C#, and Python targets through the shared compiler layer. The same workflow definition can be emitted in multiple target languages, with additional backends added as needed.

**Test Execution**:
- Go: Smart test selection based on active file, runs `go test ./...`
- Python: Discovers tests with `pytest` or falls back to `unittest`
- Java: Uses `mvn test` (Maven) or `gradle test` (Gradle)
- Rust: Runs `cargo test` with workspace support
- JavaScript/TypeScript: Executes `npm test` or `yarn test` based on lock file
- C#: Runs `dotnet test` for all projects in solution

**Code Analysis**:
- Extracts functions, methods, classes, and types using tree-sitter AST parsing
- Language-specific symbol extraction (e.g., Python `def` + `class`, Java `public class`, Rust `fn` + `struct`)
- Intelligent import/dependency detection for each language

**Context Gathering**:
- Collects relevant symbols from source files
- Detects test files using language conventions (`*_test.go`, `test_*.py`, `*Test.java`, `*.test.ts`)
- Infers module/package names from language-specific manifest files

### Cross-Language Workflows

Northstar AI handles **polyglot projects** seamlessly:

- If your repository contains multiple languages, workflows adapt per-directory
- The undo system tracks code artifacts across all languages
- Semantic search works across Python, Go, Rust, and other codebases

### Example: Python Workflow

```
You: Debug the authentication middleware

Northstar AI: [Detects Python project from pyproject.toml]
          [Starts Debugging Workflow]
          
          Step 1: Analyzing Python code...
          - Found: authenticate(), check_token(), refresh_session()
          - Tests: test_authentication.py
          
          Step 2: Running pytest...
          $ pytest tests/test_authentication.py -v
          
          Step 3: Analyzing failures...
          [Uses Python AST to extract error context]
```

### Example: Rust Workflow

```
You: Implement error handling for the parser

Northstar AI: [Detects Rust project from Cargo.toml]
          [Starts Implementation Workflow]
          
          Step 1: Analyzing Rust code structure...
          - Found: parse_token(), lex_input()
          - Existing error types: ParseError, LexError
          
          Step 2: Generating Result-based error handling...
          [Uses Rust idioms: Result<T, E>, ? operator]
          
          Step 3: Running cargo test...
          $ cargo test --workspace
```

### Language-Specific Best Practices

Northstar AI applies **language-appropriate patterns** automatically:

- **Error handling**: `Result<T, E>` in Rust, exceptions in Python/Java, error returns in Go
- **Testing**: Property-based testing hints in Rust, unittest/pytest in Python, JUnit in Java
- **Code organization**: Modules in Rust/Python, packages in Java/Go
- **Naming conventions**: snake_case for Python/Rust, camelCase for JavaScript/TypeScript, PascalCase for C#

## Team Best Practices That Stay On

Northstar AI can go beyond generic language advice. You can store your own team practices in memory and have them automatically enrich coding asks.

For example, your team might define guidance such as:

- preserve public interfaces unless a breaking change is explicitly requested
- always add targeted tests for changed behavior
- use the repository’s existing error-wrapping and logging patterns
- return a clear modification summary for review

When you ask Northstar AI to implement or refactor something, those practices can be injected automatically into the overall Ask pipeline. You do not have to paste the same checklist into every prompt.

This is especially useful in real projects because the enrichment survives different execution paths:

- direct Northstar AI coding tools can use it
- multi-phase workflow planning can use it
- delegated Cline execution can use it

So a short request like:

```text
Add retry handling to the payment webhook client.
```

can still carry the team’s expectations automatically. That gives you more consistent output with less repetitive prompting.

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

1. **Northstar AI starts executing** a workflow (e.g., implementing a login form)
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

Northstar AI: Creating workflow...
          Step 1: Designing component structure...

You: [types while running] "Also add error handling for network failures"
     [Icon changes from ❌ to →]

Northstar AI: ↪️ Got it - adding network error handling
          Step 2: Generating code with error handling...
```

Your feedback gets incorporated automatically without restarting.

### Example: Custom Debugging Workflow

```
You: Find and fix the memory leak in the cache manager

Northstar AI: Creating custom workflow...
          
          Plan: "Fix Cache Memory Leak"
          → Analyze cache implementation
          → Check memory allocation
          → Generate fix
          → Verify with tests
          
          Step 1: Analyzing cache_manager.go...
          Found: initCache(), addEntry(), evictStale()
          Potential issue: evictStale may not free references
          
          Step 2: Checking allocations...
```

Northstar AI creates the right workflow for your specific situation.

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
3. **Northstar AI classifies the new task** and starts the appropriate workflow

This gives you clean separation between different tasks and ensures each workflow can focus on a single clear objective.

### Example: Changing Task Type

```
You: Implement user authentication

Northstar AI: [Starts Implementation Workflow]
          Step 1: Designing authentication flow...

You: [Clicks ❌ to stop]
     [Workflow stops cleanly]

You: Actually, let's write comprehensive tests for the existing auth code first

Northstar AI: [Classifies as Testing task]
          [Starts Testing Workflow]
          Step 1: Analyzing existing code...
```

This approach keeps workflows focused and prevents confusion between different objectives.

It also means you do not need to switch into a separate simplified workflow just because the task got messy. The same workflow can recover, revisit earlier phases, and continue once it has the right information.

## Visual Indicators

Northstar AI provides clear visual feedback for interrupt mode:

| State | Button Icon | Input Border | Behavior |
|-------|-------------|--------------|----------|
| **Executing (no text)** | ❌ | Normal | Click to stop workflow |
| **Executing + typing** | → | Orange | Send to add modification |
| **Idle** | → | Normal | Send new message |

The ↪️ badge in chat history marks messages that modified an active workflow.

## Best Practices

### ✅ Do:
- Use interruptions for **adding features** ("also add...", "include...")
- Keep modifications **focused and specific**
- Let workflows finish when you can
- Use stop + restart for **major direction changes**

### ❌ Avoid:
- Using interruptions to **completely change direction**
- Sending **multiple rapid changes** (give each one time to incorporate)
- Interrupting to **ask questions** (wait until the workflow finishes)

### When to Use What

**Let AI decide** - Most of the time, just describe what you want and Northstar AI picks the best approach automatically.

**Need something specific?** You can request:
```
"Use the planner to figure out this complex bug"
"Follow the team's testing workflow"
```

## Common Modification Keywords

Northstar AI detects these keywords to understand your intent:

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

## Undo and Restore

Northstar AI keeps searchable restore history connected to your sessions and workstreams. When a workstream changes files, the system records the surrounding context and the file changes so you can find and restore useful points in your work later.

### Search Your Workstream History

Use natural language to find earlier changes by topic, component, file area, or time:

```text
"Show me the authentication middleware changes from earlier"
"Find the parser fix from this morning"
"Restore the latest session timeout update"
```

Results combine meaning and recency, making it easier to find the change you remember without needing to know its exact file name.

### Restore What You Need

Northstar AI supports practical recovery options:

- restore a complete change set from a workstream
- restore selected files when only part of the work should be reversed
- preview a restore before applying it

Restore history also includes useful details such as changed files, summaries, repository areas, and related code artifacts.

### Learn More

See [Undo and Restore](../sdk/undo-system) for the SDK view of session-aware workstream history, semantic search, and recovery.

---

## Summary

Northstar AI's **LLM as Orchestrator** paradigm delivers truly intelligent coding assistance:

- **LLM-guided decisions** - Northstar AI reasons about the task and chooses the next step
- **Dynamic Planner** generates custom plans tailored to your specific task
- **Phase-based execution** organizes work into clear, manageable stages
- **Adaptive intelligence** adjusts approach based on discoveries and feedback
- **Real-time adjustments** let you guide the AI's reasoning as work progresses
- **Knowledge Base templates** (optional) provide proven patterns as starting points
- **Multi-language support** works seamlessly with Go, Python, Java, Rust, JavaScript, TypeScript, and C#
- **Transparent reasoning** shows you what Northstar AI understood and plans to do

Just describe what you want to accomplish. The LLM analyzes your task, reasons about the best approach, creates an appropriate phase plan, and uses the right tools. The result is an intelligent, adaptive coding experience that stays connected to your work.

---

## For Developers & SDK Users

This guide focuses on using Northstar AI from an end-user perspective. For SDK integration details:

- **[Workflow Architecture (SDK)](../sdk/workflow-architecture)** - Complete technical documentation covering:
   - Dynamic phase planning and the Ask pipeline
   - Workstream state and phase transitions
   - Capability integration and reusable components
   - Integration best practices and testing

- **[Undo and Restore (SDK)](../sdk/undo-system)** - Session-aware workstream history, semantic search, restore, and SDK integration
