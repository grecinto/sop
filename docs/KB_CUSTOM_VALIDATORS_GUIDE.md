# KB Custom Validators Guide

## Purpose

This guide explains how to add custom validation rules to the KB quality enforcement pipeline. The validator infrastructure lives in [lib/sop/ai/agent/codingworkflow/validator.go](../lib/sop/ai/agent/codingworkflow/validator.go) and is invoked by the orchestration flow before a patch is accepted.

## Where to extend

1. Open [lib/sop/ai/agent/codingworkflow/validator.go](../lib/sop/ai/agent/codingworkflow/validator.go)
2. Add a new validation method on the Validator type, such as `checkMyRule(content, filePath)`
3. Call it from `ValidatePatch()` alongside the existing rule checks
4. Add corresponding coverage in [lib/sop/ai/agent/codingworkflow/validator_test.go](../lib/sop/ai/agent/codingworkflow/validator_test.go)

## Recommended pattern

- Keep checks focused on a single rule or convention
- Return a `Violation` with a clear `RuleName`, `Severity`, and `Suggestion`
- Prefer `SeverityWarning` for advisory guidance and `SeverityError` for strong enforcement
- Use `LineHint` and `FilePath` so failures are actionable in the UI and logs

## Example

```go
func (v *Validator) checkMyRule(content, filePath string) []Violation {
    violations := make([]Violation, 0)
    if strings.Contains(content, "panic()") {
        violations = append(violations, Violation{
            RuleName:    "custom-no-panic",
            Severity:    SeverityWarning,
            Description: "Direct panic calls are discouraged",
            FilePath:    filePath,
            Suggestion:  "Return an error instead of panicking",
        })
    }
    return violations
}
```

Then wire it into `ValidatePatch()`:

```go
if v.config.EnableErrorHandling {
    rulesChecked = append(rulesChecked, "error-handling")
    violations = append(violations, v.checkErrorHandling(newContent, patch.FilePath)...)
}

rulesChecked = append(rulesChecked, "custom-rule")
violations = append(violations, v.checkMyRule(newContent, patch.FilePath)...)
```

## Configuration guidance

Use the validator config to keep new rules optional and easy to roll out:

- `EnableComments` for comment/style rules
- `StrictMode` to treat warnings as failures
- Feature flags in the runtime config when the rule should be staged

## Testing guidance

Add tests for:
- passing cases
- known violations
- strict-mode behavior
- severity filtering

## Production notes

- Start with warnings before making a rule hard-failing
- Keep the rule deterministic and easy to explain
- Capture the reason in the violation message so users can understand the feedback
