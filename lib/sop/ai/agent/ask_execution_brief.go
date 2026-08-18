package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

type ExecutionBrief struct {
	WorkflowShape      string               `json:"workflow_shape"`
	Objective          string               `json:"objective"`
	CurrentFocus       string               `json:"current_focus,omitempty"`
	Steps              []ExecutionBriefStep `json:"steps,omitempty"`
	CompletionCriteria []string             `json:"completion_criteria,omitempty"`
	EscalationRules    []string             `json:"escalation_rules,omitempty"`
	CreatedAt          int64                `json:"created_at,omitempty"`
}

type ExecutionBriefStep struct {
	Title               string   `json:"title"`
	Phase               string   `json:"phase"`
	Description         string   `json:"description"`
	ContextInstructions []string `json:"context_instructions,omitempty"`
	Validation          []string `json:"validation,omitempty"`
}

func buildCodingExecutionBrief(query string, classification *AskTaskContext, plannedPhases []string) *ExecutionBrief {
	if classification == nil || !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return nil
	}

	objective := strings.TrimSpace(query)
	if objective == "" {
		objective = strings.TrimSpace(classification.OriginalQuery)
	}
	if objective == "" {
		objective = "Complete the active coding task"
	}

	workflowShape := inferCodingWorkflowShape(objective, plannedPhases)
	currentPhase := resolveActiveCodingRuntimePhase(classification, classification.Phase)

	return &ExecutionBrief{
		WorkflowShape:      workflowShape,
		Objective:          objective,
		CurrentFocus:       currentFocusForCodingPhase(currentPhase, workflowShape),
		Steps:              buildCodingExecutionBriefSteps(objective, workflowShape),
		CompletionCriteria: buildCodingCompletionCriteria(workflowShape),
		EscalationRules:    buildCodingEscalationRules(workflowShape),
		CreatedAt:          time.Now().UnixMilli(),
	}
}

func inferCodingWorkflowShape(query string, plannedPhases []string) string {
	lower := strings.ToLower(strings.TrimSpace(query))
	switch {
	case (strings.Contains(lower, "failing") || strings.Contains(lower, "failed") || strings.Contains(lower, "broken")) && strings.Contains(lower, "test"):
		return "checkpointed_fix"
	case strings.Contains(lower, "feature") || strings.Contains(lower, "implement") || strings.Contains(lower, "add ") || strings.Contains(lower, "build ") || strings.Contains(lower, "create "):
		if len(plannedPhases) >= 3 {
			return "feature_plan"
		}
		return "bounded_implementation"
	case strings.Contains(lower, "refactor") || strings.Contains(lower, "cleanup") || strings.Contains(lower, "simplify"):
		return "behavior_preserving_refactor"
	case strings.Contains(lower, "fix") || strings.Contains(lower, "bug") || strings.Contains(lower, "debug") || strings.Contains(lower, "error"):
		return "bounded_fix"
	default:
		if len(plannedPhases) >= 3 {
			return "feature_plan"
		}
		return "bounded_implementation"
	}
}

func currentFocusForCodingPhase(currentPhase, workflowShape string) string {
	switch currentPhase {
	case "investigation":
		if workflowShape == "checkpointed_fix" {
			return "Identify the exact failing signal, trace the path, and define the smallest repair boundary before editing."
		}
		return "Gather the evidence, affected files, and constraints that determine the next safe implementation step."
	case "planning":
		return "Turn the gathered evidence into a concrete step sequence with a narrow validation target before implementation."
	case "execution":
		return "Work through the current implementation steps in order, validate after each meaningful change, and summarize remaining risk before concluding."
	default:
		return "Stay grounded in the current code evidence and execute the next smallest validated step."
	}
}

