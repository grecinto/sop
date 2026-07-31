# Workflows

<!-- 
  Workflow Category Detection:
  Categories are automatically marked with workflow_enabled=true when:
  1. The category heading has [workflow] marker (e.g., "## My Tasks [workflow]")
  2. The category contains items with workflow_template JSON payloads
  
  Parent categories (like "Playbooks") are NOT auto-marked unless they explicitly
  have the [workflow] marker or contain workflow items directly.
  
  Workflow items are displayed as expandable tree leaves in the UI.
-->

These workflows are intentionally limited to a small set of stable, repeatable coding patterns. For more ad hoc or context-dependent work, the assistant should rely on the Ask pipeline and adapt dynamically rather than depend on a predefined workflow.

## Coding Workflows [DISABLED]

**Note:** KB-driven workflows are disabled in V1. The Coding Workflow Capability uses compiled, infrastructure-backed workflows with AST parsing, tree-sitter, and semantic analysis. These data-driven workflow definitions are preserved for future development but are not active.

- Item: Fix failing tests
  Summary: Reproduce the failure, inspect the root cause, apply a focused fix, and verify the result with tests.
  Body:
  Use this workflow for a well-understood bug-fix loop where the failure already has a clear reproduction path.

  Workflow payload:
  ```json
  {
    "workflow_name": "Fix failing tests",
    "description": "Reproduce the failure, inspect the root cause, apply a focused fix, and verify the result with tests.",
    "workflow_template": {
      "schema_version": "1.0",
      "name": "Fix failing tests",
      "description": "Reproduce the failure, inspect the root cause, apply a focused fix, and verify the result with tests.",
      "protocol_guidance": "Always capture the failing output before changing code, read the relevant test and implementation files before editing, and verify the fix with the same test afterward.",
      "steps": [
        {
          "step": 1,
          "title": "Reproduce the failure",
          "instructions": "Run the relevant test or command and capture the failing output.",
          "required_context": ["test output", "failing stack trace"],
          "success_criteria": ["The failure is reproduced and summarized."],
          "iteration_policy": "Retry up to 3 times if the failure is unclear.",
          "tools_expected": ["run_tests", "read_file"]
        },
        {
          "step": 2,
          "title": "Inspect the relevant code",
          "instructions": "Read the relevant test and implementation files, then identify the likely root cause.",
          "required_context": ["test file", "implementation file", "error context"],
          "success_criteria": ["The likely root cause is identified."],
          "tools_expected": ["read_file", "search", "grep"]
        },
        {
          "step": 3,
          "title": "Apply a focused fix",
          "instructions": "Make the smallest change that addresses the root cause while preserving behavior.",
          "required_context": ["root cause analysis", "implementation context"],
          "success_criteria": ["The fix is applied with minimal change."],
          "tools_expected": ["edit_file", "patch_file"]
        },
        {
          "step": 4,
          "title": "Verify the fix",
          "instructions": "Re-run the relevant tests and confirm the original failure no longer occurs.",
          "required_context": ["test command", "changed files"],
          "success_criteria": ["The relevant tests pass and the issue is resolved."],
          "iteration_policy": "Retry up to 3 times if verification is incomplete.",
          "tools_expected": ["run_tests", "read_file"]
        }
      ]
    }
  }
  ```

- Item: Add regression coverage
  Summary: Add focused tests for a behavior change or bug fix so the change is protected going forward.
  Body:
  Use this workflow when a change needs targeted test coverage and the expected behavior is clear.

  Workflow payload:
  ```json
  {
    "workflow_name": "Add regression coverage",
    "description": "Analyze the behavior, add focused tests, and verify them with the relevant test suite.",
    "workflow_template": {
      "schema_version": "1.0",
      "name": "Add regression coverage",
      "description": "Analyze the behavior, add focused tests, and verify them with the relevant test suite.",
      "protocol_guidance": "Read the implementation first, define the expected behavior, and add tests that cover the main success and failure cases.",
      "steps": [
        {
          "step": 1,
          "title": "Inspect the behavior",
          "instructions": "Read the relevant code and clarify what behavior should be exercised by tests.",
          "required_context": ["implementation code", "behavior expectations"],
          "success_criteria": ["The target behavior is understood."],
          "tools_expected": ["read_file", "search"]
        },
        {
          "step": 2,
          "title": "Write targeted tests",
          "instructions": "Add focused tests that exercise the intended behavior and the most relevant edge cases.",
          "required_context": ["test conventions", "target behavior"],
          "success_criteria": ["Relevant tests are added."],
          "tools_expected": ["write_file", "read_file"]
        },
        {
          "step": 3,
          "title": "Verify the tests",
          "instructions": "Run the relevant test commands and confirm the new coverage passes.",
          "required_context": ["test command"],
          "success_criteria": ["The new tests pass and provide meaningful protection."],
          "tools_expected": ["run_tests"]
        }
      ]
    }
  }
  ```




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



