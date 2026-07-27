# Workflow Manager: Signal-Based Research & Event System

## Overview

The Workflow Manager supports **dynamic research injection** through a hybrid signal and event system. Research can be triggered automatically based on step context without hardcoding workflows.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│           WorkflowManager Execution Loop            │
├─────────────────────────────────────────────────────┤
│                                                     │
│  for each Step:                                     │
│    1. Emit BeforeExecution Event                    │
│       → Global/Step-specific event handlers         │
│       → Collect signals from handlers               │
│                                                     │
│    2. Call Step.GetSignals()                        │
│       → Step decides if research needed             │
│       → Returns step-specific signals               │
│                                                     │
│    3. Merge signals (deduplicate by type)           │
│       → Keep highest priority for each type         │
│                                                     │
│    4. Process signals                               │
│       → Function Call mode: Direct KB search        │
│       → Full Step mode: Ask/ReAct pipeline          │
│       → Store results in WorkflowState              │
│                                                     │
│    5. Execute Step                                  │
│       → Step accesses research via WorkflowState    │
│       → OnBeforePrompt() injects research context   │
│                                                     │
│    6. Emit AfterExecution Event                     │
│       → Post-step analysis, logging, etc.           │
│                                                     │
└─────────────────────────────────────────────────────┘
```

## Signal Types

### Research Signals
- `SignalResearchBestPractices` - Search KB/codebase for best practices
- `SignalResearchCodePatterns` - Find existing code patterns  
- `SignalResearchDependencies` - Analyze dependencies
- `SignalResearchArchitecture` - Study architecture patterns
- `SignalResearchTestCoverage` - Analyze test coverage

### Analysis Signals
- `SignalAnalyzeImpact` - Assess change impact
- `SignalAnalyzeComplexity` - Measure code complexity

### Validation Signals
- `SignalValidateDesign` - Validate design decisions
- `SignalValidateSecurity` - Security analysis

## Signal Execution Modes

### 1. Function Call Mode (Lightweight) ⚡

**Use for:** Quick KB searches, repo lookups, data augmentation

**Characteristics:**
- No Ask/ReAct loop
- Synchronous execution
- No user interaction
- Fast (< 100ms typically)

**Example:**
```go
signal := StepSignal{
    Type: SignalResearchBestPractices,
    Mode: SignalModeFunctionCall,
    Handler: func(ctx context.Context, infra *SharedInfrastructure, signal StepSignal) (map[string]interface{}, error) {
        // Direct KB search
        practices := searchKB("go best practices")
        return map[string]interface{}{
            "practices": practices,
        }, nil
    },
    Priority: SignalPriorityHigh,
}
```

### 2. Full Step Mode (Powerful) 🔄

**Use for:** Complex research requiring LLM reasoning, user interaction

**Characteristics:**
- Full Ask/ReAct pipeline
- Multi-turn conversation
- Tool calls allowed
- User interaction possible
- Slower (seconds to minutes)

**Example:**
```go
signal := StepSignal{
    Type: SignalAnalyzeComplexity,
    Mode: SignalModeFullStep,
    StepFactory: func(signal StepSignal) Step {
        return NewComplexityAnalysisStep(signal.Context)
    },
    Priority: SignalPriorityCritical,
}
```

## Event System

### Event Types

```go
type StepEventType string