func buildCodingExecutionBriefSteps(query, workflowShape string) []ExecutionBriefStep {
	switch workflowShape {
	case "checkpointed_fix":
		return []ExecutionBriefStep{
			{Title: "Step 1", Phase: "investigation", Description: "Identify the first failing package or folder cluster and capture its current failure output.", ContextInstructions: []string{"Prefer the narrowest failing package or test command before reading broader code.", "Capture the concrete failure text for only the active cluster so the next step works from observed behavior instead of memory.", "Do not move to another failing package or folder until this cluster is repaired or explicitly blocked."}, Validation: []string{"active failing package, folder, or test command is explicit"}},
			{Title: "Step 2", Phase: "investigation", Description: "Trace the failing code path for the active cluster and form one falsifiable local hypothesis for the failure.", ContextInstructions: []string{"Read the failing test and the nearest implementation path together.", "Name the specific file, function, or invariant most likely responsible before proposing a fix.", "Keep the investigation inside the current package or folder cluster unless the evidence proves the defect crosses boundaries."}, Validation: []string{"suspect file, behavior, or invariant is named"}},
			{Title: "Step 3", Phase: "planning", Description: "Choose the smallest repair and the narrowest validation target before editing files.", ContextInstructions: []string{"Keep the repair boundary local unless the evidence proves a broader defect.", "State the exact validation target that could falsify the chosen repair."}, Validation: []string{"repair scope and validation command are concrete"}},
			{Title: "Step 4", Phase: "execution", Description: "Implement the smallest plausible fix that explains the observed failure in the active package or folder cluster.", ContextInstructions: []string{"Touch only the files needed to test the current hypothesis.", "Avoid opportunistic cleanup or refactors unless the plan explicitly requires them.", "Finish the repair attempt for the current package or folder cluster before moving to another cluster."}, Validation: []string{"change stays within the observed repair boundary"}},
			{Title: "Step 5", Phase: "execution", Description: "Rerun the narrowest relevant tests for the active cluster, then widen validation only if the local fix passes.", ContextInstructions: []string{"Run the original failing check first.", "If the first failure came from a broad suite run, convert it into a targeted package or test rerun before doing anything broader.", "Only move to the next failing package or folder after the current cluster passes or is explicitly blocked."}, Validation: []string{"original failing check rerun", "broader validation widened only when justified"}},
			{Title: "Step 6", Phase: "execution", Description: "Summarize the root cause, the fix, and any residual risk or follow-up work.", ContextInstructions: []string{"State whether the failure was local, architectural, or still partially uncertain.", "Surface remaining validation gaps instead of implying broader confidence than the evidence supports."}, Validation: []string{"result status is explicit"}},
		}
	case "feature_plan":
		return []ExecutionBriefStep{
			{Title: "Step 1", Phase: "investigation", Description: "Inspect the existing architecture, impacted files, and nearby patterns that constrain the feature.", ContextInstructions: []string{"Identify the files, interfaces, and dependencies that the first slice cannot violate.", "Prefer nearby implementations over abstract restatements of the feature."}, Validation: []string{"affected surfaces and constraints are identified"}},
			{Title: "Step 2", Phase: "planning", Description: "Define the feature slices, interfaces, risks, and acceptance criteria before changing code.", ContextInstructions: []string{"Separate must-have feature behavior from optional follow-up work.", "Make acceptance criteria concrete enough to validate incrementally."}, Validation: []string{"implementation slices and acceptance criteria are concrete"}},
			{Title: "Step 3", Phase: "planning", Description: "Choose the first implementation slice and its validation seam.", ContextInstructions: []string{"Pick the slice with the clearest test seam or user-visible check first.", "Keep the slice narrow enough to complete safely in the current turn when possible."}, Validation: []string{"first slice has a clear validation path"}},
			{Title: "Step 4", Phase: "execution", Description: "Implement the first slice with the smallest valid increment.", ContextInstructions: []string{"Keep the patch aligned to the current slice only.", "Use existing patterns when they already solve the same interface or lifecycle problem."}, Validation: []string{"change is scoped to the active slice"}},
			{Title: "Step 5", Phase: "execution", Description: "Validate the slice, then continue the remaining slices only if the current one holds.", ContextInstructions: []string{"Do not stack the next slice on top of an unvalidated change.", "If the current slice fails, step back to the slice plan instead of widening the patch."}, Validation: []string{"current slice validated before expansion"}},
			{Title: "Step 6", Phase: "execution", Description: "Review the final result against acceptance criteria and summarize any follow-up work.", ContextInstructions: []string{"Compare the final state directly against the earlier acceptance criteria.", "Call out deferred slices or risk explicitly if the feature remains partial."}, Validation: []string{"acceptance criteria checked explicitly"}},
		}
	case "behavior_preserving_refactor":
		return []ExecutionBriefStep{
			{Title: "Step 1", Phase: "investigation", Description: "Map the current behavior, affected callers, and invariants that must not change.", ContextInstructions: []string{"Read representative callers before moving code so hidden contracts stay visible.", "Name the invariants that would count as a regression before editing."}, Validation: []string{"behavioral invariants are explicit"}},
			{Title: "Step 2", Phase: "planning", Description: "Define the refactor boundary and sequencing so behavior stays stable.", ContextInstructions: []string{"Prefer incremental sequencing that keeps the code runnable after each step.", "Separate structure changes from behavior changes."}, Validation: []string{"refactor boundary is concrete"}},
			{Title: "Step 3", Phase: "execution", Description: "Apply the focused refactor incrementally.", ContextInstructions: []string{"Keep each edit mechanically understandable and locally reversible.", "Avoid mixing cleanup unrelated to the stated boundary."}, Validation: []string{"each change preserves behavior intent"}},
			{Title: "Step 4", Phase: "execution", Description: "Rerun the relevant validations and confirm behavior parity.", ContextInstructions: []string{"Prefer the tests or checks that exercise the named invariants.", "If parity is uncertain, say exactly what remains unverified."}, Validation: []string{"behavior parity stated explicitly"}},
		}
	default:
		return []ExecutionBriefStep{
			{Title: "Step 1", Phase: "investigation", Description: "Inspect the current behavior, error, or request boundary before editing.", ContextInstructions: []string{"Anchor on the most local evidence available before planning the change."}, Validation: []string{"local problem boundary is explicit"}},
			{Title: "Step 2", Phase: "planning", Description: "Choose the smallest concrete implementation step and validation target.", ContextInstructions: []string{"Prefer a plan that can be falsified quickly by one focused check."}, Validation: []string{"implementation step and validation path are concrete"}},
			{Title: "Step 3", Phase: "execution", Description: "Apply the smallest valid change.", ContextInstructions: []string{"Keep the edit within the validated boundary of the request."}, Validation: []string{"change remains scoped to the observed need"}},
			{Title: "Step 4", Phase: "execution", Description: fmt.Sprintf("Validate the result against the original request: %s", strings.TrimSpace(query)), ContextInstructions: []string{"Report the validation result in terms of the original request, not just tool success."}, Validation: []string{"relevant validation rerun or explicit blocker recorded"}},
		}
	}
}

func buildCodingCompletionCriteria(workflowShape string) []string {
	criteria := []string{
		"Current objective is satisfied or the blocking condition is explicit.",
		"The most relevant validation step has been rerun or the blocker is recorded.",
		"The final summary states what changed and what remains uncertain.",
	}
	if workflowShape == "feature_plan" {
		criteria = append(criteria, "Acceptance criteria are checked explicitly before completion.")
	}
	return criteria
}

