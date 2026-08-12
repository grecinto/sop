package codingworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	log "log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/teampatterns"
	"github.com/sharedcode/sop/ai/memory"
)

// PhaseCapability exposes a phase of the coding workflow as a capability that the
// LLM can invoke dynamically during conversation. Each phase capability owns a
// lightweight artifact contract and can persist its state into shared memory so
// later Ask turns continue from the same context. The phase flow stays simple:
// the conversation can transition to a successor phase or exit the current phase
// whenever the user or the agent decides that is appropriate.
type PhaseCapability struct {
	sharedInfra *SharedInfrastructure
	phaseID     string
	name        string
	description string
	captureTool string
	stateTool   string
}

func NewIntakePhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "intake", "Intake & Context", "requirements gathering and context assembly")
}

func NewInvestigationPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "investigation", "Investigation & Analysis", "repository investigation and technical analysis")
}

func NewPlanningPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "planning", "Planning & Design", "planning and design for implementation")
}

func NewTestingPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "testing", "Testing & Validation", "test-first validation and regression checks")
}

func NewExecutionPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "execution", "Execution & Implementation", "implementation and patch application")
}

func NewReviewPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "review", "Review & Validation", "validation, verification, and review")
}

func NewDeliveryPhaseCapability(sharedInfra *SharedInfrastructure) *PhaseCapability {
	return newPhaseCapability(sharedInfra, "delivery", "Delivery & Handoff", "delivery summarization and handoff")
}

func newPhaseCapability(sharedInfra *SharedInfrastructure, phaseID, name, description string) *PhaseCapability {
	return &PhaseCapability{
		sharedInfra: sharedInfra,
		phaseID:     phaseID,
		name:        name,
		description: description,
		captureTool: phaseID + "_capture",
		stateTool:   phaseID + "_state",
	}
}

func (c *PhaseCapability) ID() string { return "phase_" + c.phaseID }

func (c *PhaseCapability) Name() string { return c.name }

func (c *PhaseCapability) Entitlement() capabilities.Entitlement {
	return capabilities.Entitlement{
		EnabledByDefault: false,
		Tier:             "local",
		RequiredGate:     capabilities.GateWriteAccess,
		Approvals: []capabilities.ApprovalType{
			capabilities.ApprovalFileWrite,
		},
		Permissions: []capabilities.Permission{{
			Resource: "filesystem",
			Actions:  []string{"read", "write", "create", "delete"},
			Gate:     capabilities.GateWriteAccess,
		}, {
			Resource: "memory",
			Actions:  []string{"read", "write"},
			Gate:     capabilities.GateReadAccess,
		}},
	}
}

func (c *PhaseCapability) BuildGuidanceProfile(query string, state capabilities.RoutingState) capabilities.GuidanceProfile {
	lower := strings.ToLower(strings.TrimSpace(query))
	phaseRules := []capabilities.GuidanceRule{}
	verificationRules := []capabilities.GuidanceRule{}
	guardrails := []capabilities.GuidanceRule{}

	phaseRules = append(phaseRules, capabilities.GuidanceRule{
		ID:          "phase_role",
		Category:    "phase",
		Priority:    "high",
		Description: fmt.Sprintf("Operate as the %s phase capability and keep the current phase focused on its job.", c.name),
		AppliesTo:   []string{c.phaseID},
	})

	switch c.phaseID {
	case "intake":
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "intake_scope", Category: "phase", Priority: "high", Description: "Clarify scope, goals, and constraints before acting", AppliesTo: []string{"intake"}})
		verificationRules = append(verificationRules, capabilities.GuidanceRule{ID: "intake_verification", Category: "verification", Priority: "medium", Description: "Capture a concrete requirements artifact before moving on", AppliesTo: []string{"intake"}})
	case "investigation":
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "investigate_evidence", Category: "phase", Priority: "high", Description: "Gather evidence from the repository, tests, and surrounding code before proposing a fix", AppliesTo: []string{"investigation"}})
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "trace_flow", Category: "phase", Priority: "high", Description: "Trace the relevant flow or execution path before changing behavior", AppliesTo: []string{"investigation"}})
		verificationRules = append(verificationRules, capabilities.GuidanceRule{ID: "investigation_validation", Category: "verification", Priority: "medium", Description: "Support the next phase with concrete evidence and findings", AppliesTo: []string{"investigation"}})
	case "planning":
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "design_brief", Category: "phase", Priority: "high", Description: "Create a structured design brief with problem statement, constraints, trade-offs, and recommended approach", AppliesTo: []string{"planning"}})
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "team_practice", Category: "phase", Priority: "high", Description: "Look up relevant Team Practice guidance from the Core Playbook when it applies", AppliesTo: []string{"planning"}})
		verificationRules = append(verificationRules, capabilities.GuidanceRule{ID: "planning_verification", Category: "verification", Priority: "medium", Description: "Ensure the plan includes concrete steps, dependencies, and validation ideas", AppliesTo: []string{"planning"}})
	case "testing":
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "test_guidance", Category: "phase", Priority: "high", Description: "Use the relevant tests or validation surfaces to shape the change", AppliesTo: []string{"testing"}})
		verificationRules = append(verificationRules, capabilities.GuidanceRule{ID: "testing_verification", Category: "verification", Priority: "medium", Description: "Capture the exact test or validation command and result", AppliesTo: []string{"testing"}})
	case "execution":
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "preview_before_apply", Category: "phase", Priority: "high", Description: "Preview changes and obtain approval before applying patches", AppliesTo: []string{"execution"}})
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "keep_scope", Category: "phase", Priority: "medium", Description: "Keep the change scoped to the observed need and validate after each significant change", AppliesTo: []string{"execution"}})
		verificationRules = append(verificationRules, capabilities.GuidanceRule{ID: "execution_verification", Category: "verification", Priority: "high", Description: "Run the most relevant validation step before concluding the change", AppliesTo: []string{"execution"}})
	case "review":
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "review_evidence", Category: "phase", Priority: "high", Description: "Validate the implementation against requirements and evidence", AppliesTo: []string{"review"}})
		verificationRules = append(verificationRules, capabilities.GuidanceRule{ID: "review_verification", Category: "verification", Priority: "high", Description: "Report what was validated and what remains uncertain", AppliesTo: []string{"review"}})
	case "delivery":
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "handoff_summary", Category: "phase", Priority: "medium", Description: "Summarize the outcome, important decisions, and follow-up steps clearly", AppliesTo: []string{"delivery"}})
	}

	if strings.Contains(lower, "bug") || strings.Contains(lower, "debug") || strings.Contains(lower, "fix") {
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "bug_fix_focus", Category: "situation", Priority: "high", Description: "Treat the task as a bug-fix investigation and trace the failing path before changing behavior", AppliesTo: []string{"bug_fix"}})
		guardrails = append(guardrails, capabilities.GuidanceRule{ID: "bug_fix_guardrail", Category: "guardrail", Priority: "high", Description: "Do not patch the symptom without identifying the root cause", AppliesTo: []string{"bug_fix"}})
	}
	if strings.Contains(lower, "design") || strings.Contains(lower, "architecture") || strings.Contains(lower, "api") || strings.Contains(lower, "contract") {
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "design_focus", Category: "situation", Priority: "high", Description: "Frame the requested capability with constraints, trade-offs, and recommended approach before implementation", AppliesTo: []string{"design"}})
	}
	if strings.Contains(lower, "refactor") || strings.Contains(lower, "cleanup") || strings.Contains(lower, "simplify") {
		phaseRules = append(phaseRules, capabilities.GuidanceRule{ID: "refactor_focus", Category: "situation", Priority: "high", Description: "Preserve behavior while improving structure and maintainability", AppliesTo: []string{"refactor"}})
	}

	base := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityLight, fmt.Sprintf("You are the %s capability for %s.", c.name, c.description))
	base.Principles = []string{"Prefer evidence over assumption", "Keep the current phase focused on its responsibilities"}
	base.PhaseRules = phaseRules
	base.VerificationRules = verificationRules
	base.Guardrails = guardrails

	profile := capabilities.GuidanceProfile{
		Principles: []string{"Use the most relevant tools and evidence for the task"},
	}.ApplyDefaults(base)

	return profile
}

func (c *PhaseCapability) Classify(query string, state capabilities.RoutingState) capabilities.ClassificationResult {
	if c == nil {
		return capabilities.ClassificationResult{}
	}

	// If a domain is explicitly set (Spaces, Stores, etc.), defer to domain capabilities
	// Phase capabilities only apply to coding workflow domain
	if state.Domain != "" && state.Domain != "coding_workflow" && state.Domain != "CodingWorkflow" {
		return capabilities.ClassificationResult{}
	}

	// Explicit capability hint takes highest priority
	if hint := strings.TrimSpace(strings.ToLower(state.CapabilityHint)); hint != "" {
		if strings.Contains(hint, c.phaseID) || strings.Contains(hint, strings.ReplaceAll(c.name, " ", "_")) {
			return capabilities.ClassificationResult{Relevant: true, Score: 15, Mode: capabilities.CapabilityModePrimary}
		}
	}

	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "" {
		return capabilities.ClassificationResult{}
	}

	// Detect the appropriate starting phase based on task semantics
	startingPhase := detectStartingPhase(lower)
	if startingPhase == c.phaseID {
		return capabilities.ClassificationResult{Relevant: true, Score: 14, Mode: capabilities.CapabilityModePrimary}
	}

	// Check for phase-specific keywords (supporting role)
	for _, keyword := range phaseKeywords(c.phaseID) {
		if strings.Contains(lower, keyword) {
			return capabilities.ClassificationResult{Relevant: true, Score: 12, Mode: capabilities.CapabilityModeSupporting}
		}
	}
	return capabilities.ClassificationResult{}
}

// detectStartingPhase analyzes the query to determine which phase should start the workflow.
// This provides semantic routing based on task characteristics rather than simple keyword matching.
func detectStartingPhase(queryLower string) string {
	// Review-oriented tasks: validation, checking, auditing
	// Check review first because phrases like "review code for bugs" should be review, not investigation
	reviewSignals := []string{
		"review code", "code review", "audit", "check for", "verify that",
		"validate that", "ensure that", "confirm that",
	}
	for _, signal := range reviewSignals {
		if strings.Contains(queryLower, signal) {
			return "review"
		}
	}

	// Investigation-oriented tasks: problems, bugs, errors, failures
	// Start with investigation to understand the issue before planning fixes
	// Use more specific phrases to avoid false positives (e.g., "fix bug" not just "bug")
	investigationSignals := []string{
		"fix", "debug", "error", "failing", "failed", "broken", "issue", "problem",
		"not working", "doesn't work", "won't", "can't", "crash", "exception",
		"incorrect", "wrong", "unexpected", "why", "what's wrong", "troubleshoot",
		"fix bug", "find bug", "bug in", "bug with",
	}
	for _, signal := range investigationSignals {
		if strings.Contains(queryLower, signal) {
			return "investigation"
		}
	}

	// Planning-oriented tasks: design, architecture, approach
	// Start with planning when the task is about designing a solution
	planningSignals := []string{
		"design", "architect", "how should", "approach for", "strategy for",
		"best way to", "should i", "how to implement",
	}
	for _, signal := range planningSignals {
		if strings.Contains(queryLower, signal) {
			return "planning"
		}
	}

	// Direct execution tasks: specific changes with clear instructions
	// Skip to execution when the task is a simple, well-defined change
	executionSignals := []string{
		"apply this patch", "update the version", "change the config",
		"set the value", "add this line", "remove this code",
	}
	for _, signal := range executionSignals {
		if strings.Contains(queryLower, signal) {
			return "execution"
		}
	}

	// Default to intake for feature requests, unclear tasks, or new work
	// Intake phase will gather requirements and provide proper context
	return "intake"
}

func phaseKeywords(phaseID string) []string {
	switch phaseID {
	case "intake":
		return []string{"requirement", "requirements", "context", "goal", "scope", "clarify", "ask", "understand"}
	case "investigation":
		return []string{"inspect", "investigate", "analy", "debug", "trace", "error", "repo", "search"}
	case "planning":
		return []string{"plan", "design", "architecture", "approach", "proposal", "strategy"}
	case "testing":
		return []string{"test", "testing", "verify", "validate", "regression", "coverage", "assert"}
	case "execution":
		return []string{"implement", "execute", "patch", "modify", "write", "change", "fix"}
	case "review":
		return []string{"review", "validate", "verify", "check", "test", "quality"}
	case "delivery":
		return []string{"deliver", "handoff", "summarize", "summary", "report", "complete"}
	default:
		return nil
	}
}

func (c *PhaseCapability) BuildContext(query string, state capabilities.RoutingState) string {
	contextText := c.buildPhaseSpecificContext(query, state)
	artifact, err := c.loadExistingArtifact()
	if err != nil {
		return contextText
	}
	if clarificationText := formatPhaseClarificationContext(artifact); clarificationText != "" {
		return contextText + "\n\n" + clarificationText
	}
	return contextText
}

func (c *PhaseCapability) buildPhaseSpecificContext(query string, state capabilities.RoutingState) string {
	switch c.phaseID {
	case "intake":
		return c.buildIntakeContext()
	case "investigation":
		return c.buildInvestigationContext()
	case "planning":
		return c.buildPlanningContext()
	case "testing":
		return c.buildTestingContext()
	case "execution":
		return c.buildExecutionContext()
	case "review":
		return c.buildReviewContext()
	case "delivery":
		return c.buildDeliveryContext()
	default:
		return c.buildGenericContext()
	}
}

func (c *PhaseCapability) buildGenericContext() string {
	return fmt.Sprintf("Phase capability: %s\n- Use this capability when the conversation is in the %s phase.\n- Capture the current artifact into memory so later Ask turns can continue from the same context.\n- If the user wants to move on, record a successor phase or signal an exit so the workflow can proceed fluidly.", c.name, c.description)
}

