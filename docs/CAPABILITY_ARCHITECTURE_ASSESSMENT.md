# Capability Architecture Assessment

**Date**: 2026-07-24  
**Question**: Can we author another Capability (e.g., web research/crawling) that gets orchestrated like coding workflow and desktop automation?

---

## ✅ YES - Your Understanding Is Correct!

The capability architecture is **exactly what you expected**. Both coding workflow and desktop automation are capabilities that plug into the Ask/ReAct loop pipeline, and you can absolutely create new capabilities with the same orchestration power.

---

## Architecture Overview

### The Capability Contract

Every capability implements the `Capability` interface, and may also provide structured guidance through an optional guidance provider:

```go
// lib/sop/ai/agent/capabilities/capability.go
type Capability interface {
    ID() string                      // Unique identifier
    Name() string                    // Human-readable name
    Entitlement() Entitlement        // Access control (base, premium, etc.)
    Classify(query, state) ClassificationResult
    BuildContext(query, state) string // Context injection
    ExposeTools() []ToolDefinition   // Tool registration
    BuildInstructions(query, state) string // LLM instructions
    Handoff(ctx, query, state) *Handoff
    OnBefore/OnAfter(...)
}

type StructuredGuidanceProvider interface {
    BuildGuidanceProfile(query string, state RoutingState) GuidanceProfile
}
```

Structured guidance is now treated as a first-class capability surface. It gives the Ask loop:
- role and operating principles
- phase and task-family behavior guidance
- tool-selection heuristics
- validation expectations
- guardrails and escalation rules

To keep authoring simple, guidance is intentionally progressive:
- minimal guidance for simple capabilities that only need light steering
- light guidance for ordinary capabilities that benefit from role and guardrail hints
- deep guidance for complex domain-specific capabilities that need richer heuristics, examples, and verification expectations

### Progressive Guidance Model

Capability authors can choose the level of guidance that fits the task:

```go
// Simple capability: no special steering needed
profile := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityMinimal, "Answer simple requests")

// Standard capability: role + guardrails
profile := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityLight, "Help with common automation tasks")
profile.Principles = []string{"Prefer the safest available action"}
profile.Guardrails = []capabilities.GuidanceRule{{Description: "Do not mutate state without confirmation"}}

// Complex capability: richer domain-specific guidance
profile := capabilities.NewGuidanceProfile(capabilities.GuidanceIntensityDeep, "Guide web research workflows")
profile.SituationRules = []capabilities.GuidanceRule{{Description: "Use supporting evidence before drawing conclusions"}}
profile.ToolSelection = []capabilities.GuidanceRule{{Description: "Prefer search and crawl tools for discovery"}}
```

This keeps authoring lightweight for simple capabilities while leaving room for deep domain-specific behavior when a capability truly needs it.

### Capability Registry

Capabilities are registered in a central registry:

```go
// lib/sop/ai/agent/copilot.go - ensureCapabilityRegistry()
caps := []capabilities.Capability{
    desktop.NewCapability(),         // Desktop automation
    stores.NewCapability(),          // Stores (if enabled)
    spaces.NewCapability(),          // Spaces
}

// Coding workflow is registered via WithCapability()
a.capabilityRegistry = capabilities.NewRegistry(caps...)
```

### Orchestration Flow

```
User Query: "fix failing tests"
    ↓
Routing Layer (keyword/LLM)
    ↓
Capability Registry.Resolve()
    ├─ Checks each capability.CanHandle()
    └─ Returns first match
    ↓
Selected Capability
    ├─ If MultiTurnCapability: capability.Orchestrate()
    │  └─ Injects work items into queue
    │  └─ Multi-turn execution across ReAct cycles
    └─ Else: capability.Run()
       └─ Single-turn execution
    ↓
ReAct Loop Execution
    ├─ Each work item gets full tool access
    ├─ Context from capability.BuildContext()
    └─ Instructions from capability.BuildInstructions()
    ↓
Result
```

---

## Current Capabilities

### 1. Coding Workflow Capability
**Package**: `lib/sop/ai/agent/codingworkflow/`

**Implements**:
- ✅ `Capability` - Base interface
- ✅ `MultiTurnCapability` - Queue-based orchestration
- ✅ Infrastructure tools: AST, patches, tests, validation

