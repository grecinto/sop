# Knowledge Base Expertise Quality Improvement Plan

## Executive Summary

This document outlines the gaps and improvements needed to transform the Knowledge Base system from a "searchable docs layer" into an "automatic expert layer" that delivers consistently high-quality, rule-compliant code generation.

**Value Proposition Context**: For a $5/month subscription, users expect more than document retrieval. They expect the KB to reduce prompt iterations, improve first-pass code quality, and enforce domain expertise automatically. This requires closing four critical gaps.

## Current State Assessment

### Infrastructure (Strong)
- ✅ Vector storage with semantic categories
- ✅ Search and retrieval APIs  
- ✅ Expertise capsule abstraction
- ✅ Basic outcome tracking via `CodingMemory` and `LearnedRecipe`
- ✅ Category-based retrieval system
- ✅ Document reference linking

### Expertise Application Loop (Weak)
- ⚠️ KB selection is keyword-based, not adaptive
- ⚠️ No post-generation validation against KB rules
- ⚠️ Outcomes tracked but not linked back to KB effectiveness
- ⚠️ Expertise injection is prompt-dependent, not enforced
- ⚠️ No visible attribution of which KB rules were applied

## The Four Critical Gaps

### 1. Dependable Selection Across Different Tasks

**Current State:**
- Basic keyword matching in `selectExpertiseKnowledgeBaseCandidates`
- Looks for terms like "parser", "test", "guide", "pattern"
- No task-specific ranking or confidence scoring
- No learning from historical success

**What's Missing:**
- Task classifier that maps task description → KB candidates with confidence scores
- Historical success tracking: "when task was X and KB was Y, outcome was Z"
- Automatic KB expansion when initial hits are sparse
- Multi-KB blending when expertise overlaps (e.g., architecture + testing)

**Implementation Priority: High**

### 2. Strong Enforcement of Rules in Generated Output

**Current State:**
- Expertise injected via `selectedSpaceExpertContext` and `ExpertiseCapsule`
- No post-generation validation
- No automatic retry when output violates known patterns
- Validation exists for script grammar but not code quality/adherence

**What's Missing:**
- Post-generation validator that checks code against KB rules
- Automatic revision loop: if violation detected, re-prompt with explicit correction
- Quality metrics: compliance score, pattern adherence percentage
- Explicit grounding citations in generated code

**Implementation Priority: Critical**

### 3. Clear Feedback Loop for System Improvement

**Current State:**
- `SummarizeOutcomeFacts` and `LearnedRecipe` track some outcomes
- Outcomes logged but not linked back to which KB was used
- No mechanism to promote successful KB/rule combinations
- No deprecation of ineffective guidance

**What's Missing:**
- Outcome-to-KB linkage: track which KB, which rules, success/failure
- KB effectiveness scoring: success rate per KB per task type
- Automatic KB pruning: demote rules that consistently produce poor results
- User feedback capture: thumbs up/down that updates KB weights
- Periodic KB refresh: re-rank based on real-world usage

**Implementation Priority: Medium**

### 4. Automatic Product Experience

**Current State:**
- Expertise activated through explicit space selection or prompt injection
- Requires user to know which KB to select
- No visible "this code follows X pattern from Y KB" annotations
- User can't easily see what expertise was applied

**What's Missing:**
- Zero-config mode: automatic KB selection without user intervention
- Inline annotations: `// Pattern: error-handling-v2 (from Engineering KB)`
- Visible expertise dashboard: show which KBs contributed, confidence scores
- Explanation UI: "I used X pattern because task mentioned Y and Z"
- One-click refinement: "apply stricter adherence" button

**Implementation Priority: Medium**

## Detailed Implementation Plan

### Phase 1: Enforcement & Validation (Critical Path)

**Goal:** Make expertise matter to the output, not just the prompt

**Tasks:**

1. **Post-generation pattern validator**
   - Location: `lib/sop/ai/agent/codingworkflow/validator.go` (new)
   - Validate common patterns: error handling, test structure, naming conventions
   - Return compliance report with violations and suggestions
   - Integration point: after code generation, before presentation

2. **Automatic revision loop**
   - Location: `lib/sop/ai/agent/codingworkflow/orchestrator.go`
   - If validator detects violations, trigger revision with explicit correction prompt
   - Limit to 1-2 revision attempts to avoid loops
   - Track revision attempts in `ExecutionResult`