const (
    StepEventBeforeExecution  // Before step starts
    StepEventAfterExecution   // After step completes
    StepEventBeforePrompt     // During prompt construction
    StepEventAfterTool        // After tool execution
)
```

### Event Handlers

Implement `StepEventHandler` interface:

```go
type StepEventHandler interface {
    // CanHandle determines if handler should process event
    CanHandle(event StepEvent) bool
    
    // HandleEvent processes event and returns signals
    HandleEvent(ctx context.Context, event StepEvent) ([]StepSignal, error)
}
```

### Built-in Handlers

#### 1. BestPracticesHandler

Auto-injects best practices for code generation steps.

```go
wm := NewWorkflowManager(orchestrator)
wm.RegisterEventHandler(&BestPracticesHandler{
    Languages: []string{"go", "python"},
})
```

**Triggers:** Steps with `code_generation` or `implementation` tags

**Signal:** `SignalResearchBestPractices` (Function Call mode)

#### 2. ComplexityAnalysisHandler

Triggers complexity analysis after high-complexity detection.

```go
wm.RegisterEventHandler(&ComplexityAnalysisHandler{
    Threshold: "high",
})
```

**Triggers:** When previous step sets `complexity: "high"` in result data

**Signal:** `SignalAnalyzeComplexity` (Full Step mode)

#### 3. SecurityValidationHandler

Injects security validation for sensitive operations.

```go
wm.RegisterEventHandler(&SecurityValidationHandler{
    SensitiveTags: []string{"authentication", "authorization", "payment"},
})
```

**Triggers:** Steps with sensitive tags

**Signal:** `SignalValidateSecurity` (Full Step mode)

#### 4. CodePatternHandler

Researches existing code patterns for implementation steps.

```go
wm.RegisterEventHandler(&CodePatternHandler{})
```

**Triggers:** Steps with `implementation` or `refactoring` tags

**Signal:** `SignalResearchCodePatterns` (Function Call mode)

## Usage Examples

### Example 1: Global Event Handler

```go
// Setup
wm := NewWorkflowManager(orchestrator)
wm.RegisterEventHandler(&BestPracticesHandler{
    Languages: []string{"go"},
})

// Create step
step := NewCodeGenerationStep()
step.SetMetadata(StepMetadata{
    ID:   "gen_code",
    Tags: []string{"code_generation"},
})

// Execute workflow
// → BestPracticesHandler triggers automatically
// → Research injected before code generation
// → Step accesses results via WorkflowState
```

### Example 2: Step-Specific Handler

```go
// Register handler only for specific step
wm.RegisterStepEventHandler("critical_step", &SecurityValidationHandler{
    SensitiveTags: []string{"authentication"},
})

// Only "critical_step" gets security validation
// Other steps unaffected
```

### Example 3: Step Emits Signals

```go
type CustomStep struct {
    BaseStep
}

func (s *CustomStep) GetSignals(ctx context.Context, ws *WorkflowState) ([]StepSignal, error) {
    // Dynamic decision based on workflow state
    if lastResult, ok := ws.GetLastStepResult(); ok {
        if complexity, ok := lastResult.Data["complexity"].(string); ok && complexity == "high" {
            return []StepSignal{{
                Type:     SignalResearchCodePatterns,
                Mode:     SignalModeFunctionCall,
                Priority: SignalPriorityHigh,
            }}, nil
        }
    }
    return nil, nil
}
```

### Example 4: Accessing Research Results

```go
func (s *CodeGenerationStep) OnBeforePrompt(ctx context.Context, query string, state capabilities.RoutingState) (string, error) {
    ws := s.GetWorkflowState()
    if ws == nil {
        return "", nil
    }
    
    // Access research findings
    if bestPractices, ok := ws.GetResearchData(SignalResearchBestPractices); ok {
        if practices, ok := bestPractices.(map[string]interface{}); ok {
            return fmt.Sprintf(`
## Best Practices
%v

Follow these best practices when generating code.
`, practices["summary"]), nil
        }
    }
    
    return "", nil
}
```

## WorkflowState

Cross-step data sharing:

```go
type WorkflowState struct {
    SharedData   map[string]interface{} // General cross-step data
    StepHistory  []StepResult           // Previous step results
    ResearchData map[string]interface{} // Research findings by signal type
    
    WorkflowID  string
    StartTime   time.Time
    CurrentStep int
}
```

### Methods

```go
// Store research data
ws.SetResearchData(SignalResearchBestPractices, data)

// Retrieve research data
if data, ok := ws.GetResearchData(SignalResearchBestPractices); ok {
    // Use data
}

// Access previous step results
if lastResult, ok := ws.GetLastStepResult(); ok {
    complexity := lastResult.Data["complexity"]
}
```

## Signal Merging

When multiple sources emit signals of the same type:

1. **Deduplication:** Only one signal per type
2. **Priority:** Keep highest priority signal
3. **Sources:** Event handlers + Step.GetSignals()

```go
// Example: Two sources emit SignalResearchBestPractices
eventSignals := []StepSignal{{
    Type:     SignalResearchBestPractices,
    Priority: SignalPriorityLow,
}}