func buildCodingEscalationRules(workflowShape string) []string {
	rules := []string{
		"If the local hypothesis fails, step back to investigation before broadening the fix.",
		"If the change expands across subsystems, treat it as a larger planned workflow instead of patching opportunistically.",
	}
	if workflowShape == "checkpointed_fix" {
		rules = append(rules, "If the failing tests reveal missing product behavior rather than a local defect, promote the work to feature planning.")
	}
	return rules
}

func (a *CopilotAgent) attachExecutionBriefToCurrentWorkStream(classification *AskTaskContext, plannedPhases []string, query string) {
	if a == nil || classification == nil || !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return
	}
	thread := a.currentConversationThread()
	if thread == nil || thread.StreamContext == nil || thread.StreamContext.PhasePlan == nil {
		return
	}
	a.attachStructuredPlanToCurrentWorkStream(classification, plannedPhases, query)
	plan := thread.StreamContext.PhasePlan
	if authored := a.loadAuthoredExecutionBrief(classification, plannedPhases, query); authored != nil {
		plan.ExecutionBrief = authored
		plan.LastUpdated = time.Now().UnixMilli()
		return
	}
	if plan.ExecutionBrief != nil && len(plan.ExecutionBrief.Steps) > 0 {
		return
	}
	plan.ExecutionBrief = buildCodingExecutionBrief(query, classification, plannedPhases)
	plan.LastUpdated = time.Now().UnixMilli()
}

func (a *CopilotAgent) currentConversationThread() *ConversationThread {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return nil
	}
	return a.service.session.Memory.GetCurrentThread()
}

func (a *CopilotAgent) currentStructuredPlan() *cw.StructuredPlan {
	thread := a.currentConversationThread()
	if thread == nil || thread.StreamContext == nil {
		return nil
	}
	return thread.StreamContext.StructuredPlan
}

func (a *CopilotAgent) currentExecutionBrief(classification *AskTaskContext, query string) *ExecutionBrief {
	if classification == nil {
		return nil
	}
	if !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return a.executionBriefFromStructuredPlan(classification, query)
	}
	if fromPlan := a.executionBriefFromStructuredPlan(classification, query); fromPlan != nil {
		return fromPlan
	}
	if authored := a.loadAuthoredExecutionBrief(classification, classification.PhasePlan, query); authored != nil {
		return authored
	}
	if thread := a.currentConversationThread(); thread != nil && thread.StreamContext != nil && thread.StreamContext.PhasePlan != nil && thread.StreamContext.PhasePlan.ExecutionBrief != nil {
		return thread.StreamContext.PhasePlan.ExecutionBrief
	}
	return buildCodingExecutionBrief(query, classification, classification.PhasePlan)
}

func (a *CopilotAgent) attachStructuredPlanToCurrentWorkStream(classification *AskTaskContext, plannedPhases []string, query string) {
	thread := a.currentConversationThread()
	if thread == nil || thread.StreamContext == nil {
		return
	}
	if thread.StreamContext.StructuredPlan != nil && len(thread.StreamContext.StructuredPlan.Steps) > 0 {
		return
	}
	artifact, ok := a.loadStoredPlanningArtifact()
	if !ok {
		return
	}
	plan := structuredPlanFromStoredPlanningArtifact(artifact, classification, plannedPhases, query)
	if plan == nil || len(plan.Steps) == 0 {
		return
	}
	thread.StreamContext.StructuredPlan = plan
	if plan.CurrentStep >= 0 && plan.CurrentStep < len(plan.Steps) {
		thread.StreamContext.CurrentPhase = stepPhaseForStructuredPlanStep(plan.Steps[plan.CurrentStep], plan.CurrentStep)
	}
}

func (a *CopilotAgent) executionBriefFromStructuredPlan(classification *AskTaskContext, query string) *ExecutionBrief {
	thread := a.currentConversationThread()
	if thread == nil || thread.StreamContext == nil || thread.StreamContext.StructuredPlan == nil {
		return nil
	}
	plan := thread.StreamContext.StructuredPlan
	if len(plan.Steps) == 0 {
		return nil
	}
	objective := strings.TrimSpace(plan.Objective)
	if objective == "" {
		objective = strings.TrimSpace(query)
	}
	if objective == "" {
		objective = strings.TrimSpace(classification.OriginalQuery)
	}
	workflowShape := strings.TrimSpace(plan.WorkflowShape)
	if workflowShape == "" {
		workflowShape = inferWorkflowShapeForDomain(strings.TrimSpace(plan.Domain), objective, classification.PhasePlan)
	}
	currentPhase := strings.TrimSpace(classification.Phase)
	if currentPhase == "" {
		currentPhase = stepPhaseForStructuredPlanStep(plan.Steps[plan.CurrentStep], plan.CurrentStep)
	}
	briefSteps := executionBriefStepsFromStructuredPlan(plan)
	if len(briefSteps) == 0 {
		return nil
	}
	completionCriteria := plan.CompletionGate.Requirements
	if len(completionCriteria) == 0 {
		completionCriteria = buildCompletionCriteriaForDomain(strings.TrimSpace(plan.Domain), workflowShape)
	}
	return &ExecutionBrief{
		WorkflowShape:      workflowShape,
		Objective:          objective,
		CurrentFocus:       currentFocusForExecutionDomain(strings.TrimSpace(plan.Domain), currentPhase, workflowShape),
		Steps:              briefSteps,
		CompletionCriteria: completionCriteria,
		EscalationRules:    buildEscalationRulesForDomain(strings.TrimSpace(plan.Domain), workflowShape),
		CreatedAt:          plan.CreatedAt,
	}
}

