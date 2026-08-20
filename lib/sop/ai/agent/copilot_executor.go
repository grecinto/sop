package agent

import (
	"context"
	"fmt"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent/capabilities"
	"github.com/sharedcode/sop/ai/agent/classification"
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
	"github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/generator"
	"github.com/sharedcode/sop/ai/memory"
)

// NewCopilotAgent creates a new instance of CopilotAgent.
func NewCopilotAgent(cfg Config, databases map[string]sop.DatabaseOptions, systemDB *database.Database, opts ...CapabilityOption) *CopilotAgent {
	// Initialize the "Brain" (Generator) from configuration
	var gen ai.Generator
	var err error

	if cfg.Generator.Type != "" {
		gen, err = generator.New(cfg.Generator.Type, cfg.Generator.Options)
		if err != nil {
			log.Error(fmt.Sprintf("Failed to initialize generator from config (Type: %s): %v", cfg.Generator.Type, err))
		}
	}

	if err != nil {
		log.Error("Failed to initialize AI generator", "error", err)
	}

	agent := &CopilotAgent{
		Config:          cfg,
		brain:           gen,
		registry:        NewRegistry(),
		databases:       databases,
		systemDB:        systemDB,
		Memory:          memory.NewMemoryUnit(ai.AgentIDOmni),
		compiledScripts: make(map[string]CachedScript),
	}
	agent.ensureCapabilityRegistry()

	// Gap 1: Initialize authorization manager (default to GateSystemAccess for local mode)
	agent.authzManager = capabilities.NewAuthorizationManager(capabilities.GateSystemAccess)

	// Gap 3: Initialize lifecycle manager
	agent.lifecycleManager = capabilities.NewLifecycleManager(agent.capabilityRegistry)

	// Gap 2: Initialize classification engine with chained classifiers
	agent.initializeClassificationEngine()

	if cfg.ProductionMode {
		agent.EnablePhase5(nil, nil, nil, nil)
	}

	// Wire default coding workflow SharedInfrastructure with MemorySink for LTM integration
	sharedInfra := cw.NewDefaultSharedInfrastructure()
	sharedInfra.MemorySink = func(mem cw.CodingMemory) {
		if agent != nil {
			agent.recordCodingMemory(mem)
		}
	}
	// Inject LTM accessor for team patterns
	sharedInfra.LTMAccessor = func(ctx context.Context) (any, error) {
		if agent != nil {
			return agent.getOrCreateLTM(ctx)
		}
		return nil, fmt.Errorf("agent not initialized")
	}
	// Inject database accessor for team patterns vectorization
	sharedInfra.DatabaseAccessor = func(ctx context.Context) (any, error) {
		if agent != nil && agent.systemDB != nil {
			return agent.systemDB, nil
		}
		return nil, fmt.Errorf("systemDB not available")
	}
	// Inject LTM name accessor
	sharedInfra.LTMName = func() string {
		if agent != nil {
			agent.Memory.BindSession(context.Background())
			return agent.Memory.LongTermMemoryName()
		}
		return ""
	}
	// Inject LLM accessor
	sharedInfra.LLMAccessor = func(ctx context.Context) (any, error) {
		if agent != nil && agent.brain != nil {
			return agent.brain, nil
		}
		return nil, fmt.Errorf("LLM (brain) not available")
	}
	sharedInfra.PlanningContextResolver = agent.resolvePlanningContextForDomain
	cw.SetDefaultPlanningContextResolver(sharedInfra.PlanningContextResolver)
	sharedInfra.Planner = cw.NewLLMPlanner(sharedInfra.LLMAccessor, sharedInfra.Planner)
	// Inject embedder accessor
	sharedInfra.EmbedderAccessor = func(ctx context.Context) (any, error) {
		if agent != nil && agent.service != nil && agent.service.Domain() != nil {
			embedder := agent.service.Domain().Embedder()
			if embedder != nil {
				return embedder, nil
			}
		}
		return nil, fmt.Errorf("embedder not available")
	}
	// Phase capabilities now handle tool registration
	for _, opt := range opts {
		if opt != nil {
			opt(agent)
		}
	}
	// Tools are registered dynamically per Ask/Execute/ListTools call so the registry
	// can be scoped by the active routing state instead of being pre-populated globally.

	return agent
}