3. **Grounding citations**
   - Location: code generation output formatter
   - Add inline comments: `// Pattern: [pattern-name] from [kb-name]`
   - Track which rules were consulted in `ExpertiseCapsule`
   - Surface in UI alongside generated code

**Success Metrics:**
- 70%+ of generated code passes pattern validation without revision
- Clear attribution visible in at least 80% of generated code
- Revision loop reduces violations by 50%+

**Timeline:** 3-4 weeks

---

### Phase 2: Selection Intelligence (High Priority)

**Goal:** Automatically pick the right KB for the task

**Tasks:**

1. **Task classification system**
   - Location: `lib/sop/ai/agent/codingworkflow/task_classifier.go` (new)
   - Classify tasks: implementation, debugging, testing, refactoring, documentation
   - Extract signals: file type, symbols, task description keywords
   - Return ranked KB candidates with confidence scores

2. **Historical success tracking**
   - Location: `lib/sop/ai/memory/kb_effectiveness.go` (new)
   - Schema: `{task_type, kb_name, success_count, failure_count, avg_quality}`
   - Update on every outcome via `CodingMemory`
   - Expose query API: getBestKBsForTask(taskType) → ranked list

3. **Automatic KB expansion**
   - Location: `lib/sop/ai/agent/copilot.go` (update `selectExpertiseKnowledgeBaseCandidates`)
   - If initial KB retrieval yields < 3 hits, expand to related KBs
   - Use task classifier + historical data to pick fallbacks
   - Log expansion decisions for observability

4. **Multi-KB blending**
   - Location: `lib/sop/ai/agent/codingworkflow/orchestrator.go`
   - When multiple relevant KBs exist, retrieve from top 2-3
   - Merge expertise capsules with priority weighting
   - Avoid conflicting guidance via simple deduplication

**Success Metrics:**
- Task classifier achieves 85%+ correct KB selection
- Historical tracking shows improving success rates over time
- KB expansion reduces "no relevant hits" by 60%+

**Timeline:** 4-5 weeks

---

### Phase 3: Feedback & Learning (Medium Priority)

**Goal:** System improves over time based on real outcomes

**Tasks:**

1. **Outcome-to-KB linkage**
   - Location: `lib/sop/ai/agent/codingworkflow/orchestrator.go`
   - Extend `CodingMemory` with: `KBsUsed []string`, `RulesApplied []string`
   - Track in every execution result
   - Store in STM and LTM for retrieval

2. **KB effectiveness scoring**
   - Location: `lib/sop/ai/memory/kb_effectiveness.go`
   - Compute scores: `success_rate = success_count / (success_count + failure_count)`
   - Weight by recency: recent outcomes count more
   - Expose in Data Manager UI as "KB Quality Metrics"

3. **User feedback capture**
   - Location: frontend UI + backend handler
   - Add thumbs up/down on generated code
   - Link feedback to KB and rules used
   - Update effectiveness scores based on feedback

4. **Automatic KB pruning**
   - Location: background job or admin tool
   - Flag rules with success_rate < 30% over 10+ uses
   - Demote or hide ineffective rules
   - Notify admins via log or dashboard

**Success Metrics:**
- 90%+ of outcomes linked to specific KBs and rules
- Effectiveness scores correlate with user feedback
- System automatically identifies and flags poor-performing rules

**Timeline:** 3-4 weeks

---

### Phase 4: User Experience (Medium Priority)

**Goal:** Make expertise visible and controllable

**Tasks:**

1. **Zero-config automatic mode**
   - Location: `lib/sop/ai/agent/copilot.go`
   - Default behavior: auto-select KB based on task classifier + history
   - Explicit selection still overrides auto-mode
   - Log auto-selection decisions

2. **Inline code annotations**
   - Location: code generation formatter
   - Format: `// Pattern: error-handling-v2 (Engineering KB)`
   - Make annotations optional/toggleable
   - Include in UI display

3. **Expertise dashboard UI**
   - Location: frontend Data Manager
   - Show: KBs consulted, rules applied, confidence scores
   - Display per-session and per-task
   - Link to source KB/category for drill-down

4. **Explanation UI**
   - Location: chat response formatter
   - Add section: "How I used expertise: I applied [pattern] because [reason]"
   - Make it concise and user-friendly
   - Include links to relevant KB entries

5. **Refinement controls**
   - Location: UI action buttons
   - "Apply stricter adherence to KB rules" → re-generate with higher weight
   - "Use different expertise" → suggest alternative KBs
   - "Explain reasoning" → show why KB was selected

