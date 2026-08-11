# Domain Authoring Guide

This guide explains how an application domain becomes a first-class participant in the SOP agent system. The goal is to help developers plug in a new domain so AI Studio can adopt it and work effectively against that domain's workflows, tools, and data model.

## 1. What “domain adoption” means

A domain is not just a capability or a set of tools. A domain is the combination of:

- the user-facing workflow the agent should support
- the concepts and terminology the agent should understand
- the tools or operations that act on those concepts
- the guardrails and verification expectations that apply to that domain
- the guidance that tells the agent how to behave when working in that domain

When a domain is adopted well, the agent can:

- recognize when the request belongs to that domain
- route to the right capability or workflow
- speak the right vocabulary
- choose the appropriate tools
- preserve the safety boundaries of that domain
- validate changes with the right evidence

## 2. The domain adaptation model

From the system’s perspective, domain adaptation happens through four layers:

1. Core agent loop
   - Ask remains the controller.
   - It handles routing, phase progression, state, and user interaction.

2. Capability layer
   - A capability exposes the domain-specific behavior to the loop.
   - It may expose tools, context, instructions, and structured guidance.

3. Guidance layer
   - The capability or domain can steer the agent toward the right operating pattern.
   - This includes role, principles, tool selection, guardrails, and verification expectations.

4. Domain-specific tools and workflows
   - The real behavior comes from the tools and execution patterns the domain exposes.
   - This is where the domain becomes actionable.

That means a domain is adopted when the agent can understand the domain and the domain can speak back through the shared capability contract.

## 3. What a developer needs to provide

To plugin a new application domain, the developer should typically provide the following integration surfaces:

### 3.0 Core integration surfaces

A domain author is really wiring into three shared contracts:

1. The capability contract
   - the capability is the domain's entry point into Ask
   - it handles classification, context, instructions, and orchestration hooks

2. The tools API
   - the domain exposes its actions through tool definitions
   - this is how the agent can read, write, validate, or orchestrate domain operations

3. The structured guidance contract
   - the domain contributes behavioral policy through guidance profiles
   - this tells the agent how to behave in that domain rather than relying only on generic prose

If these three pieces are present, the domain can participate in the shared agent loop in a way that feels native rather than bolted on.

### 3.1 Domain identity

Define the domain's identity in the routing model:

- a domain name
- a canonical capability ID
- a human-readable capability name
- domain-specific classification signals

This allows the agent to recognize requests that belong to the domain.

### 3.2 Capability implementation

Implement a capability that satisfies the shared capability contract:

- classify requests for the domain
- build context for the agent
- expose tools relevant to the domain
- add build instructions when needed
- optionally provide structured guidance
- participate in the same Ask-driven lifecycle as other capabilities

This is the core integration point. A domain is not a separate runtime; it is a participant in the shared capability system.

### 3.3 Guidance profile

Provide a guidance profile that teaches the agent how to work in that domain through the structured guidance contract.

The guidance should cover:

- role: what the agent should be when acting in this domain
- principles: what matters most in this domain
- guardrails: what should not be done casually
- tool selection: what tool family should be preferred
- verification: what evidence is expected before concluding work

For simple domains, the capability may keep this minimal. For advanced or high-risk domains, the guidance should be stronger. The guidance contract is where the domain's operating philosophy becomes visible to the model, including role, principles, guardrails, tool selection, and verification expectations.

### 3.4 Domain tools and operations

Expose the actual operations the domain needs through the tools API:

- read operations
- write or mutation operations
- validation operations
- workflow operations
- optional approval gates where needed

The tool API should reflect the domain model directly. If the domain has concepts like stores, spaces, entities, workflows, or approvals, the tools should be shaped around those concepts rather than around generic file or shell actions alone.

A good domain tool surface does three things:

- makes the domain actionable
- gives the agent the right level of control
- preserves domain-specific safety boundaries

### 3.5 Domain-specific workflow semantics

If the domain has a meaningful workflow, define it in the same participant pattern:

- the agent should not take over the whole workflow in a controller-style loop
- the capability should contribute behavior and context
- Ask remains the orchestrator
- the workflow should be expressed as staged, resumable phases when appropriate

## 4. How phases interact with the domain, tools, and capabilities

The phase system and the domain system are meant to work together, not compete.

### 4.1 Phase behavior provides the workflow skeleton

Phases such as intake, investigation, planning, execution, review, and delivery provide the general workflow skeleton. They answer questions like:

- what should happen first?
- what evidence should be gathered before acting?
- what should be validated before concluding?

These phases are domain-agnostic. They provide structure without dictating the domain-specific details.

### 4.2 Capabilities provide the domain voice

A domain capability adds the domain-specific voice inside that workflow skeleton. It answers:

- what does this domain care about?
- what terminology should be used?
- what risks or constraints matter here?
- what tool family should be preferred?

In other words, phases define the sequence, while the capability defines the domain behavior inside that sequence.

### 4.3 Tools make the domain actionable within each phase

The tools API is what turns that guidance into action. A domain can expose different tools for different phases:

- investigation tools for evidence gathering
- planning tools for structure and design
- execution tools for mutations or operations
- review tools for validation

This means the same domain can behave differently across phases while still staying within one coherent capability model.

### 4.4 The interaction model

A good domain author should think in this way:

- the phase decides the workflow step
- the capability decides how the domain should behave in that step
- the tools decide what the agent can actually do in that step
- the guidance decides how the agent should reason about the step

That is the intended interplay: phases coordinate, capabilities specialize, tools act, and guidance steers.

## 5. The recommended authoring pattern

For a new domain, the recommended progression is:

1. Start with a simple capability that can classify and expose a few relevant tools.
2. Add domain guidance only as needed.
3. Add stronger guidance for high-impact or high-risk operations.
4. Refine with domain-specific validation and workflow semantics over time.

The simplest version can be almost empty in terms of guidance, and still work because the general phase guidance and the general capability contract already provide a baseline. The richer version adds domain-specific steering where it matters.

## 6. How this makes AI Studio “adopt” the domain

The agent becomes adopted to the domain when the system can answer three questions well:

- What is this request about?
- What capability should handle it?
- How should the agent act when working in this domain?

If those three questions are answered well, the agent will feel like it truly understands the application domain rather than just executing generic instructions.

## 7. What good domain adaptation looks like

A well-adapted domain typically has:

- clear routing signals
- a coherent capability contract
- role-based guidance that reflects the domain's priorities
- safety rules appropriate to its data or operations
- tool surfaces that match the real workflow
- validation expectations that fit the domain

## 8. Practical checklist for a new domain

When authoring a domain, ask:

- What user intents should map to this domain?
- What concepts and terminology should the agent understand?
- What tools should be exposed for this domain?
- What guardrails are essential?
- What evidence should be required before acting?
- What behavior should be defaulted, and what should be explicitly guided?

## 9. Design principle

The important design principle is this:

A domain should be integrated as a participant in the shared agent loop, not as a separate system with its own controller logic.

That preserves the architecture's main strength: one orchestrator, many adaptable domains.
