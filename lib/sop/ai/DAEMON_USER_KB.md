# Workflows

These workflows give the assistant a repeatable way to handle common tasks such as debugging failures or carrying out implementation work.

Each workflow below is written so the assistant can follow a clear sequence of steps, understand the goal, and apply the right tools to complete the task effectively.

## Coding

- Item: Fix failing tests
  Summary: Reproduce the failing behavior, inspect the root cause, apply a focused fix, and verify the result with tests.
  Summary: Use this workflow when a test is failing and the goal is to repair the underlying issue safely and systematically.
  Body:
  Use this workflow when a repository test fails and the goal is to diagnose and correct the underlying issue without introducing regressions.

  Workflow payload:
  ```json
  {
    "workflow_name": "Fix failing tests",
    "description": "Reproduce the failure, inspect the root cause, apply a focused fix, and verify the result with tests.",
    "workflow_template": {
      "name": "Fix failing tests",
      "description": "Reproduce the failure, inspect the root cause, apply a focused fix, and verify the result with tests.",
      "protocol_guidance": "Always capture the failing output before changing code. Read the relevant test and implementation files before editing. Prefer a focused patch and re-run the relevant tests before claiming success.",
      "steps": [
        {
          "step": 1,
          "title": "Reproduce and inspect the failure",
          "instructions": "Run the relevant test or command that reproduces the failure and capture the error output. Read the failing stack trace, assertion, or log before changing code.",
          "required_context": ["test output", "failing stack trace", "relevant code paths"],
          "success_criteria": ["The failure has been reproduced and summarized."],
          "iteration_policy": "Retry up to 3 times if the failure is not yet understood.",
          "tools_expected": ["run_tests", "read_file", "search"]
        },
        {
          "step": 2,
          "title": "Analyze the root cause",
          "instructions": "Inspect the relevant test and implementation files. Determine the likely root cause before editing the code.",
          "required_context": ["test file", "implementation file", "error context"],
          "success_criteria": ["The likely root cause is identified and explained."],
          "tools_expected": ["read_file", "search", "grep"]
        },
        {
          "step": 3,
          "title": "Apply a focused fix",
          "instructions": "Make the smallest change that addresses the root cause. Avoid large rewrites unless necessary.",
          "required_context": ["root cause analysis", "implementation context"],
          "success_criteria": ["The fix is applied with minimal change and preserves intended behavior."],
          "tools_expected": ["edit_file", "patch_file"]
        },
        {
          "step": 4,
          "title": "Verify the result",
          "instructions": "Re-run the relevant tests and confirm the change resolves the failure without introducing regressions.",
          "required_context": ["test command", "changed files"],
          "success_criteria": ["Relevant tests pass and the original failure is resolved."],
          "iteration_policy": "Retry up to 3 times if the fix is incomplete.",
          "tools_expected": ["run_tests", "read_file"]
        }
      ]
    }
  }
  ```
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

- Item: Coding
  Summary: Follow a structured implementation workflow that covers planning, design, coding, testing, and review.
  Summary: Use this workflow for feature work and general code changes that need a disciplined delivery path.
  Body:
  Use this workflow for general software implementation tasks that need a structured path from request to verified change.

  Workflow payload:
  ```json
  {
    "workflow_name": "Coding",
    "description": "Plan the work, design the approach, implement the change, verify it with tests, and review the final result.",
    "workflow_template": {
      "name": "Coding",
      "description": "Plan the work, design the approach, implement the change, verify it with tests, and review the final result.",
      "protocol_guidance": "Start by understanding the requirement, design the approach before coding, keep changes focused, and verify the result with relevant tests and review.",
      "steps": [
        {
          "step": 1,
          "title": "Planning and analysis",
          "instructions": "Clarify the request, identify constraints, and define the success criteria before implementation begins.",
          "required_context": ["requirements", "repo context", "constraints"],
          "success_criteria": ["The task is clearly scoped and the implementation target is understood."],
          "tools_expected": ["read_file", "search", "reasoning"]
        },
        {
          "step": 2,
          "title": "Design",
          "instructions": "Choose the approach, identify affected components, and outline the implementation plan before editing code.",
          "required_context": ["current architecture", "affected modules"],
          "success_criteria": ["A clear implementation strategy is defined."],
          "tools_expected": ["read_file", "search"]
        },
        {
          "step": 3,
          "title": "Coding",
          "instructions": "Implement the change in a focused way that matches the chosen approach and keeps the codebase maintainable.",
          "required_context": ["implementation plan", "relevant files"],
          "success_criteria": ["The implementation is complete and aligned with the plan."],
          "tools_expected": ["edit_file", "patch_file"]
        },
        {
          "step": 4,
          "title": "Testing and verification",
          "instructions": "Run the relevant tests and validation steps to confirm the change behaves as intended and does not regress existing behavior.",
          "required_context": ["test commands", "changed files"],
          "success_criteria": ["Relevant tests pass and the change is verified."],
          "iteration_policy": "Retry up to 3 times if the verification is incomplete.",
          "tools_expected": ["run_tests", "read_file"]
        },
        {
          "step": 5,
          "title": "Review and polish",
          "instructions": "Review the final diff for clarity, correctness, maintainability, and any follow-up work needed before completion.",
          "required_context": ["diff", "final implementation"],
          "success_criteria": ["The result is understandable, complete, and ready for handoff."],
          "tools_expected": ["read_file", "review_diff"]
        }
      ]
    }
  }
  ```
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

# Coding Best Practices

This category contains language-specific engineering guidance and cross-cutting best practices for coding, architecture, testing, and security.

## Go

This category captures the core idioms, patterns, and expectations for effective Go code based on Effective Go, Go Code Review Comments, and community best practices.

### Core Philosophy and Style

- Item: Use gofmt and keep formatting mechanical
  Summary: Go code should be formatted consistently with the standard toolchain rather than hand-tuned.
  Body:
  Go expects formatting to be easy and predictable. Use `gofmt` (or `go fmt`) as the default formatting path and avoid fighting the formatter with manual alignment. Run `gofmt` before committing code. If the formatting doesn't look right, rearrange your code rather than working around the formatter. This reduces review churn and keeps large codebases visually consistent. The machine handles formatting so humans can focus on logic.
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

- Item: Prefer simple, idiomatic code over clever abstractions
  Summary: Clear Go code is usually better than clever Go code.
  Body:
  Go favors straightforward implementations that are easy to read and maintain. Prefer small functions, direct control flow, and obvious naming over clever tricks or over-generalized abstractions. The idiomatic Go style is usually simpler than an equivalent solution in a more expressive language. "Clear is better than clever" is a core Go value. If you find yourself writing something clever, step back and look for the simpler approach.
  Sources: https://go.dev/doc/effective_go, https://go-proverbs.github.io/

- Item: Make the zero value useful
  Summary: Types should be usable with their zero value when possible.
  Body:
  In Go, design types so that `nil` or the default zero value is meaningful and does not force unnecessary initialization. For example, `sync.Mutex` and `bytes.Buffer` work correctly when zero-initialized. This reduces boilerplate and makes APIs easier to use. A type that is naturally valid at zero value feels lighter and more ergonomic. If zero value isn't useful, document why and provide a constructor.
  Sources: https://go.dev/doc/effective_go

### Error Handling

- Item: Treat errors as values and handle them explicitly
  Summary: Errors should be returned, inspected, wrapped, and handled deliberately.
  Body:
  Go does not rely on exceptions for normal error handling. Return errors explicitly, check them early, and wrap them with context when useful (use `fmt.Errorf` with `%w` for wrapping). Keep the happy path prominent and avoid ignoring failures. Never use `_` to discard errors unless you have a clear, documented reason. Error strings should not be capitalized or end with punctuation. This makes behavior more predictable and easier to debug. Check all error returns.
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

- Item: Indent error flow to keep happy path unindented
  Summary: Error handling should appear first, with the success path at the minimum indentation level.
  Body:
  Structure conditionals so that error cases return early, keeping the normal execution path at the lowest indentation level. This improves readability by making the flow easier to scan. For example, write `if err != nil { return err }` first, then continue with the main logic unindented. Avoid `if err != nil { /* handle */ } else { /* normal path */ }` patterns.
  Sources: https://go.dev/wiki/CodeReviewComments

- Item: Don't panic in library code; return errors instead
  Summary: Panic should be reserved for truly unrecoverable programmer errors.
  Body:
  Library code should return errors to callers rather than using panic. Panic is appropriate only for bugs (e.g., nil pointer dereferences that should never happen) or during initialization if the program cannot proceed. In normal operation, return errors. If you must panic (rare), document it clearly. Use panic only when something impossible has occurred.
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

### Concurrency

- Item: Use channels and goroutines intentionally
  Summary: Concurrency should be expressed with Go's native primitives rather than by force-fitting shared-state patterns.
  Body:
  Goroutines and channels are the preferred model for concurrent coordination in Go. Use channels to communicate ownership and state transitions, and reserve mutexes for cases where shared mutable state truly needs protection. The common guidance is "Do not communicate by sharing memory; instead, share memory by communicating." When launching a goroutine, be clear about when and how it terminates.
  Sources: https://go.dev/doc/effective_go, https://go-proverbs.github.io/

- Item: Document and manage goroutine lifetimes explicitly
  Summary: Every goroutine should have a clear termination condition and shutdown path.
  Body:
  When starting a goroutine, document how it will terminate. Leaked goroutines are a common source of bugs. Use contexts, channels, or other signals to ensure goroutines shut down cleanly. Avoid creating goroutines without a clear lifecycle. If you cannot explain when a goroutine exits, reconsider the design.
  Sources: https://go.dev/wiki/CodeReviewComments

- Item: Prefer synchronous functions unless concurrency is needed
  Summary: Functions should be synchronous by default; let callers add concurrency if they need it.
  Body:
  Synchronous functions are easier to test, reason about, and compose. Don't make a function asynchronous (return a channel, start a goroutine internally) unless there's a clear need. Callers can always wrap a synchronous call in `go func()` to get concurrency, but removing unnecessary concurrency from a library is much harder. Keep goroutines and lifetimes localized.
  Sources: https://go.dev/wiki/CodeReviewComments

### Interfaces and Types

- Item: Keep interfaces small and focused
  Summary: Broad interfaces weaken abstractions and make implementations harder to satisfy.
  Body:
  Prefer narrow interfaces that describe one capability well rather than large interfaces that bundle unrelated behavior. In Go, the interface surface should be as small as the use case requires. Many interfaces have just one or two methods. This keeps abstractions easier to understand and less brittle as code evolves. The smaller the interface, the more powerful the abstraction.
  Sources: https://go.dev/doc/effective_go, https://go-proverbs.github.io/

- Item: Accept interfaces, return concrete types
  Summary: Function parameters should be interfaces when possible; return types should be concrete unless abstraction is needed.
  Body:
  This makes APIs more flexible at the call site (accepting any implementation) while keeping return values concrete and discoverable. Don't force every type into an interface unnecessarily. Return interfaces only when you truly need polymorphism or expect the implementation to vary. Concrete return types are easier to document and use.
  Sources: https://go.dev/doc/effective_go

- Item: Use meaningful method receiver names
  Summary: Method receiver names should be short but consistent across the type.
  Body:
  Use a one- or two-character receiver name that reflects the type name (e.g., `c` for `Client`, `s` for `Server`). Be consistent: if one method uses `c`, all methods on that type should use `c`. Don't use `this` or `self` (those are not idiomatic Go). Avoid generic names like `me` or `t` unless they're truly unambiguous.
  Sources: https://go.dev/wiki/CodeReviewComments

- Item: Choose pointer vs. value receivers intentionally
  Summary: Method receivers should be pointers when they modify state or are large; otherwise use values.
  Body:
  Use pointer receivers when the method modifies the receiver, when the receiver is large (copying would be expensive), or when consistency is needed (if some methods use pointers, all should). Use value receivers for small, immutable types. Be consistent within a type. Don't mix pointer and value receivers arbitrarily.
  Sources: https://go.dev/wiki/CodeReviewComments

### Naming and Packages

- Item: Use short, descriptive names for packages
  Summary: Package names should be lowercase, single-word, and evocative of purpose.
  Body:
  Good package names are short, clear, and lowercase (e.g., `http`, `time`, `fmt`). Avoid `util`, `common`, or generic names. The package name becomes part of the identifier (`http.Get`, `time.Now`), so it should add clarity. Don't stutter: if the package is `log`, don't export `LogWriter`; export `Writer` and clients will see `log.Writer`.
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

- Item: Use MixedCaps for multi-word names
  Summary: Go names use MixedCaps or mixedCaps, not underscores.
  Body:
  Multi-word names should be `MixedCaps` (exported) or `mixedCaps` (unexported). Never use underscores for separation (except in test files or when matching external naming). Initialisms should be consistent: use `HTTP`, `URL`, `XML` (all caps) or `http`, `url`, `xml` (all lowercase), not `Http`, `Url`, `Xml`.
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

- Item: Keep variable names short in small scopes
  Summary: Variable names should be shorter when their scope is smaller.
  Body:
  In a five-line function, `i` is fine for a loop counter. In a 50-line function, use something more descriptive. The further from declaration a variable is used, the more descriptive its name should be. In tight scopes, clarity comes from compactness. In broad scopes, clarity comes from explicit naming.
  Sources: https://go.dev/wiki/CodeReviewComments

### Context and Cancellation

- Item: Use context for request-scoped values and cancellation
  Summary: Context should carry deadlines, cancellation, and request-scoped metadata through API boundaries.
  Body:
  When code crosses package or service boundaries, pass `context.Context` as the first parameter (conventionally named `ctx`) for deadlines, cancellation, and request-scoped values. Avoid storing request state in globals or hidden mutable state. Don't store context in a struct; pass it explicitly. This keeps behavior predictable for long-running operations and network-bound work.
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

- Item: Context should be the first parameter
  Summary: Functions accepting context should take it as the first parameter, named `ctx`.
  Body:
  This convention makes context usage consistent and easy to spot. Even if you don't use context now, adding it early makes future changes easier. Exception: methods on structs don't need to take context as a parameter if they're not performing operations that need cancellation (but if they later do, refactor to add it).
  Sources: https://go.dev/wiki/CodeReviewComments

- Item: Do not use context.Context to pass function parameters or state
  Summary: Context is for deadlines, cancellation, and request-scoped values only—not for passing normal function parameters.
  Body:
  Never use context.Context as a way to avoid adding function parameters. Context is specifically for: (1) cancellation signals, (2) deadlines/timeouts, (3) request-scoped values like trace IDs, user identity, or request metadata. Do NOT use it for: business logic parameters, configuration values, dependencies, or application state. Misusing context makes function signatures unclear, breaks type safety, and makes code harder to test and understand. If you need to pass parameters, add them as explicit function arguments. If you have too many parameters, consider creating a struct. Context abuse is a common anti-pattern that violates Go idioms. Example of wrong usage: `ctx.Value("userID")` for a required parameter—this should be an explicit `userID string` parameter instead.
  Sources: https://go.dev/wiki/CodeReviewComments, https://go.dev/blog/context

### Testing