// SetService sets the reference to the main service (for cache invalidation).
func (a *CopilotAgent) SetService(s *Service) {
	a.service = s
}

// initializeClassificationEngine sets up the chained classification engine (Gap 2)
func (a *CopilotAgent) initializeClassificationEngine() {
	// MRU classifier (Priority 1 - highest)
	// Simplified: Returns empty for now (full MRU integration comes later)
	mruClassifier := classification.NewMRUClassifier(func(ctx context.Context) (string, string, bool) {
		// TODO: Integrate with actual MRU tracking in agent Memory
		return "", "", false
	})

	// Keyword classifier (Priority 2)
	keywordClassifier := classification.NewKeywordClassifier()

	// LLM classifier (Priority 3 - lowest, fallback)
	// For now, we use a no-op LLM classifier (LLM integration comes later)
	llmClassifier := classification.NewLLMClassifier(nil)

	// Create chained classifier
	a.classificationEngine = classification.NewChainedClassifier(
		mruClassifier,
		keywordClassifier,
		llmClassifier,
	)

	// Register approval handlers (Gap 1)
	a.authzManager.RegisterApprovalHandler(capabilities.ApprovalFileWrite, a.handleFileWriteApproval)
	a.authzManager.RegisterApprovalHandler(capabilities.ApprovalShellExec, a.handleShellExecApproval)
	a.authzManager.RegisterApprovalHandler(capabilities.ApprovalDBMutation, a.handleDBMutationApproval)
	a.authzManager.RegisterApprovalHandler(capabilities.ApprovalSecureInput, a.handleSecureInputApproval)
}

// handleFileWriteApproval prompts user for file write approval (Gap 1)
// Future: Integrate with chat UI to prompt user with:
//
//	"⚠️ Approve file write to: {resource}?"
//	[Approve] [Deny]
func (a *CopilotAgent) handleFileWriteApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
	// For now, auto-approve in local mode (future: prompt user via UI)
	log.Info("File write approval requested", "type", opCtx.OperationType, "resource", opCtx.Resource)
	return true, nil
}

// handleShellExecApproval prompts user for shell execution approval (Gap 1)
// Future: Integrate with chat UI to prompt user with:
//
//	"⚠️ Approve shell command: {command}?"
//	[Approve] [Deny]
//
// If opCtx.SecureInputRequired=true, show privacy notice:
//
//	"🔒 This command requires secure input (password/token)"
//	"✓ Input will be sent DIRECTLY to shell (not to LLM)"
//	"✓ Your sensitive data will NOT be visible in chat history"
//	Enter value: [secure input field]
func (a *CopilotAgent) handleShellExecApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
	// For now, auto-approve in local mode (future: prompt user via UI)
	if opCtx.SecureInputRequired {
		log.Info("🔒 Secure input approval requested",
			"type", opCtx.OperationType,
			"prompt", opCtx.SecureInputPrompt,
			"privacy", "input_bypasses_llm")
	} else {
		log.Info("Shell execution approval requested", "type", opCtx.OperationType, "command", opCtx.Resource)
	}
	return true, nil
}

// handleDBMutationApproval prompts user for database mutation approval (Gap 1)
// Future: Integrate with chat UI to prompt user with:
//
//	"⚠️ Approve database mutation: {operation}?"
//	[Approve] [Deny]
func (a *CopilotAgent) handleDBMutationApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
	// For now, auto-approve in local mode (future: prompt user via UI)
	log.Info("Database mutation approval requested", "type", opCtx.OperationType, "resource", opCtx.Resource)
	return true, nil
}

