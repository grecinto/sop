# User Interjection Queue Design

## Problem Statement

**Current Behavior**: When a user sends a new prompt while an Ask request is being processed, it goes to event handlers but doesn't interrupt the current Ask flow.

**Desired Behavior**: User interjections should:
1. Be enqueued into the WorkQueue (not event handlers)
2. **Pause/interrupt** the current Ask flow
3. Trigger **LLM analysis** to understand interjection context
4. Resume or redirect based on LLM classification

---

## Current Architecture

### WorkQueue System ✅
**Location**: `lib/sop/ai/agent/workqueue.go`

```go
type WorkItem struct {
    Query         string
    Actor         string           // "user", "coding_workflow", etc.
    Priority      int              // Higher = processed first
    Context       map[string]any
    ParentItemID  string
    TurnNumber    int
    ID            string
}

type WorkQueue struct {
    items       []WorkItem        // Priority-sorted
    mu          sync.Mutex
    maxDepth    int
    maxPerActor map[string]int
    actorCount  map[string]int
}
```

**How it works**:
- Items sorted by Priority (higher first)
- Per-actor limits enforced
- Currently used by capabilities to enqueue follow-up work

### Ask Flow ✅
**Location**: `lib/sop/ai/agent/copilot.go:2664`

```go
func (a *CopilotAgent) Ask(ctx, query, cfg) (string, error) {
    queue := a.getOrCreateSessionQueue()
    
    // Enqueue initial user query
    queue.Enqueue(WorkItem{Query: query, Actor: "user"})
    
    // Process until empty
    for !queue.IsEmpty() {
        item := queue.Dequeue()
        response := a.ask(ctx, item.Query, cfg)  // Pure ReAct transaction
        // Capabilities may have enqueued next steps
    }
    
    return lastResponse, nil
}
```

**Current flow**:
1. User query enqueued
2. Queue processed sequentially
3. Each item goes through full ReAct pipeline
4. Capabilities can enqueue follow-up work during execution

**Missing**: No mechanism to handle user interjection DURING Ask execution

---

## Enhanced Design

### 1. Two Types of Queue Items

```go
// Enhanced WorkItem with source type
type WorkItem struct {
    Query         string
    Actor         string           // "user", "coding_workflow", "test_runner"
    Priority      int
    Source        WorkItemSource   // NEW: Distinguishes origin
    Context       map[string]any
    ParentItemID  string
    TurnNumber    int
    ID            string
    Timestamp     time.Time        // NEW: For interjection detection
    IsInterjection bool            // NEW: User sent while Ask active
}

type WorkItemSource string

const (
    SourceUserInitial      WorkItemSource = "user_initial"      // User's original query
    SourceUserInterjection WorkItemSource = "user_interjection" // User sent during Ask
    SourceCapability       WorkItemSource = "capability"        // Capability enqueued
)
```

**Priority Scheme**:
```go
const (
    PriorityUserInterjection = 1000  // HIGHEST - pause everything
    PriorityUserInitial      = 100   // High - normal user queries
    PriorityCapabilityUrgent = 50    // Medium - critical capability work
    PriorityCapabilityNormal = 10    // Low - regular capability work
)
```

### 2. Interjection Detection

**Location**: New file `lib/sop/ai/agent/interjection.go`

```go
// InterjectionManager handles user interjections during Ask execution
type InterjectionManager struct {
    activeAskID   string
    activeStarted time.Time
    mu            sync.RWMutex
    llmClassifier LLMInterjectionClassifier
}

// HandleUserInterjection is called when user sends message during active Ask
func (m *InterjectionManager) HandleUserInterjection(
    ctx context.Context,
    userQuery string,
    currentPhase string,
    conversationHistory []Exchange,
    queue *WorkQueue,
) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // Create high-priority interjection item
    item := WorkItem{
        Query:          userQuery,
        Actor:          "user",
        Priority:       PriorityUserInterjection,
        Source:         SourceUserInterjection,
        IsInterjection: true,
        Timestamp:      time.Now(),
        ID:             fmt.Sprintf("interjection-%d", time.Now().UnixNano()),
        Context: map[string]any{
            "active_ask_id":  m.activeAskID,
            "current_phase":  currentPhase,
            "interrupted_at": time.Now(),
        },
    }
    
    // Enqueue with highest priority (will be processed next)
    if err := queue.Enqueue(item); err != nil {
        return fmt.Errorf("failed to enqueue interjection: %w", err)
    }
    
    log.Info("User interjection enqueued",
        "query", truncateString(userQuery, 50),
        "current_phase", currentPhase,
        "queue_depth", queue.Depth())
    
    // Trigger LLM classification (async to avoid blocking)
    go m.classifyInterjectionIntent(ctx, userQuery, currentPhase, conversationHistory)
    
    return nil
}
```