func (c *PhaseCapability) buildIntakeContext() string {
	return `PHASE: Intake & Context Assembly

OBJECTIVE:
- Gather and clarify user requirements
- Build initial context about the task
- Identify scope, constraints, and success criteria
- Output: A clear requirements artifact for downstream phases

INTERACTION MODES:
This phase supports TWO interaction modes - choose based on request clarity and user engagement style:

• TRADITIONAL MODE (Complete in one turn):
  - User's request is clear and unambiguous
  - Requirements can be documented without extensive back-and-forth
  - Execute: Gather context → Document requirements → Capture artifact → Transition (all in one response)

• ITERATIVE MODE (Short interactions):
  - Request is ambiguous or has multiple interpretations
  - User prefers collaborative refinement
  - Execute: Ask 1-2 clarifying questions → Wait for user response → Continue in next Ask turn
  - Capture intermediate artifact with status="gathering" or "clarifying"

WORKFLOW:
1. Ask clarifying questions to understand the request
2. Call intake_build_context to gather repository context if applicable
3. Document requirements, constraints, and goals
4. Capture artifact with requirements list
5. Transition to investigation when requirements are clear

COMPLETION CRITERIA:
□ User's intent is clearly documented
□ Requirements list has >= 1 concrete requirement
□ Scope is defined (what's included/excluded)
□ Success criteria or acceptance criteria documented
□ Artifact captured with status="complete"
□ Ready to transition to investigation or planning

PHASE TRANSITIONS:
You can move FORWARD or BACKWARD. Capture transition intent when ready:

• FORWARD to Investigation (typical path):
  - Requirements are clear enough to research codebase
  - Capture: status="complete", transition_to="investigation", transition_direction="forward"

• FORWARD to Planning (skip investigation if context is very clear):
  - Simple change with known implementation path
  - Capture: status="complete", transition_to="planning", transition_direction="forward"

• EXIT:
  - User wants to pause or cancel
  - Capture: exit_requested=true

No backward transition from Intake (this is the first phase).

ARTIFACT FIELDS:
- summary: One-sentence description of what user wants
- status: "gathering" | "clarifying" | "complete"
- requirements: ["req 1", "req 2", ...] - concrete, testable requirements
- notes: Constraints, assumptions, risks, dependencies
- next_steps: Guidance for investigation phase

USER INTERACTION:
ASK WHEN: Requirements are ambiguous, scope unclear, multiple interpretations possible
AUTONOMOUS: When request is clear and unambiguous

QUALITY STANDARDS:
✅ GOOD: "Add Redis caching to getUserByID() with 5-minute TTL"
❌ BAD: "Make it faster"
✅ GOOD: "Cache should invalidate on user update/delete operations"
❌ BAD: "Add caching"`
}

func (c *PhaseCapability) buildInvestigationContext() string {
	return `PHASE: Investigation & Analysis

OBJECTIVE:
- Understand the relevant codebase state
- Identify relevant files, functions, patterns, and dependencies
- Produce evidence that can support planning or execution
- Output: A structured investigation artifact with findings and next-step guidance

INTERACTION MODES:
This phase supports TWO interaction modes - choose based on task complexity and user preference:

• TRADITIONAL MODE (Complete in one turn):
  - Task scope is well-defined from intake
  - Repository is familiar or well-structured
  - Execute: Build context → Search → Inspect → Document evidence → Capture → Transition (all in one response)

• ITERATIVE MODE (Short interactions):
  - Large/complex codebase requiring exploration
  - User wants to see findings progressively
  - Execute: Build context → Present initial findings → Wait for user direction → Continue investigation
  - Capture intermediate artifact with status="exploring" after each batch of findings

WORKFLOW:
1. Call investigation_build_context to understand repository structure
2. Inspect the request, the current evidence, and any relevant failure or error signals before proposing a fix.
3. Gather evidence from the repository, tests, and surrounding code before moving to planning.
4. Call investigation_inspect_repo to see current state and changes
5. Call investigation_search_repo to find relevant code, patterns, or similar implementations
6. Call investigation_list_changed_files to avoid conflicts with in-progress work
7. Document findings in evidence list
8. Capture artifact with evidence and analysis
9. Transition to planning when investigation is complete

COMPLETION CRITERIA:
□ Repository context gathered (structure understood)
□ Relevant files/functions identified
□ Similar patterns or implementations found (or documented as novel)
□ Dependencies and data flows traced
□ Evidence list has >= 3 findings
□ Artifact captured with status="complete"
□ Ready to transition to planning

PHASE TRANSITIONS:
You can move FORWARD or BACKWARD. Capture transition intent when ready:

• FORWARD to Planning (typical path):
  - Evidence gathered, ready to design solution
  - Capture: status="complete", transition_to="planning", transition_from="intake", transition_direction="forward"

• BACKWARD to Intake (rare, but valid):
  - Investigation reveals requirements were misunderstood
  - User changes scope during investigation
  - Capture: transition_to="intake", transition_from="investigation", transition_direction="backward"

• EXIT:
  - User wants to pause after seeing findings
  - Capture: exit_requested=true

ARTIFACT FIELDS:
- summary: One-sentence investigation summary
- status: "exploring" | "analyzing" | "complete"
- requirements: (from intake phase)
- evidence: ["finding 1: file.go:123 does X", "finding 2: pattern Y in lib/", ...]
- notes: Technical details, constraints, risks
- next_steps: Recommendations for planning

TOOL USAGE:
- ALWAYS call investigation_build_context first
- SEARCH before assuming patterns don't exist
- INSPECT to understand current state
- LIST changed files to avoid conflicts

QUALITY STANDARDS:
✅ GOOD: "getUserByID() in services/user_service.go:87 makes direct DB calls without caching"
❌ BAD: "User service needs caching"
✅ GOOD: "Found similar cache pattern in services/product_service.go:145 using shared/cache/manager.go"
❌ BAD: "Caching exists elsewhere"`
}

func (c *PhaseCapability) buildPlanningContext() string {
	return `PHASE: Planning & Design

OBJECTIVE:
- Turn gathered context into a concrete implementation strategy
- Create an ordered set of implementation steps
- Identify relevant files, functions, and code locations
- Output: A concrete execution plan with >= 3 steps
- Before finalizing design or implementation choices, look up relevant Team Practice guidance from the Core Playbook (CP) and incorporate it when it applies
- When code will be generated, follow the general design and coding best practices of the language and ecosystem: keep the design modular, use appropriate object-oriented structure when relevant, separate responsibilities across small focused units, prefer established patterns over ad hoc logic, and keep the implementation consistent with the surrounding codebase's architecture
- Add concise comments only where they clarify intent, contracts, invariants, or non-obvious logic

INTERACTION MODES:
This phase supports TWO interaction modes - choose based on change complexity and risk:

• TRADITIONAL MODE (Complete in one turn):
  - Changes are straightforward with clear implementation path
  - Similar patterns exist in codebase (from investigation)
  - Low risk (localized changes, good test coverage)
  - Execute: Gather context → Research patterns → Generate plan → Present for approval → Capture (all in one response)

• ITERATIVE MODE (Short interactions, design session):
  - Complex changes requiring architecture decisions
  - Multiple implementation strategies possible
  - High risk (breaking changes, cross-cutting concerns)
  - User wants to validate the design before detailed implementation
  - Execute: Present high-level design/approach → Get feedback → Refine → Present detailed plan/design → User approves → Record approved plan → Transition to execution
  - Capture intermediate artifact with status="drafting" after each iteration

IMPORTANT RULE FOR DESIGN-ORIENTED REQUESTS:
- Planning is a design session, not an implementation session.
- For feature design, architecture design, workflow design, API/module design, or proposal work, produce a design brief before proposing implementation details.
- The design brief must include scope, constraints, non-goals, assumptions, key trade-offs, risks, likely interfaces, and the recommended approach.
- If the user asks for diagrams, design docs, architecture proposals, or other design artifacts, generate those in planning and present them for review.
- Do not jump into implementation until the user signals approval of the design/plan.

WORKFLOW:
1. Review requirements from intake and evidence from investigation
2. If Continue bridge metadata is present, use that context to understand the editor-backed coding environment, but do not delegate implementation work during planning
3. Call planning_build_context to refresh repository understanding
4. Investigate the repository using the shared coding workflow tools: call planning_search_repo to find relevant implementation patterns and planning_inspect_repo to inspect the current code structure and changed files
5. Use the right tool family for the domain and phase: for coding, prefer AST/tree-sitter and repository inspection tools; for stores, prefer schema/relationship/data inspection tools before SQL or scripting; for testing, inspect test tools and current failures before proposing fixes; for other domains, use the relevant domain-specific investigation tools instead of generic search alone
6. When the task involves code structure, use the shared workflow's AST/tree-sitter-backed analysis capabilities to understand symbols, structure, and relevant code paths before designing the change
7. Check whether there is relevant Team Practice guidance in the Core Playbook (CP) for the domain, architecture, or coding pattern being considered; if present, treat it as a design constraint or strong recommendation rather than optional commentary
8. Call planning_list_changed_files to check for conflicts
9. Call planning_plan_changes to generate concrete plan
10. Review plan quality: each step references specific files/functions and the design is grounded in evidence
11. Present a clear user-visible plan message in the chat history that shows the generated plan details, design trade-offs, and any diagrams or design docs requested, and asks for approval from the user before implementation work begins
12. If the user changes requirements or requests refinements: iterate on the design and update the artifact (status="refining") until user approval is reached
13. If approved: record the approved plan in the active workstream artifact, capture status="ready_for_execution", and transition_to="execution"
14. If user requests additional documentation: generate design documentation and keep it attached to the planning artifact before moving forward
15. If changes needed: iterate on plan (update iteration counter), but remain in design-review mode until explicit approval is signaled

COMPLETION CRITERIA:
□ Context gathered and patterns researched
□ Plan has >= 3 concrete steps
□ Each step references specific files, functions, or line numbers
□ Dependencies are ordered correctly (types before usage, schema before queries)
□ Test strategy is included
□ User has reviewed and approved (or auto-approved for minor changes)
□ Artifact captured with status="ready_for_execution"
□ Transition signaled: transition_to="execution", transition_direction="forward"

PHASE TRANSITIONS:
You can move FORWARD or BACKWARD. Capture transition intent when ready:

• FORWARD to Execution (typical path):
  - Plan approved and ready for implementation
  - Capture: status="ready_for_execution", transition_to="execution", transition_from="investigation", transition_direction="forward"

• BACKWARD to Investigation (need more evidence):
  - Plan reveals gaps in understanding
  - User questions assumptions during review
  - Novel approach needs validation
  - Capture: transition_to="investigation", transition_from="planning", transition_direction="backward"

• BACKWARD to Intake (requirements changed):
  - User changes requirements during planning
  - Original scope was wrong
  - Capture: transition_to="intake", transition_from="planning", transition_direction="backward"

• EXIT:
  - User wants to review plan offline before proceeding
  - Capture: exit_requested=true

ARTIFACT FIELDS:
- summary: One-sentence plan overview
- status: "drafting" | "refining" | "ready_for_execution" | "blocked"
- requirements: (from intake)
- plan: ["step 1: specific action at file:line", "step 2: ...", ...]
- notes: Constraints, risks, dependencies, rollback strategy
- evidence: (from investigation - patterns found)
- next_steps: Guidance for execution phase
- transition_to: "execution"
- transition_direction: "forward"

TOOL SEQUENCE:
1. planning_build_context (REQUIRED FIRST repository/planning step)
2. planning_search_repo (research patterns BEFORE designing)
3. planning_inspect_repo (understand current state)
4. planning_list_changed_files (avoid conflicts)
5. planning_plan_changes (generate concrete plan)
6. planning_capture (save artifact)

CONTROL RULE:
- Planning may use Continue bridge context if present, but it should not call delegate_continue_task or otherwise delegate implementation work yet.
- Investigate the codebase with the shared coding workflow repository tools first, and use AST/tree-sitter-backed structural analysis when the feature request requires understanding existing implementation details.
- Keep Ask as the controller. Use planning to decide what should be executed later, not to start execution inside the planning phase.

QUALITY STANDARDS:
✅ CONCRETE: "Add CacheManager import to services/user_service.go (line 5)"
❌ VAGUE: "Import cache library"
✅ CONCRETE: "Wrap getUserByID() call in cache.Get() at line 87, key format: user:{id}"
❌ VAGUE: "Add caching to user lookup"
✅ CONCRETE: "Add unit tests for cache hit/miss/invalidation in user_service_test.go"
❌ VAGUE: "Add tests"
✅ ARCHITECTURE: Surface the main design trade-offs, interfaces, contracts, and non-goals before committing to a plan
✅ TEAM PRACTICE: Look up relevant Team Practice guidance from the Core Playbook (CP) for the design area and incorporate it in the proposed approach when it is applicable
✅ REVIEW: If the change is architecturally significant, explicitly note the likely risks and ask for architecture review when the approach is uncertain

BLOCKING CONDITIONS:
- Requirements unclear → Return to intake
- Repository path invalid → Ask user for correct path
- Novel approach unvalidated → Request architecture review
- User rejects plan → Iterate (increment iteration counter)
- Critical dependencies missing → Flag as blocked in status

USER INTERACTION:
ASK WHEN:
- Requirements ambiguous or conflicting
- Multiple implementation strategies exist (present options)
- Risky changes require approval
- Novel implementation without established patterns
PRESENT FOR REVIEW:
"Implementation plan: [formatted plan with steps]. Does this look correct?"`
}

func (c *PhaseCapability) buildTestingContext() string {
	return `PHASE: Testing & Validation

OBJECTIVE:
- Validate behavior with tests and regressions
- Confirm that the implementation matches the planned behavior
- Capture concrete evidence from the test run
- Default coverage target for the relevant package or affected feature area: 70% line coverage or higher, unless the user explicitly specifies a different requirement
- If the user specifies a higher target, follow the higher target; coverage is a quality bar for the relevant scope, not a reason to block normal iteration or design refinement unnecessarily

WORKFLOW:
1. Review the current plan and requirements
2. Use the relevant test or validation tools for the phase before proposing a change: inspect failing tests, relevant suites, or domain-specific validation surfaces
3. Add or refine a focused test or validation case when the change is behavioral or bug-fix oriented
4. Run the relevant test or validation command
5. Capture the result and any follow-up work
6. Transition to execution or review as appropriate

QUALITY STANDARDS:
✅ DESIGN AID: Use a focused test or validation case when it helps clarify intended behavior, contracts, or interfaces
✅ REGRESSION: Cover the bug or behavior change with a targeted test when possible
✅ COVERAGE: Default target is 70% for the relevant scope; higher user-specified thresholds override this default
✅ STALE TESTS: If a code mutation changes behavior, contracts, or interfaces, update or replace stale tests to match the new expected behavior; do not leave outdated assertions in place just because they still compile
✅ EVIDENCE: Report the exact command and pass/fail result
✅ ITERATION: Do not let a default coverage floor block normal iteration when the user is still refining design or implementation; prefer focused validation and incremental improvement`
}