**Key Features**:
```go
func (c *Capability) Orchestrate(ctx, state) (bool, error) {
    queue := getWorkQueue(ctx)
    query := ctx.Value("multi_turn_query").(string)
    
    // Classify task type
    classifier := DefaultTaskClassifier()
    classification := classifier.Classify(query, "", "")
    
    // Route to appropriate workflow
    switch classification.TaskType {
    case TaskTypeDebugging:
        return c.orchestrateTestFixWorkflow(queue, query)
    case TaskTypeArchitecture:
        return c.orchestrateArchitectureWorkflow(queue, query)
    // ... 6 workflow types total
    }
}
```

**7-Step Test Fix Workflow Example**:
```go
func orchestrateTestFixWorkflow(queue, query) {
    queue.Enqueue(WorkItem{Query: "Analyze test failures", Actor: "analyzer"})
    queue.Enqueue(WorkItem{Query: "Isolate failing cases", Actor: "isolator"})
    queue.Enqueue(WorkItem{Query: "Generate patch", Actor: "patcher"})
    queue.Enqueue(WorkItem{Query: "Validate syntax", Actor: "validator"})
    queue.Enqueue(WorkItem{Query: "Run tests", Actor: "test_runner"})
    queue.EnqueueConditional(WorkItem{Query: "Iterate if needed"})
    queue.Enqueue(WorkItem{Query: "Document resolution", Actor: "documenter"})
    return true, nil
}
```

### 2. Desktop Automation Capability
**Package**: `lib/sop/ai/agent/domains/desktop/`

**Implements**:
- ✅ `Capability` - Base interface
- ✅ `PrefixHandler` - Fast-path command execution

**Key Features**:
```go
func (c *Capability) GetHandledPrefixes() []string {
    return []string{"/c", "/cmd", "/shell", "/exec"}
}

func (c *Capability) HandlePrefix(ctx, query, normalized) (string, error) {
    // Direct execution bypassing ReAct loop
    return c.rawCommandExecutor(ctx, map[string]any{
        "command": normalized,
        "timeout": 30,
    })
}
```

**Persistent Shell Sessions**:
- Each conversation gets a persistent shell session
- `cd`, `export` commands persist across turns
- PTY-based for proper terminal emulation

---

## How to Create a Web Research Capability

### Step 1: Define the Capability Structure

```go
// lib/sop/ai/agent/webresearch/capability.go
package webresearch

import (
    "context"
    "github.com/sharedcode/sop/ai/agent/capabilities"
)

const (
    CapabilityID   = "web_research"
    CapabilityName = "Web Research & Crawling"
)

type Capability struct {
    Crawler       *WebCrawler      // Web crawler implementation
    SearchEngine  *SearchEngine    // Search API integration
    ContentParser *ContentParser   // HTML/content extraction
}

func NewCapability() *Capability {
    return &Capability{
        Crawler:       NewWebCrawler(),
        SearchEngine:  NewSearchEngine(),
        ContentParser: NewContentParser(),
    }
}
```

### Step 2: Implement the Capability Interface

```go
func (c *Capability) ID() string { return CapabilityID }
func (c *Capability) Name() string { return CapabilityName }

func (c *Capability) Entitlement() capabilities.Entitlement {
    return capabilities.Entitlement{
        EnabledByDefault: false,  // Premium feature
        Premium:          true,
        Tier:            "premium",
    }
}

func (c *Capability) CanHandle(query string, state capabilities.RoutingState) bool {
    q := strings.ToLower(query)
    
    // Check routing hint
    if state.CapabilityHint == "web_research" || state.CapabilityHint == "web_crawl" {
        return true
    }
    
    // Keyword detection
    return strings.Contains(q, "search web") ||
        strings.Contains(q, "crawl") ||
        strings.Contains(q, "research online") ||
        strings.Contains(q, "find information") ||
        strings.Contains(q, "scrape") ||
        strings.Contains(q, "gather data from")
}

func (c *Capability) BuildContext(query string, state capabilities.RoutingState) string {
    return `Structured Context: Web Research & Crawling