func executionBriefStepsFromStructuredPlan(plan *cw.StructuredPlan) []ExecutionBriefStep {
	if plan == nil || len(plan.Steps) == 0 {
		return nil
	}
	steps := make([]ExecutionBriefStep, 0, len(plan.Steps))
	for idx, step := range plan.Steps {
		description := strings.TrimSpace(step.ActionInstruction)
		if description == "" {
			description = strings.TrimSpace(step.Goal)
		}
		if description == "" {
			continue
		}
		context := make([]string, 0, 4)
		if len(step.EvidenceToCollect) > 0 {
			context = append(context, "Evidence: "+strings.Join(step.EvidenceToCollect, "; "))
		}
		if strings.TrimSpace(step.DecisionRule) != "" {
			context = append(context, "Decision rule: "+strings.TrimSpace(step.DecisionRule))
		}
		if strings.TrimSpace(step.AllowedChangeScope) != "" {
			context = append(context, "Allowed scope: "+strings.TrimSpace(step.AllowedChangeScope))
		}
		if len(step.Files) > 0 {
			context = append(context, "Focus files: "+strings.Join(step.Files, ", "))
		}
		steps = append(steps, ExecutionBriefStep{
			Title:               fallbackTitle(step.StepID, idx+1),
			Phase:               structuredPlanStepPhaseForDomain(plan.Domain, step, idx),
			Description:         description,
			ContextInstructions: context,
			Validation:          append([]string(nil), step.Validation...),
		})
	}
	return steps
}

func stepPhaseForStructuredPlanStep(step cw.StructuredPlanStep, idx int) string {
	if phase := normalizedStructuredPlanPhase(step.StepDomain); phase != "" {
		return phase
	}
	text := strings.ToLower(strings.TrimSpace(step.ActionInstruction + " " + step.Goal + " " + strings.Join(step.Validation, " ") + " " + strings.Join(step.ExitCriteria, " ")))
	switch {
	case actionLooksLikeValidation(text):
		return "review"
	case actionLooksLikePlanning(text):
		return "planning"
	case actionLooksLikeInvestigation(text):
		return "investigation"
	}
	switch {
	case idx == 0:
		return "investigation"
	case idx == 1:
		return "planning"
	default:
		return "execution"
	}
}

func structuredPlanStepPhaseForDomain(domain string, step cw.StructuredPlanStep, idx int) string {
	phase := stepPhaseForStructuredPlanStep(step, idx)
	if strings.EqualFold(strings.TrimSpace(domain), "coding_workflow") || strings.EqualFold(strings.TrimSpace(domain), "coding") {
		if phase == "review" || phase == "delivery" || phase == "testing" {
			return "execution"
		}
	}
	return phase
}

func inferWorkflowShapeForDomain(domain, query string, plannedPhases []string) string {
	if strings.EqualFold(strings.TrimSpace(domain), "coding_workflow") || strings.EqualFold(strings.TrimSpace(domain), "coding") {
		return inferCodingWorkflowShape(query, plannedPhases)
	}
	lower := strings.ToLower(strings.TrimSpace(query))
	switch {
	case strings.Contains(lower, "review") || strings.Contains(lower, "validate") || strings.Contains(lower, "verify"):
		return "verification"
	case strings.Contains(lower, "plan") || strings.Contains(lower, "design"):
		return "design"
	case strings.Contains(lower, "fix") || strings.Contains(lower, "repair") || strings.Contains(lower, "fail"):
		return "repair"
	default:
		return "execution"
	}
}

func currentFocusForExecutionDomain(domain, currentPhase, workflowShape string) string {
	if strings.EqualFold(strings.TrimSpace(domain), "coding_workflow") || strings.EqualFold(strings.TrimSpace(domain), "coding") || strings.TrimSpace(domain) == "" {
		return currentFocusForCodingPhase(currentPhase, workflowShape)
	}
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "stores":
		switch currentPhase {
		case "investigation":
			return "Confirm the target stores, schema evidence, and result contract before changing query or mutation behavior."
		case "planning":
			return "Choose the narrowest store operation or script boundary, and make read-only vs mutating scope explicit before execution."
		case "review":
			return "Validate the observed store results against the requested contract and call out remaining schema or data-shape uncertainty."
		default:
			return "Execute the smallest store change or validation step that matches the observed schema and requested result contract."
		}
	case "spaces":
		switch currentPhase {
		case "investigation":
			return "Inspect the current retrieval or knowledge surface before changing persisted knowledge state."
		case "planning":
			return "Constrain the mutation scope, semantic intent, and validation path before changing space content or configuration."
		case "review":
			return "Confirm the resulting retrieval or knowledge-state outcome and note any remaining semantic ambiguity."
		default:
			return "Apply the narrowest knowledge mutation or retrieval step that preserves the intended space semantics."
		}
	case "desktop":
		switch currentPhase {
		case "investigation":
			return "Confirm the working directory, command target, and safety boundary before running local automation."
		case "planning":
			return "Sequence the command plan so shell state, filesystem scope, and validation are explicit before execution."
		case "review":
			return "Validate command output and resulting filesystem or git state against the requested outcome."
		default:
			return "Run the narrowest safe command sequence that satisfies the requested local automation task."
		}
	default:
		return "Stay grounded in the current evidence and execute the next smallest validated step."
	}
}

func buildCompletionCriteriaForDomain(domain, workflowShape string) []string {
	if strings.EqualFold(strings.TrimSpace(domain), "coding_workflow") || strings.EqualFold(strings.TrimSpace(domain), "coding") || strings.TrimSpace(domain) == "" {
		return buildCodingCompletionCriteria(workflowShape)
	}
	return []string{
		"Current objective is satisfied or the blocking condition is explicit.",
		"The most relevant domain-specific validation step has been rerun or the blocker is recorded.",
		"The final summary states what changed and what remains uncertain.",
	}
}

