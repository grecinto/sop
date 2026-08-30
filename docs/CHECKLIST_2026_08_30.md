# Tomorrow's Checklist - Aug 30

## ☑️ FIRST THING: Validate Workaround (30 min)

```bash
cd /Volumes/T7/sop-cloud/lib/sop/cmd/sop-daemon
./sop-daemon
```

Then test: "fix failing tests" → "current"

**Look for in logs**:
- ✅ `tool:preview_patch` - SUCCESS! Workaround worked
- ❌ Still only investigation tools in iteration 1-3 - Workaround failed

## If Workaround Works ✅

1. Celebrate briefly 🎉
2. Add instrumentation logging (see RESUME doc section 2)
3. Collect data for 1 week
4. Follow CONTEXT_GENERATION_REFORM.md plan

## If Workaround Fails ❌

### Quick Fix Options:

**Option A: Increase Budget** (5 min)
```go
// File: lib/sop/ai/agent/copilot_prompt.go:~395
ComponentFocusedContext: 10000,  // Change from 2600
```

**Option B: Disable Trimming** (2 min)
```go
// File: lib/sop/ai/agent/copilot_prompt.go:~351
// Comment out trim line
```

**Option C: Phase-Specific Budgets** (10 min)
```go
// In promptBudgetProfile(), add:
if currentPhase == "execution" {
    profile.ComponentCharBudgets[ComponentFocusedContext] = 8000
}
```

Then rebuild: `go build` and retest

## Files to Reference

- **Resume Doc**: `docs/RESUME_2026_08_30.md` (full context)
- **Reform Plan**: `docs/CONTEXT_GENERATION_REFORM.md` (proper fix)
- **Audit**: `docs/CONTEXT_GENERATION_AUDIT.md` (current state)
- **Workaround Code**: `lib/sop/ai/agent/planner/phase_coding.go:318-330`
- **Budget Config**: `lib/sop/ai/agent/copilot_prompt.go:387-460`

## Status at End of 2026-08-29

- ✅ Path doubling fixed
- ✅ Query parsing fixed  
- ✅ Phase sync fixed
- ⚠️ Execution patch generation - workaround applied, untested
- 📋 Proper fix - documented, not started

## Key Metric

**Before workaround**: 0/3 execution iterations generated patches  
**Target**: ≥1/3 execution iterations generates patches

If we hit that target tomorrow, infrastructure is solid and we can focus on the proper context generation refactoring.