- Item: Write table-driven tests for behavior
  Summary: Tests should be readable, focused, and easy to extend.
  Body:
  Go testing often benefits from table-driven tests that describe a set of input and expected output cases. Define a slice of test cases with input and expected output, then loop over them. Keep test names clear, keep assertions direct, and avoid over-mocking. Tests should communicate the intended behavior of the code and protect against regressions. Name your test cases for easy debugging.
  Sources: https://go.dev/doc/effective_go

- Item: Write useful test failures with context
  Summary: Test failures should say what was wrong, what input caused it, and what was expected.
  Body:
  Use `t.Errorf("Foo(%q) = %d; want %d", input, got, want)` rather than generic assertions. Always include the input that failed, the actual result, and the expected result. This makes debugging much faster. Avoid test helpers that obscure the failure message. The person debugging your test should understand the failure without reading the test code.
  Sources: https://go.dev/wiki/CodeReviewComments

### Common Gotchas and Anti-Patterns

- Item: Declare empty slices as `var s []T`, not `s := []T{}`
  Summary: Prefer nil slices over non-nil empty slices.
  Body:
  `var s []T` declares a nil slice, while `s := []T{}` creates a non-nil but empty slice. They are functionally equivalent (`len` and `cap` both 0), but nil is the preferred style because it's simpler and uses less memory. Exception: JSON encoding differs (`nil` becomes `null`, `[]T{}` becomes `[]`), so use non-nil empty slices when that matters.
  Sources: https://go.dev/wiki/CodeReviewComments

- Item: Avoid in-band error values (like -1, "", etc.)
  Summary: Return explicit error values rather than special sentinel values.
  Body:
  Functions should return `(T, error)` rather than returning a special value (like -1 or "") to indicate error. This makes the error explicit and allows callers to handle it properly. In-band errors are confusing, harder to check, and violate Go's explicit error handling philosophy.
  Sources: https://go.dev/wiki/CodeReviewComments

- Item: Never use wildcard imports or dot imports
  Summary: Import packages explicitly with their full package name.
  Body:
  Don't use `.` imports (`import . "fmt"`) because they pollute the namespace and make it unclear where identifiers come from. Always use explicit package identifiers (`fmt.Println`). Exception: Test files can use dot imports for testing packages, but this should be rare.
  Sources: https://go.dev/wiki/CodeReviewComments

### Documentation

- Item: Write complete sentence doc comments starting with the name
  Summary: Every exported name should have a doc comment that starts with the name itself.
  Body:
  Doc comments should be complete sentences that start with the name being documented. For example: `// Get fetches the value for a given key.` not `// Fetches the value` or `// This function gets...`. Use periods. Doc comments appear in godoc, so make them clear and useful. If an API is complex, add examples in tests.
  Sources: https://go.dev/doc/effective_go, https://go.dev/wiki/CodeReviewComments

## Java

This category captures the practical conventions and design patterns used in modern Java codebases based on Google Java Style Guide and established Java idioms.

### File Organization and Structure

- Item: Keep one top-level type per file
  Summary: Java files should be organized around one primary public type to keep structure obvious.
  Body:
  The standard Java convention is to place exactly one top-level public class, interface, or enum in each file, and the file name must match the class name. Private or package-private helper classes can exist in the same file but should be small. This improves navigation, keeps files predictable, and matches the expectations of most Java tools and code review workflows. Avoid creating "utility" files with multiple unrelated classes.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Order class members consistently
  Summary: Class members should follow a standard, predictable order.
  Body:
  Arrange class members in a consistent order: static fields, instance fields, constructors, methods. Group overloaded methods together. Arrange methods logically (e.g., public API first, then private helpers), not chronologically. This makes classes easier to navigate. Google style recommends grouping by functionality when it improves understanding.
  Sources: https://google.github.io/styleguide/javaguide.html

### Naming Conventions

- Item: Use clear naming and package structure
  Summary: Types, methods, and packages should reflect intent and responsibility.
  Body:
  Java code should use `UpperCamelCase` for class names and type parameters, `camelCase` for method names and variables, and `UPPER_SNAKE_CASE` for constants (static final fields). Package names should be all lowercase without underscores (e.g., `com.example.myproject`). Names should communicate purpose rather than implementation detail. Avoid abbreviations except for very common ones (id, url). Methods that return a boolean should start with `is`, `has`, or `can`.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Use intention-revealing names for variables
  Summary: Variable names should explain their purpose, not just their type.
  Body:
  Avoid names like `list`, `map`, `i`, or `temp` except in the narrowest scopes (single-letter names are acceptable for loop indices in short loops). Use names that describe what the variable represents: `customers`, `orderById`, `activeUserCount`. The name should make the code self-documenting. For collections, prefer plural nouns.
  Sources: https://google.github.io/styleguide/javaguide.html

### Code Formatting

- Item: Use consistent brace style (K&R)
  Summary: Opening braces go on the same line as the declaration; closing braces on their own line.
  Body:
  Use K&R style: opening brace at the end of the line, closing brace on its own line, aligned with the beginning of the statement. Always use braces even for single-statement if/for/while blocks (this prevents bugs when code changes). Example: `if (condition) {` not `if (condition)\n{`. This is the standard Java convention.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Limit line length to 100 characters
  Summary: Lines should wrap at a reasonable column to improve readability.
  Body:
  Google style recommends a column limit of 100 characters (some teams use 120). Exceptions: package statements, import statements, comments with long URLs. When wrapping, indent continuation lines at least +4 from the original line. Break at higher syntactic levels (e.g., before a dot in a chain of method calls).
  Sources: https://google.github.io/styleguide/javaguide.html

### Imports and Dependencies

- Item: Avoid wildcard imports and keep imports explicit
  Summary: Imports should be clear and stable.
  Body:
  Never use wildcard imports (`import java.util.*;`) because they reduce clarity and make dependencies harder to reason about. Import each class explicitly. Order imports in groups: static imports, then non-static imports in alphabetical order. Remove unused imports. This makes review and maintenance easier and avoids accidental namespace coupling. Most IDEs can auto-organize imports.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Keep dependencies minimal and explicit
  Summary: Classes should depend only on what they actually use.
  Body:
  Avoid pulling in large dependencies for small utility functions. Make dependencies explicit through imports and constructor parameters rather than hiding them in static state. This makes testing easier and coupling more visible. Consider whether you really need that framework, or if a simpler approach would suffice.
  Sources: https://google.github.io/styleguide/javaguide.html

### Annotations and Overrides

- Item: Use `@Override` consistently
  Summary: Override annotations should be present whenever a method overrides a parent method or implements an interface method.
  Body:
  Always use `@Override` when overriding a method from a superclass or implementing an interface method (Java 6+). This improves readability, helps catch mistakes when signatures change, and makes refactoring safer. If you remove a parent method and forget to update the child, the compiler will catch it. This is one of the most important Java annotations.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Use standard annotations meaningfully
  Summary: Annotations like `@Deprecated`, `@SuppressWarnings`, and `@Nullable` should be used when they add value.
  Body:
  Use `@Deprecated` when replacing old APIs, and include a Javadoc comment explaining what to use instead. Use `@SuppressWarnings` sparingly and only for specific warnings with a comment explaining why it's safe. Use `@Nullable` and `@NonNull` (or similar annotations) to document nullability contracts. Don't overuse annotations; every annotation should serve a clear purpose.
  Sources: https://google.github.io/styleguide/javaguide.html

### Exception Handling

- Item: Prefer specific exception handling and structured cleanup
  Summary: Java code should handle real failure modes explicitly and close resources deterministically.
  Body:
  Catch specific exception types, not broad `Exception` or `Throwable`. Avoid empty catch blocks (if you must ignore an exception, name the variable `ignored` and add a comment). Never catch `Throwable` or `Error`. Use try-with-resources for any `AutoCloseable` resource (files, streams, database connections). Preserve exception context: don't swallow exceptions or throw new ones without chaining. This makes code more robust and easier to debug.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Don't use exceptions for control flow
  Summary: Exceptions should represent exceptional conditions, not normal program flow.
  Body:
  Don't use try-catch as a substitute for proper if-checks or validation. Exceptions are expensive and should represent actual error conditions. For example, checking if a value is present should use an `Optional` or null check, not catching `NoSuchElementException`. This improves performance and makes the code intent clearer.
  Sources: https://google.github.io/styleguide/javaguide.html

### Modern Java Features

- Item: Use modern Java features when they improve clarity
  Summary: Records, sealed types, switch expressions, text blocks, and pattern matching make code more expressive.
  Body:
  Modern Java (14+) provides powerful features: use records for simple data carriers, sealed classes for controlled inheritance, switch expressions for exhaustive matching, text blocks for multi-line strings, and pattern matching to reduce casting. These features reduce boilerplate and express intent more clearly. Don't force them where simpler constructs are clearer, but embrace them when they improve the code. Records are especially useful for DTOs, value objects, and immutable data.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Prefer `var` for obvious types, explicit types otherwise
  Summary: Local variable type inference (`var`) should improve readability, not obscure it.
  Body:
  Use `var` when the type is obvious from the right-hand side (`var list = new ArrayList<String>()`). Avoid it when the type is not clear (`var result = someComplexMethod()`). Good use of `var` reduces noise; bad use reduces clarity. When in doubt, use an explicit type. Never use `var` for fields, method parameters, or return types (not allowed anyway).
  Sources: https://google.github.io/styleguide/javaguide.html

### Object-Oriented Design

- Item: Keep methods and classes focused
  Summary: Java classes and methods should stay cohesive and avoid unrelated responsibilities.
  Body:
  Methods should do one thing well. If a method is longer than one screen or does multiple unrelated tasks, split it. Classes should have a single clear responsibility. Avoid "god objects" that do everything. Small, focused classes and methods are easier to test, reuse, and understand. If you struggle to name a class or method, it probably has too many responsibilities.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Favor composition over inheritance
  Summary: Prefer building behavior from smaller pieces rather than complex inheritance hierarchies.
  Body:
  Deep inheritance hierarchies are fragile and hard to understand. Prefer composition (using objects with specific capabilities) over inheritance (extending classes). Use interfaces to define contracts. Inheritance is useful for clear "is-a" relationships, but composition is more flexible for "has-a" or "uses-a" relationships. Modern Java features like default interface methods make composition even more powerful.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Program to interfaces, not implementations
  Summary: Method parameters and return types should be the most general interface that works.
  Body:
  Declare method parameters using interface types (`List`, `Map`, `Set`) rather than concrete implementations (`ArrayList`, `HashMap`, `HashSet`). This makes the code more flexible and testable. Return concrete types only when the caller needs implementation-specific methods. For example, accept `List<String>` but consider whether you could accept `Collection<String>` or even `Iterable<String>` to be even more flexible.
  Sources: https://google.github.io/styleguide/javaguide.html

### Immutability and Thread Safety

- Item: Make classes immutable by default
  Summary: Immutable classes are simpler, safer, and more predictable.
  Body:
  Prefer immutable classes (all fields final, no setters) when possible. Immutable objects are inherently thread-safe, easier to reason about, and prevent many bugs. Use builders for complex immutable objects. If you need mutability, document it clearly and consider whether the mutability should be encapsulated (private state with controlled mutation).
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Document thread-safety guarantees
  Summary: Classes should clearly document whether they are thread-safe.
  Body:
  Every class that might be used concurrently should document its thread-safety guarantees in the class-level Javadoc: "This class is thread-safe" or "This class is not thread-safe and requires external synchronization." Don't leave it ambiguous. Use annotations like `@ThreadSafe` or `@NotThreadSafe` if available. Synchronize only what needs synchronization; avoid over-synchronizing.
  Sources: https://google.github.io/styleguide/javaguide.html

### Documentation

- Item: Document public APIs with Javadoc
  Summary: Every public class and public/protected method should have Javadoc.
  Body:
  Write complete, useful Javadoc for all public classes and methods. Describe what the class/method does, all parameters, return value, exceptions thrown, and any side effects. Use `@param`, `@return`, `@throws`, and `@deprecated` tags. Describe the contract, not the implementation. Bad Javadoc just repeats the method name; good Javadoc explains why the method exists and how to use it correctly. Include examples for complex APIs.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Write clear commit messages and code comments
  Summary: Comments should explain why, not what.
  Body:
  Code should be self-explanatory through good naming. Use comments to explain why you made a choice, especially for non-obvious decisions. Avoid comments that just restate the code. Good comment: "Retry up to 3 times because the service is eventually consistent." Bad comment: "Set retries to 3." Document assumptions, invariants, and gotchas. Keep comments up to date; outdated comments are worse than no comments.
  Sources: https://google.github.io/styleguide/javaguide.html

### Testing

- Item: Write focused, readable unit tests
  Summary: Tests should clearly communicate what behavior they verify.
  Body:
  Use descriptive test names that explain what is being tested and what the expected outcome is (e.g., `shouldReturnEmptyListWhenNoResults`). Follow the Arrange-Act-Assert pattern. One logical assertion per test. Tests should be independent and deterministic. Use mocks judiciously; prefer real objects when practical. Test behavior, not implementation details.
  Sources: https://google.github.io/styleguide/javaguide.html

### Common Pitfalls

- Item: Don't use `==` for object equality; use `.equals()`
  Summary: Always use `.equals()` to compare object content, not `==`.
  Body:
  `==` compares object identity (same memory address), not content. Always use `.equals()` for `String`, `Integer`, and other objects. Be aware of null: use `Objects.equals(a, b)` to handle nulls safely. For collections, use `.equals()` as well. Exception: enums can use `==` because there's only one instance of each enum value.
  Sources: https://google.github.io/styleguide/javaguide.html

- Item: Override `hashCode()` when you override `equals()`
  Summary: These methods must be consistent to work correctly in collections.
  Body:
  If you override `equals()`, you must override `hashCode()` such that equal objects have the same hash code. This is required for correct behavior in `HashMap`, `HashSet`, etc. Use `Objects.hash()` for a simple implementation. Modern IDEs can generate both methods correctly. Records automatically provide correct `equals()` and `hashCode()`.
  Sources: https://google.github.io/styleguide/javaguide.html

## C#

This category captures the conventions and patterns that make C# code clear, modern, and maintainable based on Microsoft's official C# coding conventions and design guidelines.

### Modern Language Features

- Item: Use modern C# conventions and language features
  Summary: Prefer current C# language constructs when they improve readability and correctness.
  Body:
  Modern C# offers concise and expressive constructs: file-scoped namespaces (C# 10+), collection expressions (C# 12+), pattern matching, record types, init-only properties, required members, global usings, switch expressions, and target-typed new expressions. Use them when they make code clearer and better aligned with the current .NET ecosystem. For example, use `List<int> numbers = [1, 2, 3];` instead of `new List<int> { 1, 2, 3 }` in C# 12+. These features reduce ceremony and improve signal-to-noise ratio.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

- Item: Use file-scoped namespaces to reduce indentation
  Summary: File-scoped namespaces eliminate one level of nesting.
  Body:
  In C# 10+, use file-scoped namespace declarations to reduce indentation: `namespace MyApp.Services;` instead of wrapping the entire file in a block. This saves horizontal space and keeps code at lower indentation levels. Use this consistently across a project for all files with a single namespace.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

- Item: Use target-typed new when the type is obvious
  Summary: Omit the type on the right side when it's clear from the left.
  Body:
  When the type is specified on the left, use `new()` on the right: `List<string> names = new();` instead of `new List<string>()`. This reduces redundancy without losing clarity. Don't use it when the type isn't obvious from context. This feature works for object creation, field/property initialization, and local variables.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