func buildEscalationRulesForDomain(domain, workflowShape string) []string {
	if strings.EqualFold(strings.TrimSpace(domain), "coding_workflow") || strings.EqualFold(strings.TrimSpace(domain), "coding") || strings.TrimSpace(domain) == "" {
		return buildCodingEscalationRules(workflowShape)
	}
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "stores":
		return []string{
			"If the observed store shape conflicts with the expected contract, step back to investigation before widening the mutation.",
			"If a requested mutation requires broader schema or relation changes, promote the work to a larger planned store workflow instead of patching opportunistically.",
		}
	case "spaces":
		return []string{
			"If retrieval evidence conflicts with the expected outcome, step back to investigation before widening the knowledge mutation.",
			"If the requested change expands semantic scope across multiple spaces or categories, promote it to a broader planned workflow.",
		}
	case "desktop":
		return []string{
			"If command output does not support the intended outcome, step back to investigation before running broader or riskier commands.",
			"If the task expands beyond the explicit workspace or shell boundary, restate the command plan before continuing.",
		}
	default:
		return []string{"If the local hypothesis fails, step back to investigation before broadening scope."}
	}
}

func renderExecutionBrief(currentPhase string, brief *ExecutionBrief) string {
	if brief == nil || strings.TrimSpace(brief.Objective) == "" {
		return ""
	}

	lines := []string{"=== Active Execution Brief ===", "Objective: " + brief.Objective}
	if strings.TrimSpace(brief.WorkflowShape) != "" {
		lines = append(lines, "Workflow Shape: "+brief.WorkflowShape)
	}
	if strings.TrimSpace(brief.CurrentFocus) != "" {
		lines = append(lines, "Current Focus: "+brief.CurrentFocus)
	}
	if len(brief.Steps) > 0 {
		lines = append(lines, "Plan Sequence:")
		for _, step := range brief.Steps {
			lines = append(lines, fmt.Sprintf("- [%s] %s", step.Phase, step.Description))
			if len(step.ContextInstructions) > 0 {
				lines = append(lines, "  context: "+strings.Join(step.ContextInstructions, "; "))
			}
		}
	}

	activeSteps := make([]ExecutionBriefStep, 0, len(brief.Steps))
	upcomingSteps := make([]ExecutionBriefStep, 0, len(brief.Steps))
	for _, step := range brief.Steps {
		if step.Phase == currentPhase {
			activeSteps = append(activeSteps, step)
			continue
		}
		if len(activeSteps) == 0 || len(upcomingSteps) < 2 {
			upcomingSteps = append(upcomingSteps, step)
		}
	}
	if len(activeSteps) == 0 {
		activeSteps = brief.Steps
		upcomingSteps = nil
	}

	lines = append(lines, "Current Phase Steps:")
	for _, step := range activeSteps {
		lines = append(lines, fmt.Sprintf("- [%s] %s", step.Phase, step.Description))
		if len(step.ContextInstructions) > 0 {
			lines = append(lines, "  context: "+strings.Join(step.ContextInstructions, "; "))
		}
		if len(step.Validation) > 0 {
			lines = append(lines, "  validation: "+strings.Join(step.Validation, "; "))
		}
	}

	if len(upcomingSteps) > 0 {
		lines = append(lines, "Upcoming Steps:")
		for _, step := range upcomingSteps {
			lines = append(lines, fmt.Sprintf("- [%s] %s", step.Phase, step.Description))
		}
	}

	if len(brief.CompletionCriteria) > 0 {
		lines = append(lines, "Completion Gate:")
		for _, criterion := range brief.CompletionCriteria {
			lines = append(lines, "- "+criterion)
		}
	}
	if len(brief.EscalationRules) > 0 {
		lines = append(lines, "Escalate If:")
		for _, rule := range brief.EscalationRules {
			lines = append(lines, "- "+rule)
		}
	}

	return strings.Join(lines, "\n")
}

func renderStructuredPlanSummary(plan *cw.StructuredPlan) string {
	if plan == nil || strings.TrimSpace(plan.Objective) == "" || len(plan.Steps) == 0 {
		return ""
	}
	currentStep := plan.CurrentStep
	if currentStep < 0 || currentStep >= len(plan.Steps) {
		currentStep = 0
	}
	current := plan.Steps[currentStep]
	lines := []string{"=== Active Structured Plan ===", "Objective: " + strings.TrimSpace(plan.Objective)}
	if strings.TrimSpace(plan.WorkflowShape) != "" {
		lines = append(lines, "Workflow Shape: "+strings.TrimSpace(plan.WorkflowShape))
	}
	if strings.TrimSpace(plan.Domain) != "" {
		lines = append(lines, "Domain: "+strings.TrimSpace(plan.Domain))
	}
	lines = append(lines, fmt.Sprintf("Current Step: %d/%d", currentStep+1, len(plan.Steps)))
	if strings.TrimSpace(current.ActionInstruction) != "" {
		lines = append(lines, "Current Action: "+strings.TrimSpace(current.ActionInstruction))
	} else if strings.TrimSpace(current.Goal) != "" {
		lines = append(lines, "Current Goal: "+strings.TrimSpace(current.Goal))
	}
	if len(current.Files) > 0 {
		lines = append(lines, "Targets: "+strings.Join(current.Files, ", "))
	}
	if len(current.EvidenceToCollect) > 0 {
		lines = append(lines, "Evidence: "+strings.Join(current.EvidenceToCollect, "; "))
	}
	if strings.TrimSpace(current.DecisionRule) != "" {
		lines = append(lines, "Decision rule: "+strings.TrimSpace(current.DecisionRule))
	}
	if strings.TrimSpace(current.AllowedChangeScope) != "" {
		lines = append(lines, "Allowed scope: "+strings.TrimSpace(current.AllowedChangeScope))
	}
	if len(current.Validation) > 0 {
		lines = append(lines, "Validation: "+strings.Join(current.Validation, "; "))
	}
	if len(current.ExitCriteria) > 0 {
		lines = append(lines, "Exit criteria: "+strings.Join(current.ExitCriteria, "; "))
	}
	if currentStep+1 < len(plan.Steps) {
		next := plan.Steps[currentStep+1]
		nextText := strings.TrimSpace(next.ActionInstruction)
		if nextText == "" {
			nextText = strings.TrimSpace(next.Goal)
		}
		if nextText != "" {
			lines = append(lines, "Next Step: "+nextText)
		}
	}
	if len(plan.CompletionGate.Requirements) > 0 {
		lines = append(lines, "Completion Gate: "+strings.Join(plan.CompletionGate.Requirements, "; "))
	}
	return strings.Join(lines, "\n")
}

