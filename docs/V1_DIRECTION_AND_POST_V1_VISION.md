# V1 Direction & Post-V1 Vision

**Last Updated**: 2026-07-24  
**Status**: ✅ V1 Complete & Shipped  

---

## Executive Summary

**V1 Decision**: Ship with **pure code-driven capabilities** backed by robust infrastructure. KB-driven workflows are disabled via feature flag.

**Post-V1 Vision**: Enable **visual, data-driven workflow authoring** while preserving the infrastructure power that makes V1 workflows effective.

---

## V1: Pure Code-Driven Capabilities

### The Decision

KB-driven workflows (`workflows/*.json`) are **disabled in V1** via `kbWorkflowsEnabled = false` feature flag.

### Why This Matters

**The Core Insight**: Infrastructure is what makes workflows powerful, not text prompts.

Text-based workflow definitions lack:
- AST parsing capabilities (tree-sitter)
- Patch generation and validation engines
- Test runners with output parsing
- Context managers with dependency analysis
- Validation orchestrators for quality enforcement

**Conclusion**: V1 ships proven, infrastructure-backed workflows implemented in compiled Go code.

### V1 Capabilities

**Coding Workflows** (All Infrastructure-Backed):
1. **Test Fix Workflow** (7-step orchestration)
   - Analyze failures → Isolate cases → Generate patches → Validate syntax → Run tests → Iterate → Document
   - Uses: TestRunner, PatchEngine, AST analysis

2. **Architecture Workflow** (5-step SDLC)
   - Analyze requirements → Map dependencies → Design structure → Validate → Document
   - Uses: ContextManager, AST analysis, validation

3. **Documentation Workflow** (4-step generation)
   - Analyze codebase → Extract structure → Generate docs → Review
   - Uses: AST parsing, structure analysis

4. **Implementation Workflow** (3-step build)
   - Plan → Implement → Validate
   - Uses: Full infrastructure stack

**Desktop Automation**:
- Git/shell/postgres command execution
- PTY session persistence
- Parallel execution support

**Performance Optimizations**:
- **80%+ queries use fast-path routing** (keyword-based, zero LLM calls)
- LLM fallback only for ambiguous queries
- Backwards-compatible Stores/Spaces domain inference

### V1 Architecture

```
User Query: "fix failing tests"
  ↓
Keyword Routing (Fast Path)
  "fix" + "test" → coding_workflow capability
  ↓
Task Classifier (Keyword Scoring)
  TaskType = Debugging
  ↓
orchestrateTestFixWorkflow()
  Step 1: TestRunner.analyze()      [Infrastructure]
  Step 2: ASTParser.isolate()        [Infrastructure]
  Step 3: PatchEngine.generate()     [Infrastructure]
  Step 4: TreeSitter.validate()      [Infrastructure]
  Step 5: TestRunner.execute()       [Infrastructure]
  Step 6: Condition-based iteration  [Infrastructure]
  Step 7: Document resolution        [Agent]
  ↓
Multi-Turn Queue Execution
  Each step gets full ReAct tool access
  ↓
Verified Result
```

**Key Point**: Every step has infrastructure backing. This is why V1 workflows are consistent and powerful.

---

## Post-V1 Vision: Data-Driven + Infrastructure

### The Goal

Enable users to **define custom workflows** without writing Go code, while **preserving infrastructure power**.

### The Hybrid Model

```
┌─────────────────────────────┐     ┌──────────────────────────────┐
│  Workflow Definition (KB)   │────▶│  Infrastructure Tools (Go)   │
├─────────────────────────────┤     ├──────────────────────────────┤
│ ✏️  Visual editing          │     │ 🔧 analyze_ast()             │
│ 👥 Team customizable        │     │ 🔧 generate_patch()          │
│ 🌐 Space-specific           │     │ 🔧 run_tests()               │
│ 🎯 Trigger patterns         │     │ 🔧 validate_syntax()         │
│ 📋 Step sequences           │     │ 🔧 build_context()           │
└─────────────────────────────┘     └──────────────────────────────┘
            ↓                                    ↓
            └────────── Integration ─────────────┘
                            ↓
              Rich Visual/Data-Driven Workflows
              WITH Code-Level Infrastructure
```

### Example Post-V1 Workflow

**User-Authored Workflow** (JSON in KB):

```json
{
  "id": "custom-api-generation",
  "name": "Generate REST API Endpoint",
  "description": "Generate a new REST endpoint with tests and docs",
  "category_path": "Team/Backend/API",
  "steps": [
    {
      "type": "infrastructure_tool",
      "tool": "analyze_existing_patterns",
      "params": {
        "pattern_type": "rest_endpoints",
        "similarity_threshold": 0.8
      }
    },
    {
      "type": "infrastructure_tool",
      "tool": "generate_code_with_ast",
      "params": {
        "template": "rest_endpoint",
        "validation": true
      }
    },
    {
      "type": "infrastructure_tool",
      "tool": "apply_patch",
      "params": {
        "validation_level": "strict"
      }
    },
    {
      "type": "infrastructure_tool",
      "tool": "run_tests",
      "params": {
        "test_type": "unit",
        "coverage_required": 80
      }
    },
    {
      "type": "agent",
      "prompt": "Document the new endpoint in OpenAPI format",
      "context": ["generated_code", "test_results"]
    }
  ]
}
```

### What This Enables