stepSignals := []StepSignal{{
    Type:     SignalResearchBestPractices,
    Priority: SignalPriorityHigh, // ← This one wins
}}

// After merge: Only 1 signal with Priority=High
```

## Custom Signal Handlers

### Function Call Handler

```go
func customHandler(ctx context.Context, infra *SharedInfrastructure, signal StepSignal) (map[string]interface{}, error) {
    // Access SharedInfrastructure
    astOps := infra.NewASTOperations()
    repoOps := infra.NewRepoOperations()
    
    // Perform research
    results := doResearch(astOps, repoOps)
    
    // Return data for WorkflowState
    return map[string]interface{}{
        "findings": results,
        "summary":  "Research summary",
    }, nil
}

signal := StepSignal{
    Type:    SignalResearchCodePatterns,
    Mode:    SignalModeFunctionCall,
    Handler: customHandler,
}
```

### Full Step Handler

```go
signal := StepSignal{
    Type: SignalAnalyzeComplexity,
    Mode: SignalModeFullStep,
    StepFactory: func(signal StepSignal) Step {
        step := &ComplexityAnalysisStep{}
        step.SetMetadata(StepMetadata{
            ID:          "analyze_complexity",
            Name:        "Complexity Analysis",
            Description: "Analyze code complexity",
        })
        return step
    },
}
```

## Testing

### Test Event Handlers

```go
func TestCustomHandler(t *testing.T) {
    handler := &CustomHandler{}
    
    step := &BaseStep{}
    step.SetMetadata(StepMetadata{
        Tags: []string{"target_tag"},
    })
    
    event := StepEvent{
        Type:          StepEventBeforeExecution,
        Step:          step,
        WorkflowState: &WorkflowState{},
    }
    
    if !handler.CanHandle(event) {
        t.Error("Should handle event")
    }
    
    signals, err := handler.HandleEvent(context.Background(), event)
    if err != nil {
        t.Fatal(err)
    }
    
    if len(signals) != 1 {
        t.Errorf("Expected 1 signal, got %d", len(signals))
    }
}
```

### Test Signal Merging

```go
func TestSignalMerging(t *testing.T) {
    wm := NewWorkflowManager(orchestrator)
    
    signals1 := []StepSignal{{Type: SignalResearchBestPractices, Priority: SignalPriorityLow}}
    signals2 := []StepSignal{{Type: SignalResearchBestPractices, Priority: SignalPriorityHigh}}
    
    merged := wm.mergeSignals(signals1, signals2)
    
    if len(merged) != 1 {
        t.Errorf("Expected 1 merged signal, got %d", len(merged))
    }
    
    if merged[0].Priority != SignalPriorityHigh {
        t.Error("Should keep highest priority")
    }
}
```

## Best Practices

### 1. Event Handlers for Cross-Cutting Concerns

Use event handlers for:
- Logging/monitoring
- Security validation
- Standard research patterns
- Compliance checks

### 2. Step Signals for Step-Specific Logic

Use `GetSignals()` for:
- Dynamic research based on step state
- Conditional analysis
- Step-specific requirements

### 3. Function Call vs Full Step

**Function Call:**
- KB searches
- Simple lookups
- Data augmentation
- < 100ms execution

**Full Step:**
- Complex analysis requiring LLM
- User interaction needed
- Multi-turn conversation
- > 1 second execution

### 4. Priority Guidelines

- `SignalPriorityCritical`: Security, compliance, blocking issues
- `SignalPriorityHigh`: Best practices, architecture
- `SignalPriorityMedium`: Code patterns, complexity
- `SignalPriorityLow`: Nice-to-have research

### 5. Error Handling

Signal processing is **best-effort**:
- Failed signals don't fail the workflow
- Warnings logged but execution continues
- Steps can check if research data available

## Built-in Research Steps

The system includes 5 built-in research step implementations for Full Step mode:

### ComplexityAnalysisStep
**Factory:** `NewComplexityAnalysisStep(context)`

**Parameters:**
- `threshold` (string): "low", "medium", "high"
- `focus_areas` ([]string): Optional specific areas to analyze

**Analyzes:**
- Cyclomatic complexity (branches, loops)
- Cognitive complexity (human understanding)
- Dependency complexity (coupling)
- Data flow complexity (state management)

**Output:**
```go
{
    "cyclomatic_score": 45,
    "cognitive_score": 38,
    "complexity_level": "high",
    "hotspots": ["function xyz", "class ABC"],
    "recommendations": ["Extract method", "Simplify logic"]
}
```

### SecurityValidationStep
**Factory:** `NewSecurityValidationStep(context)`

**Parameters:**
- `sensitive_areas` ([]string): Areas to validate (auth, payment, data-access)

**Validates:**
- Input sanitization and validation
- Authentication and authorization
- Sensitive data exposure
- Injection vulnerabilities (SQL, XSS, command)
- Cryptographic practices

**Output:**
```go
{
    "vulnerabilities": [
        {"type": "SQL Injection", "severity": "high", "location": "user.go:45"}
    ],
    "recommendations": ["Use parameterized queries", "Validate input"],
    "security_score": 7.5
}
```

### ArchitectureResearchStep
**Factory:** `NewArchitectureResearchStep(context)`

**Parameters:**
- `focus` (string): Architecture aspect (patterns, scalability, layering)

**Researches:**
- Architecture patterns used
- Dependencies between modules
- Design patterns implementation
- Layering and separation of concerns
- Scalability considerations

**Output:**
```go
{
    "patterns": ["Repository", "Factory", "Strategy"],
    "layering": "3-tier (handler -> service -> repository)",
    "coupling_score": "medium",
    "recommendations": ["Consider dependency injection"]
}
```

### DependencyResearchStep
**Factory:** `NewDependencyResearchStep(context)`

**Parameters:**
- `language` (string): Programming language (go, python, javascript)
- `check_vulnerabilities` (bool): Enable security scanning

**Analyzes:**
- Direct dependencies
- Transitive dependencies
- Version constraints
- Security vulnerabilities (if enabled)
- Dependency bloat

**Output:**
```go
{
    "direct_deps": 12,
    "transitive_deps": 45,
    "vulnerabilities": [
        {"package": "lodash", "version": "4.17.15", "cve": "CVE-2020-8203"}
    ],
    "bloat_score": "low",
    "recommendations": ["Update lodash to 4.17.21"]
}
```

### TestCoverageResearchStep
**Factory:** `NewTestCoverageResearchStep(context)`

**Parameters:**
- `target_path` (string): Path to analyze (e.g., "./lib/sop")
- `include_integration` (bool): Include integration tests

**Analyzes:**
- Test files and coverage
- Missing test cases
- Edge case coverage
- Test quality (assertions, mocks)

**Output:**
```go
{
    "line_coverage": 78.5,
    "branch_coverage": 65.2,
    "missing_tests": ["error handling in login.go", "edge case for empty input"],
    "test_quality": "good",
    "recommendations": ["Add tests for error paths"]
}
```

## Signal Caching

To avoid redundant research across workflow executions, the system implements **signal result caching**.

### How It Works

1. **Cache Key Generation:** SHA256 hash of signal type + context
2. **TTL-based Expiration:** Default 5 minutes
3. **Thread-safe:** Uses sync.RWMutex for concurrent access
4. **Automatic:** Transparently integrated into signal processing

### Cache Key Algorithm

```go
key = SHA256(signalType + JSON(signalContext))
```

**Example:**
```go
// Signal 1
signal1 := StepSignal{
    Type: SignalResearchBestPractices,
    Context: map[string]interface{}{"language": "go"},
}
// → Cache key: "a1b2c3d4..."