type storedPlanningArtifact struct {
	Summary        string         `json:"summary"`
	Status         string         `json:"status"`
	Plan           []string       `json:"plan,omitempty"`
	Notes          []string       `json:"notes,omitempty"`
	NextSteps      []string       `json:"next_steps,omitempty"`
	StructuredData map[string]any `json:"structured_data,omitempty"`
}

func (a *CopilotAgent) loadAuthoredExecutionBrief(classification *AskTaskContext, plannedPhases []string, query string) *ExecutionBrief {
	artifact, ok := a.loadStoredPlanningArtifact()
	if !ok {
		return nil
	}
	brief := executionBriefFromStoredPlanningArtifact(artifact, classification, plannedPhases, query)
	if brief == nil || len(brief.Steps) == 0 {
		return nil
	}
	return brief
}

func (a *CopilotAgent) loadStoredPlanningArtifact() (storedPlanningArtifact, bool) {
	if a == nil || a.service == nil || a.service.session == nil || a.service.session.Memory == nil {
		return storedPlanningArtifact{}, false
	}
	values, err := a.service.session.Memory.LoadWorkflowMemory("phase-artifact:planning")
	if err != nil {
		return storedPlanningArtifact{}, false
	}
	raw := strings.TrimSpace(values["artifact_json"])
	if raw == "" {
		return storedPlanningArtifact{}, false
	}
	var artifact storedPlanningArtifact
	if err := json.Unmarshal([]byte(raw), &artifact); err != nil {
		return storedPlanningArtifact{}, false
	}
	return artifact, true
}

func (a *CopilotAgent) hasStoredPlanningArtifact() bool {
	_, ok := a.loadStoredPlanningArtifact()
	return ok
}

func (a *CopilotAgent) hasExecutionReadyPlanningArtifact() bool {
	artifact, ok := a.loadStoredPlanningArtifact()
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(artifact.Status), "ready_for_execution")
}

func (a *CopilotAgent) promoteExecutionReadinessFromPlanningArtifact(classification *AskTaskContext) {
	if classification == nil || classification.ExecutionReady {
		return
	}
	if !askClassificationHasDomain(classification.Domains, "coding_workflow") {
		return
	}
	if !a.hasExecutionReadyPlanningArtifact() {
		return
	}
	classification.ExecutionReady = true
	if strings.TrimSpace(classification.Phase) == "" || strings.EqualFold(strings.TrimSpace(classification.Phase), "investigation") {
		classification.Phase = "execution"
	}
}

func executionBriefFromStoredPlanningArtifact(artifact storedPlanningArtifact, classification *AskTaskContext, plannedPhases []string, query string) *ExecutionBrief {
	if classification == nil {
		return nil
	}
	if brief := executionBriefFromStructuredPlanArtifact(artifact.StructuredData, classification, query); brief != nil {
		return brief
	}
	steps := executionBriefStepsFromStructuredData(artifact.StructuredData)
	if len(steps) == 0 {
		steps = executionBriefStepsFromPlanStrings(artifact.Plan, plannedPhases)
	}
	if len(steps) == 0 {
		return nil
	}
	objective := strings.TrimSpace(query)
	if objective == "" {
		objective = strings.TrimSpace(artifact.Summary)
	}
	if objective == "" {
		objective = strings.TrimSpace(classification.OriginalQuery)
	}
	if objective == "" {
		objective = "Complete the active coding task"
	}
	workflowShape := inferCodingWorkflowShape(objective, plannedPhases)
	if miniPlan, ok := artifact.StructuredData["phase_mini_plan"].(map[string]any); ok {
		if features, ok := miniPlan["functional_features"].([]any); ok && len(features) >= 3 {
			workflowShape = "feature_plan"
		}
	}
	brief := &ExecutionBrief{
		WorkflowShape:      workflowShape,
		Objective:          objective,
		CurrentFocus:       currentFocusForCodingPhase(resolveActiveCodingRuntimePhase(classification, classification.Phase), workflowShape),
		Steps:              steps,
		CompletionCriteria: buildCodingCompletionCriteria(workflowShape),
		EscalationRules:    buildCodingEscalationRules(workflowShape),
		CreatedAt:          time.Now().UnixMilli(),
	}
	if len(artifact.NextSteps) > 0 {
		brief.EscalationRules = append(brief.EscalationRules, "Planner next steps: "+strings.Join(artifact.NextSteps, " | "))
	}
	return brief
}

