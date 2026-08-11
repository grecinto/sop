# Structured Guidance Model for Developers

This document explains the guidance model used by the Ask pipeline and the capability system. It is meant for developers who are adding a new capability, a new domain, or a new phase behavior.

## 1. The core idea

The system uses layered guidance instead of hardcoding behavior inside one giant prompt.

The effective behavior comes from three layers:

1. Phase guidance
   - broad expectations for the current workflow phase such as investigation, planning, testing, execution, or review.
2. Capability guidance
   - default behavior for the capability that is currently active.
3. Domain-specific guidance
   - stronger or more specialized guidance for a specific domain such as desktop automation, stores, spaces, or coding.

This design keeps the system generic by default, while allowing specific capabilities to become more opinionated when they need to.

## 2. How the layers work together

The model is progressive:

- simple capabilities can stay quiet
- ordinary capabilities can provide a small profile
- advanced capabilities can provide rich, explicit guidance

In practice, the Ask pipeline composes the relevant layers into the effective prompt. The general phase guidance remains available as the baseline, and the capability can either:

- stay minimal and let the general guidance do most of the work, or
- add its own role, principles, guardrails, verification rules, or tool-selection hints.

## 3. The fallback rule for new capabilities

For a low- or medium-complexity capability, it is perfectly acceptable for the capability to return an empty or nearly empty guidance profile.

That means:

- the capability does not need to "speak up" unless it has special behavior
- the general phase guidance remains the primary driver
- the system stays simple and consistent for ordinary cases

For advanced capabilities, the capability should return explicit guidance so the model understands how to behave in that domain.

## 4. When to use explicit guidance

A capability should return explicit guidance when it needs to influence one or more of the following:

- its role in the workflow
- the safety or guardrail expectations
- how it should select tools
- what evidence or verification it should prefer
- how it should behave in a specific phase or task situation

If none of that is necessary, returning an empty profile is the right default.

## 5. Domain guidance can enhance or replace the generic behavior

A domain-specific capability can do two things:

- enhance the generic guidance by adding stronger rules, or
- replace the generic behavior by providing a more specific role or a different set of principles.

This is important because the system should remain general, but still be able to become more precise in a specialized domain.

The intended pattern is:

- start with a baseline profile
- add domain-specific rules on top of it
- override only the parts that need to differ

## 6. Suggested authoring pattern

For new capabilities, prefer the following progression:

1. Start with the general phase guidance.
2. If the capability needs no special behavior, return an empty profile.
3. If the capability needs a clear role or a few guardrails, use a light profile.
4. If the capability works in a high-risk or specialized domain, use a richer profile with explicit rules.

A typical implementation looks like this:

```go
base := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityLight, "Guide this capability safely")
base.Principles = []string{"Prefer simple, predictable steps"}

profile := capabilities.GuidanceProfile{
    Principles: []string{"Add a domain-specific rule here"},
}.ApplyDefaults(base)

return profile
```

This keeps the authoring model lightweight while still allowing richer behavior where it is justified.

## 7. Design goals

This model was designed to satisfy a few specific goals:

- keep the system simple by default
- avoid overloading every capability with repetitive prompts
- let the phase layer provide broad workflow behavior
- let capabilities add only the guidance they truly need
- allow domain-specific capabilities to be more precise without breaking the generic model

## 8. Mental model for developers

Think of the guidance system as a layered policy contract:

- phases define the workflow
- capabilities define their behavior
- domains refine the behavior further

The best default is usually: be quiet unless the capability truly needs to steer the model.

That is the intended balance between simplicity and expressiveness.