- Use web research tools for information gathering from the internet
- Respect robots.txt and rate limits
- Extract structured data from web pages
- Aggregate results from multiple sources`
}

func (c *Capability) ExposeTools() []capabilities.ToolDefinition {
    return []capabilities.ToolDefinition{
        {
            Name:        "search_web",
            Description: "Searches the web using multiple search engines",
            Schema:      `{"query": "string", "num_results": "number"}`,
        },
        {
            Name:        "crawl_website",
            Description: "Crawls a website and extracts structured content",
            Schema:      `{"url": "string", "max_depth": "number"}`,
        },
        {
            Name:        "extract_content",
            Description: "Extracts and structures content from a web page",
            Schema:      `{"url": "string", "selectors": "array"}`,
        },
    }
}

func (c *Capability) BuildInstructions(query string, state capabilities.RoutingState) string {
    return `Web Research Workflow Guide:

1. Search Strategy:
   - Use search_web() to find relevant sources
   - Prioritize authoritative, recent sources
   - Cross-reference multiple sources

2. Crawling:
   - Respect robots.txt and crawl delays
   - Set reasonable max_depth to avoid overwhelming sites
   - Extract only necessary content

3. Content Extraction:
   - Use CSS selectors for structured data
   - Clean and normalize extracted text
   - Preserve source attribution

4. Synthesis:
   - Aggregate findings from multiple sources
   - Cite sources clearly
   - Highlight conflicting information`
}

func (c *Capability) Run(query string, state capabilities.RoutingState) (capabilities.CapabilityResult, error) {
    // Single-turn execution (if not using orchestration)
    results, err := c.SearchEngine.Search(query, 5)
    if err != nil {
        return capabilities.CapabilityResult{}, err
    }
    
    return capabilities.CapabilityResult{
        Summary: fmt.Sprintf("Found %d results", len(results)),
        Data: map[string]any{
            "results": results,
        },
    }, nil
}
```

### Step 3: Add Multi-Turn Orchestration (Optional but Powerful)

```go
// Implement MultiTurnCapability for complex workflows
func (c *Capability) Orchestrate(ctx context.Context, state capabilities.RoutingState) (bool, error) {
    queue, ok := getWorkQueue(ctx)
    if !ok {
        return false, fmt.Errorf("work queue not found")
    }
    
    query := ctx.Value("multi_turn_query").(string)
    
    // Multi-step research workflow
    // Step 1: Search for sources
    queue.Enqueue(map[string]interface{}{
        "query":    fmt.Sprintf("Search web for: %s", query),
        "actor":    "web_research.searcher",
        "priority": 10,
    })
    
    // Step 2: Crawl top sources
    queue.Enqueue(map[string]interface{}{
        "query":    "Crawl top 5 sources and extract content",
        "actor":    "web_research.crawler",
        "priority": 10,
    })
    
    // Step 3: Extract structured data
    queue.Enqueue(map[string]interface{}{
        "query":    "Extract and structure key information",
        "actor":    "web_research.extractor",
        "priority": 10,
    })
    
    // Step 4: Synthesize findings
    queue.Enqueue(map[string]interface{}{
        "query":    "Synthesize findings and cite sources",
        "actor":    "web_research.synthesizer",
        "priority": 10,
    })
    
    return true, nil
}
```

### Step 4: Register the Capability

```go
// lib/sop/ai/agent/copilot.go - ensureCapabilityRegistry()
func (a *CopilotAgent) ensureCapabilityRegistry() {
    if a == nil || a.capabilityRegistry != nil {
        return
    }
    
    caps := []capabilities.Capability{
        desktop.NewCapability(),
        webresearch.NewCapability(),  // ← Add web research capability
    }
    
    if StoresFeatureEnabled() {
        caps = append(caps, stores.NewCapability())
    }
    
    caps = append(caps, spaces.NewCapability())
    a.capabilityRegistry = capabilities.NewRegistry(caps...)
}
```

### Step 5: Add Routing Keywords

```go
// lib/sop/ai/agent/copilot_routing.go - inferCapabilityRoutingSignal()
func inferCapabilityRoutingSignal(query string, domain string) (string, string, string) {
    q := strings.ToLower(strings.TrimSpace(query))
    
    // ... existing routing logic ...
    
    // Add web research routing
    if strings.Contains(q, "search web") || 
       strings.Contains(q, "crawl") || 
       strings.Contains(q, "research online") {
        return intentFamilyWebResearch, capabilityHintWebResearch, inferredDomain
    }
    
    // ... rest of routing logic ...
}
```