// Signal 2 (same context)
signal2 := StepSignal{
    Type: SignalResearchBestPractices,
    Context: map[string]interface{}{"language": "go"},
}
// → Cache key: "a1b2c3d4..." (same - cache hit!)

// Signal 3 (different context)
signal3 := StepSignal{
    Type: SignalResearchBestPractices,
    Context: map[string]interface{}{"language": "python"},
}
// → Cache key: "e5f6g7h8..." (different - cache miss)
```

### Cache Configuration

```go
wm := NewWorkflowManager(orchestrator)

// Enable/disable caching
wm.EnableCache(true)  // Enabled by default

// Set cache TTL
wm.SetCacheTTL(10 * time.Minute)  // Default: 5 minutes

// Clear cache
wm.ClearCache()

// Prune expired entries
removed := wm.PruneCache()

// Check cache size
size := wm.GetCacheSize()
```

### Cache Behavior

**Function Call Signals:**
1. Check cache with `generateSignalCacheKey(signal)`
2. If hit: Return cached data immediately
3. If miss: Execute handler, store result, return

**Full Step Signals:**
1. Check cache before creating research step
2. If hit: Skip step execution, use cached result
3. If miss: Execute step, store result in cache

**Workflow Result Tracking:**
- Cached signals: `signal_<type>_cached` entry in StepResults
- Fresh signals: `signal_<type>` entry in StepResults

### Performance Impact

| Scenario | Without Cache | With Cache | Improvement |
|----------|---------------|------------|-------------|
| Function call signal (KB search) | 50-100ms | 0.1ms | 500-1000x |
| Full step signal (LLM analysis) | 5-30s | 0.1ms | 50,000-300,000x |
| Repeated workflows | O(n * research_time) | O(research_time) | n-fold |

### Cache Limitations

- **Memory usage:** Each entry stores full result map
- **No persistence:** Cache cleared on process restart
- **No cross-process:** Each WorkflowManager has separate cache
- **Future:** Persistent cache with Redis/Memcached

### Disabling Cache

For testing or debugging, disable caching:

```go
wm.EnableCache(false)
// Every signal execution is fresh, no cache hits
```

## Integration with AI Studio

Future enhancement: Configuration-driven signals via AI Studio Workflow Editor.

```yaml
# workflow_config.yaml (future)
workflow_id: "test_fix_workflow"
global_signals:
  - type: "research_best_practices"
    mode: "function_call"
    enabled: true