func (c *PhaseCapability) buildExecutionContext() string {
	return `PHASE: Execution & Implementation

OBJECTIVE:
- Apply planned changes to the codebase
- Follow the execution plan from the planning phase
- Validate the work with the most relevant evidence
- Produce code that is modular, maintainable, readable, and aligned with the language's general design and coding best practices, including object-oriented organization, separation of concerns, and established design patterns where appropriate
- Add brief, useful comments for non-obvious logic, contracts, invariants, or API/behavior intent; avoid noisy comments that only restate obvious code
- Output: Applied changes and validation results

INTERACTION MODES:
This phase supports TWO interaction modes - user approval is MANDATORY for safety in BOTH modes:

• TRADITIONAL MODE (Batched execution):
  - Plan has 3-5 related changes that are safe to batch
  - Changes are independent (no cross-dependencies)
  - Execute: Preview ALL changes → Get bulk approval → Apply all → Run tests → Capture (2-3 interactions total)

• ITERATIVE MODE (Step-by-step execution):
  - Plan has many changes (6+ steps)
  - Changes have dependencies (must be applied in sequence)
  - User wants to validate each change before proceeding
  - High risk changes (schema migrations, refactoring)
  - Execute: Preview step 1 → Approve → Apply → Test → Present results → Preview step 2 → ...
  - Capture intermediate artifact with status="applying" after each step

NOTE: Even in traditional mode, ALWAYS preview before apply and get user approval. Safety is non-negotiable.

CONTROL RULE:
- Keep Ask as the controller. Use IDE worker delegation only for one bounded execution step, then return to the daemon-owned workflow.
- Prefer delegate_continue_task only when the edit is bounded to the active file and is clearly editor-native (for example a focused patch, refactor, or localized change).
- Prefer daemon-native editing when the change is broader multi-file, cross-cutting, or better handled as a direct edit by AI Studio itself.
1. Review plan from planning phase artifact
2. Use the right tool family for the domain and phase: for coding, prefer code-editing and AST-backed inspection tools; for stores, prefer the store/query tooling over ad hoc scripting; for testing, use the relevant test execution and inspection tools; for other domains, rely on the phase-specific toolset rather than generic shell commands
3. If editor-native execution would help and Continue context is available, consider whether this step should be delegated as one bounded Continue coding objective; prefer Continue only for bounded edits scoped to the active file, and prefer daemon-native editing for broader multi-file or cross-cutting changes
4. For each step in plan:
	a. Before any implementation or shell command is run, show a user-visible plan message in the chat history that includes the generated plan details and asks for approval from the user before running commands
	b. Call execution_preview_patch to preview changes
	c. Get user approval for preview (MANDATORY for safety)
	d. If a Continue worker is connected and the task is a bounded editor-native subtask scoped to the active file, call delegate_continue_task to execute that single coding step through Continue
	e. Otherwise call execution_apply_patch to apply changes directly
	f. Call execution_run_tests to validate
4. Capture artifact with results
5. If tests pass: Transition to review
6. If tests fail: inspect the failure, adjust the patch, and rerun the same targeted tests before expanding scope or returning to planning

COMPLETION CRITERIA:
□ All planned changes applied
□ Each patch previewed before application (safety)
□ Tests executed and passing
□ No regressions introduced
□ Artifact captured with status="complete"
□ Ready to transition to review

PHASE TRANSITIONS:
You can move FORWARD or BACKWARD. Capture transition intent when ready:

• FORWARD to Review (typical path):
  - All changes applied and tests passing
  - Capture: status="complete", transition_to="review", transition_from="planning", transition_direction="forward"

• BACKWARD to Planning (plan was flawed):
  - Implementation reveals plan won't work
  - Better approach discovered during execution
  - Tests fail due to design issues (not bugs)
  - Capture: transition_to="planning", transition_from="execution", transition_direction="backward", notes=[reason]

• EXIT:
  - User wants to pause mid-implementation
  - Capture: exit_requested=true

ARTIFACT FIELDS:
- summary: One-sentence execution summary
- status: "applying" | "testing" | "complete" | "failed"
- plan: (from planning - what was executed)
- notes: Implementation details, issues encountered
- evidence: Patches applied, test results
- next_steps: Guidance for review phase
- transition_to: "review"
- transition_direction: "forward"

SAFETY RULES (CRITICAL):
⚠️  MUST preview patch before applying
⚠️  MUST get user approval for destructive changes
⚠️  MUST run tests after each significant change
⚠️  MUST capture rollback information

TOOL SEQUENCE:
1. execution_preview_patch (show user what will change)
2. [WAIT for approval]
3. delegate_continue_task OR execution_apply_patch (choose the safer bounded path for this step)
4. execution_run_tests (validate changes)
5. execution_capture (save results)

QUALITY STANDARDS:
✅ SAFE: Preview → Approval → Apply → Validate
❌ UNSAFE: Apply without preview
✅ INCREMENTAL: Apply one logical change at a time
❌ BULK: Apply all changes at once without validation
✅ GROUNDED: Validate changes with the most relevant evidence and keep the fix scoped to the observed need

USER INTERACTION:
ALWAYS ASK:
- Before applying patches (show preview)
- When validation fails (explain the result, propose the next step)
- When encountering unexpected state`
}

func (c *PhaseCapability) buildReviewContext() string {
	return `PHASE: Review & Validation

OBJECTIVE:
- Validate that the implementation meets the requirements
- Review the resulting changes and evidence
- Check for regressions or unresolved issues
- Output: Validation results and approval status

INTERACTION MODES:
This phase supports TWO interaction modes - choose based on testing complexity and findings:

• TRADITIONAL MODE (Complete in one turn):
  - Simple changes with straightforward validation
  - All tests pass on first run
  - No issues found during review
  - Execute: Build context → Run tests → Validate requirements → Capture → Transition (all in one response)

• ITERATIVE MODE (Investigative validation):
  - Tests fail and require investigation
  - Quality issues need discussion
  - Requirements partially met (need clarification)
  - Execute: Run tests → Present results → Discuss failures → Fix → Re-test (multiple iterations)
  - Capture intermediate artifact with status="reviewing" after each validation attempt

CONTROL RULE:
- Keep Ask as the controller. Use IDE worker delegation only for one bounded review or validation step, then continue the daemon-owned review flow.

WORKFLOW:
1. Call review_build_context to understand current state
2. Call review_inspect_repo to see applied changes
3. Use the right tool family for the domain and phase: for coding, inspect the relevant code paths and diffs; for stores, validate against schema/data expectations; for testing, review the relevant test results and failure context; for other domains, use the review-focused toolset for that domain
4. If IDE-native review or validation would help and Continue context is available, consider whether this slice should be delegated as one bounded Continue coding objective
5. Call review_run_tests to validate functionality
6. If a bounded editor-native review step is useful, call delegate_continue_task for a focused diff review, IDE-scoped validation, or localized follow-up check
7. Compare results against requirements from intake
8. Check quality: code style, test coverage, documentation
9. Capture artifact with validation results
10. If approved: Transition to delivery
11. If issues found: Return to execution or planning

COMPLETION CRITERIA:
□ All requirements validated
□ Tests passing (unit, integration)
□ Code quality meets standards
□ No regressions detected
□ Documentation updated if needed
□ Artifact captured with status="approved"
□ Ready to transition to delivery

PHASE TRANSITIONS:
You can move FORWARD or BACKWARD. Capture transition intent when ready:

• FORWARD to Delivery (typical path):
  - All tests pass, validation complete
  - Capture: status="approved", transition_to="delivery", transition_from="execution", transition_direction="forward"

• BACKWARD to Execution (tests fail, minor fixes):
  - Test failures due to implementation bugs (not design issues)
  - Edge cases need handling
  - Capture: transition_to="execution", transition_from="review", transition_direction="backward", notes=[failures]

• BACKWARD to Planning (fundamental issues):
  - Design flaws discovered during testing
  - Requirements not actually met by implementation
  - Major refactoring needed
  - Capture: transition_to="planning", transition_from="review", transition_direction="backward", notes=[issues]

• EXIT:
  - User wants to review failures before deciding next steps
  - Capture: exit_requested=true

ARTIFACT FIELDS:
- summary: One-sentence review summary
- status: "reviewing" | "approved" | "rejected"
- requirements: (from intake - validation checklist)
- evidence: Test results, quality checks
- notes: Issues found, recommendations
- next_steps: For delivery or for fixing issues
- transition_to: "delivery" | "execution" (if issues found)
- transition_direction: "forward" | "backward"

VALIDATION CHECKLIST:
□ Functional requirements met
□ Tests passing
□ No regressions
□ Code quality acceptable
□ Documentation updated
□ Performance acceptable

USER INTERACTION:
ASK WHEN:
- Tests fail (show results, explain issue)
- Quality issues found (explain impact)
- Requirements not fully met (show gap)`
}

func (c *PhaseCapability) buildDeliveryContext() string {
	return `PHASE: Delivery & Handoff

OBJECTIVE:
- Summarize completed work
- Document key changes and decisions
- Prepare a clear handoff to the user
- Output: A concise delivery summary

INTERACTION MODES:
This phase supports TWO interaction modes - choose based on delivery scope:

• TRADITIONAL MODE (Complete in one turn):
  - Standard delivery with straightforward summary
  - No additional documentation or handoff needed
  - Execute: Review artifacts → Summarize → Document changes → Capture → Done (all in one response)

• ITERATIVE MODE (Collaborative handoff):
  - Complex delivery requiring detailed documentation
  - User wants to review summary sections before finalization
  - Additional artifacts needed (diagrams, runbooks, migration guides)
  - Execute: Present summary draft → Get feedback → Add documentation → Present → Finalize
  - Capture intermediate artifact with status="drafting" while refining

WORKFLOW:
1. Review artifacts from all phases
2. Summarize: requirements → investigation → plan → execution → validation
3. Document changes made
4. Capture artifact with delivery summary
5. Signal exit to complete workflow

COMPLETION CRITERIA:
□ All phase artifacts reviewed
□ Changes summarized clearly
□ Decisions documented
□ User questions answered
□ Artifact captured with status="delivered"
□ Exit requested

PHASE TRANSITIONS:
You can move FORWARD or BACKWARD. Capture transition intent when ready:

• FORWARD / EXIT (typical path):
  - Work is complete and user accepts delivery
  - Capture: status="delivered", exit_requested=true

• BACKWARD to Review (user requests changes):
  - User wants modifications to delivered work
  - Additional validation needed
  - Capture: transition_to="review", transition_from="delivery", transition_direction="backward", notes=[requested changes]

• BACKWARD to Execution (user requests significant changes):
  - Major rework needed based on delivery review
  - Capture: transition_to="execution", transition_from="delivery", transition_direction="backward", notes=[rework needed]

No forward transition from Delivery (this is the final phase). Typically exit_requested=true.

ARTIFACT FIELDS:
- summary: Comprehensive delivery summary
- status: "preparing" | "delivered"
- requirements: (original requirements from intake)
- evidence: Changes made, files modified
- notes: Key decisions, future recommendations
- exit_requested: true

DELIVERY SUMMARY FORMAT:
"Completed: [objective from intake]

Changes Made:
- [file 1]: [changes]
- [file 2]: [changes]

Testing:
- [test results summary]

Notes:
- [key decisions or considerations]

Next Steps:
- [if any follow-up needed]"

USER INTERACTION:
- Present clear, readable summary
- Answer questions about implementation
- Provide guidance for future changes`
}

func (c *PhaseCapability) ExposeTools() []capabilities.ToolDefinition {
	captureSchema := `{
  "type": "object",
  "required": ["summary", "status"],
  "properties": {
    "summary": {"type": "string", "description": "One-sentence phase summary (required)"},
    "status": {"type": "string", "description": "Phase status: exploring|drafting|refining|ready_for_execution|complete|blocked|approved|delivered"},
    "requirements": {"type": "array", "items": {"type": "string"}, "description": "List of requirements (from intake phase)"},
    "plan": {"type": "array", "items": {"type": "string"}, "description": "Ordered implementation steps (concrete, file-specific)"},
    "notes": {"type": "array", "items": {"type": "string"}, "description": "Constraints, risks, considerations"},
    "evidence": {"type": "array", "items": {"type": "string"}, "description": "Findings, patterns, references"},
    "next_steps": {"type": "array", "items": {"type": "string"}, "description": "Guidance for next phase"},
    "transition_to": {"type": "string", "description": "Next phase ID: intake|investigation|planning|execution|review|delivery"},
    "transition_from": {"type": "string", "description": "Previous phase ID"},
    "transition_direction": {"type": "string", "description": "Transition direction: forward|backward"},
    "exit": {"type": "boolean", "description": "Set true to signal phase completion and workflow exit"},
    "structured_data": {"type": "object", "description": "Phase-specific structured data (test results, findings objects, metrics, etc.)"}
  }
}`

	stateSchema := `{
  "type": "object",
  "properties": {},
  "description": "No arguments required. Returns current phase artifact if it exists."
}`

	tools := []capabilities.ToolDefinition{{
		Name:        c.captureTool,
		Description: fmt.Sprintf("Captures %s phase artifact to memory for cross-turn continuity. Call this to save progress, signal transitions, or mark completion. WHEN TO CALL: After gathering context, completing analysis, finalizing plans, applying changes, or delivering results.", c.name),
		Schema:      captureSchema,
		Tags:        []string{"C", "U"}, // Creates new or updates existing phase artifacts
	}, {
		Name:        c.stateTool,
		Description: fmt.Sprintf("Returns the current persisted state for %s phase. WHEN TO CALL: At phase start to check if prior work exists, or when resuming after interruption.", c.name),
		Schema:      stateSchema,
		Tags:        []string{"R"}, // Reads existing phase state
	}}
	for _, tool := range c.phaseWorkflowTools() {
		tools = append(tools, tool)
	}
	return tools
}