### 3. LLM Interjection Classification

**Location**: `lib/sop/ai/agent/interjection_classifier.go`

```go
// InterjectionIntent describes why user interjected
type InterjectionIntent struct {
    Type            InterjectionType
    TargetPhase     string           // Phase user wants to jump to
    IsRejection     bool             // User rejecting current approach
    IsRedirection   bool             // User changing scope/goal
    IsClarification bool             // User providing info
    IsCancel        bool             // User wants to abort
    Confidence      float64          // 0.0-1.0
    Reasoning       string           // LLM explanation
}

type InterjectionType string

const (
    InterjectionReject     InterjectionType = "reject"      // "No, do X instead"
    InterjectionRedirect   InterjectionType = "redirect"    // "Actually, let's focus on Y"
    InterjectionClarify    InterjectionType = "clarify"     // "By the way, Z is important"
    InterjectionCancel     InterjectionType = "cancel"      // "Stop, nevermind"
    InterjectionContinue   InterjectionType = "continue"    // "Yes, proceed"
    InterjectionQuestion   InterjectionType = "question"    // "What are you doing?"
)

// ClassifyInterjection uses LLM to understand user intent
func (c *LLMInterjectionClassifier) ClassifyInterjection(
    ctx context.Context,
    userQuery string,
    currentPhase string,
    currentAction string,
    conversationHistory []Exchange,
    gen ai.Generator,
) (*InterjectionIntent, error) {
    systemPrompt := `You are an interjection classifier. The user has sent a message while the agent is actively working.

Analyze the user's message to determine their intent:

**Interjection Types:**
- reject: User disagrees with current approach ("No, don't do that", "Actually, that's wrong")
- redirect: User wants to change scope or goal ("Let's focus on X instead", "Change of plans")
- clarify: User providing additional information ("By the way, ...", "Also, ...")
- cancel: User wants to abort ("Stop", "Never mind", "Cancel that")
- continue: User confirming/encouraging ("Yes", "Go ahead", "Looks good")
- question: User asking about status ("What are you doing?", "Why is this taking so long?")

**Current Context:**
Phase: ` + currentPhase + `
Current Action: ` + currentAction + `

**User Message:** "` + userQuery + `"

**Recent Conversation:**
` + formatConversationHistory(conversationHistory) + `

Respond with JSON:
{
  "type": "reject|redirect|clarify|cancel|continue|question",
  "target_phase": "investigation|planning|execution|review|intake|delivery|null",
  "is_rejection": true/false,
  "is_redirection": true/false,
  "is_clarification": true/false,
  "is_cancel": true/false,
  "confidence": 0.0-1.0,
  "reasoning": "Brief explanation of intent"
}
`

    response, err := gen.Generate(ctx, systemPrompt)
    if err != nil {
        return nil, fmt.Errorf("LLM classification failed: %w", err)
    }
    
    var intent InterjectionIntent
    if err := json.Unmarshal([]byte(response), &intent); err != nil {
        return nil, fmt.Errorf("failed to parse LLM response: %w", err)
    }
    
    return &intent, nil
}
```

### 4. Ask Flow Enhancement

**Location**: Modify `lib/sop/ai/agent/copilot.go`