**For Users**:
- ✏️ Define workflows in UI, no code required
- 🎨 Visual workflow builder with drag-and-drop
- 👥 Share workflows via Spaces
- 📊 Workflow analytics and optimization

**For Teams**:
- 🏢 Organization-specific workflows
- 🔄 Workflow versioning and evolution
- 📚 Workflow marketplace/library
- 🔐 Access control per workflow

**For SOP**:
- 💪 Proven infrastructure remains the foundation
- 🚀 Users customize workflows to their needs
- 🎯 Best of both worlds: flexibility + power
- 📈 Workflows improve through usage data

### Technical Approach

**Infrastructure Tool Registry**:

```go
// Expose infrastructure tools to workflow system
type InfrastructureToolRegistry struct {
    tools map[string]InfrastructureTool
}

type InfrastructureTool struct {
    Name        string
    Description string
    Execute     func(ctx context.Context, params map[string]any) (Result, error)
    Schema      ToolParamSchema
}

// Example registrations
registry.Register("analyze_ast", ASTAnalyzer{})
registry.Register("generate_patch", PatchEngine{})
registry.Register("run_tests", TestRunner{})
registry.Register("validate_syntax", TreeSitterValidator{})
```

**Workflow Execution**:

```go
// Workflow executor calls infrastructure tools
func (we *WorkflowExecutor) ExecuteStep(step WorkflowStep) error {
    switch step.Type {
    case "infrastructure_tool":
        tool := we.registry.GetTool(step.Tool)
        return tool.Execute(step.Params)
    case "agent":
        return we.orchestrator.InjectPrompt(step.Prompt, step.Context)
    case "conditional":
        return we.evaluateCondition(step.Condition)
    }
}
```

---

## Migration Path: V1 → Post-V1

### What Stays

✅ All infrastructure tools (AST, patches, tests, validation)  
✅ Multi-turn queue orchestration  
✅ Capability registry and routing  
✅ Fast-path keyword optimization  
✅ ReAct tool integration  

### What Changes

📝 Workflow definitions move: Go code → JSON in KB  
📝 Add infrastructure tool registry  
📝 Enable visual workflow builder UI  
📝 Re-enable `kbWorkflowsEnabled` feature flag  

### Backward Compatibility

- ✅ Code-driven capabilities remain as fallback
- ✅ Existing workflows (test fix, architecture) become templates
- ✅ Users choose: curated workflows OR custom definitions
- ✅ No breaking changes to existing functionality

### Implementation Phases

**V1** (✅ Complete):
- Pure code-driven capabilities
- Proven infrastructure foundation
- Fast-path routing optimization

**V1.1** (Post-V1 Early):
- Infrastructure tool registry
- JSON workflow schema validation
- Basic workflow execution engine

**V1.2** (Post-V1 Mid):
- Visual workflow builder (alpha)
- Workflow sharing via Spaces
- Workflow analytics

**V2** (Post-V1 Long-term):
- Full workflow marketplace
- AI-assisted workflow optimization
- Advanced workflow debugging tools

---

## Key Lessons

### What We Learned

1. **Infrastructure > Text Prompts**: The power comes from AST parsing, patch engines, and test runners—not from clever prompts

2. **Code-Driven Works**: V1's 7-step test fix workflow delivers consistent results because it has infrastructure backing every step

3. **Don't Mix Abstractions**: V1 initially mixed code-driven capabilities with KB workflow fallbacks. This was wrong. They're fundamentally different approaches.

4. **Fast Routing Wins**: 80% of queries are clear intent—keyword matching is sufficient and eliminates unnecessary LLM calls

5. **SDLC Needs Structure**: Architecture and documentation workflows benefit from same infrastructure as implementation workflows

### Design Principles for Post-V1

1. **Infrastructure First**: Make tools powerful before making them configurable

2. **Gradual Enablement**: Ship proven foundation, add flexibility incrementally

3. **User Control**: Give users the keys to customize, but provide great defaults

4. **Backwards Compatible**: Never break existing workflows when adding new capabilities

5. **Observable**: Every workflow step should be traceable and debuggable

---

## References

- [WORKFLOWS.md](WORKFLOWS.md) - Complete workflow documentation
- [CLI_CODING_WORKFLOW_IMPLEMENTATION.md](CLI_CODING_WORKFLOW_IMPLEMENTATION.md) - V1 implementation details
- [DEVELOPMENT_GUIDE.md](DEVELOPMENT_GUIDE.md) - General development guidance

---

## FAQ

**Q: Why disable KB workflows in V1?**  
A: Text-based definitions lack infrastructure power. V1 proves code-driven works. Post-V1 wraps infrastructure in data-driven layer.

**Q: Will KB workflows come back?**  
A: Yes, but enhanced. Post-V1 workflows will call infrastructure tools as first-class steps, not just text prompts.

**Q: Can users customize V1 workflows?**  
A: Not yet. V1 ships curated workflows. Post-V1 enables full customization.

**Q: What happens to workflow JSON files?**  
A: They remain in codebase as examples/templates for post-V1 implementation.

**Q: How do I add a new workflow in V1?**  
A: Implement in Go code in `codingworkflow/` package. Post-V1 will support JSON definitions.

**Q: Why keyword routing instead of always using LLM?**  
A: Performance and cost. 80% of queries have clear intent—keyword matching is instant and free.
