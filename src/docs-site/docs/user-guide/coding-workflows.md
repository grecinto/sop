---
sidebar_position: 2
title: Coding Workflows
description: Guide your AI assistant through multi-step coding tasks with intelligent workflows and mid-execution modifications.
---

# Coding Workflows

AI Studio breaks down complex coding tasks into clear, manageable steps. Just describe what you want, and it figures out the best way to get there—whether that's debugging a tricky issue, implementing a feature, or refactoring code.

:::tip For Developers
Want to understand the technical architecture? See the SDK documentation:
- **[Workflow Architecture](../sdk/workflow-architecture)** - Dynamic Planner, Ask pipeline, participant pattern, OnFinish hooks
- **[Undo System](../sdk/undo-system)** - Transaction management, semantic search, multi-dimensional indexing
:::

## How It Works

### Smart Planning 🤖

AI Studio analyzes your request and creates a custom plan:

- **Understands your task** - Figures out what type of work you need (debugging, implementation, testing, etc.)
- **Creates a step-by-step plan** - Breaks down complex tasks into clear actions
- **Adapts to your codebase** - Uses what it knows about your project structure and patterns
- **Stays flexible** - Adjusts the plan as you provide feedback

**Example:**
```
You: Debug the authentication timeout issue

AI Studio: Creating workflow...
          
          Plan: "Fix Authentication Timeout"
          1. Analyze session middleware code
          2. Check timeout configuration
          3. Generate and apply fix
          4. Verify with tests
          
          Starting Step 1...
```

### Using Team Templates 📚

For common tasks, AI Studio can follow proven patterns your team has defined:

- **Debugging**: Analyze → Generate Fix → Apply → Verify
- **Testing**: Run Tests → Analyze Failures → Fix → Re-test
- **Refactoring**: Analyze Structure → Plan Changes → Apply
- **Implementation**: Design → Generate Code → Apply → Test

These templates ensure consistency and let you encode best practices that everyone follows.

## Multi-Language Support

AI Studio workflows provide **comprehensive support for multiple programming languages**, automatically adapting behavior based on your project type.

### Supported Languages

| Language | Test Execution | Code Analysis | Context Gathering | Project Detection |
|----------|----------------|---------------|-------------------|-------------------|
| **Go** | ✅ `go test` | ✅ AST-based | ✅ Full | `go.mod` |
| **Python** | ✅ `pytest` / `unittest` | ✅ AST-based | ✅ Full | `requirements.txt`, `setup.py`, `pyproject.toml` |
| **Java** | ✅ `mvn test` / `gradle test` | ✅ AST-based | ✅ Full | `pom.xml`, `build.gradle` |
| **Rust** | ✅ `cargo test` | ✅ AST-based | ✅ Full | `Cargo.toml` |
| **JavaScript/TypeScript** | ✅ `npm test` / `yarn test` | ✅ AST-based | ✅ Full | `package.json` |
| **C#** | ✅ `dotnet test` | ⚠️ Limited | ⚠️ Limited | `*.csproj`, `*.sln` |

### Automatic Language Detection

When you open a project, AI Studio:

1. **Detects your project type** by scanning for marker files (e.g., `go.mod`, `package.json`, `Cargo.toml`)
2. **Configures language-specific tooling** for tests, code analysis, and context gathering
3. **Adapts workflow behavior** to use the appropriate commands and patterns

You don't need to configure anything—workflows automatically use the right tools for your language.

### Language-Specific Features

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

AI Studio handles **polyglot projects** seamlessly:

- If your repository contains multiple languages, workflows adapt per-directory
- The undo system tracks code artifacts across all languages
- Semantic search works across Python, Go, Rust, and other codebases

### Example: Python Workflow

```
You: Debug the authentication middleware

AI Studio: [Detects Python project from pyproject.toml]
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

AI Studio: [Detects Rust project from Cargo.toml]
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

AI Studio applies **language-appropriate patterns** automatically:

- **Error handling**: `Result<T, E>` in Rust, exceptions in Python/Java, error returns in Go
- **Testing**: Property-based testing hints in Rust, unittest/pytest in Python, JUnit in Java
- **Code organization**: Modules in Rust/Python, packages in Java/Go
- **Naming conventions**: snake_case for Python/Rust, camelCase for JavaScript/TypeScript, PascalCase for C#

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

AI Studio: Creating workflow...
          Step 1: Designing component structure...

You: [types while running] "Also add error handling for network failures"
     [Icon changes from ❌ to →]

AI Studio: ↪️ Got it - adding network error handling
          Step 2: Generating code with error handling...
```

Your feedback gets incorporated automatically without restarting.

### Example: Custom Debugging Workflow

```
You: Find and fix the memory leak in the cache manager

AI Studio: Creating custom workflow...
          
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

AI Studio creates the right workflow for your specific situation.

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
- Use interruptions for **adding features** ("also add...", "include...")
- Keep modifications **focused and specific**
- Let workflows finish when you can
- Use stop + restart for **major direction changes**

### ❌ Avoid:
- Using interruptions to **completely change direction**
- Sending **multiple rapid changes** (give each one time to incorporate)
- Interrupting to **ask questions** (wait until the workflow finishes)

### When to Use What

**Let AI decide** - Most of the time, just describe what you want and AI Studio picks the best approach automatically.

**Need something specific?** You can request:
```
"Use the planner to figure out this complex bug"
"Follow the team's testing workflow"
```

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

For complete technical details and SDK integration:
- **[Workflow Undo System (SDK)](../sdk/undo-system)** - Architecture documentation covering:
  - Transaction lifecycle and commit/rollback mechanisms
  - Multi-dimensional indexing (date, code artifacts, workflow types)
  - Semantic search with time ranking algorithms
  - AST-based code artifact extraction
  - Storage architecture and performance optimizations
  - CLI integration and testing strategies

---

## Summary

Coding workflows in AI Studio make complex tasks manageable:

- **Smart planning** creates custom workflows for your specific needs
- **Team templates** ensure consistent execution of common tasks
- **Real-time adjustments** let you refine as you go
- **Multi-language** support works with Go, Python, Java, Rust, JavaScript, TypeScript, and C#

Just describe what you want to accomplish, and AI Studio figures out the steps to get there. Whether it's debugging, implementing features, or refactoring code, the workflows adapt to your codebase and keep you in control.

---

## For Developers & SDK Users

This guide focuses on using workflows from an end-user perspective. If you're integrating AI Studio or want to understand the technical architecture:

- **[Workflow Architecture (SDK)](../sdk/workflow-architecture)** - Complete technical documentation covering:
  - Dynamic Planner implementation and OnFinish hooks
  - Ask pipeline architecture and execution model
  - Participant vs Controller pattern explained
  - WorkQueue orchestration and context injection
  - Knowledge Base workflow structure
  - Step API and reusable components
  - Integration best practices and testing

- **[Undo System Architecture (SDK)](../sdk/undo-system)** - Technical details on the undo system:
  - Transaction lifecycle and atomic commits
  - Multi-dimensional indexing strategies
  - Semantic search with time ranking
  - AST-based code artifact extraction
  - Storage architecture and performance
  - Rollback mechanisms and integrity checks
  - CLI integration and SDK best practices
