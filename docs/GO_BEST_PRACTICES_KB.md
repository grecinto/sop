# Go Best Practices and Patterns KB (Trial Draft)

## Purpose

This document is a compact, trial-ready knowledge base for Go coding guidance. It is designed for use in the KB/research phase of the copilot workflow so the agent can retrieve and apply Go-specific best practices before editing or generating code.

## Sources

This draft is based on authoritative Go guidance from:
- Go Effective Go: https://go.dev/doc/effective_go
- Go Proverbs: https://go-proverbs.github.io/
- Google Go Style Guide: https://google.github.io/styleguide/go/guide

## Core Guidance

### 1. Prefer clarity over cleverness
- Write code that is easy for the next reader to understand.
- Clear naming, simple control flow, and explicit structure are more valuable than clever tricks.
- Avoid over-abstraction when a simple function or struct will do.

### 2. Follow idiomatic Go formatting
- Use gofmt for formatting.
- Use tabs for indentation.
- Keep code simple and readable rather than forcing manual alignment.

### 3. Use Go naming conventions
- Use short, descriptive names.
- Prefer MixedCaps or mixedCaps over snake_case.
- Package names should be short and concise.
- Exported names should be clear and meaningful.
- Avoid unnecessary prefixes like Get when a method name already communicates intent.

### 4. Make the zero value useful
- Design types so that the zero value is valid and meaningful.
- This helps reduce boilerplate and makes initialization simpler.

### 5. Use errors as values
- Return errors explicitly rather than using panic for ordinary failures.
- Handle errors close to the source when possible.
- Wrap errors with context when useful.
- Prefer `if err != nil { return err }` style patterns.

### 6. Prefer simple error handling patterns
- Use early returns to reduce nesting.
- Keep the happy path prominent.
- Avoid ignoring errors.
- Use `errors.Is` and `errors.As` for matching and inspecting errors.

### 7. Use slices, maps, and channels idiomatically
- Prefer slices over arrays in most cases.
- Prefer maps for associative lookups and sets.
- Use `make` for slices, maps, and channels.
- Use `append` carefully and return the updated slice when needed.

### 8. Favor composition over inheritance-like patterns
- Go does not have classical inheritance.
- Prefer small structs, interfaces, and composition.
- Keep interfaces small and focused.
- Avoid huge interfaces with many unrelated methods.

### 9. Keep interfaces small and meaningful
- The bigger the interface, the weaker the abstraction.
- Prefer narrow interfaces that describe a single capability.
- `interface{}` says nothing; use a concrete interface when possible.

### 10. Use concurrency carefully and intentionally
- Do not communicate by sharing memory; share memory by communicating.
- Channels orchestrate; mutexes serialize.
- Use channels for coordination and synchronization where appropriate.
- Use mutexes only when state must be protected explicitly.
- Remember: concurrency is not parallelism.

### 11. Prefer simplicity over dependency-heavy solutions
- A little copying is often better than introducing a new dependency.
- Prefer the standard library when it is sufficient.
- Avoid adding complexity unless it is clearly justified.

### 12. Avoid panic unless it is truly unrecoverable
- Panic is for exceptional situations, not ordinary control flow.
- Avoid panicking in normal library code.
- Use recover only for controlled shutdown or isolation of a failing goroutine.

### 13. Keep code easy to test
- Prefer small functions with clear responsibilities.
- Use table-driven tests where appropriate.
- Keep tests focused and readable.
- Use examples and tests to document intended behavior.

### 14. Prefer context for request-scoped values and cancellation
- Use `context.Context` for deadlines, cancellation, and request-scoped metadata.
- Pass context through API boundaries rather than using global state.

### 15. Make the happy path obvious
- Structure code so the successful path is the main flow.
- Handle failures as early exits or short branches.
- Avoid deep nesting where possible.

## Practical Patterns

### Good patterns
- Use `if err := doSomething(); err != nil { return err }`
- Prefer `for _, item := range items` when only the value is needed
- Use `for key, value := range m` when iterating maps
- Use `context.Context` for request-scoped operations
- Use a small interface for a specific capability
- Prefer composition and small types over large inheritance-like hierarchies

### Anti-patterns to avoid
- Returning nil errors with no context
- Ignoring returned errors
- Using `panic` for ordinary workflow failures
- Writing overly clever abstractions
- Creating very large interfaces
- Using shared mutable state without clear synchronization
- Making unnecessary dependencies for simple problems

## Suggested KB Prompt for Copilot

When working on Go code, follow these priorities:
1. Keep the code idiomatic and clear.
2. Prefer simple, well-named code over clever abstractions.
3. Handle errors explicitly and gracefully.
4. Use Go's standard patterns for slices, maps, interfaces, and concurrency.
5. Favor maintainability and readability over premature optimization.

## Optional Trial Scope

For a first trial, focus on these high-value areas:
- error handling
- naming and readability
- interface design
- concurrency patterns
- slice/map usage
- simple, idiomatic tests

---

## Related: KB Quality Improvement Initiative

This KB is part of a broader effort to make expertise **enforced** rather than just **available**. 

**Current limitation**: The KB provides patterns, but there's no validation that generated code actually follows them.

**Planned improvements** (see [KB_EXPERTISE_QUALITY_IMPROVEMENT.md](KB_QUALITY.md)):
- Post-generation validator that checks code against these patterns
- Automatic revision loop when violations are detected
- Inline citations showing which pattern was applied
- Historical tracking of which patterns lead to successful outcomes
- User feedback loop to improve pattern effectiveness over time

**What this means for this KB**:
- Patterns here will become **quality gates** for code generation
- The system will learn which patterns work best for different Go tasks
- Users will see explicit attribution: `// Pattern: error-handling from Go KB`
- Poor-performing patterns will be automatically flagged and refined

This transforms the KB from a reference doc into an active quality enforcement layer — which is the competitive edge needed for a $5/month subscription to be compelling.