**Success Metrics:**
- Users can see which KB was used in 100% of generations
- Refinement controls lead to measurably better outcomes
- Auto-mode selection matches or beats manual selection

**Timeline:** 4-5 weeks

---

## Technical Touchpoints

### Key Files to Modify

**Orchestrator & workflow:**
- `lib/sop/ai/agent/codingworkflow/orchestrator.go` - main workflow, add validation loop
- `lib/sop/ai/agent/codingworkflow/orchestrator_test.go` - tests

**Expertise retrieval:**
- `lib/sop/ai/agent/copilot.go` - KB selection logic
- `lib/sop/ai/agent/copilot_selected_space.go` - expert context building

**New components:**
- `lib/sop/ai/agent/codingworkflow/validator.go` - post-generation validation
- `lib/sop/ai/agent/codingworkflow/task_classifier.go` - task classification
- `lib/sop/ai/memory/kb_effectiveness.go` - effectiveness tracking

**Memory & outcome tracking:**
- `lib/sop/ai/memory/memory_unit.go` - extend episode logging
- `lib/sop/ai/outcome_summary.go` - link outcomes to KB usage

**Frontend (if applicable):**
- Expertise dashboard components
- Feedback capture UI
- Refinement control buttons

### Data Schema Extensions

**CodingMemory additions:**
```go
type CodingMemory struct {
    // ... existing fields ...
    KBsUsed         []string          // which KBs were consulted
    RulesApplied    []string          // which specific rules/patterns
    ComplianceScore float64           // 0.0-1.0, how well output matched KB
    RevisionCount   int               // how many validation revisions
}
```

**New KB effectiveness schema:**
```go
type KBEffectiveness struct {
    KBID           string
    TaskType       string
    SuccessCount   int
    FailureCount   int
    AvgCompliance  float64
    LastUpdated    time.Time
}
```

## Success Criteria

### Phase 1 (Enforcement):
- [ ] Post-generation validator operational for error handling, naming, test structure
- [ ] Automatic revision loop reduces violations by 50%+
- [ ] Inline citations visible in 80%+ of generated code

### Phase 2 (Selection):
- [ ] Task classifier achieves 85%+ correct KB selection vs. manual baseline
- [ ] Historical tracking shows improving success rates over 2+ weeks
- [ ] KB expansion reduces "no relevant hits" by 60%+

### Phase 3 (Feedback):
- [ ] 90%+ of outcomes linked to specific KBs and rules
- [ ] Effectiveness scores correlate with user feedback (r > 0.7)
- [ ] System flags and demotes poor-performing rules automatically

### Phase 4 (UX):
- [ ] Users can see which KB was used in 100% of generations
- [ ] Auto-mode selection matches or beats manual selection
- [ ] Refinement controls measurably improve outcomes

## Value Justification

**For $5/month to be compelling:**
- Users must see fewer prompt iterations (measurable: avg iterations per task)
- Code quality must be noticeably better (measurable: fewer bugs, better adherence)
- Expertise must feel automatic, not manual (measurable: auto vs. manual KB selection rate)

**This plan directly addresses:**
- Fewer iterations → automatic validation & revision (Phase 1)
- Better quality → enforcement & grounding (Phase 1 & 2)
- Automatic feel → zero-config mode & UX (Phase 4)

## Risk Mitigation

**Risk: Validation too strict, frustrates users**
- Mitigation: Start with warnings, not hard failures; add user override

**Risk: Historical data insufficient for good predictions**
- Mitigation: Bootstrap with curated KB-to-task mappings; improve over time

**Risk: Inline annotations clutter code**
- Mitigation: Make them toggleable; use comment format that's easy to remove

**Risk: Revision loops slow generation**
- Mitigation: Limit to 1-2 revisions; make async where possible

## Next Actions

1. Review this plan with stakeholders
2. Prioritize Phase 1 tasks for immediate implementation
3. Create detailed task breakdown for first 2-week sprint
4. Set up observability for KB effectiveness metrics
5. Define success metrics and data collection strategy

## References

- Current KB implementation: `lib/sop/ai/memory/knowledge_base.go`
- Expertise retrieval: `lib/sop/ai/agent/copilot.go:1822-1900`
- Outcome tracking: `lib/sop/ai/outcome_summary.go`
- Coding workflow: `lib/sop/ai/agent/codingworkflow/orchestrator.go`
