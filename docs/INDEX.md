# Documentation Index

This index separates current contracts from proposals and historical implementation notes. Code and tests are authoritative when an implementation detail has changed.

## Start Here

| Topic | Document |
|---|---|
| Repository setup, boundaries, and contributor workflow | [DEVELOPMENT_GUIDE.md](DEVELOPMENT_GUIDE.md) |
| Product and runtime overview | [AI_STUDIO.md](AI_STUDIO.md) |
| Current local coding copilot integration | [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md) |
| Add or adapt a coding-agent backend | [IDE_COPILOT_INTEGRATION_GUIDE.md](IDE_COPILOT_INTEGRATION_GUIDE.md) |
| Backend-neutral delegated worker contract | [IDE_COPILOT_BACKEND_SDK.md](IDE_COPILOT_BACKEND_SDK.md) |
| LLM provider pool selection and fallback | [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md#northstar-ai-provider-pool-and-preferred-llm) |
| Unified Ask/ReAct architecture | [ASK_V2_ARCHITECTURE.md](ASK_V2_ARCHITECTURE.md) |
| Workflow and participant-pattern guidance | [WORKFLOWS.md](WORKFLOWS.md) |

## Current Architecture and Contracts

| Area | Canonical document |
|---|---|
| Ask pipeline, phases, provider loops, cognition | [ASK_V2_ARCHITECTURE.md](ASK_V2_ARCHITECTURE.md) |
| Ask implementation status | [ASK_V2_IMPLEMENTATION_SUMMARY.md](ASK_V2_IMPLEMENTATION_SUMMARY.md) |
| Capability/domain authoring | [DOMAIN_AUTHORING_GUIDE.md](DOMAIN_AUTHORING_GUIDE.md), [STRUCTURED_GUIDANCE_MODEL.md](STRUCTURED_GUIDANCE_MODEL.md) |
| Workflow ownership and participant pattern | [WORKFLOWS.md](WORKFLOWS.md) |
| Phase usage | [PHASE_PLAN_GUIDE.md](PHASE_PLAN_GUIDE.md) |
| Provider-owned ReAct loops | [PROVIDER_OWNED_REACT_LOOPS_ARCHITECTURE.md](PROVIDER_OWNED_REACT_LOOPS_ARCHITECTURE.md) |
| Retry, escalation, and recovery state | [RETRY_ORCHESTRATION_STATE.md](RETRY_ORCHESTRATION_STATE.md) |
| Delegated IDE worker interface and results | [IDE_COPILOT_BACKEND_SDK.md](IDE_COPILOT_BACKEND_SDK.md) |
| Adding a coding-agent integration | [IDE_COPILOT_INTEGRATION_GUIDE.md](IDE_COPILOT_INTEGRATION_GUIDE.md) |
| Provider-pool order, retry, and outcome telemetry | [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md#northstar-ai-provider-pool-and-preferred-llm) |

## AI Studio and Platform Features

| Area | Documents |
|---|---|
| Daemon mode and floating Data Manager | [AI_STUDIO.md](AI_STUDIO.md) |
| Authentication and secure input | [AUTHENTICATION.md](AUTHENTICATION.md), [SECURE_INPUT_HANDLING.md](SECURE_INPUT_HANDLING.md) |
| Approvals and entitlements | [APPROVALS.md](APPROVALS.md), [ENTITLEMENT_ARCHITECTURE.md](ENTITLEMENT_ARCHITECTURE.md) |
| Spaces, documents, and expertise | [SPACES.md](SPACES.md), [STREAMING_DOCUMENT_STORAGE.md](STREAMING_DOCUMENT_STORAGE.md), [KB_QUALITY.md](KB_QUALITY.md) |
| Memory and workstream continuity | [LONG_TERM_MEMORY.md](LONG_TERM_MEMORY.md), [STM_WORKSTREAM_STACK_DESIGN.md](STM_WORKSTREAM_STACK_DESIGN.md), [MANUAL_RESUME_FEATURE_GUIDE.md](MANUAL_RESUME_FEATURE_GUIDE.md), [STARTUP_REHYDRATION_IMPLEMENTATION.md](STARTUP_REHYDRATION_IMPLEMENTATION.md), [VIRTUAL_MEMORY_RECALL_PLAN.md](VIRTUAL_MEMORY_RECALL_PLAN.md) |
| Undo and restore | [WORKFLOW_UNDO_SYSTEM.md](WORKFLOW_UNDO_SYSTEM.md) |
| Federation and fleet learning | [FEDERATION.md](FEDERATION.md), [FLEET_LEARNING.md](FLEET_LEARNING.md), [FEDERATED_CONTENT_STRATEGY.md](FEDERATED_CONTENT_STRATEGY.md) |
| Payments and private downloads | [PAYMENTS.md](PAYMENTS.md), [CLOUDFLARE_PRIVATE_DOWNLOADS.md](CLOUDFLARE_PRIVATE_DOWNLOADS.md) |
| Operations and observability | [OBSERVABILITY_PLAN.md](OBSERVABILITY_PLAN.md), [OBSERVABILITY_KB_QUALITY.md](OBSERVABILITY_KB_QUALITY.md), [PTY_WORKER_ISOLATION.md](PTY_WORKER_ISOLATION.md) |
| Large tool outputs | [FILE_BACKED_TOOL_RESULTS.md](FILE_BACKED_TOOL_RESULTS.md) |

## Coding and Workflow References

- [CLI_CODING_WORKFLOW_IMPLEMENTATION.md](CLI_CODING_WORKFLOW_IMPLEMENTATION.md): current objective-driven coding runtime and inventory concepts.
- [PHASE_SYSTEM_ARCHITECTURE.md](PHASE_SYSTEM_ARCHITECTURE.md): phase model and domain participation.
- [PHASE_PLAN_IMPLEMENTATION_DESIGN.md](PHASE_PLAN_IMPLEMENTATION_DESIGN.md): implementation-oriented phase-plan reference.
- [WORK_INVENTORY_GENERALIZATION.md](WORK_INVENTORY_GENERALIZATION.md): reusable artifact inventory model.
- [MANUAL_RESUME_FEATURE_GUIDE.md](MANUAL_RESUME_FEATURE_GUIDE.md): pause and resume behavior.
- [STARTUP_REHYDRATION_IMPLEMENTATION.md](STARTUP_REHYDRATION_IMPLEMENTATION.md): startup restoration of workstream state.
- [SHELL_SESSION_PERSISTENCE.md](SHELL_SESSION_PERSISTENCE.md): persistent shell behavior.
- [DUAL_PTY_PARALLEL_EXECUTION.md](DUAL_PTY_PARALLEL_EXECUTION.md): parallel command execution.
- [TERMINAL_LAUNCH_FEATURE.md](TERMINAL_LAUNCH_FEATURE.md): terminal launch modes.

## Cline (Current) and Continue (Retired)

- [IDE_COPILOT_INTEGRATION_GUIDE.md](IDE_COPILOT_INTEGRATION_GUIDE.md) is the developer walkthrough for adding another backend, using current Cline and retained Continue patterns as examples.
- [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md) is the current Cline SDK, MCP, packaging, and validation reference.
- [IDE_COPILOT_BACKEND_SDK.md](IDE_COPILOT_BACKEND_SDK.md) is the backend-neutral interface contract.
- [CONTINUE_REFERENCE.md](CONTINUE_REFERENCE.md) consolidates Continue's retained compatibility lessons. The two detailed legacy sources are the [transport/API spec](CONTINUE_BRIDGE_API_SPEC.md) and [client integration notes](IDE_COPILOT_CLIENT_INTEGRATION.md); neither is a shipping setup guide.

## Knowledge, Quality, and Provider Guides

- [KB_QUALITY.md](KB_QUALITY.md) and [KB_QUALITY_DASHBOARD.md](KB_QUALITY_DASHBOARD.md): knowledge quality behavior and dashboard.
- [GO_BEST_PRACTICES_KB.md](GO_BEST_PRACTICES_KB.md): Go practice content.
- [FALLBACK_PROVIDER_SETUP.md](FALLBACK_PROVIDER_SETUP.md): fallback provider configuration.
- [TOKEN_CONSUMPTION_ANALYSIS.md](TOKEN_CONSUMPTION_ANALYSIS.md) and [TOKEN_CONSUMPTION_SUMMARY.md](TOKEN_CONSUMPTION_SUMMARY.md): cost and context analyses; treat recommendations as dated findings.
- [LLM_ENHANCEMENT_PLAN.md](LLM_ENHANCEMENT_PLAN.md): proposed provider improvements, not a shipped contract.
- [COPILOT_ENGINEERING_JUDGMENT_DESIGN.md](COPILOT_ENGINEERING_JUDGMENT_DESIGN.md), [COPILOT_JUDGMENT_PHASE_A_TRACKER.md](COPILOT_JUDGMENT_PHASE_A_TRACKER.md): judgment-layer design and dated tracker.
- [V2_ASK_LLM_ORCHESTRATOR_GAPS.md](V2_ASK_LLM_ORCHESTRATOR_GAPS.md), [V2_ASK_REACT_LOOP_GAP_ANALYSIS.md](V2_ASK_REACT_LOOP_GAP_ANALYSIS.md): gap analyses; compare claims with current source before treating them as open.

## Proposals and Assessments

These documents describe work under consideration. A proposal is not evidence that the feature shipped; verify against code and tests before treating it as current behavior.

- [ARCHITECTURE_DESIGN_PROMPT_PLAN.md](ARCHITECTURE_DESIGN_PROMPT_PLAN.md)
- [CAPABILITY_ARCHITECTURE_ASSESSMENT.md](CAPABILITY_ARCHITECTURE_ASSESSMENT.md)
- [CAPABILITY_GAPS_IMPLEMENTATION_PLAN.md](CAPABILITY_GAPS_IMPLEMENTATION_PLAN.md)
- [CAPABILITY_PLUGIN_ARCHITECTURE_ASSESSMENT.md](CAPABILITY_PLUGIN_ARCHITECTURE_ASSESSMENT.md)
- [CONTEXT_GENERATION_AUDIT.md](CONTEXT_GENERATION_AUDIT.md), [CONTEXT_GENERATION_REFORM.md](CONTEXT_GENERATION_REFORM.md), [CONTEXT_GENERATION_TASKS.md](CONTEXT_GENERATION_TASKS.md)
- [DESKTOP_CODING_WORKFLOW_SEPARATION_REASSESSMENT.md](DESKTOP_CODING_WORKFLOW_SEPARATION_REASSESSMENT.md)
- [DOMAIN_AGNOSTIC_PHASE_SYSTEM.md](DOMAIN_AGNOSTIC_PHASE_SYSTEM.md)
- [HYBRID_CLASSIFICATION_REASSESSMENT.md](HYBRID_CLASSIFICATION_REASSESSMENT.md)
- [LOCAL_RAG_APP_STORE_CONCEPT.md](LOCAL_RAG_APP_STORE_CONCEPT.md)
- [MULTI_STEP_WORKFLOW_DESIGN.md](MULTI_STEP_WORKFLOW_DESIGN.md)
- [SOPBACKEND_HOST_FRAMEWORK_REFACTOR_PLAN.md](SOPBACKEND_HOST_FRAMEWORK_REFACTOR_PLAN.md)
- [STRUCTURED_PLANNER_EXECUTION_DESIGN.md](STRUCTURED_PLANNER_EXECUTION_DESIGN.md)
- [TEAM_PATTERNS_LTM_DESIGN.md](TEAM_PATTERNS_LTM_DESIGN.md), [TEAM_PATTERNS_VECTORIZATION_ASSESSMENT.md](TEAM_PATTERNS_VECTORIZATION_ASSESSMENT.md)
- [USER_INTERJECTION_QUEUE_DESIGN.md](USER_INTERJECTION_QUEUE_DESIGN.md)
- [V1_DIRECTION_AND_POST_V1_VISION.md](V1_DIRECTION_AND_POST_V1_VISION.md)

## Implementation and Historical Records

Dated reports and implementation summaries are evidence of work at a point in time, not live status pages. Use the canonical architecture and feature guides above for current behavior.

- [ASK_V2_VERIFICATION.md](ASK_V2_VERIFICATION.md): historical record of the original feature-flagged Ask V2 path.
- [AUTONOMOUS_TEST_FIXING_DEBUG_LOG.md](AUTONOMOUS_TEST_FIXING_DEBUG_LOG.md), [PHASE_DEBUGGING_QUICK_REF.md](PHASE_DEBUGGING_QUICK_REF.md), [PHASE_SYSTEM_STATUS_AUG_2026.md](PHASE_SYSTEM_STATUS_AUG_2026.md): debugging and dated phase history.
- [CHECKLIST_2026_08_30.md](CHECKLIST_2026_08_30.md), [RESUME_2026_08_30.md](RESUME_2026_08_30.md), [RESUME_2026_08_31.md](RESUME_2026_08_31.md): dated session notes; do not use as current task state.
- [PHASE_AWARE_CONTEXT_OPTIMIZATION.md](PHASE_AWARE_CONTEXT_OPTIMIZATION.md): historical implementation and validation record.
- [PHASE_TOOL_AVAILABILITY_ASSESSMENT.md](PHASE_TOOL_AVAILABILITY_ASSESSMENT.md), [PHASE_DOMAIN_OPERATION_TOOL_FILTERING.md](PHASE_DOMAIN_OPERATION_TOOL_FILTERING.md): tool-surface research and proposals.
- [REACT_LOOP_SYNC_GAP_ASSESSMENT.md](REACT_LOOP_SYNC_GAP_ASSESSMENT.md): resolved provider-loop alignment assessment.
- [TIER1_REACT_GAP_FIXES_IMPLEMENTATION.md](TIER1_REACT_GAP_FIXES_IMPLEMENTATION.md), [TIER2A_LLM_CONTROLLED_RETRY_IMPLEMENTATION.md](TIER2A_LLM_CONTROLLED_RETRY_IMPLEMENTATION.md), [TIER2B_PROGRESSIVE_ESCALATION_IMPLEMENTATION.md](TIER2B_PROGRESSIVE_ESCALATION_IMPLEMENTATION.md), [TIER3_ESCALATION_PHASE_SUCCESSION_IMPLEMENTATION.md](TIER3_ESCALATION_PHASE_SUCCESSION_IMPLEMENTATION.md): staged retry/escalation implementation history.
- [CRUD_TOOL_FILTERING_IMPLEMENTATION_SUMMARY.md](CRUD_TOOL_FILTERING_IMPLEMENTATION_SUMMARY.md), [DYNAMIC_PLANNER_IMPLEMENTATION.md](DYNAMIC_PLANNER_IMPLEMENTATION.md), [WORKSTREAM_PHASE_JUMPING_IMPLEMENTATION.md](WORKSTREAM_PHASE_JUMPING_IMPLEMENTATION.md): implementation records; check source before relying on old file locations.
- [TEAM_PATTERNS_IMPLEMENTATION_SUMMARY.md](TEAM_PATTERNS_IMPLEMENTATION_SUMMARY.md): dated integration snapshot; verify current behavior in source and tests.
- [TEST_MODE_FALLBACK_SYSTEM.md](TEST_MODE_FALLBACK_SYSTEM.md), [TEST_MODE_IMPLEMENTATION_STATUS.md](TEST_MODE_IMPLEMENTATION_STATUS.md), [TEST_MODE_REFACTORING_FINDINGS.md](TEST_MODE_REFACTORING_FINDINGS.md): test-mode design and historical findings.

## Business and Maintainer Material

- [MAINTAINER_GUIDANCE.md](MAINTAINER_GUIDANCE.md)
- [NORTHSTAR_SDK_PRICING.md](NORTHSTAR_SDK_PRICING.md)
- [NORTHSTAR_SDK_SALES_GUIDE.md](NORTHSTAR_SDK_SALES_GUIDE.md)
- [MARKETING_KB_QUALITY_UPDATES.md](MARKETING_KB_QUALITY_UPDATES.md)
- [model_catalog_federation.md](model_catalog_federation.md)

## Maintenance Rules

- Update the canonical guide when runtime behavior changes; add dated implementation reports only when they preserve useful test or decision history.
- Label proposals, compatibility references, and historical snapshots explicitly.
- Before adding a new document, search for an existing canonical guide to extend.
- Keep local Markdown links relative to the file and validate link targets when editing documentation.