func (c *PhaseCapability) BuildInstructions(query string, state capabilities.RoutingState) string {
	switch c.phaseID {
	case "intake":
		return "Use Intake capability to: 1) Ask clarifying questions, 2) Gather requirements, 3) Define scope and constraints, 4) Capture requirements artifact, 5) Transition to investigation when clear."
	case "investigation":
		return "Use Investigation capability to: 1) Build context, 2) Inspect the request and gather evidence, 3) Search for patterns, 4) List changed files, 5) Document evidence, 6) Transition to planning when analysis complete."
	case "planning":
		return "Use Planning capability to: 1) Treat planning as a first-class phase, not an afterthought, 2) Use Continue bridge context when available to understand the editor-backed coding environment, 3) Build context, 4) Research patterns and confirm assumptions, 5) Generate a concrete plan with file/function references, 6) Check relevant Team Practice guidance from the Core Playbook (CP) before finalizing design or implementation choices, 7) Use tests or validation cases as a design aid when they clarify contracts, interfaces, or expected behavior, 8) Emit a user-visible plan message in the chat history that shows the generated plan details and asks for approval from the user before implementation work begins, 9) Capture artifact with status='ready_for_execution', 10) Transition to execution when approved. SAFETY: Do not call delegate_continue_task during planning; keep Ask as the controller."
	case "testing":
		return "Use Testing capability to: 1) Add or refine tests or validation cases when they clarify the intended behavior, 2) Use tests as a tool for design clarity and regression confidence, 3) Run the relevant test or validation command, 4) Capture the results, 5) Transition to execution or review as appropriate."
	case "execution":
		return "Use Execution capability to: 1) Show a user-visible plan message in the chat history that includes the generated plan details, 2) Ask for approval from the user before running commands, 3) Preview patches, 4) Get user approval, 5) If the task is under-specified, pause and do additional investigation/research before changing code, 6) Decide whether the current step should be delegated to Continue as one bounded coding objective, 7) Use delegate_continue_task only when the edit is bounded to the active file and the task is editor-native (for example a focused patch, refactor, or localized change); if the task is broader multi-file or better handled as direct daemon-native editing, apply patches directly instead, 8) Run relevant validation after each change, 9) Capture results, 10) Transition to review when complete. SAFETY: Always preview before applying and keep Ask as the controller."
	case "review":
		return "Use Review capability to: 1) Build context, 2) Inspect changes, 3) If the review scope is unclear or the implementation details are incomplete, do additional investigation/research before finalizing the result, 4) Decide whether a bounded IDE-native review or validation slice should be delegated to Continue, 5) Use delegate_continue_task only for that bounded subtask when appropriate, 6) Run relevant validation, 7) Validate against requirements, 8) Capture validation results, 9) Transition to delivery if approved or back to execution if issues found."
	case "delivery":
		return "Use Delivery capability to: 1) Review all phase artifacts, 2) Summarize changes made, 3) Document decisions, 4) Capture delivery summary, 5) Signal exit to complete workflow."
	default:
		return fmt.Sprintf("Use the %s capability to gather or record the current phase state, preserve a meaningful artifact in memory, and hand the conversation forward with a clear summary for the next phase when the user chooses to move on.", c.name)
	}
}

func (c *PhaseCapability) Handoff(ctx context.Context, query string, state capabilities.RoutingState) *capabilities.Handoff {
	_ = query
	_ = state
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
	if c == nil {
		return nil
	}
	artifact, err := c.loadExistingArtifact()
	if err != nil || artifact.PhaseID == "" {
		return nil
	}
	if artifact.ExitRequested {
		return &capabilities.Handoff{Direction: "exit", Prompt: fmt.Sprintf("Complete the %s phase and return to the main workflow.", c.name)}
	}
	direction := strings.ToLower(strings.TrimSpace(artifact.TransitionDirection))
	target := strings.TrimSpace(artifact.TransitionTo)
	if direction == "backward" {
		target = strings.TrimSpace(artifact.TransitionFrom)
	}
	if direction == "forward" && target != "" {
		prompt := c.buildHandoffPrompt("forward", target, artifact)
		return &capabilities.Handoff{Direction: "forward", Target: target, Prompt: prompt}
	}
	if direction == "backward" && target != "" {
		prompt := c.buildHandoffPrompt("backward", target, artifact)
		return &capabilities.Handoff{Direction: "backward", Target: target, Prompt: prompt}
	}
	return nil
}

func (c *PhaseCapability) OnBefore(ctx context.Context, query string, state capabilities.RoutingState) error {
	_ = ctx
	_ = query
	_ = state
	return nil
}

// OnAfter handles phase succession after ReAct loop completes.
// This is called AFTER all provider-specific ReAct loops (ChatGPT, Anthropic, Gemini, default)
// complete, making it provider-agnostic and the single source of truth for phase transitions.
func (c *PhaseCapability) OnAfter(ctx context.Context, query string, state capabilities.RoutingState, result capabilities.CapabilityResult) error {
	// 0. Check for Tier 3 escalation FIRST (takes priority over normal phase succession)
	if escalation, ok := result.Data["escalation_state"].(map[string]any); ok && escalation != nil {
		tier, _ := escalation["tier"].(int)
		if tier >= 3 {
			// Progressive retry escalated to Tier 3 - trigger backward phase transition
			return c.handleEscalation(ctx, query, escalation)
		}
	}

	// 0b. Structured clarification request pauses phase succession and records the
	// phase as clarifying until the user responds.
	if clarificationRequest := clarificationRequestFromCapabilityResult(result); clarificationRequest != nil {
		return c.handleClarificationRequest(query, clarificationRequest)
	}

	// 1. Check if a capture tool was called (phase completion signal)
	if !c.wasCaptureToolCalled(result) {
		return nil // Phase still in progress
	}

	// 2. Load artifact to check transition intent
	artifact, err := c.loadExistingArtifact()
	if err != nil {
		// No artifact yet or load failed - not an error, just no transition
		return nil
	}

	// 3. Check if exit was requested
	if artifact.ExitRequested {
		log.Info("Phase succession: Exit requested, workflow complete",
			"phase", c.phaseID,
			"artifact_status", artifact.Status)
		return nil
	}

	// 4. Check if next phase is specified
	if artifact.TransitionTo == "" {
		// No transition specified - phase may be incomplete or workflow complete
		log.Debug("Phase succession: No transition specified",
			"phase", c.phaseID,
			"artifact_status", artifact.Status)
		return nil
	}

	// 5. Enqueue next phase (AFTER ReAct, in common pipeline)
	if c.sharedInfra == nil || c.sharedInfra.GetSessionQueue == nil {
		log.Warn("Phase succession: No queue accessor available",
			"phase", c.phaseID,
			"next_phase", artifact.TransitionTo)
		return nil // Not an error - infrastructure may not support queueing
	}

	queue := c.sharedInfra.GetSessionQueue()
	if queue == nil {
		log.Warn("Phase succession: Queue accessor returned nil",
			"phase", c.phaseID,
			"next_phase", artifact.TransitionTo)
		return nil
	}

	// Build work item for next phase
	// Use anonymous struct to avoid import cycle - WorkQueueAccessor handles conversion
	workItem := struct {
		Query    string
		Actor    string
		Priority int
		Context  map[string]any
	}{
		Query:    query, // Carry forward original query
		Actor:    "phase:" + artifact.TransitionTo,
		Priority: 10, // PriorityPhaseSuccession
		Context: map[string]any{
			"from_phase":           c.phaseID,
			"transition_direction": artifact.TransitionDirection,
		},
	}

	if preferredModel := extractPreferredModelFromArtifact(artifact); preferredModel != "" {
		if workItem.Context == nil {
			workItem.Context = map[string]any{}
		}
		workItem.Context["preferred_model"] = preferredModel
	}

	if err := queue.Enqueue(workItem); err != nil {
		log.Error("Phase succession: Failed to enqueue next phase",
			"phase", c.phaseID,
			"next_phase", artifact.TransitionTo,
			"error", err)
		return fmt.Errorf("failed to enqueue next phase %s: %w", artifact.TransitionTo, err)
	}

	log.Info("Phase succession: Enqueued next phase",
		"from_phase", c.phaseID,
		"to_phase", artifact.TransitionTo,
		"direction", artifact.TransitionDirection,
		"artifact_status", artifact.Status)

	return nil
}

func extractPreferredModelFromArtifact(artifact phaseArtifact) string {
	if artifact.StructuredData == nil {
		return ""
	}
	if raw, ok := artifact.StructuredData["phase_mini_plan"]; ok && raw != nil {
		if miniPlan, ok := raw.(map[string]any); ok {
			if routing, ok := miniPlan["routing"].(map[string]any); ok {
				if preferredModel, ok := routing["preferred_model"].(string); ok {
					return strings.TrimSpace(preferredModel)
				}
			}
		}
	}
	if raw, ok := artifact.StructuredData["detailed_execution_plan"]; ok && raw != nil {
		if steps, ok := raw.([]map[string]any); ok {
			for _, step := range steps {
				routing, ok := step["routing"].(map[string]any)
				if !ok || len(routing) == 0 {
					continue
				}
				if preferredModel, ok := routing["preferred_model"].(string); ok {
					return strings.TrimSpace(preferredModel)
				}
			}
		}
	}
	return ""
}

func clarificationRequestFromCapabilityResult(result capabilities.CapabilityResult) *ai.ClarificationRequest {
	if result.Data == nil {
		return nil
	}
	if request, ok := result.Data["clarification_request"].(*ai.ClarificationRequest); ok && request != nil {
		return request
	}
	if raw, ok := result.Data["clarification_request"].(map[string]any); ok && raw != nil {
		request := &ai.ClarificationRequest{}
		if value, ok := raw["target_query"].(string); ok {
			request.TargetQuery = strings.TrimSpace(value)
		}
		if value, ok := raw["assistant_question"].(string); ok {
			request.AssistantQuestion = strings.TrimSpace(value)
		}
		if value, ok := raw["missing_fact"].(string); ok {
			request.MissingFact = strings.TrimSpace(value)
		}
		if value, ok := raw["reason"].(string); ok {
			request.Reason = strings.TrimSpace(value)
		}
		if value, ok := raw["source"].(string); ok {
			request.Source = strings.TrimSpace(value)
		}
		if value, ok := raw["status"].(string); ok {
			request.Status = strings.TrimSpace(value)
		}
		if request.AssistantQuestion != "" || request.TargetQuery != "" {
			return request
		}
	}
	return nil
}

func (c *PhaseCapability) handleClarificationRequest(query string, request *ai.ClarificationRequest) error {
	previous, err := c.loadExistingArtifact()
	if err != nil {
		return nil
	}
	structuredData := clonePhaseStructuredData(previous.StructuredData)
	structuredData["clarification_request"] = map[string]any{
		"target_query":       strings.TrimSpace(request.TargetQuery),
		"assistant_question": strings.TrimSpace(request.AssistantQuestion),
		"missing_fact":       strings.TrimSpace(request.MissingFact),
		"reason":             strings.TrimSpace(request.Reason),
		"source":             strings.TrimSpace(request.Source),
		"status":             strings.TrimSpace(request.Status),
	}
	notes := append([]string(nil), previous.Notes...)
	if question := strings.TrimSpace(request.AssistantQuestion); question != "" {
		notes = append(notes, "Clarification requested: "+question)
	}
	if missingFact := strings.TrimSpace(request.MissingFact); missingFact != "" {
		notes = append(notes, "Missing fact: "+missingFact)
	}
	if reason := strings.TrimSpace(request.Reason); reason != "" {
		notes = append(notes, "Clarification reason: "+reason)
	}
	artifact := previous
	artifact.PhaseID = c.phaseID
	artifact.Status = "clarifying"
	artifact.TransitionTo = ""
	artifact.TransitionDirection = ""
	artifact.ExitRequested = false
	artifact.UpdatedAt = time.Now().UTC()
	artifact.Iteration++
	if artifact.Iteration <= 0 {
		artifact.Iteration = 1
	}
	if strings.TrimSpace(artifact.Summary) == "" {
		artifact.Summary = fmt.Sprintf("%s phase awaiting clarification", c.name)
	}
	artifact.Notes = notes
	artifact.StructuredData = structuredData
	if err := c.persistArtifact(artifact); err != nil {
		return err
	}
	log.Info("Phase succession: Clarification requested, phase marked as clarifying",
		"phase", c.phaseID,
		"missing_fact", request.MissingFact,
		"source", request.Source)
	_ = query
	return nil
}

func clonePhaseStructuredData(data map[string]any) map[string]any {
	if len(data) == 0 {
		return make(map[string]any)
	}
	cloned := make(map[string]any, len(data))
	for key, value := range data {
		cloned[key] = value
	}
	return cloned
}

// wasCaptureToolCalled checks if any tool call in the result was a capture tool.
// Capture tools signal phase completion (e.g., "investigation_capture", "planning_capture").
func (c *PhaseCapability) wasCaptureToolCalled(result capabilities.CapabilityResult) bool {
	toolCalls, ok := result.Data["tool_calls"].([]ai.ToolCall)
	if !ok {
		return false
	}

	for _, tc := range toolCalls {
		// Check if tool name ends with "_capture" and matches our phase
		if strings.HasSuffix(tc.Name, "_capture") && strings.HasPrefix(tc.Name, c.phaseID+"_") {
			return true
		}
	}

	return false
}