```go
func (a *CopilotAgent) Ask(ctx context.Context, query string, cfg *ai.ConfigMap) (string, error) {
    queue := a.getOrCreateSessionQueue()
    interjectionMgr := a.getOrCreateInterjectionManager()
    
    // Mark Ask as active for interjection detection
    askID := fmt.Sprintf("ask-%d", time.Now().UnixNano())
    interjectionMgr.SetActive(askID)
    defer interjectionMgr.SetInactive()
    
    // Enqueue initial user query
    if err := queue.Enqueue(WorkItem{
        Query:    query,
        Actor:    "user",
        Priority: PriorityUserInitial,
        Source:   SourceUserInitial,
        ID:       fmt.Sprintf("user-initial-%d", time.Now().UnixNano()),
    }); err != nil {
        return "", fmt.Errorf("failed to enqueue query: %w", err)
    }
    
    // Inject queue into context for interjection handler access
    ctx = WithWorkQueue(ctx, queue)
    ctx = WithInterjectionManager(ctx, interjectionMgr)
    
    var lastResponse string
    var turnResults []TurnResult
    
    // Process queue with interjection awareness
    for !queue.IsEmpty() {
        item, err := queue.Dequeue()
        if err != nil {
            break
        }
        
        // Check if this is an interjection
        if item.IsInterjection {
            log.Info("Processing user interjection",
                "query", truncateString(item.Query, 50),
                "interrupted_phase", item.Context["current_phase"])
            
            // Classify interjection intent
            currentPhase := a.getCurrentPhase()
            intent, err := interjectionMgr.ClassifyInterjection(
                ctx,
                item.Query,
                currentPhase,
                a.getCurrentAction(),
                a.getConversationHistory(),
                a.generator,
            )
            
            if err != nil {
                log.Warn("Interjection classification failed", "error", err)
                // Fall through to normal processing
            } else {
                // Handle based on intent
                handled, response := a.handleInterjectionIntent(ctx, intent, item, cfg)
                if handled {
                    return response, nil
                }
            }
        }
        
        log.Info("Processing queue item",
            "actor", item.Actor,
            "source", item.Source,
            "priority", item.Priority,
            "query_preview", truncateString(item.Query, 80))
        
        // Execute pure ask (delegates to ReAct)
        response, err := a.ask(ctx, item.Query, cfg)
        if err != nil {
            return response, err
        }
        
        lastResponse = response
        turnResults = append(turnResults, TurnResult{
            Actor:    item.Actor,
            Query:    item.Query,
            Response: response,
        })
        
        // Check for new interjections after each turn
        // (queue may have been modified by interjection handler)
    }
    
    return lastResponse, nil
}
```

### 5. Interjection Intent Handling

**Location**: `lib/sop/ai/agent/copilot_interjection.go`

```go
// handleInterjectionIntent decides how to respond to user interjection
func (a *CopilotAgent) handleInterjectionIntent(
    ctx context.Context,
    intent *InterjectionIntent,
    item WorkItem,
    cfg *ai.ConfigMap,
) (handled bool, response string) {
    
    switch intent.Type {
    case InterjectionCancel:
        // User wants to abort
        return true, "Canceling current operation. How can I help you?"
        
    case InterjectionReject:
        // User rejecting current approach
        if intent.TargetPhase != "" {
            // Force route to target phase
            state := a.buildRoutingState()
            state.CapabilityHint = intent.TargetPhase + "_phase"
            log.Info("Interjection rejected current phase",
                "from", a.getCurrentPhase(),
                "to", intent.TargetPhase,
                "reasoning", intent.Reasoning)
        }
        // Continue processing with new routing
        return false, ""
        
    case InterjectionRedirect:
        // User changing scope/goal
        // Clear queue of capability-enqueued items (they're obsolete)
        queue := a.GetSessionQueue()
        queue.ClearCapabilityItems()
        log.Info("Interjection redirected goal, cleared capability queue")
        // Continue processing with new query
        return false, ""
        
    case InterjectionClarify:
        // User providing additional info
        // Add to context, continue current phase
        a.addConversationContext(item.Query)
        log.Info("Interjection added clarification to context")
        return false, ""
        
    case InterjectionContinue:
        // User confirming, just acknowledge
        return true, "Continuing..."
        
    case InterjectionQuestion:
        // User asking status
        currentAction := a.getCurrentAction()
        currentPhase := a.getCurrentPhase()
        return true, fmt.Sprintf("I'm currently in the %s phase, %s. %s",
            currentPhase,
            currentAction,
            intent.Reasoning)
        
    default:
        // Unknown intent, treat as new query
        return false, ""
    }
}
```

### 6. HTTP Handler Integration

**Location**: `lib/sop/cmd/sop-daemon/rag.go`