### Type Usage and Variables

- Item: Use `var` only when the type is obvious
  Summary: Implicit typing should help readability, not hide intent.
  Body:
  Use `var` when the type is clear from the right-hand side: `var customer = new Customer();` or `var count = customers.Count;`. Avoid `var` when the type is not obvious: prefer explicit types for method returns, complex expressions, or when clarity matters. The goal is to reduce noise without reducing understanding. Teams should agree on a consistent style.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

- Item: Prefer meaningful variable names over comments
  Summary: Variable names should be self-documenting.
  Body:
  Use descriptive names that explain intent: `orderTotal` not `temp`, `activeUsers` not `list`. Avoid single-letter names except for tight loops or lambda parameters. Names should make the code readable without comments. For locals, use camelCase; for fields, use _camelCase (with underscore prefix for private fields).
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

### Null Handling

- Item: Use nullable reference types and explicit null handling
  Summary: Nullability should be part of the contract, not an afterthought.
  Body:
  Enable nullable reference types (`<Nullable>enable</Nullable>` in .csproj) to make nullability explicit in APIs and implementations. Use `string?` for nullable strings, `string` for non-null strings. This reduces null-related bugs and makes method contracts clearer. Use `!` (null-forgiving operator) sparingly and only when you're certain a value isn't null but the compiler can't prove it. Prefer explicit null checks instead.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

- Item: Use null coalescing and conditional operators idiomatically
  Summary: C# has concise operators for null handling that improve readability.
  Body:
  Use `??` for default values: `name = input ?? "default"`. Use `?.` for safe member access: `length = person?.Name?.Length`. Use `??=` for lazy initialization: `_cache ??= new Dictionary<string, string>()`. These operators are more concise and expressive than explicit null checks. Don't chain them excessively; if the logic gets complex, use explicit if statements.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

### Resource Management

