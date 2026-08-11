# Architecture & Design Prompt Hardening Plan

**Last Updated**: 2026-08-10
**Status**: Proposed
**Goal**: Make the agent behave more like a strong architect/designer for feature, module, API, and system-design requests without over-constraining it with rigid TDD or tool mandates.

## Why this matters

The current prompt stack already does a good job of grounding work in the repository, encouraging the right domain tools, and consulting Team Practice from the Core Playbook. The remaining gap is that design-only requests can still jump too quickly into implementation details instead of producing a structured design brief with trade-offs, interfaces, constraints, and risks.

## Primary objective

Make the planning phase explicitly produce a design-oriented artifact for architecture/design requests before implementation is proposed.

## Proposed plan

### 1. Strengthen the planning contract for design requests

For requests that are primarily about design, planning should require the model to produce:

- a short problem framing statement
- scope and success criteria
- constraints and non-goals
- relevant existing patterns or system boundaries
- candidate options or architectural approaches
- the recommended approach with trade-offs
- key risks, assumptions, and validation ideas

This should be framed as a required design brief, not as optional commentary.

### 2. Keep Team Practice and Core Playbook lookup explicit

The model should continue to consult Team Practice from the Core Playbook before finalizing architecture or implementation choices. This should remain a strong recommendation for design work and not be diluted by the newer guidance around tests or flexibility.

### 3. Make tool-selection expectations visible but not overly rigid

The model should continue to use the right tool family for the domain and phase:

- coding/design work: repo inspection, AST/tree-sitter, structural analysis
- stores/domain data: schema/relationship/data inspection tools before scripting
- testing/review: relevant test and validation tools before proposing fixes

The guidance should emphasize “use the appropriate tool family” rather than forcing a single tool path.

### 4. Add a design-oriented artifact shape

The planning artifact should support a structured design section such as:

- design_summary
- problem_statement
- constraints
- non_goals
- options
- recommended_approach
- trade_offs
- risks
- validation_plan

This makes the design output easier to inspect, review, and refine.

### 5. Add regression coverage and evaluation prompts

Add prompt and behavior tests to ensure:

- planning guidance still mentions design trade-offs and architecture review
- Team Practice lookup remains part of planning guidance
- design requests are nudged toward structured design artifacts instead of immediate implementation plans

Use a small set of sample prompts such as:

- “Design a new feature for the platform”
- “Design an API for this capability”
- “Design a module boundary for this workflow”

### 6. Validate with human review

Review a handful of real design prompts and check whether the output includes:

- clear framing
- trade-offs
- boundaries and interfaces
- risks and assumptions
- enough grounding in repo/domain context

## Acceptance criteria

The work is considered successful when the agent, for design-oriented requests, consistently:

- gathers the right context before proposing a design
- references relevant repo/domain evidence
- consults Team Practice from the Core Playbook when appropriate
- produces a structured design brief with trade-offs and risks
- avoids jumping straight to implementation details without first framing the design

## Suggested rollout

1. Update planning prompts and phase instructions.
2. Add a structured design artifact field to planning artifacts.
3. Add regression tests around planning guidance.
4. Validate with sample design prompts and refine wording.
5. Document the expected design output shape in the planning guide.