// handleEscalation processes Tier 3 progressive retry escalation by enqueueing a backward phase transition.
// This enables automatic error recovery when the LLM exhausts retry attempts within a phase.
func (c *PhaseCapability) handleEscalation(ctx context.Context, query string, escalation map[string]any) error {
	failedTool, _ := escalation["failed_tool"].(string)
	failureCount, _ := escalation["failure_count"].(int)
	errorType, _ := escalation["error_type"].(string)

	log.Info("Phase succession: Handling Tier 3 escalation",
		"current_phase", c.phaseID,
		"failed_tool", failedTool,
		"failure_count", failureCount,
		"error_type", errorType)

	// Determine backward phase transition based on current phase
	var targetPhase string
	var transitionReason string

	switch c.phaseID {
	case "execution":
		targetPhase = "planning"
		transitionReason = "Execution phase encountered repeated failures - returning to planning to revise approach"
	case "planning":
		targetPhase = "investigation"
		transitionReason = "Planning phase encountered repeated failures - returning to investigation to gather more context"
	case "review":
		targetPhase = "execution"
		transitionReason = "Review phase encountered repeated failures - returning to execution to fix issues"
	case "delivery":
		targetPhase = "review"
		transitionReason = "Delivery phase encountered repeated failures - returning to review to verify outputs"
	case "investigation":
		// Investigation is the first phase - stay in phase and let LLM clarify with user
		log.Info("Phase succession: Tier 3 escalation in investigation phase, no backward transition available",
			"phase", c.phaseID,
			"action", "staying_in_phase")
		return nil
	default:
		// Unknown phase or no backward transition available
		log.Warn("Phase succession: No backward transition available for phase",
			"phase", c.phaseID)
		return nil
	}

	// Get queue accessor
	if c.sharedInfra == nil || c.sharedInfra.GetSessionQueue == nil {
		log.Warn("Phase succession: No queue accessor available for escalation handling",
			"phase", c.phaseID,
			"target_phase", targetPhase)
		return nil
	}

	queue := c.sharedInfra.GetSessionQueue()
	if queue == nil {
		log.Warn("Phase succession: Queue accessor returned nil for escalation handling",
			"phase", c.phaseID,
			"target_phase", targetPhase)
		return nil
	}

	// Enqueue backward phase transition with high priority
	workItem := struct {
		Query    string
		Actor    string
		Priority int
		Context  map[string]any
	}{
		Query:    query, // Carry forward original query
		Actor:    "phase:" + targetPhase,
		Priority: 15, // Higher than normal phase succession (10)
		Context: map[string]any{
			"transition_reason": "tier3_escalation",
			"transition_type":   "backward",
			"from_phase":        c.phaseID,
			"failed_tool":       failedTool,
			"failure_count":     failureCount,
			"error_type":        errorType,
			"escalation":        escalation,
			"explanation":       transitionReason,
		},
	}

	if err := queue.Enqueue(workItem); err != nil {
		log.Error("Phase succession: Failed to enqueue backward transition",
			"phase", c.phaseID,
			"target_phase", targetPhase,
			"error", err)
		return fmt.Errorf("failed to enqueue backward transition to %s: %w", targetPhase, err)
	}

	log.Info("Phase succession: Enqueued backward phase transition due to Tier 3 escalation",
		"from_phase", c.phaseID,
		"to_phase", targetPhase,
		"failed_tool", failedTool,
		"failure_count", failureCount,
		"reason", transitionReason)

	return nil
}

// buildHandoffPrompt creates a context-rich handoff prompt including artifacts from previous phases
func (c *PhaseCapability) buildHandoffPrompt(direction, targetPhase string, currentArtifact phaseArtifact) string {
	var prompt string
	if direction == "forward" {
		prompt = fmt.Sprintf("Continue the workflow by moving to the %s phase.", targetPhase)
	} else {
		prompt = fmt.Sprintf("Return to the %s phase and continue the workflow.", targetPhase)
	}

	// Add summary from current phase
	if currentArtifact.Summary != "" {
		prompt = fmt.Sprintf("%s %s", prompt, strings.TrimSpace(currentArtifact.Summary))
	}

	// Add context from previous phases (if available)
	contextParts := c.gatherPreviousPhaseContext()
	if len(contextParts) > 0 {
		prompt += "\n\nContext from previous phases:"
		for _, part := range contextParts {
			prompt += "\n- " + part
		}
	}

	// Add structured data highlights if available
	if currentArtifact.StructuredData != nil && len(currentArtifact.StructuredData) > 0 {
		if findings, ok := currentArtifact.StructuredData["findings"]; ok {
			prompt += fmt.Sprintf("\n\nFindings: %v", findings)
		}
		if testResults, ok := currentArtifact.StructuredData["test_results"]; ok {
			prompt += fmt.Sprintf("\n\nTest Results: %v", testResults)
		}
	}
	if clarificationText := formatPhaseClarificationContext(currentArtifact); clarificationText != "" {
		prompt += "\n\n" + clarificationText
	}

	// Add next steps guidance
	if len(currentArtifact.NextSteps) > 0 {
		prompt += "\n\nNext steps:"
		for i, step := range currentArtifact.NextSteps {
			if i < 3 { // Limit to top 3 next steps
				prompt += "\n" + fmt.Sprintf("%d. %s", i+1, step)
			}
		}
	}

	return prompt
}

// gatherPreviousPhaseContext collects key information from previous phase artifacts
func (c *PhaseCapability) gatherPreviousPhaseContext() []string {
	var context []string

	// Try to load artifacts from previous phases in typical workflow order
	previousPhases := c.getPreviousPhases()

	for _, phaseID := range previousPhases {
		artifact := c.loadArtifactForPhase(phaseID)
		if artifact.PhaseID == "" {
			continue
		}

		// Include key summary from each previous phase
		if artifact.Summary != "" {
			context = append(context, fmt.Sprintf("%s: %s", phaseID, artifact.Summary))
		}

		// Include requirements if from intake
		if phaseID == "intake" && len(artifact.Requirements) > 0 && len(artifact.Requirements) <= 3 {
			for _, req := range artifact.Requirements {
				context = append(context, fmt.Sprintf("Requirement: %s", req))
			}
		}

		// Include key evidence if from investigation
		if phaseID == "investigation" && len(artifact.Evidence) > 0 && len(artifact.Evidence) <= 3 {
			for _, ev := range artifact.Evidence {
				context = append(context, fmt.Sprintf("Evidence: %s", ev))
			}
		}

		// Include plan highlights if from planning
		if phaseID == "planning" && len(artifact.Plan) > 0 && len(artifact.Plan) <= 3 {
			for _, step := range artifact.Plan {
				context = append(context, fmt.Sprintf("Plan: %s", step))
			}
		}
	}

	return context
}

// getPreviousPhases returns the phases that come before the current phase in typical workflow order
func (c *PhaseCapability) getPreviousPhases() []string {
	// Define typical phase progression
	allPhases := []string{"intake", "investigation", "planning", "execution", "review", "delivery"}

	var previous []string
	for _, phase := range allPhases {
		if phase == c.phaseID {
			break
		}
		previous = append(previous, phase)
	}

	return previous
}

// loadArtifactForPhase loads an artifact for a specific phase ID
func (c *PhaseCapability) loadArtifactForPhase(phaseID string) phaseArtifact {
	storage := c.ensureSharedInfrastructure()
	if storage.Memory == nil {
		return phaseArtifact{}
	}

	values, err := storage.Memory.Load("phase-artifact:" + phaseID)
	if err != nil {
		return phaseArtifact{}
	}

	artifactJSON := values["artifact_json"]
	if artifactJSON == "" {
		return phaseArtifact{}
	}

	var artifact phaseArtifact
	if err := json.Unmarshal([]byte(artifactJSON), &artifact); err != nil {
		return phaseArtifact{}
	}

	return artifact
}

func (c *PhaseCapability) ExecuteTool(ctx context.Context, toolName string, args map[string]any, state capabilities.RoutingState) (string, error) {
	_ = ctx
	_ = state
	name := strings.TrimSpace(strings.ToLower(toolName))
	if name == "" {
		return "", fmt.Errorf("empty tool name")
	}
	switch name {
	case strings.ToLower(c.captureTool):
		return c.runCapture(args)
	case strings.ToLower(c.stateTool):
		return c.runState(args)
	case strings.ToLower(c.phaseToolName("assess_and_route")):
		return c.runAssessAndRoute(args)
	case strings.ToLower(c.phaseToolName("create_succession_path")):
		return c.runCreateSuccessionPath(args)
	case strings.ToLower(c.phaseToolName("build_context")):
		return c.runBuildContext(args)
	case strings.ToLower(c.phaseToolName("inspect_repo")):
		return c.runInspectRepo(args)
	case strings.ToLower(c.phaseToolName("search_repo")):
		return c.runSearchRepo(args)
	case strings.ToLower(c.phaseToolName("list_changed_files")):
		return c.runListChangedFiles(args)
	case strings.ToLower(c.phaseToolName("index_repo_structure")):
		return c.runIndexRepoStructure(args)
	case strings.ToLower(c.phaseToolName("search_team_patterns")):
		return c.runSearchTeamPatterns(ctx, args)
	case strings.ToLower(c.phaseToolName("vectorize_team_patterns")):
		return c.runVectorizeTeamPatterns(ctx, args)
	case strings.ToLower(c.phaseToolName("plan_changes")):
		return c.runPlanChanges(args)
	case strings.ToLower(c.phaseToolName("preview_patch")):
		return c.runPreviewPatch(args)
	case strings.ToLower(c.phaseToolName("apply_patch")):
		return c.runApplyPatch(args)
	case strings.ToLower(c.phaseToolName("run_tests")):
		return c.runTests(args)
	default:
		return "", fmt.Errorf("unsupported tool %q", toolName)
	}
}