```go
func (s *localRAGService) handleAsk(w http.ResponseWriter, r *http.Request) {
    // ... existing setup
    
    // Check if there's an active Ask for this session
    sessionID := payload.SessionID
    activeAsk := getActiveAsk(sessionID)
    
    if activeAsk != nil {
        // Ask is already running for this session
        // This is an interjection!
        
        copilotAgent := getCopilotAgentForSession(sessionID)
        if copilotAgent != nil {
            interjectionMgr := copilotAgent.GetInterjectionManager()
            queue := copilotAgent.GetSessionQueue()
            
            // Enqueue as interjection (will interrupt current Ask)
            err := interjectionMgr.HandleUserInterjection(
                ctx,
                req.Query,
                activeAsk.CurrentPhase,
                activeAsk.ConversationHistory,
                queue,
            )
            
            if err != nil {
                log.Printf("Failed to handle interjection: %v", err)
                // Fall through to normal handling
            } else {
                // Return immediately - interjection enqueued
                sendEvent("interjection", map[string]any{
                    "status":  "enqueued",
                    "message": "Your message has been prioritized",
                })
                return
            }
        }
    }
    
    // Normal Ask flow
    // ... existing code
}
```

---

## Example Scenarios

### Scenario 1: Reject Current Approach

```
[Agent in Investigation Phase]
Agent: "Let me analyze the test failures by reading each test file..."

User: "No, just run the tests first to see the error output"
↓
Interjection detected
↓
LLM Classification:
  type: "reject"
  target_phase: "execution"
  is_rejection: true
  reasoning: "User wants direct execution, not investigation"
↓
Queue reprioritized:
  1. User interjection (priority 1000)
  2. [Investigation items cleared]
↓
Agent: "Running tests to capture error output..."
```

### Scenario 2: Redirect Goal

```
[Agent in Planning Phase]
Agent: "Here's my plan to refactor the auth module..."

User: "Actually, let's fix the urgent bug in payments first"
↓
Interjection detected
↓
LLM Classification:
  type: "redirect"
  target_phase: "investigation"
  is_redirection: true
  reasoning: "User changed priority from refactoring to bug fix"
↓
Queue cleared of capability items
New interjection processed
↓
Agent: "Understood. Let me investigate the payments bug..."
```

### Scenario 3: Clarify Context

```
[Agent in Execution Phase]
Agent: "Applying patch to user_service.go..."

User: "By the way, make sure to update the tests too"
↓
Interjection detected
↓
LLM Classification:
  type: "clarify"
  is_clarification: true
  reasoning: "User adding requirement to include test updates"
↓
Context enriched, execution continues
↓
Agent: "Noted. I'll update the corresponding tests as well."
```

### Scenario 4: Cancel Operation

```
[Agent in Review Phase]
Agent: "Validating changes across 50 files..."

User: "Stop, I need to make some manual changes first"
↓
Interjection detected
↓
LLM Classification:
  type: "cancel"
  is_cancel: true
  reasoning: "User wants to abort current validation"
↓
Queue cleared, execution stopped
↓
Agent: "Operation cancelled. Let me know when you're ready to continue."
```

### Scenario 5: Status Question

```
[Agent in Investigation Phase]
Agent: [Running long analysis...]

User: "What's taking so long?"
↓
Interjection detected
↓
LLM Classification:
  type: "question"
  reasoning: "User asking for status update"
↓
Immediate response
↓
Agent: "I'm analyzing 15 test failures to identify patterns. Found 3 distinct root causes so far..."
```

---

## Implementation Plan

### Phase 1: Core Infrastructure (2-3 days)

**Files to create**:
- `lib/sop/ai/agent/interjection.go` - InterjectionManager
- `lib/sop/ai/agent/interjection_classifier.go` - LLM classification
- `lib/sop/ai/agent/copilot_interjection.go` - Intent handling

**Files to modify**:
- `lib/sop/ai/agent/workqueue.go` - Add Source, IsInterjection fields
- `lib/sop/ai/agent/copilot.go` - Enhance Ask loop

**Tasks**:
1. ✅ Add WorkItemSource enum
2. ✅ Add Priority constants (PriorityUserInterjection, etc.)
3. ✅ Create InterjectionManager with SetActive/SetInactive
4. ✅ Create LLMInterjectionClassifier with ClassifyInterjection
5. ✅ Add handleInterjectionIntent to CopilotAgent