// handleSecureInputApproval prompts user for secure data entry with privacy guarantee (Gap 1)
// This handler is invoked when a capability needs sensitive input (passwords, API keys, tokens)
//
// Privacy-First Data Flow:
//  1. User is prompted via chat UI with CLEAR privacy notice
//  2. User enters sensitive data in chat editbox
//  3. Frontend sends data DIRECTLY to tool via special secure channel
//  4. LLM NEVER sees the sensitive data (not in prompt, not in history)
//  5. Data is passed to shell/tool execution only
//
// Example UI Flow:
//
//	Agent: "🔒 Secure Input Required"
//	Agent: "Command needs: database password"
//	Agent: "✓ Your input will be sent DIRECTLY to the command"
//	Agent: "✓ The AI model will NOT see your password"
//	Agent: "✓ Not stored in chat history"
//	User: [types password in editbox with masked display]
//	System: [routes to shell, bypasses LLM context]
func (a *CopilotAgent) handleSecureInputApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
	// For now, auto-approve in local mode (future: prompt user via secure UI flow)
	log.Info("🔒 Secure input approval requested",
		"type", opCtx.OperationType,
		"prompt", opCtx.SecureInputPrompt,
		"privacy_guarantee", "input_bypasses_llm_context",
		"flow", "user_prompt_via_chat_ui")
	return true, nil
}

// shouldObfuscate determines if obfuscation should be applied for a given database.
func (a *CopilotAgent) shouldObfuscate(dbName string) bool {
	// Look up database options
	if opts, ok := a.databases[dbName]; ok {
		return opts.EnableObfuscation
	}
	// Fallback to mode if no DB options found (e.g. legacy or system)
	// But usually systemDB is also in databases map.
	return false
}

// SetVerbose enables or disables verbose output.
func (a *CopilotAgent) SetVerbose(v bool) {
	a.Config.Verbose = v
}

// Open initializes the agent's resources.
func (a *CopilotAgent) Open(ctx context.Context) error {
	p := ai.GetSessionPayload(ctx)

	// If CurrentDB is set, start a transaction
	if p != nil && p.CurrentDB != "" {
		dbName := p.CurrentDB

		log.Debug(fmt.Sprintf("CopilotAgent.Open: Checking DB '%s', SystemDB available: %v", dbName, a.systemDB != nil))

		// Check for system DB
		if dbName == SystemDBName && a.systemDB != nil {
			if p.Transaction == nil {
				tx, err := a.systemDB.BeginTransaction(ctx, sop.ForWriting)
				if err != nil {
					return fmt.Errorf("failed to begin transaction on system database: %w", err)
				}
				p.Transaction = tx
			}
		} else if dbOpts, ok := a.databases[dbName]; ok {
			db := database.NewDatabase(dbOpts)
			if p.Transaction == nil {
				tx, err := db.BeginTransaction(ctx, sop.ForWriting)
				if err != nil {
					return fmt.Errorf("failed to begin transaction on database '%s': %w", dbName, err)
				}
				p.Transaction = tx
			}
		} else {
			return fmt.Errorf("database '%s' not found in agent configuration", dbName)
		}
	}

	// Initialize strict physical cognitive buffers only for service-backed sessions.
	// Standalone tests and lightweight agent instances can run without the background
	// STM worker, which avoids leaking on-disk state into temp directories.
	if a.service != nil || ai.GetSessionPayload(ctx) != nil {
		if err := a.InitializePhysicalMemory(ctx); err != nil {
			log.Warn("CopilotAgent: STM Initialization failure", "error", err)
		}
	}

	// Tools are registered per-turn by Phase Factory after classification.
	// No global registration at initialization.

	// Gap 3: Enable all registered capabilities
	if a.lifecycleManager != nil && a.capabilityRegistry != nil {
		for _, cap := range a.capabilityRegistry.ListAll() {
			if err := a.lifecycleManager.EnableCapability(ctx, cap.ID()); err != nil {
				log.Warn("Failed to enable capability", "capability", cap.ID(), "error", err)
			}
		}
	}

	return nil
}