func (c *PhaseCapability) phaseWorkflowTools() []capabilities.ToolDefinition {
	taskSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "Task description or query"},
    "working_dir": {"type": "string", "description": "Repository root path (optional, inferred if omitted)"},
    "active_file": {"type": "string", "description": "Primary file for changes (optional, inferred if omitted)"}
  }
}`

	searchSchema := `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "Task description"},
    "search": {"type": "string", "description": "Search query for patterns or code (optional)"}
  }
}`

	patchSchema := `{
  "type": "object",
  "required": ["file_path", "old_content", "new_content"],
  "properties": {
    "file_path": {"type": "string", "description": "Path to file to modify"},
    "old_content": {"type": "string", "description": "Content to replace"},
    "new_content": {"type": "string", "description": "New content"},
    "description": {"type": "string", "description": "Description of change"}
  }
}`

	switch c.phaseID {
	case "intake":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("build_context"),
			Description: "OPTIONAL: Builds repository context if task involves code. Call when user mentions files, repos, or code locations. Returns repository structure, files, dependencies.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only context building
		}}
	case "investigation":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("build_context"),
			Description: "REQUIRED FIRST STEP: Builds repository context (structure, files, dependencies). Must call before other investigation tools. Returns ContextPack with working directory, active file, repository summary.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only context building
		}, {
			Name:        c.phaseToolName("inspect_repo"),
			Description: "Inspects current repository state and changed files. WHEN TO CALL: After build_context, to understand current state before analysis. Returns changed files list and diff preview.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only inspection
		}, {
			Name:        c.phaseToolName("search_repo"),
			Description: "Searches repository for patterns, implementations, or code. WHEN TO CALL: To find similar code, error patterns, or existing implementations before concluding something is novel. Returns matching code locations.",
			Schema:      searchSchema,
			Tags:        []string{"R"}, // Read-only search
		}, {
			Name:        c.phaseToolName("list_changed_files"),
			Description: "Lists files with uncommitted changes. WHEN TO CALL: To identify in-progress work and avoid conflicts. Returns list of changed file paths.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only list
		}, {
			Name:        c.phaseToolName("search_team_patterns"),
			Description: "Searches team-specific coding conventions and patterns in Long-Term Memory. Use when planning code generation to find team's preferred helpers, anti-patterns, and conventions. Returns pattern details including convention, anti-pattern, examples, and file references. WHEN TO CALL: Before designing or implementing features to ensure code follows team standards.",
			Schema: `{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {
      "type": "string",
      "description": "Semantic search query for team patterns (e.g., 'authentication HTTP handlers', 'error handling', 'database queries', 'testing utilities')"
    },
    "pattern_type": {
      "type": "string",
      "description": "Optional filter by pattern type",
      "enum": ["authentication", "error_handling", "database", "testing", "api_design", "background_jobs"]
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of patterns to return (default 5)",
      "default": 5
    }
  }
}`,
			Tags: []string{"R"}, // Read-only search
		}, {
			Name:        c.phaseToolName("vectorize_team_patterns"),
			Description: "Vectorizes all team patterns for semantic search. WORKFLOW: After user adds/updates patterns, ASK THE USER DIRECTLY: 'Are you done specifying all your team's patterns?' Wait for user confirmation (yes/done/that's all). Once confirmed, call this tool to index patterns. Do NOT call without asking first. Performance: ~1-2 seconds.",
			Schema: `{
  "type": "object",
  "properties": {}
}`,
			Tags: []string{"U"}, // Updates/indexes pattern embeddings
		}}
	case "planning":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("assess_and_route"),
			Description: "PLANNING-FIRST ARCHITECTURE: Assesses the user query and determines the appropriate starting phase. Always call this FIRST in planning phase to understand what needs to be done. Analyzes query semantics and creates execution strategy. Returns assessment with recommended starting phase and reasoning.",
			Schema:      taskSchema,
			Tags:        []string{"C"}, // Creates assessment/strategy
		}, {
			Name:        c.phaseToolName("create_succession_path"),
			Description: "Creates a phase succession path with checkpoints. Call after assess_and_route to define the full sequence of phases needed to complete the task. Returns planned path: [investigation, planning, execution, review, delivery] with checkpoints for re-assessment.",
			Schema: `{
  "type": "object",
  "required": ["phases"],
  "properties": {
    "phases": {"type": "array", "items": {"type": "string"}, "description": "Ordered phase sequence: ['investigation', 'planning', 'execution', 'review', 'delivery']"},
    "checkpoints": {"type": "array", "items": {"type": "string"}, "description": "Checkpoint names: ['after_investigation', 'after_execution']"},
    "reasoning": {"type": "string", "description": "Why this path was chosen"},
    "confidence": {"type": "string", "description": "Confidence level: high|medium|low"}
  }
}`,
			Tags: []string{"C"}, // Creates phase succession path
		}, {
			Name:        c.phaseToolName("build_context"),
			Description: "REQUIRED FIRST STEP: Builds repository context for planning. Must call before planning_plan_changes. Returns ContextPack with repository structure needed for concrete plan generation.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only context building
		}, {
			Name:        c.phaseToolName("inspect_repo"),
			Description: "Inspects current repository state. WHEN TO CALL: Before planning, to understand existing code structure and patterns. Returns changed files and current state.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only inspection
		}, {
			Name:        c.phaseToolName("search_repo"),
			Description: "Searches repository for implementation patterns. WHEN TO CALL: Before designing, to find similar implementations and follow established patterns rather than inventing new approaches. Returns matching patterns and code locations.",
			Schema:      searchSchema,
			Tags:        []string{"R"}, // Read-only search
		}, {
			Name:        c.phaseToolName("list_changed_files"),
			Description: "Lists changed files to avoid conflicts. WHEN TO CALL: Before finalizing plan, to ensure plan doesn't conflict with in-progress work. Returns list of changed files.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only list
		}, {
			Name:        c.phaseToolName("index_repo_structure"),
			Description: "Indexes repository structure using AST/tree-sitter extraction so planning can reason over symbols, imports, and structural paths. WHEN TO CALL: Before designing changes in unfamiliar repositories or when repo structure is needed for planning memory. Returns indexed file entries with functions/imports/structural paths.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only repo indexing
		}, {
			Name:        c.phaseToolName("plan_changes"),
			Description: "Generates concrete execution plan from gathered context. WHEN TO CALL: After build_context, search_repo, and inspect_repo complete. This is the core planning step. Returns ordered list of implementation steps with file/function specifics. IF EMPTY: Requirements may be unclear - ask clarifying question.",
			Schema:      taskSchema,
			Tags:        []string{"C"}, // Creates execution plan
		}}
	case "execution":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("preview_patch"),
			Description: "SAFETY REQUIRED: Previews patch before applying. WHEN TO CALL: Before every apply_patch call. Shows user what will change. Returns patch preview with old/new content comparison.",
			Schema:      patchSchema,
			Tags:        []string{"R"}, // Read-only preview
		}, {
			Name:        c.phaseToolName("apply_patch"),
			Description: "Applies patch to codebase. SAFETY RULE: Must call preview_patch and get user approval BEFORE calling this. WHEN TO CALL: Only after preview shown and approved. Returns confirmation of applied changes.",
			Schema:      patchSchema,
			Tags:        []string{"U"}, // Updates code files
		}, {
			Name:        c.phaseToolName("run_tests"),
			Description: "Runs tests to validate changes. WHEN TO CALL: After each apply_patch call, to ensure changes work correctly and don't introduce regressions. Returns test results (pass/fail) and output.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only test execution
		}}
	case "review":
		return []capabilities.ToolDefinition{{
			Name:        c.phaseToolName("build_context"),
			Description: "Builds repository context for review. WHEN TO CALL: At review start, to understand current state after execution. Returns repository structure and current state.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only context building
		}, {
			Name:        c.phaseToolName("inspect_repo"),
			Description: "Inspects repository state to see applied changes. WHEN TO CALL: To review what was changed during execution phase. Returns changed files and diff.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only inspection
		}, {
			Name:        c.phaseToolName("run_tests"),
			Description: "Runs comprehensive tests for validation. WHEN TO CALL: To validate all requirements are met and no regressions introduced. Returns test results and coverage.",
			Schema:      taskSchema,
			Tags:        []string{"R"}, // Read-only test execution
		}}
	default:
		return nil
	}
}

func (c *PhaseCapability) phaseToolName(suffix string) string {
	if c == nil {
		return suffix
	}
	return c.phaseID + "_" + suffix
}

func (c *PhaseCapability) ensureSharedInfrastructure() *SharedInfrastructure {
	if c == nil {
		return NewDefaultSharedInfrastructure()
	}
	if c.sharedInfra == nil {
		c.sharedInfra = NewDefaultSharedInfrastructure()
	}
	return c.sharedInfra.ensureDefaults()
}

func formatContextPack(pack ContextPack) string {
	parts := []string{"Context Pack"}
	if pack.WorkingDir != "" {
		parts = append(parts, fmt.Sprintf("WorkingDir: %s", pack.WorkingDir))
	}
	if pack.ActiveFile != "" {
		parts = append(parts, fmt.Sprintf("ActiveFile: %s", pack.ActiveFile))
	}
	if len(pack.RelatedFiles) > 0 {
		parts = append(parts, fmt.Sprintf("RelatedFiles: %s", strings.Join(pack.RelatedFiles, ", ")))
	}
	if len(pack.TestFiles) > 0 {
		parts = append(parts, fmt.Sprintf("TestFiles: %s", strings.Join(pack.TestFiles, ", ")))
	}
	if pack.RepositorySummary != "" {
		parts = append(parts, fmt.Sprintf("Summary: %s", pack.RepositorySummary))
	}
	return strings.Join(parts, "\n")
}

func formatPhaseClarificationContext(artifact phaseArtifact) string {
	if !strings.EqualFold(strings.TrimSpace(artifact.Status), "clarifying") || artifact.StructuredData == nil {
		return ""
	}
	raw, ok := artifact.StructuredData["clarification_request"].(map[string]any)
	if !ok || raw == nil {
		return ""
	}
	parts := []string{"Pending Clarification"}
	if question, ok := raw["assistant_question"].(string); ok && strings.TrimSpace(question) != "" {
		parts = append(parts, fmt.Sprintf("Question: %s", strings.TrimSpace(question)))
	}
	if missingFact, ok := raw["missing_fact"].(string); ok && strings.TrimSpace(missingFact) != "" {
		parts = append(parts, fmt.Sprintf("Missing Fact: %s", strings.TrimSpace(missingFact)))
	}
	if reason, ok := raw["reason"].(string); ok && strings.TrimSpace(reason) != "" {
		parts = append(parts, fmt.Sprintf("Reason: %s", strings.TrimSpace(reason)))
	}
	if source, ok := raw["source"].(string); ok && strings.TrimSpace(source) != "" {
		parts = append(parts, fmt.Sprintf("Source: %s", strings.TrimSpace(source)))
	}
	if len(parts) == 1 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func (c *PhaseCapability) buildTask(args map[string]any) Task {
	query := readPhaseString(args, "query", "")
	if query == "" {
		query = readPhaseString(args, "task", "")
	}
	if query == "" {
		query = readPhaseString(args, "description", "")
	}

	workingDir := readPhaseString(args, "working_dir", "")
	activeFile := readPhaseString(args, "active_file", "")
	if workingDir == "" || activeFile == "" {
		inferredDir, inferredFile := inferTaskInputs(query)
		if workingDir == "" {
			workingDir = inferredDir
		}
		if activeFile == "" {
			activeFile = inferredFile
		}
	}

	state := RunState{
		Args: map[string]string{},
		Metadata: map[string]string{
			"phase": c.phaseID,
		},
	}
	return Task{
		WorkingDir:  workingDir,
		ActiveFile:  activeFile,
		Description: query,
		TaskType:    c.phaseID,
		State:       state,
	}
}

func (c *PhaseCapability) sharedInfrastructure() *SharedInfrastructure {
	return c.ensureSharedInfrastructure()
}

func isGitRepositoryDir(dir string) bool {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(filepath.Clean(trimmed), ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

func resolveRepoWorkingDir(task Task) (Task, []string) {
	resolved := task
	baseDir := strings.TrimSpace(task.WorkingDir)
	if baseDir == "" {
		baseDir = "."
	}
	baseDir = filepath.Clean(baseDir)
	if isGitRepositoryDir(baseDir) {
		resolved.WorkingDir = baseDir
		return resolved, []string{baseDir}
	}
	candidates := discoverWorkingDirCandidates(baseDir)
	if len(candidates) > 0 {
		resolved.WorkingDir = candidates[0]
		return resolved, candidates
	}
	resolved.WorkingDir = baseDir
	return resolved, nil
}

func formatRepoResolutionFallback(toolLabel string, task Task, candidates []string, err error) string {
	parts := []string{fmt.Sprintf("%s Unavailable", toolLabel)}
	if err != nil {
		parts = append(parts, fmt.Sprintf("Reason: %s", err.Error()))
	}
	requested := strings.TrimSpace(task.WorkingDir)
	if requested == "" {
		requested = "(empty)"
	}
	parts = append(parts, fmt.Sprintf("Requested WorkingDir: %s", requested))
	if len(candidates) > 0 {
		parts = append(parts, fmt.Sprintf("Resolved WorkingDir: %s", candidates[0]))
		parts = append(parts, fmt.Sprintf("Detected Repositories: %s", strings.Join(candidates, ", ")))
		parts = append(parts, "Next Step: retry with working_dir set to the resolved repository path.")
	} else {
		parts = append(parts, "Detected Repositories: none")
		parts = append(parts, "Next Step: provide working_dir for a git repository, then retry.")
	}
	return strings.Join(parts, "\n")
}

func (c *PhaseCapability) runBuildContext(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	packOps := infra.NewContextPackOperations()
	pack, err := packOps.BuildContextForTask(task, task.State)
	if err != nil {
		return "", fmt.Errorf("build context: %w", err)
	}
	if infra.RepoIndexer != nil {
		_, _ = infra.RepoIndexer.Index(task, task.State)
	}
	contextText := formatContextPack(pack)
	if artifact, err := c.loadExistingArtifact(); err == nil {
		if clarificationText := formatPhaseClarificationContext(artifact); clarificationText != "" {
			contextText += "\n\n" + clarificationText
		}
	}
	return contextText, nil
}

func (c *PhaseCapability) runInspectRepo(args map[string]any) (string, error) {
	task, candidates := resolveRepoWorkingDir(c.buildTask(args))
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	repoOps := infra.NewRepoOperations()
	summary, err := repoOps.InspectRepo(task, task.State)
	if err != nil {
		return formatRepoResolutionFallback("Repository Inspection", task, candidates, err), nil
	}
	parts := []string{"Repository Inspection"}
	if len(summary.ChangedFiles) > 0 {
		parts = append(parts, fmt.Sprintf("Changed Files: %s", strings.Join(summary.ChangedFiles, ", ")))
	} else {
		parts = append(parts, "Changed Files: none")
	}
	if strings.TrimSpace(summary.DiffPreview) != "" {
		parts = append(parts, fmt.Sprintf("Diff Preview: %s", summary.DiffPreview))
	}
	return strings.Join(parts, "\n"), nil
}

func (c *PhaseCapability) runSearchRepo(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	repoOps := infra.NewRepoOperations()
	searchQuery := readPhaseString(args, "search", "")
	if searchQuery == "" {
		searchQuery = task.Description
	}
	results, err := repoOps.SearchRepo(task, task.State, searchQuery)
	if err != nil {
		return "", fmt.Errorf("search repo: %w", err)
	}
	if len(results) == 0 {
		return "Search Results: none", nil
	}
	return fmt.Sprintf("Search Results:\n%s", strings.Join(results, "\n")), nil
}

func (c *PhaseCapability) runListChangedFiles(args map[string]any) (string, error) {
	task, candidates := resolveRepoWorkingDir(c.buildTask(args))
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	repoOps := infra.NewRepoOperations()
	files, err := repoOps.ListChangedFiles(task, task.State)
	if err != nil {
		return formatRepoResolutionFallback("Changed Files Inspection", task, candidates, err), nil
	}
	if len(files) == 0 {
		return "Changed Files: none", nil
	}
	return fmt.Sprintf("Changed Files:\n%s", strings.Join(files, "\n")), nil
}

// RepositoryStructureIndexEntry captures lightweight repo-structure metadata for planning and memory indexing.
type RepositoryStructureIndexEntry struct {
	Path           string
	Language       string
	Functions      []string
	Imports        []string
	ASTLeafNodes   []string
	StructuralPath string
	Summary        string
}

func collectRepositoryStructureIndexEntries(task Task, infra *SharedInfrastructure) ([]RepositoryStructureIndexEntry, error) {
	resolvedTask := task
	workingDir := strings.TrimSpace(resolvedTask.WorkingDir)
	if workingDir == "" {
		workingDir = "."
	}
	workingDir = filepath.Clean(workingDir)

	entries := []RepositoryStructureIndexEntry{}
	seen := map[string]struct{}{}

	ops := &ASTOperations{grammar: NewGrammarRegistry()}
	if infra != nil && infra.GrammarRegistry != nil {
		ops.grammar = infra.GrammarRegistry
	}

	walkErr := filepath.WalkDir(workingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" || name == ".next" || name == "coverage" {
				return filepath.SkipDir
			}
			return nil
		}
		if path == filepath.Join(workingDir, ".git") || strings.HasPrefix(filepath.ToSlash(path), "/proc/") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == "" || !isSupportedRepoIndexExtension(ext) {
			return nil
		}
		if relPath, relErr := filepath.Rel(workingDir, path); relErr == nil {
			if relPath == "." || relPath == "" {
				return nil
			}
			if _, exists := seen[relPath]; exists {
				return nil
			}
			seen[relPath] = struct{}{}
			entries = append(entries, RepositoryStructureIndexEntry{Path: filepath.ToSlash(relPath)})
			return nil
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	if len(entries) == 0 {
		if resolvedTask.ActiveFile != "" {
			activePath := resolvedTask.ActiveFile
			if !filepath.IsAbs(activePath) {
				activePath = filepath.Join(workingDir, activePath)
			}
			if info, err := os.Stat(activePath); err == nil && !info.IsDir() {
				if relPath, relErr := filepath.Rel(workingDir, activePath); relErr == nil {
					entries = append(entries, RepositoryStructureIndexEntry{Path: filepath.ToSlash(relPath)})
				}
			}
		}
	}

	for i := range entries {
		entryPath := entries[i].Path
		absPath := entryPath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(workingDir, absPath)
		}
		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			continue
		}
		entryLang := resolveTreeSitterLanguageAliasFromExtension(filepath.Ext(absPath))
		if entryLang == "" {
			entryLang = "text"
		}
		entries[i].Language = entryLang
		if _, root, err := ops.ParseFile(absPath, content); err == nil {
			entries[i].Functions = ops.ExtractFunctions(root, content)
			entries[i].Imports = ops.ExtractImports(root, content)
			if len(entries[i].Functions) > 0 {
				entries[i].ASTLeafNodes = append([]string{}, entries[i].Functions...)
			}
			if len(entries[i].Functions) > 0 {
				entries[i].StructuralPath = buildRepoStructurePath(workingDir, entryPath, entries[i].Functions[0])
			}
		}
		if entries[i].Summary == "" {
			summaryParts := []string{}
			if len(entries[i].Functions) > 0 {
				summaryParts = append(summaryParts, fmt.Sprintf("functions=%s", strings.Join(entries[i].Functions, ",")))
			}
			if len(entries[i].Imports) > 0 {
				summaryParts = append(summaryParts, fmt.Sprintf("imports=%s", strings.Join(entries[i].Imports, ",")))
			}
			if len(summaryParts) > 0 {
				entries[i].Summary = strings.Join(summaryParts, "; ")
			} else {
				entries[i].Summary = "ast-indexed"
			}
		}
	}

	return entries, nil
}

func buildRepoStructurePath(workingDir, relPath, firstFunction string) string {
	baseName := filepath.Base(filepath.Clean(workingDir))
	if baseName == "." || baseName == string(filepath.Separator) || baseName == "" {
		baseName = "repo"
	}
	cleanRel := filepath.ToSlash(filepath.Clean(relPath))
	if strings.HasSuffix(cleanRel, "/") {
		cleanRel = strings.TrimSuffix(cleanRel, "/")
	}
	if cleanRel == "." || cleanRel == "" {
		return fmt.Sprintf("%s/%s", baseName, firstFunction)
	}
	return fmt.Sprintf("%s/%s/%s", baseName, strings.TrimSuffix(cleanRel, filepath.Ext(cleanRel)), firstFunction)
}

func isSupportedRepoIndexExtension(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".java", ".rs", ".cs":
		return true
	default:
		return false
	}
}

func (c *PhaseCapability) runIndexRepoStructure(args map[string]any) (string, error) {
	task := c.buildTask(args)
	resolvedTask, candidates := resolveRepoWorkingDir(task)
	if resolvedTask.WorkingDir == "" {
		resolvedTask.WorkingDir = task.WorkingDir
	}
	entries, err := collectRepositoryStructureIndexEntries(resolvedTask, c.sharedInfra)
	if err != nil {
		return formatRepoResolutionFallback("Repository Structure Indexing", resolvedTask, candidates, err), nil
	}
	if len(entries) == 0 {
		return "Repository Structure Index: none", nil
	}
	if c.sharedInfra != nil && c.sharedInfra.MemorySink != nil {
		for _, entry := range entries {
			c.sharedInfra.MemorySink(CodingMemory{
				Description:    fmt.Sprintf("Repository structure index for %s", entry.Path),
				WorkingDir:     resolvedTask.WorkingDir,
				ActiveFile:     entry.Path,
				RepositoryHint: filepath.Base(resolvedTask.WorkingDir),
				StructuralPath: entry.StructuralPath,
				ASTLeafNodes:   entry.ASTLeafNodes,
				Plan:           []string{"Indexed repository structure"},
			})
		}
	}
	parts := []string{"Repository Structure Index"}
	for _, entry := range entries {
		parts = append(parts, fmt.Sprintf("- %s [%s] %s", entry.Path, entry.Language, entry.Summary))
	}
	return strings.Join(parts, "\n"), nil
}

func (c *PhaseCapability) runSearchTeamPatterns(ctx context.Context, args map[string]any) (string, error) {
	// Check if LTM accessor is configured
	if c.sharedInfra == nil || c.sharedInfra.LTMAccessor == nil {
		return "Team patterns not available (LTM not configured)", nil
	}

	// Get LTM instance
	ltmInterface, err := c.sharedInfra.LTMAccessor(ctx)
	if err != nil {
		return "Team patterns not available (LTM access failed)", nil
	}

	// Type assert to concrete KnowledgeBase type
	ltm, ok := ltmInterface.(*memory.KnowledgeBase[map[string]any])
	if !ok {
		return "Team patterns not available (LTM type mismatch)", nil
	}

	// Extract arguments
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}

	patternType := ""
	if pt, ok := args["pattern_type"].(string); ok {
		patternType = pt
	}

	limit := 5
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	// Call team patterns library
	patterns, err := teampatterns.SearchTeamPatterns(ctx, ltm, query, patternType, limit)
	if err != nil {
		return "", fmt.Errorf("team pattern search failed: %w", err)
	}

	if len(patterns) == 0 {
		return fmt.Sprintf("No team patterns found for: %s", query), nil
	}

	// Format results using existing formatter
	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d team patterns:\n\n", len(patterns)))

	for i, pattern := range patterns {
		output.WriteString(teampatterns.FormatPatternForDisplay(pattern, i+1))
	}

	return output.String(), nil
}

func (c *PhaseCapability) runVectorizeTeamPatterns(ctx context.Context, args map[string]any) (string, error) {
	// Validate all required accessors are configured
	if c.sharedInfra == nil {
		return "Team patterns vectorization not available (infrastructure not configured)", nil
	}

	if c.sharedInfra.DatabaseAccessor == nil {
		return "Team patterns vectorization not available (database accessor not configured)", nil
	}

	if c.sharedInfra.LTMName == nil {
		return "Team patterns vectorization not available (LTM name not configured)", nil
	}

	if c.sharedInfra.LLMAccessor == nil {
		return "Team patterns vectorization not available (LLM accessor not configured)", nil
	}

	if c.sharedInfra.EmbedderAccessor == nil {
		return "Team patterns vectorization not available (embedder accessor not configured)", nil
	}

	// Get database instance
	dbInterface, err := c.sharedInfra.DatabaseAccessor(ctx)
	if err != nil {
		return "Team patterns vectorization not available (database access failed)", nil
	}

	// Get LLM instance
	llmInterface, err := c.sharedInfra.LLMAccessor(ctx)
	if err != nil {
		return "Team patterns vectorization not available (LLM access failed)", nil
	}

	// Get embedder instance
	embedderInterface, err := c.sharedInfra.EmbedderAccessor(ctx)
	if err != nil {
		return "Team patterns vectorization not available (embedder access failed)", nil
	}

	// Get LTM name
	kbName := c.sharedInfra.LTMName()
	if kbName == "" {
		return "Team patterns vectorization not available (LTM name is empty)", nil
	}

	// Call vectorization function with database-level vectorization
	// This vectorizes ONLY the "Team Patterns" category, not the entire LTM
	if err := teampatterns.VectorizeTeamPatterns(ctx, dbInterface, kbName, llmInterface, embedderInterface); err != nil {
		return "", fmt.Errorf("team pattern vectorization failed: %w", err)
	}

	return "✅ Team patterns are now indexed and ready for search.", nil
}

func (c *PhaseCapability) runPlanChanges(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	packOps := infra.NewContextPackOperations()
	pack, err := packOps.BuildContextForTask(task, task.State)
	if err != nil {
		return "", fmt.Errorf("build context for planning: %w", err)
	}
	if infra.Planner == nil {
		return "", fmt.Errorf("planner not configured")
	}
	plan, err := infra.Planner.Plan(task, task.State, pack)
	if err != nil {
		return "", fmt.Errorf("plan changes: %w", err)
	}
	if len(plan) == 0 {
		return "Execution Plan: none", nil
	}
	return fmt.Sprintf("Execution Plan:\n%s", strings.Join(plan, "\n")), nil
}

func (c *PhaseCapability) runPreviewPatch(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	patch := c.readPatch(args)
	if patch == nil {
		return "", fmt.Errorf("missing patch inputs")
	}
	patchOps := infra.NewPatchOperations()
	preview, err := patchOps.PreviewPatch(task, patch, task.State)
	if err != nil {
		return "", fmt.Errorf("preview patch: %w", err)
	}
	return preview, nil
}

func (c *PhaseCapability) runApplyPatch(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	patch := c.readPatch(args)
	if patch == nil {
		return "", fmt.Errorf("missing patch inputs")
	}
	if infra.Policy != nil {
		if err := infra.Policy.Allow(task, task.State); err != nil {
			return "", err
		}
	}
	patchOps := infra.NewPatchOperations()
	if err := patchOps.ApplyPatch(task, patch, task.State); err != nil {
		return "", fmt.Errorf("apply patch: %w", err)
	}
	return fmt.Sprintf("Applied patch to %s", patch.FilePath), nil
}

func (c *PhaseCapability) runTests(args map[string]any) (string, error) {
	task := c.buildTask(args)
	infra := c.sharedInfrastructure()
	if infra == nil {
		return "", fmt.Errorf("shared infrastructure not configured")
	}
	testOps := infra.NewTestOperations()
	result, err := testOps.RunTests(task, task.State)
	if err != nil {
		return "", fmt.Errorf("run tests: %w", err)
	}
	if result.Passed {
		return fmt.Sprintf("Tests passed\n%s", result.Output), nil
	}
	return fmt.Sprintf("Tests failed\n%s", result.Output), nil
}

// runAssessAndRoute performs Planning-First assessment of the query
func (c *PhaseCapability) runAssessAndRoute(args map[string]any) (string, error) {
	query := readPhaseString(args, "query", "")
	if query == "" {
		query = readPhaseString(args, "task", "")
	}
	if query == "" {
		return "", fmt.Errorf("query is required for assessment")
	}

	// Use semantic detection as initial heuristic
	queryLower := strings.ToLower(strings.TrimSpace(query))
	detectedPhase := detectStartingPhase(queryLower)

	// Analyze query characteristics
	characteristics := []string{}

	// Check for bug/error indicators (investigation)
	if strings.Contains(queryLower, "fix") || strings.Contains(queryLower, "bug") ||
		strings.Contains(queryLower, "error") || strings.Contains(queryLower, "failing") {
		characteristics = append(characteristics, "🔍 Bug/error fixing - requires investigation")
	}

	// Check for feature indicators (intake → planning → execution)
	if strings.Contains(queryLower, "add") || strings.Contains(queryLower, "implement") ||
		strings.Contains(queryLower, "create") || strings.Contains(queryLower, "new feature") {
		characteristics = append(characteristics, "✨ Feature implementation - full workflow")
	}

	// Check for validation indicators (review)
	if strings.Contains(queryLower, "review") || strings.Contains(queryLower, "validate") ||
		strings.Contains(queryLower, "check") || strings.Contains(queryLower, "verify") {
		characteristics = append(characteristics, "✅ Validation/review task")
	}

	// Check for planning/design indicators
	if strings.Contains(queryLower, "design") || strings.Contains(queryLower, "architect") ||
		strings.Contains(queryLower, "plan") || strings.Contains(queryLower, "approach") {
		characteristics = append(characteristics, "📐 Design/planning task")
	}

	// Build assessment result
	result := fmt.Sprintf(`PHASE ASSESSMENT