- Item: Prefer `using` statements and async cleanup
  Summary: Resource lifetime should be clear and deterministic.
  Body:
  Use `using` for IDisposable resources and `await using` for IAsyncDisposable resources. Prefer the simplified using declaration syntax (C# 8+): `using var stream = File.OpenRead(path);` which disposes at the end of the enclosing scope. This is more reliable than manual cleanup and makes intent obvious. Never forget to dispose resources like streams, database connections, or HTTP clients that wrap connections.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

### Async Programming

- Item: Prefer async/await for I/O-bound operations
  Summary: Asynchronous workflows should be expressed with the language's native async model.
  Body:
  Use `async` and `await` for I/O-bound work such as network calls, file access, database operations. Return `Task` or `Task<T>` from async methods. Name async methods with an `Async` suffix (e.g., `GetDataAsync`). Never block on async code with `.Result` or `.Wait()` in production code—this can cause deadlocks, especially in UI or ASP.NET contexts. If you must block (e.g., in Main), use `.GetAwaiter().GetResult()` which has better exception semantics.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/asynchronous-programming/async-scenarios

- Item: Avoid async void except for event handlers
  Summary: Async methods should return Task or Task<T> so callers can await them.
  Body:
  `async void` methods cannot be awaited and make error handling difficult (exceptions crash the process or get lost). Use `async Task` instead. The only exception is event handlers, which are required to be void by the event signature. For fire-and-forget scenarios, consider returning Task and explicitly ignoring it at the call site with a comment explaining why.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/asynchronous-programming/async-scenarios

- Item: Use ConfigureAwait(false) in library code
  Summary: Libraries shouldn't capture the synchronization context unnecessarily.
  Body:
  In library code (not UI or ASP.NET request handlers), use `await operation.ConfigureAwait(false)` to avoid capturing the synchronization context. This improves performance and prevents deadlocks. In application code (UI, ASP.NET), omit ConfigureAwait or use ConfigureAwait(true) to stay on the original context. Know the difference; incorrect use can cause bugs.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/asynchronous-programming/async-scenarios

- Item: Don't use async for CPU-bound work
  Summary: Async/await is for I/O-bound work; use Task.Run for CPU-bound parallelism.
  Body:
  Don't mark methods async if they only do CPU-bound work. Use `Task.Run(() => CpuBoundWork())` to run CPU-bound operations on a thread pool thread. Async/await is designed for I/O-bound asynchrony where threads can be released while waiting. For CPU-bound parallelism, use Task Parallel Library (TPL) or Parallel LINQ. Mixing them incorrectly hurts performance.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/asynchronous-programming/async-scenarios

### LINQ and Collections

- Item: Use LINQ and expression-based patterns carefully
  Summary: Compact expressions should improve clarity without becoming opaque.
  Body:
  LINQ is powerful for querying collections, but long chains of transformations can become hard to read. Use it where it improves readability and expressiveness. Break complex queries into steps with intermediate variables. Use method syntax for simple queries (`.Where()`, `.Select()`) and query syntax when readability improves (joins, grouping). If a query becomes difficult to follow, prefer a more explicit loop or helper method.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

- Item: Understand deferred execution in LINQ
  Summary: Many LINQ operations are lazy and execute only when enumerated.
  Body:
  LINQ queries using `Where`, `Select`, `OrderBy`, etc. use deferred execution—they don't run until you enumerate the result (foreach, ToList, ToArray, Count, etc.). This can cause subtle bugs if the underlying collection changes or if you enumerate multiple times (re-executing the query). Use `.ToList()` or `.ToArray()` if you need to materialize the result immediately or enumerate multiple times.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/linq/

- Item: Prefer collection initializers and collection expressions
  Summary: Use concise syntax for initializing collections.
  Body:
  Use collection initializers: `var numbers = new List<int> { 1, 2, 3 };`. In C# 12+, use collection expressions: `List<int> numbers = [1, 2, 3];`. These are more concise than repeatedly calling `.Add()`. For dictionaries, use: `new Dictionary<string, int> { ["key"] = value }` or dictionary initializer syntax.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

### Pattern Matching and Switch Expressions

- Item: Use pattern matching to reduce casting and null checks
  Summary: Pattern matching makes type checks and data extraction more concise.
  Body:
  Use pattern matching instead of `as` followed by null checks: `if (obj is Customer customer)` instead of `var customer = obj as Customer; if (customer != null)`. Use switch expressions for exhaustive matching on types or values. Use property patterns to match on object members: `if (order is { Status: "Pending", Total: > 100 })`. This reduces boilerplate and makes intent clearer.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

- Item: Prefer switch expressions over switch statements
  Summary: Switch expressions are more concise and enforce exhaustiveness.
  Body:
  Switch expressions (`var result = value switch { ... }`) are often clearer than switch statements because they must return a value and the compiler enforces exhaustiveness with a discard pattern `_`. Use them for mapping values or pattern matching. Use traditional switch statements for imperative flows with side effects.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

### Record Types and Immutability

- Item: Use record types for immutable data objects
  Summary: Records provide value-based equality and concise syntax for immutable data.
  Body:
  Use `record` or `record struct` for DTOs, value objects, and immutable data carriers. Records automatically generate value-based `Equals`, `GetHashCode`, and `ToString`. Use positional syntax for simple records: `public record Person(string Name, int Age);`. Use nominal syntax for complex records with additional members. Records support with-expressions for non-destructive mutation.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

- Item: Use init-only properties for immutability
  Summary: Init-only setters allow object initialization but prevent later mutation.
  Body:
  Use `init` instead of `set` to make properties settable only during object initialization: `public string Name { get; init; }`. This works with object initializers and provides a middle ground between mutable and fully immutable objects. Use `required` modifier (C# 11+) to enforce that a property must be set during initialization: `public required string Name { get; init; }`.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

### Dependency Injection and Architecture

- Item: Favor dependency injection and explicit dependencies
  Summary: Components should declare what they depend on instead of hiding it in global state.
  Body:
  C# systems are easier to test and evolve when dependencies are explicit and injected (constructor injection is preferred) rather than hidden in static singletons or global state. Use the built-in DI container in ASP.NET Core or a third-party container. Register services with appropriate lifetimes (Singleton, Scoped, Transient). Clear dependency boundaries make code easier to reason about and less tightly coupled.
  Sources: https://learn.microsoft.com/en-us/dotnet/core/extensions/dependency-injection

- Item: Avoid service locator anti-pattern
  Summary: Don't inject IServiceProvider to resolve dependencies dynamically.
  Body:
  Injecting `IServiceProvider` and calling `GetService<T>()` hides dependencies and makes testing harder. It's a service locator anti-pattern. Prefer explicit constructor injection. Exception: framework code or plugins where types aren't known at compile time. Even then, consider using factory patterns instead.
  Sources: https://learn.microsoft.com/en-us/dotnet/core/extensions/dependency-injection

### API Design

- Item: Keep APIs consistent with .NET design guidelines
  Summary: Public APIs should be easy to discover, use, and evolve.
  Body:
  .NET design guidance favors intuitive naming (nouns for classes, verbs for methods), consistent behavior, and clear contracts. Use PascalCase for public members, prefix interfaces with `I`, suffix exception types with `Exception`. Public APIs should provide predictable failure semantics (throw exceptions for errors, return null/empty for "not found"). Avoid surprising defaults. Use overloads thoughtfully. Document pre/post conditions.
  Sources: https://learn.microsoft.com/en-us/dotnet/standard/design-guidelines/

- Item: Prefer properties over getter methods for data access
  Summary: Properties are the idiomatic way to expose data in C#.
  Body:
  Use properties (`public string Name { get; set; }`) instead of getter/setter methods. Properties are semantically cleaner and work with features like object initializers, expression-bodied members, and data binding. Use methods for operations that do significant work, have side effects, or take parameters. If getting a value is expensive or can throw, use a method instead.
  Sources: https://learn.microsoft.com/en-us/dotnet/standard/design-guidelines/

### Exception Handling

- Item: Use exceptions for exceptional conditions, not flow control
  Summary: Exceptions are expensive and should represent errors, not expected conditions.
  Body:
  Don't use exceptions for normal control flow. For example, use `TryParse` instead of catching parse exceptions. Exceptions should represent truly exceptional situations: invalid input, resource exhaustion, network failures. Use return values, out parameters, or result types for expected failure paths. This improves performance and makes the code intent clearer.
  Sources: https://learn.microsoft.com/en-us/dotnet/standard/exceptions/best-practices-for-exceptions

- Item: Catch specific exceptions and preserve context
  Summary: Exception handling should be precise and informative.
  Body:
  Catch specific exception types, not general `Exception`. Don't swallow exceptions silently (empty catch). When rethrowing, use `throw;` not `throw ex;` to preserve the stack trace. When wrapping exceptions, use `InnerException` to chain them. Add context to exceptions when rethrowing: `throw new ApplicationException("Failed to process order", ex)`. Log exceptions at the right boundary (usually near the top of the call stack).
  Sources: https://learn.microsoft.com/en-us/dotnet/standard/exceptions/best-practices-for-exceptions

### Testing

- Item: Write clear, focused unit tests
  Summary: Tests should clearly document expected behavior.
  Body:
  Use descriptive test method names that explain what is being tested and the expected outcome (e.g., `WhenOrderIsEmpty_TotalShouldBeZero`). Follow Arrange-Act-Assert pattern. One logical assertion per test. Tests should be independent, fast, and deterministic. Use mocking frameworks sparingly; prefer real objects when practical. Test behavior, not implementation details.
  Sources: https://learn.microsoft.com/en-us/dotnet/core/testing/

### Common Pitfalls

- Item: Don't compare strings with == for case-insensitive comparison
  Summary: Use StringComparison enum for proper string comparison.
  Body:
  Never use `.ToLower() == .ToLower()` for case-insensitive comparison. Use `string.Equals(a, b, StringComparison.OrdinalIgnoreCase)` or `.Equals(..., StringComparison.CurrentCultureIgnoreCase)`. This is more correct, more efficient, and handles cultures properly. `==` uses ordinal comparison which may not be what you want for internationalized text.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

- Item: Understand value vs reference types and boxing
  Summary: Structs are value types; classes are reference types. Boxing has performance implications.
  Body:
  Value types (structs, primitives) are copied when passed around; reference types (classes) are passed by reference. Structs should be small and immutable. Boxing occurs when a value type is converted to `object` or an interface—this allocates heap memory and hurts performance. Use generic collections (`List<int>`) instead of non-generic ones (`ArrayList`) to avoid boxing.
  Sources: https://learn.microsoft.com/en-us/dotnet/csharp/fundamentals/coding-style/coding-conventions

## Rust

This category captures the idioms and safety model that make Rust code robust and expressive based on The Rust Book, Rust API Guidelines, and community patterns.

### Ownership and Memory Safety

- Item: Embrace ownership and borrowing as the primary design model
  Summary: Rust code should be structured around ownership rules rather than fighting them.
  Body:
  The central Rust pattern is to model data ownership explicitly and allow the compiler to enforce safety. Prefer borrowing (`&T`, `&mut T`) over unnecessary cloning. Make lifetimes and mutation boundaries clear through the type system. Code that works with ownership idiomatically is easier to reason about, more efficient, and prevents entire classes of bugs (use-after-free, double-free, data races). When the borrow checker complains, the solution is usually to clarify ownership, not to add more lifetime parameters.
  Sources: https://doc.rust-lang.org/book/, https://rust-lang.github.io/api-guidelines/

- Item: Prefer borrowing over cloning for performance
  Summary: Pass references when you don't need ownership.
  Body:
  Functions should accept `&T` or `&mut T` when they don't need to own the data. Reserve cloning (`clone()`) for cases where you truly need an independent copy. Borrowing is zero-cost; cloning can be expensive. If your function signature requires `T` (owned value), consider whether `&T` would work. Immutable borrows (`&T`) can be shared; mutable borrows (`&mut T`) are exclusive.
  Sources: https://doc.rust-lang.org/book/

- Item: Use smart pointers intentionally for shared ownership
  Summary: Rc, Arc, Box, and RefCell solve specific ownership patterns.
  Body:
  Use `Box<T>` for heap allocation and owned data. Use `Rc<T>` for shared ownership in single-threaded contexts. Use `Arc<T>` for shared ownership across threads. Use `RefCell<T>` for interior mutability (runtime borrow checking) when compile-time checks are too restrictive. Use `Mutex<T>` or `RwLock<T>` for thread-safe interior mutability. Each has trade-offs; understand when to use which. Prefer simple ownership when possible.
  Sources: https://doc.rust-lang.org/book/

- Item: Understand lifetimes as borrowing constraints, not complexity
  Summary: Lifetimes document how long references remain valid; they usually can be inferred.
  Body:
  Rust's compiler usually infers lifetimes automatically (lifetime elision rules). Explicit lifetime annotations are needed when the compiler can't determine relationships. Lifetimes aren't about "managing memory"—they're about expressing borrowing relationships. If you find yourself fighting lifetimes, step back and reconsider whether ownership or cloning would simplify the design. Most Rust code has very few explicit lifetimes.
  Sources: https://doc.rust-lang.org/book/

### Error Handling

- Item: Use `Result` and `Option` instead of ad hoc error flags
  Summary: Failure handling should be explicit and typed.
  Body:
  Rust code should favor `Result<T, E>` for recoverable errors and `Option<T>` for optional values. These types force callers to handle errors explicitly. The `?` operator propagates errors concisely. Use `unwrap()` and `expect()` only in prototypes, tests, or when failure is truly impossible (and document why). In library code, always return Results so callers can handle errors appropriately. Custom error types should implement `std::error::Error`.
  Sources: https://doc.rust-lang.org/book/, https://rust-lang.github.io/api-guidelines/

- Item: Use `?` operator for clean error propagation
  Summary: The `?` operator makes error handling concise without hiding it.
  Body:
  `?` automatically converts errors (via `From` trait) and returns early if the result is `Err`. It's equivalent to `match result { Ok(v) => v, Err(e) => return Err(e.into()) }`. This keeps happy-path code clean while preserving explicit error propagation. Use it liberally in functions returning `Result`. It works with `Option` too (returns early on `None`).
  Sources: https://doc.rust-lang.org/book/

- Item: Avoid panicking in library code; return Results instead
  Summary: Panics should be reserved for truly unrecoverable bugs, not errors.
  Body:
  Library functions should return `Result` for errors that callers might want to handle. Panic only for programmer errors (contract violations, unreachable code paths) where recovery isn't meaningful. Use `panic!`, `unreachable!`, and `unimplemented!` sparingly. Document panic conditions in function documentation. In application code, panics are more acceptable but should still be deliberate.
  Sources: https://rust-lang.github.io/api-guidelines/

- Item: Design error types that compose well
  Summary: Error types should be informative and work with `?` and `From` conversions.
  Body:
  Custom error types should implement `std::error::Error`, `Display`, and `Debug`. Use enums to represent multiple error cases. Consider using crates like `thiserror` to reduce boilerplate. Implement `From` to enable automatic error conversions with `?`. Provide context: error messages should help users understand what went wrong and why.
  Sources: https://rust-lang.github.io/api-guidelines/

### Pattern Matching and Control Flow

- Item: Prefer pattern matching over imperative branching when it improves clarity
  Summary: Enums and structured data should be handled with pattern matching.
  Body:
  Rust makes `match` a first-class tool for expressing decision logic. Use `match` for exhaustive matching on enums, structs, and tuples. Use `if let` for matching a single pattern when you don't care about other cases. Pattern matching is more than a switch statement—it destructures data and enforces exhaustiveness. This is especially useful for enums, option values, and state machines. Prefer matching over boolean chains when you have structured data.
  Sources: https://doc.rust-lang.org/book/

- Item: Make illegal states unrepresentable with type design
  Summary: Use the type system to encode invariants and prevent invalid states.
  Body:
  Design types so that invalid states can't be constructed. For example, use enums instead of booleans to represent states. Use newtypes (tuple structs) to distinguish semantically different values of the same underlying type (e.g., `struct Meters(f64)` vs `struct Seconds(f64)`). Use `Option` instead of sentinel values (like -1). The compiler will prevent misuse at compile time, eliminating entire classes of bugs.
  Sources: https://doc.rust-lang.org/book/

### API Design

- Item: Keep APIs ergonomic and minimal
  Summary: Rust APIs should be easy to use and hard to misuse.
  Body:
  Rust API design should emphasize clarity, minimal surprise, and ergonomic use. Follow naming conventions: types are `PascalCase`, functions and variables are `snake_case`, constants are `SCREAMING_SNAKE_CASE`, lifetimes are `'lowercase`. Prefer small, composable abstractions. Avoid unnecessarily complex generic signatures. Good Rust APIs let common usage be obvious and make advanced cases possible without friction. Don't over-genericize unless it provides real value.
  Sources: https://rust-lang.github.io/api-guidelines/

- Item: Follow naming conventions and standard patterns
  Summary: Rust has strong conventions that improve consistency across the ecosystem.
  Body:
  Method names should follow conventions: `.iter()` for iterators, `.into_*()` for consuming conversions, `.to_*()` for copying conversions, `.as_*()` for cheap reference-to-reference conversions. Getters don't use `get_` prefix (just `name()`, not `get_name()`). Mutable methods use `*_mut` suffix. Constructors are `new()`, fallible constructors return `Result`. Boolean queries use `is_` or `has_` prefix. These conventions make APIs predictable.
  Sources: https://rust-lang.github.io/api-guidelines/

- Item: Design for zero-cost abstractions
  Summary: APIs should avoid runtime overhead when possible.
  Body:
  Rust's design goal is "zero-cost abstractions": high-level code should compile to the same machine code as low-level code. Prefer generics over trait objects when performance matters. Use `impl Trait` for return types to avoid heap allocation. Avoid unnecessary boxing or dynamic dispatch. Use `#[inline]` for small, hot functions. Design APIs so the compiler can optimize aggressively.
  Sources: https://rust-lang.github.io/api-guidelines/

- Item: Accept borrowed types for flexibility
  Summary: Function parameters should accept the most general borrowed form.
  Body:
  Instead of `&String`, accept `&str`. Instead of `&Vec<T>`, accept `&[T]`. Instead of `&Box<T>`, accept `&T`. This makes APIs more flexible: they work with owned types, borrowed types, and string/vector literals. Callers can pass more types without conversion. Exception: when you need the specific functionality of the owned type.
  Sources: https://rust-lang.github.io/api-guidelines/

### Iterators and Collections

- Item: Prefer iterators and collection methods over manual loops when they are clearer
  Summary: Idiomatic Rust uses iterator pipelines for transformation and filtering.
  Body:
  Iterators are a core part of Rust style and often produce clearer, more efficient code than manually managing indices or temporary collections. Use `.iter()`, `.iter_mut()`, and `.into_iter()` appropriately. Chain iterator adaptors (`.map()`, `.filter()`, `.fold()`, `.collect()`) to express transformations functionally. Iterators are lazy and zero-cost. If a loop is more readable, that's still valid. Don't force iterator chains that obscure intent.
  Sources: https://doc.rust-lang.org/book/

- Item: Understand when iterators consume and when they borrow
  Summary: Know the difference between iter(), iter_mut(), and into_iter().
  Body:
  `.iter()` iterates over immutable references (`&T`), `.iter_mut()` over mutable references (`&mut T`), `.into_iter()` consumes the collection and yields owned values (`T`). For loops use `into_iter()` by default; collections implement `IntoIterator` differently depending on whether they're borrowed or owned. Understanding this prevents ownership bugs.
  Sources: https://doc.rust-lang.org/book/

- Item: Collect iterators with type annotations when needed
  Summary: `.collect()` can produce many types; sometimes you must specify which.
  Body:
  `.collect()` can produce any type that implements `FromIterator`. Usually the type is inferred, but sometimes you need to annotate: `let vec: Vec<_> = iter.collect();` or use turbofish syntax: `iter.collect::<Vec<_>>()`. The `_` lets Rust infer the element type while you specify the collection type.
  Sources: https://doc.rust-lang.org/book/

### Traits and Generics

- Item: Implement standard traits for your types
  Summary: Deriving or implementing Debug, Clone, Copy, PartialEq makes types more usable.
  Body:
  Derive `Debug` for all public types to enable debugging. Implement `Display` for user-facing output. Derive `Clone` if cloning is meaningful, `Copy` if the type is small and bitwise-copyable. Implement `PartialEq` and `Eq` for equality, `PartialOrd` and `Ord` for ordering. These standard traits integrate your types with the ecosystem. Use `#[derive(Debug, Clone, PartialEq)]` liberally.
  Sources: https://rust-lang.github.io/api-guidelines/

- Item: Use trait bounds to express requirements clearly
  Summary: Generic parameters should have minimal, precise bounds.
  Body:
  Use `where` clauses for complex bounds to keep signatures readable. Only require the traits you actually use. Overly generic code can be hard to understand; sometimes concrete types are better. Use `impl Trait` for input parameters (`fn foo(x: impl Display)`) when you don't need to name the type. Use it for return types to hide implementation details and improve compilation time.
  Sources: https://rust-lang.github.io/api-guidelines/

### Modules and Visibility

- Item: Design module hierarchies that reflect usage patterns
  Summary: Modules should group related functionality and control visibility.
  Body:
  Use modules (`mod`) to organize code logically. Keep types, functions, and traits that work together in the same module. Use `pub` to export public APIs, leave internal helpers private. Use `pub(crate)` for items that should be public within the crate but not outside. Use re-exports (`pub use`) in lib.rs to create a clean public API. Good module design makes the crate easy to navigate and understand.
  Sources: https://rust-lang.github.io/api-guidelines/

- Item: Prefer flat, intuitive module structures
  Summary: Avoid deep nesting; keep common items easy to import.
  Body:
  Users should be able to import what they need with short paths. Re-export important types at the crate root or high-level modules. For example, if `crate::foo::bar::Thing` is commonly used, re-export it as `crate::Thing`. Avoid forcing users to navigate deep hierarchies for common APIs.
  Sources: https://rust-lang.github.io/api-guidelines/

### Concurrency

- Item: Leverage Rust's fearless concurrency guarantees
  Summary: The type system prevents data races at compile time.
  Body:
  Rust's `Send` and `Sync` marker traits ensure that only thread-safe types can be shared across threads. This prevents data races at compile time. Use `Arc` for shared ownership across threads, `Mutex`/`RwLock` for shared mutable state. Use channels (`std::sync::mpsc` or `crossbeam_channel`) for message passing between threads. Rust makes concurrency much safer than in most languages.
  Sources: https://doc.rust-lang.org/book/

- Item: Prefer message passing over shared mutable state
  Summary: Channels and message passing reduce coupling and prevent races.
  Body:
  When possible, structure concurrent programs so threads communicate by sending messages (channels) rather than sharing memory. This reduces the need for locks and makes the program easier to reason about. Use `mpsc::channel()` for asynchronous communication. Reserve shared mutable state (behind `Mutex`/`RwLock`) for cases where it truly simplifies the design.
  Sources: https://doc.rust-lang.org/book/

### Documentation and Testing

- Item: Write comprehensive documentation with examples
  Summary: Public items should have doc comments with usage examples.
  Body:
  Every public function, type, and module should have a doc comment (`///`). Explain what the item does, when to use it, and any important behavior. Include examples in doc comments—they're executable as tests (via `cargo test`). Use `//!` for module-level documentation. Good docs make your crate easier to use and maintain. Run `cargo doc --open` to preview.
  Sources: https://doc.rust-lang.org/book/, https://rust-lang.github.io/api-guidelines/

- Item: Write unit tests alongside the code they validate
  Summary: Rust makes it easy to keep tests close to implementation.
  Body:
  Rust supports unit tests naturally with `#[cfg(test)] mod tests` at the bottom of each file. Keep tests close to the code they exercise. Write integration tests in the `tests/` directory for public API validation. Use `#[test]` for test functions. Use `assert!`, `assert_eq!`, and `assert_ne!` for assertions. Test edge cases, error paths, and invariants. Tests are documentation of expected behavior.
  Sources: https://doc.rust-lang.org/book/

### Tooling and Workflow

- Item: Use cargo fmt and clippy as part of the normal workflow
  Summary: Tooling should enforce style and catch common issues automatically.
  Body:
  Run `cargo fmt` to standardize formatting automatically (uses rustfmt). Run `cargo clippy` to surface common mistakes, anti-patterns, and performance issues (Clippy has hundreds of lints). These tools reduce style churn, encourage consistency, and help developers focus on architecture and correctness. Make them part of CI. Use `rustfmt.toml` and `.clippy.toml` for project-specific configuration.
  Sources: https://doc.rust-lang.org/book/

- Item: Run clippy regularly and understand its suggestions
  Summary: Clippy lints represent accumulated community wisdom.
  Body:
  Clippy suggests improvements for correctness, performance, and idiomaticity. Don't ignore its warnings blindly; understand why it's suggesting a change. Most suggestions make code better. You can disable specific lints with `#[allow(clippy::lint_name)]` when you have a good reason, but document why. Clippy helps you write more idiomatic Rust.
  Sources: https://doc.rust-lang.org/book/

### Common Patterns and Anti-Patterns

- Item: Use the newtype pattern for type safety
  Summary: Wrap primitive types in tuple structs to prevent misuse.
  Body:
  Wrap primitive types (like `u64`, `String`) in newtypes to make invalid operations impossible. For example, `struct UserId(u64)` prevents mixing user IDs with order IDs. Newtypes are zero-cost at runtime (single-element tuple structs are optimized away). This prevents bugs at compile time and makes APIs clearer.
  Sources: https://doc.rust-lang.org/book/

- Item: Use builder pattern for complex construction
  Summary: Builders make objects with many optional parameters easier to construct.
  Body:
  When a type has many optional fields, use the builder pattern instead of multiple constructors. The builder lets callers specify only the fields they care about. Example: `Config::builder().timeout(30).retries(3).build()`. Libraries like `typed-builder` can generate builders automatically. This is more ergonomic than constructors with many optional parameters.
  Sources: https://rust-lang.github.io/api-guidelines/

- Item: Avoid stringly-typed APIs; use enums
  Summary: Don't use strings for values that have a fixed set of options.
  Body:
  Instead of accepting `&str` with values like "json" or "xml", define an enum: `enum Format { Json, Xml }`. This prevents typos, enables exhaustive matching, and makes the API self-documenting. Use strings only when the set of values is truly open-ended.
  Sources: https://rust-lang.github.io/api-guidelines/

- Item: Don't clone unnecessarily to appease the borrow checker
  Summary: If you're cloning a lot, your ownership design may need refinement.
  Body:
  Cloning is often used as a quick fix when the borrow checker complains, but excessive cloning hurts performance and hides design issues. If you find yourself cloning frequently, step back and reconsider your ownership model. Sometimes you need shared ownership (`Rc`/`Arc`), sometimes you need to restructure the code, sometimes lifetime parameters help. Cloning should be deliberate, not a workaround.
  Sources: https://doc.rust-lang.org/book/

### Unsafe Code

- Item: Avoid unsafe except when absolutely necessary
  Summary: Unsafe code should be rare, encapsulated, and well-documented.
  Body:
  Unsafe code opts out of Rust's safety guarantees and must be carefully reviewed. Use it only when necessary (FFI, low-level optimizations, certain data structures). Encapsulate unsafe code in safe APIs. Document invariants that must be maintained. Minimize the unsafe surface area. Most Rust programs have zero unsafe blocks; most libraries have only a few carefully audited ones.
  Sources: https://doc.rust-lang.org/book/

- Item: Document safety invariants clearly
  Summary: Every unsafe block must explain why it's safe.
  Body:
  Use `// SAFETY:` comments to explain why each unsafe block upholds Rust's safety guarantees. What invariants must hold? What could go wrong? How do you prevent it? This makes code review possible and prevents bugs when the code changes. Treat unsafe code with extreme care; it's where memory safety bugs can occur.
  Sources: https://doc.rust-lang.org/book/

## Python

This category captures the conventions that make Python code readable, maintainable, and predictable based on PEP 8, Google Python Style Guide, and community best practices.

### Philosophy and Style

- Item: Follow PEP 8 and the Zen of Python
  Summary: Python code should be readable first and visually consistent.
  Body:
  Python values readability above almost all else. Follow PEP 8 for layout and naming: 4 spaces per indentation level (never tabs), 79-100 character line length, two blank lines between top-level definitions, one blank line between methods. The Zen of Python (`import this`) guides design: "Explicit is better than implicit," "Simple is better than complex," "Readability counts." Code should be easy to scan and understand by the next person who reads it.
  Sources: https://peps.python.org/pep-0008/, https://peps.python.org/pep-0020/

- Item: There should be one obvious way to do it
  Summary: Python prefers a single, clear idiom over many equivalent approaches.
  Body:
  When faced with multiple ways to solve a problem, prefer the most obvious and readable one. Python's design encourages consistency. For example, use `with` for resource management, list comprehensions for simple transformations, `is` for singleton comparisons. This philosophy makes Python codebases easier to understand across different teams.
  Sources: https://peps.python.org/pep-0020/

### Naming Conventions

- Item: Use descriptive snake_case names and keep functions short
  Summary: Names should communicate intent and functions should stay focused.
  Body:
  Use `snake_case` for functions, variables, and module names. Use `CapWords` (PascalCase) for class names. Use `UPPER_CASE` for constants. Prefer descriptive names: `calculate_total_price()` not `calc()`. Functions should do one thing well and fit on a screen. Short, focused functions are easier to test, reuse, and understand. Avoid single-letter names except for narrow scopes (loop counters, lambda parameters).
  Sources: https://peps.python.org/pep-0008/

- Item: Use leading underscores for internal implementation
  Summary: Single underscore indicates internal use; double underscore triggers name mangling.
  Body:
  Prefix internal functions, methods, and variables with a single underscore (`_helper`) to indicate they're not part of the public API. Use double underscores (`__private`) sparingly—it triggers name mangling to avoid name conflicts in subclasses. Don't use trailing underscores except to avoid keyword conflicts (`class_` when `class` is a keyword). `_` alone is used for throwaway variables in loops or unpacking.
  Sources: https://peps.python.org/pep-0008/

### Imports

- Item: Organize imports in standard order
  Summary: Standard library, third-party, local imports, each group alphabetically.
  Body:
  Group imports in this order, separated by blank lines: (1) standard library imports, (2) third-party library imports, (3) local application imports. Within each group, sort alphabetically. Use absolute imports for clarity. Avoid wildcard imports (`from module import *`) as they pollute the namespace. Use `from module import specific_name` when you frequently use specific names.
  Sources: https://peps.python.org/pep-0008/

- Item: Avoid circular imports through better module design
  Summary: Circular imports indicate coupling that should be refactored.
  Body:
  If two modules import each other, the design likely needs improvement. Extract shared code to a third module, use dependency injection, or import inside functions (as a last resort). Circular imports cause cryptic errors and suggest tight coupling. Refactoring to eliminate them usually improves design.
  Sources: https://peps.python.org/pep-0008/

### Functions and Arguments

- Item: Avoid mutable default arguments
  Summary: Mutable defaults like `[]` or `{}` are shared across calls, causing bugs.
  Body:
  Never use mutable objects as default arguments: `def foo(items=[])` is wrong. The default is created once at function definition and shared across all calls. Use `None` and initialize inside: `def foo(items=None): items = items or []`. This is one of Python's most common gotchas. Immutable defaults (numbers, strings, tuples, `None`) are safe.
  Sources: https://peps.python.org/pep-0008/, https://docs.python-guide.org/writing/gotchas/

- Item: Use `*args` and `**kwargs` for flexible APIs
  Summary: Variadic arguments make functions extensible without breaking callers.
  Body:
  Use `*args` to accept any number of positional arguments, `**kwargs` for any keyword arguments. This is useful for wrappers, decorators, and extensible APIs. Example: `def wrapper(*args, **kwargs): return func(*args, **kwargs)`. Be careful not to overuse; explicit parameters are clearer when you know what you need.
  Sources: https://peps.python.org/pep-0008/

- Item: Prefer keyword arguments for clarity
  Summary: Keyword arguments make call sites self-documenting.
  Body:
  For functions with multiple parameters, especially booleans or similar types, use keyword arguments at the call site: `send_email(to=user, subject="Hello", urgent=True)` is clearer than `send_email(user, "Hello", True)`. You can enforce keyword-only arguments with `*`: `def foo(arg1, *, kwonly)` makes `kwonly` keyword-only.
  Sources: https://peps.python.org/pep-0008/

### Data Structures and Idioms

- Item: Use list comprehensions and generator expressions idiomatically
  Summary: Comprehensions are concise for simple transformations; avoid complexity.
  Body:
  Use list comprehensions for simple mappings and filtering: `[x*2 for x in items if x > 0]`. Use generator expressions for lazy evaluation: `(x*2 for x in items)`. Don't nest more than two levels or make them too complex. If the comprehension is hard to read, use a regular loop. Comprehensions should improve clarity, not obscure it.
  Sources: https://peps.python.org/pep-0008/

- Item: Prefer dataclasses and plain data structures for simple models
  Summary: Simple data containers should be expressed directly rather than over-engineered.
  Body:
  For small structured data, use `@dataclass` (Python 3.7+) or `NamedTuple` rather than elaborate custom classes. Dataclasses generate `__init__`, `__repr__`, and `__eq__` automatically. For simple cases, dictionaries are fine. Don't create heavyweight class hierarchies for simple data. Keep it simple and Pythonic.
  Sources: https://peps.python.org/pep-0557/

- Item: Understand when to use lists, tuples, sets, and dicts
  Summary: Choose the right data structure for the use case.
  Body:
  Lists (`[]`) for ordered, mutable sequences. Tuples (`()`) for immutable sequences, often used for heterogeneous data. Sets (`{}` or `set()`) for unique, unordered collections with fast membership tests. Dicts (`{}`) for key-value mappings. Use sets for membership testing, deduplication, and set operations. Don't use lists when you mean sets. Know the time complexities: list append is O(1), list insert is O(n); set/dict lookup is O(1) average.
  Sources: https://docs.python.org/3/tutorial/datastructures.html

### Context Managers and Resources

- Item: Use context managers for resources
  Summary: Files and other resources should be managed deterministically with `with`.
  Body:
  Always use `with` statements for resources: `with open(filename) as f:` ensures the file is closed even if an exception occurs. This works for files, locks, database connections, network sockets—anything implementing the context manager protocol (`__enter__`/`__exit__`). Never manually open and close resources without good reason; `with` is the idiomatic way.
  Sources: https://peps.python.org/pep-0343/

- Item: Write your own context managers when appropriate
  Summary: Context managers aren't just for files; use them for any setup/teardown pattern.
  Body:
  Use `contextlib.contextmanager` decorator to create simple context managers with generator functions. Use classes with `__enter__` and `__exit__` for more complex cases. Context managers are great for temporary state changes, acquiring and releasing locks, timing blocks, transaction management. The pattern makes resource management explicit and exception-safe.
  Sources: https://docs.python.org/3/library/contextlib.html

### Exception Handling

- Item: Use exceptions for real exceptional cases
  Summary: Exceptions should be specific and intentional, not used for flow control.
  Body:
  Python embraces EAFP (Easier to Ask for Forgiveness than Permission): try the operation and catch exceptions rather than checking preconditions. However, don't use exceptions for normal control flow. Catch specific exceptions (`FileNotFoundError`, `ValueError`), not broad `Exception`. Never use bare `except:` unless you re-raise or have a very good reason. Exceptions should represent actual error conditions.
  Sources: https://peps.python.org/pep-0008/

- Item: Preserve exception context when re-raising
  Summary: Use `raise from` to chain exceptions and preserve context.
  Body:
  When catching an exception and raising a different one, use `raise NewException() from original_exception` to preserve the stack trace and context. This makes debugging much easier. Use `raise` alone (no arguments) to re-raise the current exception. Never swallow exceptions silently unless you have a documented reason.
  Sources: https://peps.python.org/pep-3134/

- Item: Use finally or context managers for cleanup
  Summary: Ensure cleanup code runs even if exceptions occur.
  Body:
  Use `try/finally` to ensure cleanup code runs: `try: ... finally: cleanup()`. Even better, use context managers (`with`) which handle this automatically. Never put critical cleanup in bare except blocks—they might not execute if the exception is different than expected. `finally` always runs; `else` only runs if no exception occurred.
  Sources: https://docs.python.org/3/tutorial/errors.html

### Type Hints

- Item: Use type hints where they improve clarity
  Summary: Type hints should make the contract more explicit when they help.
  Body:
  Python supports gradual typing with type hints (PEP 484). Use them for function signatures, especially in public APIs, complex modules, or where types aren't obvious: `def greet(name: str) -> str:`. Use `mypy` or similar tools to check types. Don't over-annotate simple code where the intent is already obvious. Type hints are documentation that tools can verify. Modern Python code increasingly uses them.
  Sources: https://peps.python.org/pep-0484/

- Item: Understand common type hint patterns
  Summary: Use Optional, Union, List, Dict, Protocol, TypeVar appropriately.
  Body:
  `Optional[T]` is shorthand for `Union[T, None]`. Use `List[int]`, `Dict[str, int]` for generic collections (or `list[int]`, `dict[str, int]` in Python 3.9+). Use `Protocol` for structural subtyping (duck typing with types). Use `TypeVar` for generic functions. Use `Callable[[ArgTypes], ReturnType]` for functions. Learn the typing module well if you use type hints extensively.
  Sources: https://docs.python.org/3/library/typing.html

### Object-Oriented Programming

- Item: Prefer composition over inheritance
  Summary: Favor building objects from smaller pieces rather than deep hierarchies.
  Body:
  Deep inheritance hierarchies are hard to understand and maintain. Prefer composition (objects containing other objects) over inheritance. Use mixins sparingly. Use abstract base classes (ABC module) to define interfaces when needed. Python's duck typing often makes formal inheritance unnecessary. Focus on what objects can do, not what they are.
  Sources: https://peps.python.org/pep-0008/

- Item: Use properties for getters and setters
  Summary: Properties provide attribute syntax with method behavior.
  Body:
  Don't write Java-style getters/setters in Python. Use direct attribute access or properties: `@property` decorator for getters, `@name.setter` for setters. This keeps the syntax clean while allowing you to add validation or logic later. Python users expect direct attribute access; properties let you maintain that interface while controlling behavior.
  Sources: https://docs.python.org/3/library/functions.html#property

- Item: Use `__str__` and `__repr__` appropriately
  Summary: `__repr__` is for developers (unambiguous), `__str__` is for end users (readable).
  Body:
  Implement `__repr__` to return a string that could recreate the object: `Point(1, 2)`. This is for debugging. Implement `__str__` for user-facing output. If you only implement one, implement `__repr__`. The default `__str__` falls back to `__repr__`. Good `__repr__` makes debugging much easier. Use `f"{obj!r}"` to force repr in f-strings.
  Sources: https://docs.python.org/3/reference/datamodel.html

### Iterators and Generators

- Item: Use generators for lazy sequences
  Summary: Generators produce values on demand, saving memory for large sequences.
  Body:
  Use `yield` to create generator functions that produce values lazily: `def fibonacci(): a, b = 0, 1; while True: yield a; a, b = b, a+b`. Generators are memory-efficient for large datasets and infinite sequences. They implement the iterator protocol automatically. Use generator expressions `(x for x in items)` for simple cases. Prefer generators over building full lists when processing large data.
  Sources: https://peps.python.org/pep-0255/

- Item: Understand the iterator protocol
  Summary: Iterators implement `__iter__` and `__next__`.
  Body:
  Iterables implement `__iter__()` which returns an iterator. Iterators implement `__next__()` which returns the next value or raises `StopIteration`. Generators are the easiest way to create iterators. You can create custom iterators by implementing these methods. Understanding this protocol helps you work with for loops, comprehensions, and iteration tools effectively.
  Sources: https://docs.python.org/3/library/stdtypes.html#iterator-types

### Standard Library and Idioms

- Item: Know the standard library well
  Summary: Python's standard library is extensive; use it before adding dependencies.
  Body:
  Python's standard library has solutions for most common problems: `collections` (deque, Counter, defaultdict), `itertools` (efficient iteration tools), `functools` (higher-order functions), `pathlib` (path manipulation), `dataclasses`, `typing`, `contextlib`, `argparse`, `json`, `csv`, and much more. Learn it well to avoid reinventing wheels and adding unnecessary dependencies.
  Sources: https://docs.python.org/3/library/

- Item: Use `enumerate` instead of `range(len())`
  Summary: `enumerate` gives you both index and value in loops.
  Body:
  Don't use `for i in range(len(items)): item = items[i]`. Use `for i, item in enumerate(items):` instead. This is more Pythonic, more readable, and less error-prone. If you only need values, use `for item in items:`. If you only need indices (rare), use range.
  Sources: https://peps.python.org/pep-0008/

- Item: Use `zip` to iterate over multiple sequences
  Summary: `zip` pairs elements from multiple iterables.
  Body:
  To iterate over multiple sequences in parallel, use `zip`: `for name, score in zip(names, scores):`. `zip` stops when the shortest sequence is exhausted. Use `itertools.zip_longest` if you need to handle sequences of different lengths. This is more Pythonic than manually indexing multiple lists.
  Sources: https://docs.python.org/3/library/functions.html#zip

- Item: Use `any` and `all` for boolean collections
  Summary: `any` returns True if any element is truthy; `all` if all are.
  Body:
  Instead of looping to check conditions, use `any()` and `all()`: `if any(user.is_admin for user in users):` or `if all(item.is_valid for item in items):`. These are more concise and expressive than manual loops. They short-circuit (stop as soon as the result is known), making them efficient.
  Sources: https://docs.python.org/3/library/functions.html

### Testing

- Item: Use pytest-style tests for focused behavior
  Summary: Python tests should be small, explicit, and easy to read.
  Body:
  Most modern Python projects use pytest. Write test functions starting with `test_`. Use descriptive names: `test_user_creation_with_valid_email`. Use plain `assert` statements (pytest enhances them). Use fixtures for setup. Keep tests focused on one behavior. Tests should be readable and serve as documentation. Parametrize tests with `@pytest.mark.parametrize` for multiple cases.
  Sources: https://docs.pytest.org/

- Item: Use mocks and patches judiciously
  Summary: Mock external dependencies, but don't overuse mocks.
  Body:
  Use `unittest.mock` or `pytest-mock` to mock external services, file I/O, network calls. Don't mock the code you're testing—that defeats the purpose. Mocks should simulate external dependencies only. Over-mocking makes tests brittle and coupled to implementation. Prefer real objects when practical. Tests should verify behavior, not implementation.
  Sources: https://docs.python.org/3/library/unittest.mock.html

### Performance and Best Practices

- Item: Use `is` for singleton comparisons, `==` for value equality
  Summary: `is` checks object identity; `==` checks value equality.
  Body:
  Use `is None` and `is not None` for None checks (not `== None`). Use `is True` and `is False` only when you specifically need identity checking (rare). For all other comparisons, use `==`. `is` is faster for singletons because it's just pointer comparison. Checking `if x is None` is the idiomatic way.
  Sources: https://peps.python.org/pep-0008/

- Item: Don't prematurely optimize; profile first
  Summary: Readable code is more important than premature optimization.
  Body:
  Write clear, readable code first. Optimize only when profiling shows a bottleneck. Python has excellent profiling tools (`cProfile`, `line_profiler`, `memory_profiler`). Premature optimization makes code harder to read and maintain. Most Python code is fast enough. When you do optimize, measure before and after to verify improvement. Donald Knuth: "Premature optimization is the root of all evil."
  Sources: https://docs.python.org/3/library/profile.html

- Item: Use list comprehensions, not map/filter, for simple cases
  Summary: List comprehensions are more Pythonic and readable than map/filter.
  Body:
  Prefer `[x*2 for x in items]` over `list(map(lambda x: x*2, items))`. Prefer `[x for x in items if x > 0]` over `list(filter(lambda x: x > 0, items))`. Comprehensions are clearer, more Pythonic, and don't require lambdas. Use `map`/`filter` only when you have an existing named function to pass.
  Sources: https://peps.python.org/pep-0008/

### Common Pitfalls

- Item: Understand mutable vs immutable types
  Summary: Strings, tuples, numbers are immutable; lists, dicts, sets are mutable.
  Body:
  Immutable types can't be changed after creation; methods return new objects. Mutable types can be modified in place. This affects function arguments: mutating a list inside a function affects the caller's list. Understand this to avoid bugs. Tuples are immutable (good for dict keys), lists are not. Strings are immutable; concatenation creates new strings (expensive in loops—use join instead).
  Sources: https://docs.python.org/3/reference/datamodel.html

- Item: Use `dict.get()` to avoid KeyError
  Summary: `dict.get(key, default)` safely retrieves values with a fallback.
  Body:
  Instead of `value = d[key]` which raises `KeyError` if missing, use `value = d.get(key, default_value)`. Or use `defaultdict` from collections for automatic defaults. Or check with `if key in d:` before accessing. The `get` method is concise for simple cases. EAFP style would use try/except KeyError for more complex cases.
  Sources: https://docs.python.org/3/library/stdtypes.html#dict

- Item: Use `join` for string concatenation in loops
  Summary: `"".join(items)` is much faster than repeated `+=` for strings.
  Body:
  Don't use `s = s + item` in a loop; strings are immutable so this creates many temporary objects. Use `"".join(items)` or collect items in a list and join at the end. For simple concatenation outside loops, `+` or f-strings are fine. For building strings in loops, join is the idiomatic and efficient way.
  Sources: https://docs.python.org/3/library/stdtypes.html#str.join

---

# Architecture and Design Patterns

This section covers cross-cutting architectural patterns, design principles, and strategies that apply across languages and can guide system design and code generation.

## Software Architecture Patterns

### Layered Architecture

- Item: Organize code into distinct layers with clear boundaries
  Summary: Separate presentation, business logic, and data access into layers that depend only on layers below them.
  Body:
  Layered architecture organizes code into horizontal layers: presentation (UI), application (orchestration), domain (business logic), infrastructure (data access, external services). Each layer should only depend on layers below it, never above. This creates clear separation of concerns and makes testing easier. The domain layer should be independent of infrastructure details. Use dependency inversion (interfaces) to keep domain logic pure. Common in enterprise applications. Avoid the temptation to let infrastructure concerns leak into domain logic.
  Sources: Clean Architecture (Robert Martin), Domain-Driven Design (Eric Evans)

- Item: Keep business logic independent of frameworks and infrastructure
  Summary: Core business rules should not depend on databases, web frameworks, or external services.
  Body:
  The most valuable code in your system is the business logic that embodies your domain understanding. Keep this code free of framework dependencies, database coupling, and infrastructure details. Use ports and adapters (hexagonal architecture) to define interfaces that infrastructure implements. This makes the core logic easier to test, understand, and evolve. Frameworks and databases are details; business logic is the essence. Design so you could swap frameworks or databases without rewriting business rules.
  Sources: Clean Architecture (Robert Martin), Hexagonal Architecture (Alistair Cockburn)

### Microservices Architecture

- Item: Design services around business capabilities, not technical layers
  Summary: Each microservice should own a complete business capability with its own data store.
  Body:
  Microservices work best when organized around business domains (e.g., Orders, Inventory, Payments) rather than technical concerns (e.g., API Gateway, Database Service). Each service should be independently deployable and own its data—avoid shared databases. Services communicate via well-defined APIs (REST, gRPC, events). This enables teams to work independently and scale services differently. But microservices add complexity: distributed systems are harder to debug, test, and reason about. Start with a modular monolith unless you have clear scaling or organizational needs for microservices.
  Sources: Building Microservices (Sam Newman), Microservices Patterns (Chris Richardson)

- Item: Implement service boundaries with clear contracts
  Summary: Services should communicate through versioned, documented APIs with backward compatibility.
  Body:
  Define explicit contracts between services using API specifications (OpenAPI, gRPC proto files). Version your APIs and maintain backward compatibility when evolving them. Use semantic versioning. Add new fields as optional; deprecate old ones gracefully. Avoid breaking changes; if unavoidable, coordinate carefully. Use consumer-driven contract tests to verify integrations. Clear contracts make services truly independent—teams can evolve their services without breaking others.
  Sources: Building Microservices (Sam Newman)

- Item: Design for failure in distributed systems
  Summary: Microservices must handle network failures, service outages, and partial failures gracefully.
  Body:
  In distributed systems, failures are normal. Implement timeouts on all remote calls. Use circuit breakers to prevent cascading failures (when a service is down, fail fast rather than overwhelming it). Implement retries with exponential backoff and jitter. Use bulkheads to isolate failures. Design for idempotency so retries don't cause duplicate effects. Implement health checks and graceful degradation. Monitor everything. The network is unreliable; your design must account for this.
  Sources: Release It! (Michael Nygard), Microservices Patterns (Chris Richardson)

### Event-Driven Architecture

- Item: Use events for asynchronous communication and decoupling
  Summary: Services publish events when state changes; other services subscribe and react independently.
  Body:
  Event-driven architecture decouples services through asynchronous messaging. When something happens (order placed, payment received), publish an event to a message broker (Kafka, RabbitMQ, AWS SNS/SQS). Interested services subscribe and react independently. This reduces coupling—publishers don't know about subscribers. Events enable eventual consistency, scalability, and flexibility. But they add complexity: debugging async flows is harder, ensuring message ordering is difficult, and eventual consistency requires careful design. Use events for integration between bounded contexts, not within them.
  Sources: Enterprise Integration Patterns (Hohpe & Woolf), Designing Event-Driven Systems (Ben Stopford)

- Item: Design events as immutable facts about what happened
  Summary: Events should describe what occurred, not commands about what to do.
  Body:
  Events represent facts: "OrderPlaced", "PaymentReceived", "UserRegistered". They should be past tense, immutable, and contain all relevant data. Don't design events as commands ("PlaceOrder")—commands are requests that can be rejected; events are facts that happened. Include enough context so consumers don't need to query back. Version events carefully—they're part of your contract. Store events in an event store for audit trails and debugging. Event sourcing takes this further by using events as the primary source of truth.
  Sources: Domain-Driven Design (Eric Evans), Designing Event-Driven Systems (Ben Stopford)

### Domain-Driven Design Patterns

- Item: Model the problem domain with ubiquitous language
  Summary: Use the same terminology in code, documentation, and conversations with domain experts.
  Body:
  DDD emphasizes close collaboration with domain experts to build a shared understanding. The ubiquitous language is the vocabulary you develop together and use everywhere: in code (class names, method names), in documentation, and in conversations. This reduces translation errors and keeps code aligned with business understanding. If a developer and domain expert can't understand each other's language, the model is wrong. Refactor both code and language together as understanding evolves.
  Sources: Domain-Driven Design (Eric Evans)

- Item: Organize code into bounded contexts with explicit boundaries
  Summary: Large systems should be divided into bounded contexts, each with its own model and language.
  Body:
  Don't try to create one unified model for the entire system—different parts have different concerns. Bounded contexts are explicit boundaries where a particular model applies. Within a context, maintain a consistent ubiquitous language. Across contexts, use integration patterns (shared kernel, anti-corruption layer, published language). A User in the Ordering context might have different attributes than a User in the Support context—that's okay. Each context can use different technologies and databases. Map contexts explicitly and design their integration points carefully.
  Sources: Domain-Driven Design (Eric Evans)

- Item: Separate domain logic into entities, value objects, and aggregates
  Summary: Use DDD tactical patterns to organize domain logic clearly.
  Body:
  Entities have identity and lifecycle (User, Order). Value objects are immutable and defined by their attributes (Money, Address). Aggregates are consistency boundaries—clusters of entities/value objects with one root entity (Order aggregate might include OrderLines). All external access goes through the aggregate root, which enforces invariants. This prevents invalid states and makes transaction boundaries clear. Keep aggregates small for performance. Domain services handle operations that don't naturally belong to any entity. Repositories provide persistence for aggregate roots. These patterns keep domain logic organized and maintainable.
  Sources: Domain-Driven Design (Eric Evans)

## Design Patterns

### Creational Patterns

- Item: Use factory patterns to encapsulate object creation logic
  Summary: Factories centralize complex construction and enable polymorphic creation.
  Body:
  When object creation is complex or involves choosing between types, use a factory. Factory Method lets subclasses decide which class to instantiate. Abstract Factory provides an interface for creating families of related objects. Simple factories (static methods) work for basic cases. Builders handle objects with many optional parameters. Factories improve testability (inject mock factories), hide construction complexity, and make code more flexible. Don't overuse—simple constructors are fine when creation is straightforward.
  Sources: Design Patterns (Gang of Four)

- Item: Use dependency injection for loose coupling and testability
  Summary: Pass dependencies to objects rather than having them create or find dependencies.
  Body:
  Dependency injection inverts control: instead of objects creating their dependencies, they receive them (usually via constructor). This makes dependencies explicit, improves testability (inject mocks), and reduces coupling. DI containers/frameworks automate wiring but aren't required—manual injection (pure DI) is often simpler. Prefer constructor injection for required dependencies, property injection for optional ones. Avoid service locator pattern (asking a global registry for dependencies)—it hides dependencies and makes testing harder.
  Sources: Dependency Injection in .NET (Mark Seemann)

### Structural Patterns

- Item: Use adapter pattern to integrate incompatible interfaces
  Summary: Adapters translate between interfaces without modifying existing code.
  Body:
  When you need to use an existing class but its interface doesn't match what you need, create an adapter. This is especially useful for integrating third-party libraries or legacy code. The adapter implements the interface you need and delegates to the adaptee. Anti-corruption layers in DDD are essentially adapters that protect your domain from external models. Adapters enable you to change external dependencies without affecting core code.
  Sources: Design Patterns (Gang of Four)

- Item: Use decorator pattern to add behavior without modifying classes
  Summary: Decorators wrap objects to extend functionality, following Open/Closed Principle.
  Body:
  Decorators add behavior to objects dynamically without subclassing. They implement the same interface as the object they wrap. Common uses: logging, caching, transaction management, authentication. In languages with function decorators (Python, TypeScript), use those. Otherwise, use explicit decorator classes. This follows Open/Closed: open for extension, closed for modification. Decorators can be composed (stack multiple decorators). Middleware in web frameworks are decorators.
  Sources: Design Patterns (Gang of Four)

- Item: Use repository pattern to abstract data access
  Summary: Repositories provide a collection-like interface for accessing domain objects.
  Body:
  Repositories encapsulate data access logic and provide a domain-oriented API: `userRepository.findByEmail(email)` not `db.query("SELECT * FROM users WHERE...")`. This keeps SQL/database concerns out of business logic. Repositories return domain objects, not DTOs or database records. Each aggregate root gets a repository. This improves testability (mock repositories) and makes it easier to change data stores. Avoid generic repositories that expose too much—design repositories around domain needs, not CRUD operations.
  Sources: Domain-Driven Design (Eric Evans), Patterns of Enterprise Application Architecture (Martin Fowler)

### Behavioral Patterns

- Item: Use strategy pattern to make algorithms interchangeable
  Summary: Define a family of algorithms and make them interchangeable through a common interface.
  Body:
  Strategy pattern encapsulates algorithms behind an interface, allowing them to vary independently. Use it for: payment methods, sorting algorithms, validation rules, pricing strategies. The context uses a strategy interface; concrete strategies implement the algorithm. This eliminates conditionals, makes adding new strategies easy, and improves testability. In functional languages, strategies can be functions. Common in Go (interfaces with single methods), Java (functional interfaces), and modern languages with first-class functions.
  Sources: Design Patterns (Gang of Four)

- Item: Use observer pattern for decoupled event handling
  Summary: Observers subscribe to subjects and get notified of state changes.
  Body:
  Observer pattern implements publish-subscribe: subjects notify observers when state changes. Observers register with subjects, get notified, and react independently. This decouples components—subjects don't know about specific observers. Common uses: UI updates (MVC, reactive frameworks), event handling, logging. Modern implementations use event emitters, reactive streams (RxJS, Reactor), or message brokers. Be cautious of memory leaks (observers not unregistering) and circular notifications.
  Sources: Design Patterns (Gang of Four)

## API Design Principles

### RESTful API Design

- Item: Design REST APIs around resources, not actions
  Summary: URLs should represent resources; HTTP methods represent actions.
  Body:
  REST APIs model resources as nouns: `/users`, `/orders`, `/products/{id}`. Use HTTP methods for actions: GET (retrieve), POST (create), PUT (replace), PATCH (partial update), DELETE (remove). URLs should be plural nouns. Avoid verbs in URLs (`/getUser` is wrong). Use nested resources for relationships: `/orders/{id}/items`. Return appropriate status codes: 200 OK, 201 Created, 204 No Content, 400 Bad Request, 404 Not Found, 500 Internal Server Error. Design for resources, not RPC-style operations.
  Sources: RESTful Web APIs (Richardson & Ruby)

- Item: Use proper HTTP status codes and error responses
  Summary: Status codes communicate results; error responses provide actionable details.
  Body:
  Use correct status codes: 2xx for success, 3xx for redirection, 4xx for client errors, 5xx for server errors. Be specific: 400 (bad request), 401 (unauthenticated), 403 (forbidden), 404 (not found), 409 (conflict), 422 (validation error), 429 (rate limited). Return structured error responses with error codes, messages, and field-level validation errors. Don't return 200 with `{success: false}` in the body—use proper status codes. Consistent error handling makes APIs easier to use.
  Sources: RESTful Web APIs (Richardson & Ruby)

- Item: Version APIs explicitly and maintain backward compatibility
  Summary: APIs must evolve without breaking existing clients.
  Body:
  Version APIs through URL (`/v1/users`), headers (`Accept: application/vnd.api+json; version=1`), or query parameters. URL versioning is most visible. Introduce breaking changes in new versions only. Within a version, maintain backward compatibility: add optional fields, don't remove or rename fields, don't change semantics. Deprecate old versions gracefully with clear timelines. Document migration paths. Semantic versioning helps: major version for breaking changes, minor for additions, patch for fixes. Consider API gateways for versioning logic.
  Sources: Building Microservices (Sam Newman)

- Item: Implement pagination, filtering, and sorting consistently
  Summary: Collections should support pagination, filtering, sorting, and field selection.
  Body:
  For collection endpoints, support: pagination (offset/limit or cursor-based), filtering (`?status=active`), sorting (`?sort=-createdAt`), field selection (`?fields=id,name`). Return pagination metadata: total count, next/previous links. Use cursor-based pagination for large datasets. Be consistent across all endpoints. Document query parameters. Consider GraphQL if clients need flexible querying. Rate limiting and caching headers are important for large collections.
  Sources: RESTful Web APIs (Richardson & Ruby)

### gRPC and RPC API Design

- Item: Define clear, versioned service contracts with Protocol Buffers
  Summary: gRPC services should have well-documented proto definitions with evolution in mind.
  Body:
  Design gRPC services with clear method names and message types. Use Proto3 for modern APIs. Add field numbers carefully—they're part of your contract. Use reserved keywords for removed fields. Add optional/required appropriately. Group related RPCs in services. Use streaming RPCs for large data or real-time updates. Document services and messages thoroughly—proto comments become API docs. Consider backward compatibility: add new fields, don't remove or change types of existing ones. Use semantic versioning for proto packages.
  Sources: gRPC documentation, API Design Patterns (JJ Geewax)

- Item: Choose appropriate RPC types for your use case
  Summary: Unary, server streaming, client streaming, and bidirectional streaming serve different needs.
  Body:
  Unary RPCs (request-response) for simple operations. Server streaming for returning large result sets (pagination, log streaming). Client streaming for uploading data. Bidirectional streaming for real-time communication (chat, multiplayer games). Streaming improves performance for large data and enables low-latency communication. But streaming is more complex—handle errors, backpressure, cancellation carefully. Don't use streaming if request-response suffices. gRPC's performance benefits apply to all types.
  Sources: gRPC documentation

### GraphQL API Design

- Item: Design GraphQL schemas around the client's data needs
  Summary: GraphQL lets clients specify exactly what data they need; design the schema accordingly.
  Body:
  GraphQL schemas should reflect how clients want to consume data, not your database structure. Use types for entities, fields for attributes and relationships. Queries for reads, mutations for writes, subscriptions for real-time updates. Design resolvers to fetch data efficiently—avoid N+1 queries with DataLoader. Use nullable vs non-nullable carefully. Deprecate fields instead of removing them. GraphQL works best when it provides a unified API across multiple services. But it shifts complexity to the server—caching and query optimization are harder than with REST.
  Sources: GraphQL Best Practices (graphql.org)

- Item: Implement proper authorization and query complexity limits
  Summary: GraphQL's flexibility requires careful security and performance controls.
  Body:
  GraphQL clients can request arbitrary data and deeply nested queries. Implement field-level authorization—just because a user can query a User doesn't mean they can see all fields. Use query complexity analysis to prevent expensive queries. Implement depth limits, rate limiting, and timeouts. Consider persisted queries (pre-approved query whitelist) for production. Monitor query patterns to identify performance issues. GraphQL's power requires robust safeguards.
  Sources: GraphQL Best Practices (graphql.org)

## Code Review Best Practices

### What to Look for in Code Reviews

- Item: Check for correctness and edge case handling
  Summary: Does the code do what it's supposed to do, including error and boundary cases?
  Body:
  Verify the code solves the stated problem correctly. Check error handling: are errors caught, logged, and handled appropriately? Look for edge cases: empty inputs, null values, boundary conditions, concurrent access. Check assumptions—are they documented and validated? Look for off-by-one errors, incorrect comparisons, timezone issues, encoding problems. Test coverage should include happy path and error cases. Correctness is the most important review criterion.
  Sources: Code Review Best Practices (Google, Microsoft)

- Item: Evaluate code readability and maintainability
  Summary: Can other developers understand and modify this code easily?
  Body:
  Code should be self-explanatory through good naming, clear structure, and appropriate comments. Check: Are names descriptive? Is the structure logical? Are functions small and focused? Is complexity justified? Are comments explaining why, not what? Is the code consistent with the project's style? Readability matters more than cleverness. Code is read 10x more than it's written—optimize for readers. If you have to ask "what does this do?", it needs improvement.
  Sources: Code Complete (Steve McConnell), Clean Code (Robert Martin)

- Item: Verify architectural consistency and design quality
  Summary: Does the code follow established patterns and maintain good boundaries?
  Body:
  Check that code fits the existing architecture. Look for: proper layer separation, appropriate use of interfaces, correct dependency direction (high-level doesn't depend on low-level), reasonable abstractions, and adherence to SOLID principles. Watch for God objects, tight coupling, hidden dependencies, and violations of single responsibility. New code should improve the codebase, not add technical debt. If the design is questionable, discuss before implementation is complete.
  Sources: Clean Architecture (Robert Martin)

- Item: Assess performance and resource usage
  Summary: Are there obvious performance issues or resource leaks?
  Body:
  Look for N+1 queries, unnecessary loops, inefficient algorithms, excessive allocations, resource leaks (unclosed files/connections), missing indexes. Check that expensive operations are cached or optimized. Look for blocking operations on critical paths. However, don't prematurely optimize—focus on obvious problems. Profile before optimizing non-obvious cases. Consider whether the code will scale with realistic data volumes. Performance issues are easier to fix early.
  Sources: Release It! (Michael Nygard)

- Item: Ensure proper testing and testability
  Summary: Is the code testable and adequately tested?
  Body:
  Review tests alongside code. Check: Are tests clear and focused? Do they test behavior, not implementation? Is coverage adequate (especially for edge cases and error paths)? Are tests deterministic and independent? Is the code designed for testability (dependency injection, small functions, clear interfaces)? Integration tests for workflows, unit tests for logic. Tests are documentation—they show how code should be used. Missing or poor tests are a red flag.
  Sources: Growing Object-Oriented Software, Guided by Tests (Freeman & Pryce)

- Item: Check security and data handling practices
  Summary: Are there security vulnerabilities or data protection issues?
  Body:
  Look for: SQL injection (use parameterized queries), XSS (escape output), authentication/authorization issues, exposed secrets (API keys in code), insecure defaults, unvalidated input, information leaks in errors, missing encryption for sensitive data. Check compliance with data protection regulations (GDPR, CCPA). Verify that user input is validated, sanitized, and handled safely. Security bugs are expensive; catch them in review.
  Sources: OWASP Top Ten

### Code Review Process Best Practices

- Item: Keep changes small and focused
  Summary: Smaller pull requests are reviewed more thoroughly and merged faster.
  Body:
  Aim for PRs under 400 lines of code. Large PRs are hard to review well—reviewers get fatigued and miss issues. Break work into logical chunks. Each PR should have a single, clear purpose. Small PRs enable faster feedback cycles and easier rollbacks if issues arise. If a change must be large, consider breaking it into a series of PRs or providing extra context (design doc, video walkthrough).
  Sources: Code Review Best Practices (Google)

- Item: Write clear PR descriptions with context
  Summary: Explain what changed, why, and how to test it.
  Body:
  Good PR descriptions include: what problem you're solving, how you solved it, what alternatives you considered, how to test/verify the change, any potential risks. Link to relevant tickets/docs. Screenshots/videos for UI changes. Call out areas where you want specific feedback. This context helps reviewers understand your choices and review more effectively. Don't assume reviewers know the background.
  Sources: Code Review Best Practices (Google, Atlassian)

- Item: Give actionable, respectful feedback
  Summary: Comments should be specific, constructive, and focus on the code, not the person.
  Body:
  Good review comments: point to specific lines, explain the issue, suggest alternatives, cite standards/docs when applicable. Use "we" not "you" ("we should handle nulls here"). Distinguish between must-fix issues and suggestions. Praise good solutions. Avoid "just" and "simply"—they're condescending. Ask questions instead of making demands. Remember: you're reviewing code, not judging the author. The goal is to improve the code and share knowledge, not to show off or criticize.
  Sources: How to Do Code Reviews Like a Human (Michael Lynch)

## Testing Strategies

### Test Pyramid

- Item: Structure tests as a pyramid: many unit tests, fewer integration tests, few E2E tests
  Summary: Different test types have different costs and benefits; balance them appropriately.
  Body:
  The test pyramid: broad base of fast, focused unit tests; middle layer of integration tests verifying components work together; small top layer of expensive, slow end-to-end tests. Unit tests are cheap, fast, and easy to maintain—use them extensively for business logic. Integration tests verify interactions (database, external services)—slower but necessary. E2E tests verify full user workflows—expensive and brittle, use sparingly for critical paths only. Inverted pyramids (mostly E2E tests) are slow and fragile.
  Sources: Succeeding with Agile (Mike Cohn)

### Unit Testing Principles

- Item: Test behavior, not implementation details
  Summary: Tests should verify what code does, not how it does it.
  Body:
  Tests should interact with code through its public API and verify observable behavior. Don't test private methods directly—test them through public methods. Don't assert on internal state unless necessary. This makes tests resilient to refactoring. If you refactor and all tests break, they're too coupled to implementation. Good tests: arrange inputs, call public methods, assert observable outputs/side effects. Think like a user of the API, not its implementer.
  Sources: Growing Object-Oriented Software, Guided by Tests (Freeman & Pryce)

- Item: Keep tests focused, fast, and independent
  Summary: Each test should verify one thing and run quickly without depending on other tests.
  Body:
  Unit tests should be FIRST: Fast (milliseconds), Independent (no shared state), Repeatable (deterministic), Self-validating (pass/fail, no manual inspection), Timely (written with or before code). One logical assertion per test. Use descriptive names that explain what's being tested. Avoid test interdependencies—tests should pass in any order. Use test fixtures/setup for common initialization, but be careful not to hide important context. Fast, focused tests give rapid feedback and encourage running tests frequently.
  Sources: Clean Code (Robert Martin)

### Integration Testing

- Item: Use integration tests to verify component interactions
  Summary: Test that components work together correctly: database, external APIs, message queues.
  Body:
  Integration tests verify that your code works with real external systems. Test database operations with a real database (use test containers or in-memory databases). Test API integrations with real endpoints (use test instances or contract tests). Test message handling with real brokers. These tests are slower than unit tests but catch issues unit tests can't. Use them judiciously—not every path needs integration tests, just critical interactions. Consider contract testing (Pact) for API integrations.
  Sources: Growing Object-Oriented Software, Guided by Tests (Freeman & Pryce)

### End-to-End Testing

- Item: Reserve E2E tests for critical user journeys
  Summary: E2E tests are expensive; use them for high-value, business-critical workflows only.
  Body:
  E2E tests verify complete user workflows through the entire system. They're slow, brittle, and expensive to maintain. Use them for: critical business flows (signup, checkout, payment), smoke tests (is the system alive?), and workflows that cross many components. Don't use E2E tests to verify edge cases—that's what unit tests are for. Keep E2E tests stable by using good selectors, explicit waits, and retry logic. Run them less frequently (nightly builds, pre-release). Consider API-level E2E tests instead of UI tests when possible.
  Sources: Testing Strategies in a Microservice Architecture (Toby Clemson)

### Test Doubles and Mocking

- Item: Use test doubles strategically to isolate units under test
  Summary: Mocks, stubs, and fakes enable focused testing but can make tests brittle.
  Body:
  Test doubles replace dependencies: stubs return predetermined values, mocks verify interactions, fakes are working implementations (in-memory database). Use them to isolate the unit under test and control test conditions. But over-mocking makes tests brittle and couples them to implementation. Prefer real objects when practical. Mock I/O boundaries (HTTP, database, file system), not domain objects. Be especially careful with partial mocks—they're usually a design smell. If mocking is painful, the design might need improvement.
  Sources: Growing Object-Oriented Software, Guided by Tests (Freeman & Pryce)

## Security Best Practices

### Input Validation and Sanitization

- Item: Validate and sanitize all external input
  Summary: Never trust user input; validate, sanitize, and enforce constraints.
  Body:
  All input from users, APIs, file uploads, or external systems is untrusted. Validate early: check types, lengths, formats, ranges. Use allowlists (known good values) not denylists (known bad values). Sanitize input before use: escape for context (HTML, SQL, shell), strip dangerous characters. Reject invalid input with clear error messages. This prevents injection attacks (SQL, XSS, command injection), buffer overflows, and malformed data issues. Input validation is your first line of defense.
  Sources: OWASP Top Ten, Secure Coding Guidelines

- Item: Use parameterized queries to prevent SQL injection
  Summary: Never concatenate user input into SQL queries; use parameters instead.
  Body:
  SQL injection is still a top vulnerability. Always use parameterized queries/prepared statements: `SELECT * FROM users WHERE email = ?` with parameter binding, never `SELECT * FROM users WHERE email = '${email}'`. ORMs usually handle this correctly, but verify. Even "safe" input can be dangerous. Parameterized queries prevent injection by keeping data separate from code. This applies to all databases. No exceptions.
  Sources: OWASP SQL Injection Prevention Cheat Sheet

### Authentication and Authorization

- Item: Implement defense in depth for authentication
  Summary: Use multiple security layers: strong passwords, MFA, rate limiting, session management.
  Body:
  Strong authentication requires: password requirements (length, complexity, no common passwords), secure hashing (bcrypt, Argon2, scrypt—never MD5/SHA1), MFA for sensitive operations, rate limiting on login attempts, account lockout for brute force protection, secure session management (HTTPOnly, Secure, SameSite cookies), password reset flows that don't leak information. Use established authentication libraries/services (Auth0, OAuth2) rather than rolling your own. Authentication bugs are expensive.
  Sources: OWASP Authentication Cheat Sheet

- Item: Implement proper authorization checks at every layer
  Summary: Verify permissions on every operation, not just at entry points.
  Body:
  Authorization must be enforced consistently: check permissions on every API endpoint, every database query, every business operation. Don't rely on UI hiding for security—users can bypass UIs. Check authorization after authentication. Use role-based or attribute-based access control. Default to deny—require explicit grants. Be especially careful with horizontal authorization (user A accessing user B's data). Log authorization failures. Test authorization thoroughly including edge cases. Many breaches result from authorization bugs.
  Sources: OWASP Authorization Cheat Sheet

### Secrets Management

- Item: Never commit secrets to version control
  Summary: Use environment variables, secret managers, or vaults for sensitive data.
  Body:
  Never hardcode secrets (API keys, passwords, tokens, certificates) in code or commit them to git. Use environment variables, secret management services (AWS Secrets Manager, HashiCorp Vault, Azure Key Vault), or configuration files excluded from version control. Rotate secrets regularly. Use different secrets for different environments. Scan repositories for accidentally committed secrets (git-secrets, truffleHog). If a secret is committed, assume it's compromised—rotate immediately and investigate. Git history persists forever.
  Sources: OWASP Secrets Management Cheat Sheet

### Secure Communication

- Item: Use TLS for all network communication
  Summary: Encrypt data in transit; don't send sensitive data over unencrypted connections.
  Body:
  Use HTTPS (TLS 1.2+) for all web traffic. Use TLS for database connections, internal service communication, and API calls. Don't allow fallback to unencrypted connections. Validate certificates properly—don't disable verification in production. Use HSTS headers to enforce HTTPS. Encrypt sensitive data at rest as well. Consider mutual TLS for service-to-service authentication. Network traffic can be intercepted; encryption is essential.
  Sources: OWASP Transport Layer Protection Cheat Sheet

## Performance Optimization Patterns

### Database Performance

- Item: Design indexes to match query patterns
  Summary: Indexes speed up queries but slow down writes; design them strategically.
  Body:
  Analyze query patterns and add indexes on: WHERE clause columns, JOIN columns, ORDER BY columns, frequently queried foreign keys. Composite indexes for multi-column queries (order matters: most selective first). Don't over-index—each index costs write performance and storage. Use EXPLAIN/ANALYZE to verify indexes are used. Consider covering indexes (include all columns needed by a query). Monitor slow queries and add indexes iteratively. Different databases have different index types (B-tree, hash, GiN, GiST)—choose appropriately.
  Sources: High Performance MySQL (Schwartz et al.)

- Item: Avoid N+1 query problems
  Summary: Load related data eagerly or batch queries to prevent excessive database roundtrips.
  Body:
  N+1 queries occur when you load a collection and then query for each item's relationships: one query for the list, N queries for related data. This is catastrophic for performance. Solutions: eager loading (JOIN or separate batch query), lazy loading with batching (DataLoader in GraphQL), caching. ORMs make N+1 easy to accidentally create—use query analyzers to detect. Profile database queries in development. One query returning 1000 rows is much faster than 1000 queries returning one row each.
  Sources: High Performance MySQL (Schwartz et al.)

### Caching Strategies

- Item: Implement caching at multiple layers
  Summary: Cache aggressively but invalidate correctly; use appropriate cache levels.
  Body:
  Caching improves performance dramatically but adds complexity. Cache at multiple levels: CDN (static assets), HTTP cache (response caching with ETags/Cache-Control), application cache (Redis, Memcached), database query cache. Choose TTL-based expiration for data that can be stale, or implement cache invalidation for data that must be fresh. Consider read-through, write-through, write-behind patterns. Remember: cache invalidation is hard—Phil Karlton said there are only two hard things in computer science: cache invalidation and naming things.
  Sources: Designing Data-Intensive Applications (Martin Kleppmann)

- Item: Use the right caching pattern for your needs
  Summary: Different caching patterns suit different requirements.
  Body:
  Cache-aside: application checks cache, queries on miss, updates cache. Read-through: cache loads data on miss. Write-through: writes go through cache to database. Write-behind: cache buffers writes. Refresh-ahead: proactively refresh before expiration. Choose based on: read/write ratio, consistency requirements, failure modes. Cache-aside is most common and flexible. Consider cache warming for critical data. Monitor hit rates. Distributed caches need thoughtful key design and eviction policies.
  Sources: Designing Data-Intensive Applications (Martin Kleppmann)

### Asynchronous Processing

- Item: Offload slow operations to background jobs
  Summary: Don't make users wait for slow operations; process them asynchronously.
  Body:
  Long-running operations (sending emails, generating reports, image processing, external API calls) should be asynchronous. Return immediately to the user and process in the background. Use job queues (Sidekiq, Celery, Bull, AWS SQS) with workers. Make jobs idempotent (safe to retry). Implement retry with exponential backoff. Provide status updates (webhooks, polling, WebSockets). This improves user experience and system resilience. But async adds complexity: debugging is harder, monitoring is essential, eventual consistency requires careful design.
  Sources: Release It! (Michael Nygard)

### Resource Management

- Item: Use connection pooling for database and external service connections
  Summary: Creating connections is expensive; reuse them through pooling.
  Body:
  Opening database connections, HTTP connections, or other network connections is expensive. Use connection pools to reuse connections. Configure pool size appropriately: too small causes queuing, too large exhausts server resources. Monitor pool usage—exhausted pools indicate performance issues. Most frameworks provide pooling (database drivers, HTTP clients). For serverless, consider connection management strategies (connection poolers like PgBouncer, HTTP keep-alive). Proper connection management prevents resource exhaustion.
  Sources: High Performance MySQL (Schwartz et al.)

---

# Usage Guide

# AI Copilot Workflows

Practical guidance for working with Spaces, the daemon, approvals, and daily AI-assisted coding workflows.

## Spaces and Knowledge

Understanding Spaces as personal knowledge experts that automatically improve code generation.

- Item: Understanding Spaces
  Summary: Spaces are personal knowledge experts that automatically improve code generation.
  Summary: AI analyzes requests and picks relevant Spaces to apply proven patterns.
  Summary: Generated code is validated against Space rules and auto-revised if issues found.
  Body:
  Spaces work behind the scenes when you ask AI to write code. They apply proven patterns from your knowledge base, catch common mistakes before you see them, suggest the right approach for each task, and learn from your feedback to get better over time. AI analyzes your request and picks relevant Spaces automatically. For example, "implement user authentication" uses go-best-practices and security-guidelines, while "fix this test failure" uses testing-patterns and debugging-checklist. Generated code is automatically checked against Space rules. If validation finds issues, AI fixes them (up to 2 attempts) so you get corrected code on the first try.
  Sources: docs/SPACES_USER_GUIDE.md

- Item: Task types recognized by Spaces
  Summary: Spaces recognize implementation, debugging, testing, refactoring, documentation, and architecture tasks.
  Summary: Each task type triggers different Space selection and validation rules.
  Body:
  Task types recognized include: Implementation (building new features), Debugging (finding and fixing bugs), Testing (writing or updating tests), Refactoring (improving existing code), Documentation (writing docs or comments), and Architecture (design and structure decisions). Each task type triggers different Space selection and validation logic.
  Sources: docs/SPACES_USER_GUIDE.md

- Item: Importing Knowledge Base into Spaces
  Summary: Import curated Knowledge Base JSON files into Spaces for coding, review, and research.
  Summary: Subscriber-only feature that makes organizational knowledge available to copilot.
  Summary: Plain Markdown must be compiled to KB JSON format first.
  Body:
  Subscriber-only feature that imports curated Knowledge Base JSON files into Spaces so AI Copilot can use your team's expertise during coding, review, and research. Use when you need to seed a Space with coding standards, architecture guidance, or product playbooks. Import curated Knowledge Base exports or make organizational knowledge available to the copilot. You need a Space-compatible Knowledge Base JSON file (plain Markdown must be compiled/exported first). Steps: Open Data Manager or Space view, create new Space or select existing one, click Import JSON action, choose your Knowledge Base JSON file, confirm import and wait for contents to load, start using imported expertise in coding workflows. Best practices: keep imported KB focused on clear domain or team need, use consistent categories and concise items, re-import after revising underlying guidance, test in small Space before broad use.
  Sources: docs/SPACES_USER_GUIDE.md

## Daemon Integration

Working with the local daemon for terminal execution and connected AI workflows.

- Item: Starting the daemon
  Summary: Launch daemon or local workflow before using AI Copilot for connected tasks.
  Summary: Confirm workflow is reachable at http://127.0.0.1:9090.
  Body:
  Before using AI Copilot for connected tasks, launch the daemon or local workflow. Confirm workflow is reachable (typically http://127.0.0.1:9090). Verify connection before relying on automation.
  Sources: docs/TERMINAL_LAUNCH_FEATURE.md

- Item: Terminal execution modes
  Summary: Commands can run in Terminal Mode (interactive, visible) or Background Mode (silent, output captured).
  Summary: Terminal Mode opens OS native terminal for interactive prompts like passwords.
  Summary: Background Mode runs invisibly and shows results in chat for simple commands.
  Body:
  Commands can run in two modes. Terminal Mode opens OS native terminal window where interactive prompts work (passwords, confirmations), terminal stays open after completion, best for: `sudo`, `ssh`, `vim`, `top`, `tail -f`. Background Mode runs invisibly, captures output, results shown in chat, no interaction possible, best for: `ls`, `pwd`, `git status`, `cat file.txt`. Smart defaults automatically suggest the right mode. Terminal for: `sudo`, `ssh`, editors, monitors. Background for: directory ops, file viewing, git queries. You can always override the suggestion. Example sudo command (auto-terminal): User asks "Run sudo apt update", approval modal suggests terminal mode, user clicks Allow, command launches in terminal window for password input. Example simple command (auto-background): User asks "Show current directory", approval modal defaults to background, user clicks Allow, local command executed with output shown directly.
  Sources: docs/TERMINAL_LAUNCH_FEATURE.md

## Approvals and Automation

Understanding approval guardrails and session-based auto-approval for trusted workflows.

- Item: Understanding approvals
  Summary: Approvals are guardrails for automation and sensitive actions requiring human review.
  Summary: Session-based auto-approval available for trusted workflows without repeated prompts.
  Body:
  Approvals are guardrails for automation and sensitive actions. They let you review what the system proposes before it acts. Human review is available for important decisions. Session-based auto-approval is available for trusted workflows so you don't get repeated prompts.
  Sources: docs/SESSION_APPROVAL_GUIDE.md

- Item: Session-based auto-approval
  Summary: Enable full automation for specific action types without repeated prompts.
  Summary: Action types include Shell Commands and Code Patches.
  Summary: Visual indicators show when auto-approval is active with badge and markers.
  Body:
  Enable full automation for specific action types without repeated prompts. Action types: Shell Commands (`local_shell_execution`) and Code Patches (`coding_workflow_patch`). To enable: perform a sensitive operation, when approval modal appears check "Always allow [action type] for this session", click Allow, future operations auto-execute. Visual indicators: 🚀 badge appears when auto-approval active, executed actions show *(auto-approved)* marker, click badge to view details or clear. To disable: click "Clear Session Approvals" in next modal, reload page (clears all session state), or click 🚀 badge and confirm clear. Session scope: active only for current browser session, survives tab switching, cleared on page reload, per-action-type (shell and code tracked separately).
  Sources: docs/SESSION_APPROVAL_GUIDE.md

- Item: When to use auto-approval
  Summary: Use auto-approval in trusted development environments for rapid iteration.
  Summary: Use caution with production systems, shared environments, and sensitive data.
  Body:
  Safe scenarios for auto-approval: trusted development environment, well-known codebase, rapid iteration workflows, debugging sessions, personal projects. Use caution with: production systems, shared environments, unfamiliar codebases, critical infrastructure, sensitive data operations. Best practices: review first action before enabling, enable only action types you trust, clear approvals after workflow completes, monitor 🚀 badge to know when active, remember reload resets approvals.
  Sources: docs/SESSION_APPROVAL_GUIDE.md

## Feedback and Quality

Giving feedback to improve Space performance and monitoring quality metrics.

- Item: Giving feedback to Spaces
  Summary: Click Helpful or Needs Work after Spaces are used to improve future performance.
  Summary: Good feedback increases Space priority, negative feedback reduces it.
  Summary: Feedback stored locally, no code sent to external servers.
  Body:
  After Spaces are used, you'll see feedback buttons: "👍 Helpful" and "👎 Needs Work". Click Helpful when: code works correctly on first try, patterns match your team's style, error handling is appropriate, tests are well-structured. Click Needs Work when: code doesn't match requirements, patterns feel too strict or loose, better approach should have been used, validation missed real issues. Why feedback matters: good feedback means Space used more often for similar tasks, negative feedback means Space used less and alternatives tried, system learns your preferences over time. Privacy: feedback stored locally, no code sent to external servers.
  Sources: docs/SPACES_USER_GUIDE.md

- Item: Understanding explanations
  Summary: Click How I used expertise to see what Spaces were used and why.
  Summary: Shows list of selected knowledge bases, code changes, and validation results.
  Body:
  Click "How I used expertise" to see: what Spaces were used, list of selected knowledge bases, why they were chosen for this task, what changed in the code, validation results and compliance score.
  Sources: docs/SPACES_USER_GUIDE.md

- Item: Quality dashboard
  Summary: Access http://127.0.0.1:9090/kb-quality for local daemon quality metrics.
  Summary: Shows total Spaces tracked, average success rate, critical issues, and performer rankings.
  Body:
  Access quality dashboard at http://127.0.0.1:9090/kb-quality (local daemon). What you can see: total Spaces tracked, average success rate, critical issues requiring attention, top and poor performers, usage counts and task types. Actions available: run quality check now, download full report, refresh data.
  Sources: docs/SPACES_USER_GUIDE.md

## Troubleshooting

Common issues and solutions when working with Spaces, daemon, and approvals.

- Item: No Spaces available for task
  Summary: Create relevant Spaces with knowledge files or broaden task description.
  Summary: Check Space status in quality dashboard.
  Body:
  When you see "No Spaces available for this task", try: create relevant Spaces with knowledge files, broaden task description with recognized keywords, check Space status in quality dashboard.
  Sources: docs/SPACES_USER_GUIDE.md

- Item: Space validation too strict
  Summary: Give Needs Work feedback to lower priority or review Space content.
  Summary: Use session override to disable temporarily.
  Body:
  When Space validation is too strict: give "Needs Work" feedback to lower priority, review Space content and update rules, use session override to disable temporarily.
  Sources: docs/SPACES_USER_GUIDE.md

- Item: Daemon not reachable
  Summary: Verify daemon is running and http://127.0.0.1:9090 is accessible.
  Summary: Confirm no firewall blocking local port.
  Body:
  When daemon is not reachable: verify daemon is running, check http://127.0.0.1:9090 is accessible, confirm no firewall blocking local port.
  Sources: docs/TERMINAL_LAUNCH_FEATURE.md

- Item: Imported content doesn't appear
  Summary: Verify target Space was selected and import completed successfully.
  Summary: Review category mapping in Space view.
  Body:
  When imported content doesn't appear: verify target Space was selected, check import completed successfully, review category mapping in Space view.
  Sources: docs/SPACES_USER_GUIDE.md

- Item: Approvals not showing
  Summary: Verify session flow is active and workflow state is correct.
  Summary: Confirm daemon connection is established.
  Body:
  When approvals are not showing: verify session flow is active, check current workflow state, confirm daemon connection established.
  Sources: docs/SESSION_APPROVAL_GUIDE.md

- Item: AI output doesn't match expectations
  Summary: Check if correct Space is active and verify knowledge source being used.
  Summary: Give feedback to improve future results.
  Body:
  When AI output doesn't match expectations: check if correct Space is active, verify knowledge source being used, give feedback to improve future results.
  Sources: docs/SPACES_USER_GUIDE.md

## Memory Architecture

Understanding how the System organizes data persistence and semantic memory.

- Item: Stores vs Spaces separation
  Summary: Stores are for structured data persistence, Spaces are for semantic memory.
  Summary: Both coexist but serve different purposes in the platform.
  Body:
  Stores vs Spaces: Stores handle structured data persistence (databases, transactions, CRUD operations). Spaces handle semantic memory (knowledge, categories, retrieval). Both coexist but serve different purposes. This separation keeps deterministic persistence and semantic retrieval aligned without forcing them into one abstraction.
  Sources: docs/SPACES_USER_GUIDE.md, lib/sop/ai/SOP_CURATED_KB.md

- Item: Knowledge bases as portable assets
  Summary: Export and import knowledge across environments while preserving structure.
  Summary: Move curated spaces between databases and deployments.
  Body:
  Knowledge bases are portable assets. You can export and import across environments, preserve category structure and semantic knowledge, and move curated spaces between databases and deployments.
  Sources: docs/SPACES_USER_GUIDE.md

- Item: Memory model
  Summary: Short-term memory is session-scoped state, long-term memory is persistent reusable knowledge.
  Summary: Knowledge Bases carry prompts, embedders, tool restrictions scoped to active domain.
  Body:
  Memory model includes: short-term memory (session-scoped state), long-term memory (persistent reusable knowledge). Knowledge Bases carry prompts, embedders, tool restrictions. Memory is scoped to the active domain.
  Sources: docs/SPACES_USER_GUIDE.md, lib/sop/ai/SOP_CURATED_KB.md