---

## Advanced Features Available

### 1. Prefix Handlers (Fast-Path Execution)

If you want instant execution without ReAct reasoning:

```go
func (c *Capability) GetHandledPrefixes() []string {
    return []string{"/search", "/crawl", "/extract"}
}

func (c *Capability) HandlePrefix(ctx context.Context, query, normalized string) (string, error) {
    // Direct execution bypassing ReAct
    if strings.HasPrefix(normalized, "search ") {
        searchQuery := strings.TrimPrefix(normalized, "search ")
        return c.SearchEngine.Search(searchQuery, 5)
    }
    return "", fmt.Errorf("unknown prefix command")
}
```

Usage: `/search latest AI research papers`

### 2. Participant Hooks (Lightweight Integration)

For contributing context without taking over routing:

```go
func (c *Capability) Participate(ctx context.Context, query string, state capabilities.RoutingState) (capabilities.ParticipationResult, bool, error) {
    // Check if we should add research context
    if strings.Contains(strings.ToLower(query), "research") {
        return capabilities.ParticipationResult{
            Summary: "Web research capability available for online information gathering",
            Data: map[string]any{
                "search_engines": []string{"google", "bing", "scholar"},
            },
        }, true, nil
    }
    return capabilities.ParticipationResult{}, false, nil
}
```

### 3. Infrastructure Integration

You can provide infrastructure tools like coding workflow does:

```go
type WebCrawler struct {
    HTTPClient *http.Client
    Parser     *goquery.Document
    RateLimiter *rate.Limiter
}

func (w *WebCrawler) Crawl(url string, maxDepth int) ([]Page, error) {
    // Use infrastructure (HTTP client, parser, rate limiter)
    // Similar to how coding workflow uses AST, patches, tests
}
```

---

## Comparison Table

| Feature | Coding Workflow | Desktop Automation | Your Web Research Capability |
|---------|----------------|-------------------|------------------------------|
| **Base Interface** | ✅ Capability | ✅ Capability | ✅ Capability |
| **Multi-Turn** | ✅ MultiTurnCapability | ❌ (single-turn) | ✅ MultiTurnCapability |
| **Prefix Handler** | ❌ | ✅ PrefixHandler (/c, /cmd) | ✅ PrefixHandler (/search, /crawl) |
| **Infrastructure** | ✅ AST, patches, tests | ✅ PTY, shell sessions | ✅ HTTP, parser, rate limiter |
| **Orchestration** | ✅ 7-step workflows | ❌ | ✅ 4-step research pipeline |
| **Tool Registration** | ✅ coding_workflow | ✅ execute_local_command | ✅ search_web, crawl_website |
| **Routing** | Keywords + hint | Domain-based | Keywords + hint |

---

## Summary

### ✅ Your Understanding Is 100% Correct

1. **Yes**, coding workflow is a capability that plugs into the Ask/ReAct pipeline
2. **Yes**, desktop automation is another capability with the same pattern
3. **Yes**, you can create a web research/crawling capability using the same architecture
4. **Yes**, it will get orchestrated the same way with multi-turn support
5. **Yes**, you can leverage infrastructure tools (HTTP client, parser) just like coding workflow uses AST/patches

### The Architecture Enables

- ✅ **Pluggable capabilities** via registry pattern
- ✅ **Multi-turn orchestration** via WorkQueue injection
- ✅ **Infrastructure backing** for powerful workflows
- ✅ **Fast-path execution** via prefix handlers
- ✅ **Modular tool registration** per capability
- ✅ **Flexible routing** via CanHandle + hints

### Next Steps to Add Web Research

1. Create `lib/sop/ai/agent/webresearch/` package
2. Implement `Capability` interface with `MultiTurnCapability`
3. Build infrastructure (crawler, search API, content parser)
4. Register in `ensureCapabilityRegistry()`
5. Add routing keywords in `inferCapabilityRoutingSignal()`
6. Optionally add prefix handlers (`/search`, `/crawl`)

The capability architecture is **exactly designed for this use case**. You have a robust, extensible system ready for new capabilities! 🚀