Query: %s

Detected Starting Phase: %s

Characteristics:
%s

Recommended Action:
- Start in %s phase
- This phase will assess context and determine next steps
- Use planning_create_succession_path to define full workflow

Analysis Method: Semantic heuristics (keyword-based detection)
Confidence: Medium (heuristic-based, LLM confirmation recommended)`,
		query,
		detectedPhase,
		strings.Join(characteristics, "\n"),
		detectedPhase)

	return result, nil
}

// runCreateSuccessionPath creates a phase succession path
func (c *PhaseCapability) runCreateSuccessionPath(args map[string]any) (string, error) {
	phases := readPhaseStringSlice(args, "phases")
	if len(phases) == 0 {
		return "", fmt.Errorf("phases array is required")
	}

	// Validate phase names
	validPhases := map[string]bool{
		"intake": true, "investigation": true, "planning": true,
		"execution": true, "review": true, "delivery": true,
	}
	for _, phase := range phases {
		if !validPhases[phase] {
			return "", fmt.Errorf("invalid phase name: %s (must be one of: intake, investigation, planning, execution, review, delivery)", phase)
		}
	}

	checkpoints := readPhaseStringSlice(args, "checkpoints")
	reasoning := readPhaseString(args, "reasoning", "No reasoning provided")
	confidence := readPhaseString(args, "confidence", "medium")

	// Build checkpoints map
	checkpointMap := make(map[string]bool)
	for _, cp := range checkpoints {
		checkpointMap[cp] = true
	}

	// Store PhasePlan in work stream context via STM
	if c.sharedInfra != nil && c.sharedInfra.STMAccessor != nil {
		stm := c.sharedInfra.STMAccessor()
		if stm != nil {
			if err := stm.CreatePhasePlan(phases, checkpointMap, reasoning, confidence); err != nil {
				return "", fmt.Errorf("failed to store phase plan: %w", err)
			}
		}
	}

	result := fmt.Sprintf(`✅ PHASE PLAN CREATED

Planned Phases: %s
Total Phases: %d

Checkpoints:
%s

Reasoning:
%s

Confidence: %s

📋 EXECUTION ROADMAP:
%s

The plan has been stored in WorkStream and will guide phase execution.
Each phase will automatically transition to the next phase upon completion.
At checkpoints, you can reassess and adjust the plan if needed.