func structuredPlanFromStoredPlanningArtifact(artifact storedPlanningArtifact, classification *AskTaskContext, plannedPhases []string, query string) *cw.StructuredPlan {
	if classification == nil {
		return nil
	}
	if plan := structuredPlanFromStructuredPlanArtifact(artifact.StructuredData, classification, query); plan != nil {
		return plan
	}
	steps := structuredPlanStepsFromStructuredData(artifact.StructuredData)
	if len(steps) == 0 {
		steps = structuredPlanStepsFromPlanStrings(artifact.Plan, plannedPhases)
	}
	if len(steps) == 0 {
		return nil
	}
	objective := firstNonEmptyBriefValue(strings.TrimSpace(query), strings.TrimSpace(artifact.Summary), strings.TrimSpace(classification.OriginalQuery), "Complete the active coding task")
	workflowShape := inferCodingWorkflowShape(objective, plannedPhases)
	if miniPlan, ok := artifact.StructuredData["phase_mini_plan"].(map[string]any); ok {
		if features, ok := miniPlan["functional_features"].([]any); ok && len(features) >= 3 {
			workflowShape = "feature_plan"
		}
	}
	now := time.Now().UnixMilli()
	steps[0].Status = "active"
	steps[0].UpdatedAt = now
	return &cw.StructuredPlan{
		Objective:      objective,
		WorkflowShape:  workflowShape,
		Domain:         "coding_workflow",
		Steps:          steps,
		CurrentStep:    0,
		CompletionGate: cw.StructuredCompletionGate{Requirements: buildCodingCompletionCriteria(workflowShape)},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func structuredPlanFromStructuredPlanArtifact(data map[string]any, classification *AskTaskContext, query string) *cw.StructuredPlan {
	if len(data) == 0 {
		return nil
	}
	raw, ok := data["structured_plan"]
	if !ok || raw == nil {
		return nil
	}
	planMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	bytes, err := json.Marshal(planMap)
	if err != nil {
		return nil
	}
	var plan cw.StructuredPlan
	if err := json.Unmarshal(bytes, &plan); err != nil {
		return nil
	}
	if len(plan.Steps) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	plan.Objective = firstNonEmptyBriefValue(strings.TrimSpace(plan.Objective), strings.TrimSpace(query), strings.TrimSpace(classification.OriginalQuery))
	if strings.TrimSpace(plan.WorkflowShape) == "" {
		plan.WorkflowShape = inferCodingWorkflowShape(query, classification.PhasePlan)
	}
	if strings.TrimSpace(plan.Domain) == "" {
		plan.Domain = "coding_workflow"
	}
	if plan.CurrentStep < 0 || plan.CurrentStep >= len(plan.Steps) {
		plan.CurrentStep = 0
	}
	if len(plan.CompletionGate.Requirements) == 0 {
		plan.CompletionGate = cw.StructuredCompletionGate{Requirements: buildCodingCompletionCriteria(plan.WorkflowShape)}
	}
	for idx := range plan.Steps {
		if idx == plan.CurrentStep {
			if strings.TrimSpace(plan.Steps[idx].Status) == "" || plan.Steps[idx].Status == "pending" {
				plan.Steps[idx].Status = "active"
			}
		} else if strings.TrimSpace(plan.Steps[idx].Status) == "" {
			plan.Steps[idx].Status = "pending"
		}
		if plan.Steps[idx].UpdatedAt == 0 {
			plan.Steps[idx].UpdatedAt = now
		}
	}
	if plan.CreatedAt == 0 {
		plan.CreatedAt = now
	}
	plan.UpdatedAt = now
	return &plan
}

func structuredPlanStepsFromStructuredData(data map[string]any) []cw.StructuredPlanStep {
	if len(data) == 0 {
		return nil
	}
	raw, ok := data["detailed_execution_plan"]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]map[string]any); ok {
			items = make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		} else {
			return nil
		}
	}
	steps := make([]cw.StructuredPlanStep, 0, len(items))
	for idx, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		description, _ := entry["description"].(string)
		description = strings.TrimSpace(description)
		if description == "" {
			continue
		}
		phase := inferStepPhaseForText(description, idx, len(items))
		steps = append(steps, cw.StructuredPlanStep{
			StepID:            fmt.Sprintf("authored_step_%d", idx+1),
			StepDomain:        phase,
			Goal:              fallbackTitle(stringValueFromMap(entry, "title"), idx+1),
			Files:             stringSliceFromAny(entry["files"]),
			EvidenceToCollect: stringSliceFromAny(entry["evidence"]),
			ActionInstruction: description,
			Validation:        stringSliceFromAny(entry["validation"]),
			ExitCriteria:      stringSliceFromAny(entry["exit_criteria"]),
			EscalateIf:        stringSliceFromAny(entry["escalate_if"]),
			Status:            "pending",
		})
	}
	return steps
}

func structuredPlanStepsFromPlanStrings(plan []string, plannedPhases []string) []cw.StructuredPlanStep {
	if len(plan) == 0 {
		return nil
	}
	steps := make([]cw.StructuredPlanStep, 0, len(plan))
	for idx, description := range plan {
		description = strings.TrimSpace(description)
		if description == "" {
			continue
		}
		phase := inferStepPhaseForText(description, idx, len(plan))
		if idx < len(plannedPhases) {
			phase = plannedPhases[minInt(idx, len(plannedPhases)-1)]
		}
		steps = append(steps, cw.StructuredPlanStep{
			StepID:            fmt.Sprintf("authored_step_%d", idx+1),
			StepDomain:        phase,
			Goal:              fmt.Sprintf("Step %d", idx+1),
			ActionInstruction: description,
			Status:            "pending",
		})
	}
	return steps
}

func stringValueFromMap(entry map[string]any, key string) string {
	if entry == nil {
		return ""
	}
	value, _ := entry[key].(string)
	return strings.TrimSpace(value)
}