step_configs:
  code_generation:
    before_signals:
      - type: "research_code_patterns"
        mode: "full_step"
        enabled: true
```

## Migration from Queue-Based System

Old system had **static workflows** with hardcoded research steps.

New system has **dynamic workflows** where research is injected based on context.

**Before:**
```go
// Hardcoded research in workflow
steps := []Step{
    NewResearchStep(),        // Always runs
    NewImplementationStep(),
}
```

**After:**
```go
// Research triggered by signal
steps := []Step{
    NewImplementationStep(),  // Research auto-injected if needed
}

// Handler decides dynamically
wm.RegisterEventHandler(&BestPracticesHandler{})
```

## Performance Considerations

- **Function Call signals:** ~10-100ms overhead (< 0.1ms with cache hit)
- **Full Step signals:** 1-30 seconds depending on complexity (< 0.1ms with cache hit)
- **Signal merging:** Negligible (< 1ms)
- **Event emission:** < 1ms per event
- **Cache lookup:** < 0.1ms with thread-safe read lock
- **Cache write:** < 1ms with thread-safe write lock

## Future Enhancements

1. **Configuration Layer:** AI Studio integration for visual workflow editing
2. **Persistent Cache:** Redis/Memcached backing for cross-process caching
3. **Async Signals:** Non-blocking research execution
4. **Signal Metrics:** Track signal effectiveness and cache hit rates
5. **Custom Signal Types:** User-defined signal types via plugins
6. **Cross-Workflow Cache:** Share cache between different workflow instances
7. **Cache Warming:** Pre-populate cache with common research patterns
8. **Adaptive TTL:** Dynamic TTL based on signal type and context stability

## Implementation Status

✅ **Completed:**
- Signal types and modes
- Event handler system
- WorkflowState with cross-step data sharing
- Built-in event handlers (4 handlers)
- Built-in research steps (5 implementations)
- Signal caching with SHA256 keys and TTL
- Cache configuration and management API
- Comprehensive test coverage (30+ tests)

🔄 **In Progress:**
- Integration testing with real workflows
- Performance benchmarking

📋 **Planned:**
- AI Studio configuration UI
- Persistent cache backend
- Signal metrics dashboard

## References

- [PHASE2_ASK_PIPELINE_INTEGRATION.md](PHASE2_ASK_PIPELINE_INTEGRATION.md) - Step lifecycle hooks
- [IMPLEMENTATION_SUMMARY.md](IMPLEMENTATION_SUMMARY.md) - Overall architecture
- [MIGRATION_GUIDE.md](MIGRATION_GUIDE.md) - Migration from old system
