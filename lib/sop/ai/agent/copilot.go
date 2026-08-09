package agent

import "time"

const (
	MaxMRUSize            = 20
	MaxExchangesInHistory = 20
	// selectedSpaceReinforcementWindow is a TTL for selected-space expert framing.
	// We only fully re-inject the Space guidance when the current Space has not been
	// reinforced recently, otherwise we keep the prompt compact for continuity.
	selectedSpaceReinforcementWindow = 30 * time.Minute
)

const (
	MRUSourceUnknown     = ""
	MRUSourcePersona     = "persona"
	MRUSourceSystemTools = "system_tools"
	MRUSourceAskOutcome  = "ask_outcome"
	MRUSourceAskProgress = "ask_progress"
	MRUSourcePlaybook    = "playbook"
)

const (
	MRUScopeSession = "session"
	MRUScopeAsk     = "ask"
)

const (
	maxProjectedPersonaEntries    = 2
	maxProjectedSystemToolEntries = 1
	maxProjectedAskOutcomeEntries = 10
	maxProjectedPlaybookEntries   = 4
)

const (
	ctxKeyDeferImplicitSessionTxClose = "_defer_implicit_session_tx_close"
	ctxKeyWorkingDir                  = "working_dir"
	ctxKeySelectedCapability          = "selected_capability"
	ctxKeyCapabilityResult            = "capability_result"
	ctxKeyActiveStep                  = "active_step"
)

const (
	askOutcomeMRUCategoryHeader          = "ASK_OUTCOME_HEADER"
	askOutcomeMRUCategoryDatabase        = "ASK_OUTCOME_DATABASE"
	askOutcomeMRUCategoryDomain          = "ASK_OUTCOME_DOMAIN"
	askOutcomeMRUCategoryQuery           = "ASK_OUTCOME_QUERY"
	askOutcomeMRUCategoryResult          = "ASK_OUTCOME_RESULT"
	askOutcomeMRUCategoryStoreSchema     = "ASK_OUTCOME_STORE_SCHEMA"
	askOutcomeMRUCategoryRelations       = "ASK_OUTCOME_STORE_RELATIONS"
	askOutcomeMRUCategoryJoinSelection   = "ASK_OUTCOME_JOIN_SELECTION"
	askOutcomeMRUCategoryFilterSelection = "ASK_OUTCOME_FILTER_SELECTION"
	askOutcomeMRUCategoryConfirmed       = "ASK_OUTCOME_CONFIRMED"
	askOutcomeMRUCategoryToolPattern     = "ASK_OUTCOME_TOOL_PATTERN"
	askOutcomeMRUCategoryCarryover       = "ASK_OUTCOME_CARRYOVER"
	askOutcomeMRUCategoryGuidance        = "ASK_OUTCOME_GUIDANCE"
	askProgressMRUCategoryHeader         = "ASK_PROGRESS_HEADER"
	askProgressMRUCategoryResult         = "ASK_PROGRESS_RESULT"
	askProgressMRUCategoryToolPattern    = "ASK_PROGRESS_TOOL_PATTERN"
	askProgressMRUCategoryConfirmed      = "ASK_PROGRESS_CONFIRMED"
	askProgressMRUCategoryGuidance       = "ASK_PROGRESS_GUIDANCE"
)

const personaSourceMRUCategoryPrefix = "PERSONA_SOURCE_"

// MRUItem represents a single category currently in working memory.
type MRUItem struct {
	Category     string
	LastAccessed int64
	Context      string
	Source       string
	Scope        string
}