func executionBriefFromStructuredPlanArtifact(data map[string]any, classification *AskTaskContext, query string) *ExecutionBrief {
	if len(data) == 0 {
		return nil
	}
	raw, ok := data["structured_plan"]
	if !ok || raw == nil {
		return nil
	}
	planMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	bytes, err := json.Marshal(planMap)
	if err != nil {
		return nil
	}
	var plan cw.StructuredPlan
	if err := json.Unmarshal(bytes, &plan); err != nil {
		return nil
	}
	if len(plan.Steps) == 0 {
		return nil
	}
	workflowShape := strings.TrimSpace(plan.WorkflowShape)
	if workflowShape == "" {
		workflowShape = inferCodingWorkflowShape(query, classification.PhasePlan)
	}
	currentPhase := resolveActiveCodingRuntimePhase(classification, classification.Phase)
	completionCriteria := plan.CompletionGate.Requirements
	if len(completionCriteria) == 0 {
		completionCriteria = buildCodingCompletionCriteria(workflowShape)
	}
	return &ExecutionBrief{
		WorkflowShape:      workflowShape,
		Objective:          firstNonEmptyBriefValue(strings.TrimSpace(plan.Objective), strings.TrimSpace(query), strings.TrimSpace(classification.OriginalQuery)),
		CurrentFocus:       currentFocusForCodingPhase(currentPhase, workflowShape),
		Steps:              executionBriefStepsFromStructuredPlan(&plan),
		CompletionCriteria: completionCriteria,
		EscalationRules:    buildCodingEscalationRules(workflowShape),
		CreatedAt:          plan.CreatedAt,
	}
}

func executionBriefStepsFromStructuredData(data map[string]any) []ExecutionBriefStep {
	if len(data) == 0 {
		return nil
	}
	raw, ok := data["detailed_execution_plan"]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]map[string]any); ok {
			items = make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		} else {
			return nil
		}
	}
	steps := make([]ExecutionBriefStep, 0, len(items))
	for idx, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		description, _ := entry["description"].(string)
		if strings.TrimSpace(description) == "" {
			continue
		}
		title, _ := entry["title"].(string)
		validation := stringSliceFromAny(entry["validation"])
		steps = append(steps, ExecutionBriefStep{
			Title:               fallbackTitle(title, idx+1),
			Phase:               inferStepPhase(idx, len(items)),
			Description:         description,
			ContextInstructions: contextInstructionsFromStructuredStep(entry),
			Validation:          validation,
		})
	}
	return steps
}

func executionBriefStepsFromPlanStrings(plan []string, plannedPhases []string) []ExecutionBriefStep {
	if len(plan) == 0 {
		return nil
	}
	steps := make([]ExecutionBriefStep, 0, len(plan))
	for idx, description := range plan {
		description = strings.TrimSpace(description)
		if description == "" {
			continue
		}
		phase := inferStepPhase(idx, len(plan))
		if idx < len(plannedPhases) {
			phase = plannedPhases[minInt(idx, len(plannedPhases)-1)]
		}
		steps = append(steps, ExecutionBriefStep{Title: fmt.Sprintf("Step %d", idx+1), Phase: phase, Description: description, ContextInstructions: []string{"Use the authored plan wording as the local objective for this step."}})
	}
	return steps
}

func contextInstructionsFromStructuredStep(entry map[string]any) []string {
	var instructions []string
	if files := stringSliceFromAny(entry["files"]); len(files) > 0 {
		instructions = append(instructions, "Focus files: "+strings.Join(files, ", "))
	}
	if exitCriteria := stringSliceFromAny(entry["exit_criteria"]); len(exitCriteria) > 0 {
		instructions = append(instructions, "Exit criteria: "+strings.Join(exitCriteria, "; "))
	}
	if routing, ok := entry["routing"].(map[string]any); ok && len(routing) > 0 {
		if verification, _ := routing["expected_verification"].(string); strings.TrimSpace(verification) != "" {
			instructions = append(instructions, "Expected verification: "+strings.TrimSpace(verification))
		}
		if risk, _ := routing["risk"].(string); strings.TrimSpace(risk) != "" {
			instructions = append(instructions, "Risk level: "+strings.TrimSpace(risk))
		}
		if complexity, _ := routing["complexity"].(string); strings.TrimSpace(complexity) != "" {
			instructions = append(instructions, "Complexity: "+strings.TrimSpace(complexity))
		}
	}
	return instructions
}

func stringSliceFromAny(raw any) []string {
	if raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func inferStepPhase(idx, total int) string {
	if idx == 0 {
		return "investigation"
	}
	if total <= 2 {
		return "execution"
	}
	if idx == total-1 {
		return "execution"
	}
	if idx == 1 {
		return "planning"
	}
	return "execution"
}

func inferStepPhaseForText(description string, idx, total int) string {
	text := strings.ToLower(strings.TrimSpace(description))
	switch {
	case actionLooksLikePatch(text):
		return "execution"
	case actionLooksLikeValidation(text):
		return "execution"
	case actionLooksLikePlanning(text):
		return "planning"
	case actionLooksLikeInvestigation(text):
		return "investigation"
	default:
		return inferStepPhase(idx, total)
	}
}

func fallbackTitle(title string, stepNum int) string {
	if strings.TrimSpace(title) == "" {
		return fmt.Sprintf("Step %d", stepNum)
	}
	return title
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func firstNonEmptyBriefValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type sessionWorkflowMemoryAdapter struct {
	stm *ShortTermMemory
}

func (m *sessionWorkflowMemoryAdapter) Save(key string, values map[string]string) error {
	if m == nil || m.stm == nil {
		return fmt.Errorf("session workflow memory not available")
	}
	return m.stm.SaveWorkflowMemory(key, values)
}

func (m *sessionWorkflowMemoryAdapter) Load(key string) (map[string]string, error) {
	if m == nil || m.stm == nil {
		return nil, fmt.Errorf("session workflow memory not available")
	}
	return m.stm.LoadWorkflowMemory(key)
}