// Close cleans up the agent's resources.
func (a *CopilotAgent) Close(ctx context.Context) error {
	// Gap 3: Shutdown all capabilities gracefully
	if a.lifecycleManager != nil {
		if err := a.lifecycleManager.ShutdownAll(ctx); err != nil {
			log.Warn("Error during capability shutdown", "error", err)
		}
	}
	if a != nil && a.service != nil && a.service.session != nil && a.service.session.Memory != nil {
		a.recordSessionBoundary(ctx, string(SessionBoundaryEnd), "Session ended", "agent closed")
	}

	if a != nil && a.memoryWorkersCancel != nil {
		a.memoryWorkersCancel()
		a.memoryWorkersCancel = nil
	}
	if a != nil && a.Memory != nil {
		if stm := a.Memory.STMStore(); stm != nil {
			memory.UnregisterShortTermMemoryStore(stm)
		}
		a.Memory.CloseShortTermMemory()
	}

	// Transaction lifecycle is managed by Service.Close(), not here.
	// CopilotAgent has no agent-specific cleanup needed.
	return nil
}

// Search performs a search using the agent's capabilities.
func (a *CopilotAgent) Search(ctx context.Context, query string, limit int) ([]ai.Hit[map[string]any], error) {
	return []ai.Hit[map[string]any]{}, nil
}

// Ask is the primary entry point for processing a user's conversational query to the AI Copilot.
//
// The functional flow follows a multi-layered ReAct architecture:
//
//  1. Generator Resolution:
//     Dynamically selects the appropriate LLM provider (e.g., Gemini, OpenAI, Claude) based on
//     the active session context or falling back to the system default configuration.
//
//  2. Direct Invocation Handling:
//     Checks if the query is a direct tool invocation (e.g., "/help" or "/clear_memory").
//     If so, it bypasses the LLM reasoning loop to immediately execute the deterministic command.
//
//  3. Intent Classification (Router):
//     Evaluates if the query should be routed to a specific sub-agent (Avatar/Persona).
//     If the intent indicates a specialized Avatar (e.g., a "Legal Auditor" persona), it delegates
//     execution entirely to that sub-agent.
//
//  4. Context Classification:
//     For generic queries (intent == "OMNI"), it performs a lightweight three-gate classification to
//     identify the semantic domain (e.g., "Spaces" or "Stores"), likely artifacts, and CRUD layer.
//     This stage is classification-only: it updates routing state, but does not assemble prompt slices
//     or mutate System_Tools context.
//
//  5. Episode Metadata Tracking (MRU Cache):
//     Analyzes the user's prior chat exchange inside the short-term episodic memory. If the user
//     remains engaged in the same topic and database context, it pulls the Most-Recently-Used (MRU)
//     semantic boundaries so the LLM retains coherent situational context across turns.
//
//  6. System Prompt Construction:
//     Assembles the multi-part context prompt (using SystemPromptBuilder) linking the Core Persona,
//     Active Playbooks/KBs, stable System Tools context, targeted focused execution/tool guidance
//     derived from the current classification, semantic memory boundaries, and conversation history.
//
//  7. Reasoning Engine Delegation:
//     Packages the assembled context and delegates execution to the ReAct engine. The engine loops
//     autonomously over the LLM generation and local tool executions (API-level tool calling) until
//     it produces a final answer.
//
//  8. Epilogue & Cleanup:
//     Records the completed dialogue and active track-state into the short-term memory transcript,
//     clears the volatile MRU buffer, and returns the final text response to the client.
//
// Ask is the main entry point for processing queries. For single-turn execution only.
// For multi-turn/workflow execution, use entry points that create queues internally.
// Ask is the public API that processes queries through a session queue.
// The queue enables workflow automation where capabilities can enqueue next steps.
// ReAct loop handles multi-turn execution and clarifications natively.
// Ask is the main entry point for Copilot queries and delegates to the
// canonical LLM-as-Orchestrator + Phase Factory pipeline.