### Phase 2: Queue Enhancement (1 day)

**Modifications**:
```go
// workqueue.go additions
type WorkItem struct {
    // ... existing fields
    Source         WorkItemSource
    IsInterjection bool
    Timestamp      time.Time
}

// New method to clear capability items (for redirections)
func (q *WorkQueue) ClearCapabilityItems() {
    q.mu.Lock()
    defer q.mu.Unlock()
    
    var userItems []WorkItem
    for _, item := range q.items {
        if item.Source == SourceUserInitial || item.Source == SourceUserInterjection {
            userItems = append(userItems, item)
        }
    }
    q.items = userItems
}
```

### Phase 3: HTTP Integration (1-2 days)

**Modify**: `lib/sop/cmd/sop-daemon/rag.go`

**Tasks**:
1. ✅ Track active Ask sessions (session ID → active Ask state)
2. ✅ Detect interjections in handleAsk
3. ✅ Call InterjectionManager.HandleUserInterjection
4. ✅ Return immediate response for interjections

### Phase 4: Testing (2 days)

**Test files to create**:
- `lib/sop/ai/agent/interjection_test.go`
- `lib/sop/ai/agent/interjection_classifier_test.go`
- `lib/sop/ai/agent/copilot_interjection_test.go`

**Test scenarios**:
1. ✅ Interjection enqueued with high priority
2. ✅ LLM classification for each intent type
3. ✅ Queue reprioritization
4. ✅ Capability items cleared on redirect
5. ✅ Cancel stops execution
6. ✅ Multiple interjections handled
7. ✅ HTTP integration end-to-end

---

## Total Implementation: 6-8 Days

**Breakdown**:
- Phase 1 (Core): 2-3 days
- Phase 2 (Queue): 1 day
- Phase 3 (HTTP): 1-2 days
- Phase 4 (Testing): 2 days

---

## Key Benefits

### 1. **Responsive UX**
- User can interrupt at any time
- No need to wait for long operations
- Immediate feedback on interjection

### 2. **Intelligent Routing**
- LLM understands user intent
- Automatic phase correction
- Context-aware responses

### 3. **Queue-Based Architecture**
- Single execution model (queue)
- No race conditions
- Predictable prioritization

### 4. **Minimal Code Changes**
- Leverages existing WorkQueue
- Extends, doesn't replace
- Backward compatible

---

## Related to Simple Adaptive Phase Routing

**This complements the adaptive routing system**:

1. **Heuristics** → Initial phase selection ✅
2. **User rejection at turn boundary** → Adaptive classification ✅
3. **User interjection DURING turn** → This design ⭐ NEW
4. **Natural handoff** → Phase transitions ✅

**All four mechanisms work together**:
```
Heuristics (fast, initial)
    ↓
User rejects at turn end → Adaptive LLM classification
    ↓
User interjects mid-turn → Interjection LLM classification (this design)
    ↓
Phase completes → Natural handoff
```

---

## LLM Intervention Opportunities Summary

**3 LLM Classification Points**:

1. **Adaptive Phase Classification** (turn boundary)
   - When: User rejects phase at end of turn
   - Input: User query, current phase, conversation history
   - Output: Correct target phase
   - Frequency: 5-10% of turns

2. **User Interjection Classification** (mid-turn) ⭐ **NEW**
   - When: User sends message during Ask execution
   - Input: User query, current phase, current action, conversation history
   - Output: InterjectionIntent (reject/redirect/clarify/cancel/continue/question)
   - Frequency: 2-5% of asks

3. **Frustration Detection** (optional, future)
   - When: Repeated failures or user dissatisfaction
   - Input: Query similarity, outcome patterns, sentiment
   - Output: Escalation decision
   - Frequency: <1% of asks

**Combined**: LLM invoked intelligently, not every turn, maximum responsiveness.

---

## Next Steps

1. **Review this design** - Confirm approach before implementation
2. **Phase 1 implementation** - Core interjection infrastructure
3. **Integration with adaptive routing** - Ensure both work together
4. **Testing with real scenarios** - Validate UX improvements

**Ready to start Phase 1 when approved!** 🚀