🚀 Next: Begin execution with the first phase (%s)`,
		strings.Join(phases, " → "),
		len(phases),
		formatCheckpoints(checkpointMap),
		reasoning,
		confidence,
		formatPhaseRoadmap(phases, checkpointMap),
		phases[0])

	return result, nil
}

func formatCheckpoints(checkpoints map[string]bool) string {
	if len(checkpoints) == 0 {
		return "  None defined"
	}
	parts := []string{}
	for cp := range checkpoints {
		parts = append(parts, fmt.Sprintf("  ✓ %s", cp))
	}
	return strings.Join(parts, "\n")
}

func formatPhaseRoadmap(phases []string, checkpoints map[string]bool) string {
	var roadmap strings.Builder
	for i, phase := range phases {
		// Phase number and name
		roadmap.WriteString(fmt.Sprintf("%d. %s", i+1, formatPhaseName(phase)))

		// Check if this is a checkpoint phase
		checkpointKey := fmt.Sprintf("after_%s", phase)
		if checkpoints[checkpointKey] {
			roadmap.WriteString(" 🔍 (checkpoint)")
		}

		if i < len(phases)-1 {
			roadmap.WriteString("\n   ↓\n")
		}
	}
	return roadmap.String()
}

func formatPhaseName(phaseID string) string {
	names := map[string]string{
		"intake":        "Intake & Context",
		"investigation": "Investigation & Analysis",
		"planning":      "Planning & Design",
		"execution":     "Execution & Implementation",
		"review":        "Review & Validation",
		"delivery":      "Delivery & Handoff",
	}
	if name, ok := names[phaseID]; ok {
		return name
	}
	return phaseID
}

func (c *PhaseCapability) readPatch(args map[string]any) *Patch {
	if args == nil {
		return nil
	}
	if raw, ok := args["patch"]; ok {
		switch typed := raw.(type) {
		case *Patch:
			return typed
		case Patch:
			return &typed
		case map[string]any:
			return c.readPatch(typed)
		}
	}
	filePath := readPhaseString(args, "file_path", "")
	oldContent := readPhaseString(args, "old_content", "")
	newContent := readPhaseString(args, "new_content", "")
	description := readPhaseString(args, "description", "")
	if filePath == "" && oldContent == "" && newContent == "" && description == "" {
		return nil
	}
	return &Patch{FilePath: filePath, OldContent: oldContent, NewContent: newContent, Description: description}
}

func (c *PhaseCapability) runCapture(args map[string]any) (string, error) {
	previous, err := c.loadExistingArtifact()
	if err != nil {
		return "", err
	}

	// Extract structured_data if provided
	var structuredData map[string]any
	if sd, ok := args["structured_data"].(map[string]any); ok && sd != nil {
		structuredData = sd
	} else if previous.StructuredData != nil {
		// Preserve previous structured data if not provided in this call
		structuredData = previous.StructuredData
	}

	artifact := phaseArtifact{
		PhaseID:             c.phaseID,
		Summary:             readPhaseString(args, "summary", previous.Summary),
		Status:              readPhaseString(args, "status", previous.Status),
		Requirements:        mergePhaseStringSlice(readPhaseStringSlice(args, "requirements"), previous.Requirements),
		Plan:                mergePhaseStringSlice(readPhaseStringSlice(args, "plan"), previous.Plan),
		Notes:               mergePhaseStringSlice(readPhaseStringSlice(args, "notes"), previous.Notes),
		Evidence:            mergePhaseStringSlice(readPhaseStringSlice(args, "evidence"), previous.Evidence),
		NextSteps:           mergePhaseStringSlice(readPhaseStringSlice(args, "next_steps"), previous.NextSteps),
		TransitionTo:        readPhaseString(args, "transition_to", previous.TransitionTo),
		TransitionFrom:      readPhaseString(args, "transition_from", previous.TransitionFrom),
		TransitionDirection: readPhaseString(args, "transition_direction", previous.TransitionDirection),
		ExitRequested:       readPhaseBool(args, "exit") || previous.ExitRequested,
		Iteration:           previous.Iteration + 1,
		UpdatedAt:           time.Now().UTC(),
		StructuredData:      structuredData,
	}
	if artifact.Iteration <= 0 {
		artifact.Iteration = 1
	}
	if artifact.Status == "" {
		artifact.Status = "in_progress"
	}
	if artifact.Summary == "" {
		artifact.Summary = readPhaseString(args, "description", "")
	}
	if artifact.Summary == "" {
		artifact.Summary = fmt.Sprintf("%s phase artifact", c.name)
	}
	if artifact.TransitionTo == "" && previous.TransitionTo != "" {
		artifact.TransitionTo = previous.TransitionTo
	}
	if artifact.TransitionFrom == "" && previous.TransitionFrom != "" {
		artifact.TransitionFrom = previous.TransitionFrom
	}
	if artifact.TransitionDirection == "" && previous.TransitionDirection != "" {
		artifact.TransitionDirection = previous.TransitionDirection
	}

	// Phase Succession: Determine next phase from PhasePlan if complete
	// Priority Rules:
	// 1. Exit requests: Always honored (artifact.ExitRequested)
	// 2. Explicit transitions: LLM-provided transition_to overrides plan (checkpoints or user requests)
	// 3. Automatic succession: Use plan if no explicit transition
	if strings.ToLower(strings.TrimSpace(artifact.Status)) == "complete" {
		if artifact.TransitionTo == "" && !artifact.ExitRequested {
			// No explicit transition provided - use plan's next phase
			nextPhase, direction := c.determineNextPhaseFromPlan()
			if nextPhase != "" {
				artifact.TransitionTo = nextPhase
				artifact.TransitionDirection = direction
			}
		}
	}

	if err := c.persistArtifact(artifact); err != nil {
		return "", err
	}

	// Build response message
	payload, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal artifact: %w", err)
	}

	response := fmt.Sprintf("Recorded %s phase artifact:\n%s", c.name, string(payload))

	// Add completion notice if status is "complete"
	if strings.ToLower(strings.TrimSpace(artifact.Status)) == "complete" {
		response += fmt.Sprintf("\n\n✅ Phase %s marked as COMPLETE", c.name)

		// Check if this is a checkpoint phase
		isCheckpoint := c.isCheckpointPhase()
		if isCheckpoint {
			response += " 🔍 (CHECKPOINT PHASE)"
		}

		// Show progress through plan
		progressInfo := c.getProgressInfo()
		if progressInfo != "" {
			response += "\n\n" + progressInfo
		}

		if artifact.TransitionTo != "" {
			response += fmt.Sprintf("\n\n→ Next phase: %s", formatPhaseName(artifact.TransitionTo))
			if isCheckpoint {
				response += "\n\n💡 TIP: At checkpoints, you can override the planned path by specifying a different transition_to if the situation has changed."
			}
		} else if artifact.ExitRequested {
			response += "\n\n→ Exiting workflow"
		}
	} else if artifact.Status == "blocked" || artifact.Status == "failed" {
		// Handle blocked/failed states
		response += fmt.Sprintf("\n\n⚠️  Phase %s status: %s", c.name, strings.ToUpper(artifact.Status))

		if artifact.ExitRequested {
			response += "\n\n→ Exit requested. Workflow will stop."
		} else {
			response += "\n\n🔧 OPTIONS:"
			response += "\n   1. Retry this phase after addressing the issue"
			response += "\n   2. Skip to a different phase by setting transition_to"
			response += "\n   3. Request exit by setting exit=true"

			// Show what's remaining in the plan
			progressInfo := c.getProgressInfo()
			if progressInfo != "" {
				response += "\n\n" + progressInfo
			}
		}
	}

	return response, nil
}

func (c *PhaseCapability) runState(args map[string]any) (string, error) {
	storage := c.ensureSharedInfrastructure()
	if storage.Memory == nil {
		storage.Memory = NewInMemorySessionMemory()
	}
	values, err := storage.Memory.Load("phase-artifact:" + c.phaseID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "memory not found") {
			return fmt.Sprintf("No persisted state found for %s yet.", c.name), nil
		}
		return "", fmt.Errorf("load phase state: %w", err)
	}
	artifactJSON := values["artifact_json"]
	if artifactJSON == "" {
		return fmt.Sprintf("No persisted state found for %s yet.", c.name), nil
	}
	return fmt.Sprintf("Current %s phase state:\n%s", c.name, artifactJSON), nil
}

func (c *PhaseCapability) loadExistingArtifact() (phaseArtifact, error) {
	storage := c.ensureSharedInfrastructure()
	if storage.Memory == nil {
		storage.Memory = NewInMemorySessionMemory()
	}
	values, err := storage.Memory.Load("phase-artifact:" + c.phaseID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "memory not found") {
			return phaseArtifact{}, nil
		}
		return phaseArtifact{}, fmt.Errorf("load phase artifact: %w", err)
	}
	artifactJSON := values["artifact_json"]
	if artifactJSON == "" {
		return phaseArtifact{}, nil
	}
	var artifact phaseArtifact
	if err := json.Unmarshal([]byte(artifactJSON), &artifact); err != nil {
		return phaseArtifact{}, fmt.Errorf("decode phase artifact: %w", err)
	}
	return artifact, nil
}

func (c *PhaseCapability) persistArtifact(artifact phaseArtifact) error {
	storage := c.ensureSharedInfrastructure()
	if storage.Memory == nil {
		storage.Memory = NewInMemorySessionMemory()
	}
	payload, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("marshal phase artifact: %w", err)
	}
	values := map[string]string{
		"artifact_json": string(payload),
		"phase":         artifact.PhaseID,
		"summary":       artifact.Summary,
		"status":        artifact.Status,
		"updated_at":    artifact.UpdatedAt.Format(time.RFC3339),
	}
	if err := storage.Memory.Save("phase-artifact:"+artifact.PhaseID, values); err != nil {
		return fmt.Errorf("persist phase artifact: %w", err)
	}
	if storage != nil {
		_ = storage
	}
	return nil
}

// determineNextPhaseFromPlan reads the PhasePlan and determines the next phase
// Returns (nextPhase, direction) or ("", "") if no plan or no next phase
func (c *PhaseCapability) determineNextPhaseFromPlan() (string, string) {
	if c == nil || c.sharedInfra == nil || c.sharedInfra.STMAccessor == nil {
		return "", ""
	}

	// Get STM and current thread
	stm := c.sharedInfra.STMAccessor()
	if stm == nil {
		return "", ""
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return "", ""
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return "", ""
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return "", ""
	}

	// Find current phase in plan
	plannedPhases := plan.GetPlannedPhases()
	currentIdx := plan.GetCurrentIndex()

	// Verify we're at the right phase
	if currentIdx < 0 || currentIdx >= len(plannedPhases) {
		return "", ""
	}

	if plannedPhases[currentIdx] != c.phaseID {
		// Plan is out of sync - try to find current phase
		for i, phase := range plannedPhases {
			if phase == c.phaseID {
				// Update plan index
				plan.SetCurrentIndex(i)
				currentIdx = i
				break
			}
		}
	}

	// Advance to next phase
	nextPhase, hasNext := plan.AdvancePhase()
	if !hasNext {
		return "", "" // No more phases
	}

	// Check if next phase is a checkpoint
	checkpoints := plan.GetCheckpoints()
	checkpointKey := fmt.Sprintf("after_%s", c.phaseID)
	isCheckpoint := checkpoints != nil && checkpoints[checkpointKey]

	direction := "forward"
	if isCheckpoint {
		// Checkpoint: Handoff() will re-assess and potentially change direction
		// For now, still advance forward, Handoff will handle re-routing
		direction = "forward"
	}

	return nextPhase, direction
}

// isCheckpointPhase checks if the current phase is a checkpoint in the PhasePlan
func (c *PhaseCapability) isCheckpointPhase() bool {
	if c == nil || c.sharedInfra == nil || c.sharedInfra.STMAccessor == nil {
		return false
	}

	stm := c.sharedInfra.STMAccessor()
	if stm == nil {
		return false
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return false
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return false
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return false
	}

	checkpoints := plan.GetCheckpoints()
	if checkpoints == nil {
		return false
	}

	// Check for checkpoint after current phase
	checkpointKey := fmt.Sprintf("after_%s", c.phaseID)
	return checkpoints[checkpointKey]
}

// getProgressInfo returns a formatted string showing progress through the phase plan
func (c *PhaseCapability) getProgressInfo() string {
	if c == nil || c.sharedInfra == nil || c.sharedInfra.STMAccessor == nil {
		return ""
	}

	stm := c.sharedInfra.STMAccessor()
	if stm == nil {
		return ""
	}

	thread := stm.GetCurrentThread()
	if thread == nil {
		return ""
	}

	streamCtx := thread.GetStreamContext()
	if streamCtx == nil {
		return ""
	}

	plan := streamCtx.GetPhasePlan()
	if plan == nil {
		return ""
	}

	phases := plan.GetPlannedPhases()
	currentIdx := plan.GetCurrentIndex()

	if currentIdx < 0 || currentIdx >= len(phases) {
		return ""
	}

	// Calculate progress
	// Note: CurrentIndex points to the NEXT phase to execute (cursor already advanced)
	// So CurrentIndex itself is the number of completed phases
	completed := currentIdx
	total := len(phases)

	if completed > total {
		completed = total // Safety check
	}

	percentage := 0
	if total > 0 {
		percentage = (completed * 100) / total
	}

	var progress strings.Builder
	progress.WriteString(fmt.Sprintf("📊 PROGRESS: %d/%d phases complete (%d%%)", completed, total, percentage))
	progress.WriteString("\n\n")

	// Show visual progress bar
	progress.WriteString("   ")
	for i, phase := range phases {
		if i < completed {
			progress.WriteString("✅ ")
		} else if i == completed {
			progress.WriteString("⏭️  ")
		} else {
			progress.WriteString("⬜ ")
		}
		progress.WriteString(formatPhaseName(phase))
		if i < len(phases)-1 {
			progress.WriteString("\n   ")
		}
	}

	return progress.String()
}

func readPhaseString(args map[string]any, key, fallback string) string {
	if args == nil {
		return fallback
	}
	if value, ok := args[key]; ok {
		if str, ok := value.(string); ok {
			return strings.TrimSpace(str)
		}
	}
	return fallback
}

func readPhaseStringSlice(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	if value, ok := args[key]; ok {
		switch typed := value.(type) {
		case []string:
			return sanitizePhaseStrings(typed)
		case []any:
			items := make([]string, 0, len(typed))
			for _, item := range typed {
				if str, ok := item.(string); ok {
					items = append(items, strings.TrimSpace(str))
				}
			}
			return sanitizePhaseStrings(items)
		case string:
			parts := strings.Split(typed, ",")
			items := make([]string, 0, len(parts))
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					items = append(items, trimmed)
				}
			}
			return sanitizePhaseStrings(items)
		}
	}
	return nil
}

func sanitizePhaseStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return filtered
}

func mergePhaseStringSlice(current, previous []string) []string {
	if len(current) > 0 {
		return current
	}
	if len(previous) > 0 {
		return previous
	}
	return nil
}

func readPhaseBool(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	if value, ok := args[key]; ok {
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return strings.EqualFold(typed, "true") || strings.EqualFold(typed, "yes") || strings.EqualFold(typed, "1")
		case int:
			return typed != 0
		case int64:
			return typed != 0
		case float64:
			return typed != 0
		}
	}
	return false
}

type phaseArtifact struct {
	PhaseID             string         `json:"phase_id"`
	Summary             string         `json:"summary"`
	Status              string         `json:"status"`
	Requirements        []string       `json:"requirements,omitempty"`
	Plan                []string       `json:"plan,omitempty"`
	Notes               []string       `json:"notes,omitempty"`
	Evidence            []string       `json:"evidence,omitempty"`
	NextSteps           []string       `json:"next_steps,omitempty"`
	TransitionTo        string         `json:"transition_to,omitempty"`
	TransitionFrom      string         `json:"transition_from,omitempty"`
	TransitionDirection string         `json:"transition_direction,omitempty"`
	ExitRequested       bool           `json:"exit_requested,omitempty"`
	Iteration           int            `json:"iteration"`
	UpdatedAt           time.Time      `json:"updated_at"`
	StructuredData      map[string]any `json:"structured_data,omitempty"` // Phase-specific structured data (test results, findings, etc.)
}
