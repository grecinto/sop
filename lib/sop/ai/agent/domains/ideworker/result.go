package ideworker

import (
	"encoding/json"
	"sort"
	"strings"
)

// Standard capability identifiers a Backend implementation should advertise in
// SessionState.WorkerCapabilities. Ask uses these to decide when delegation is
// possible and, for CapabilityApplyPatch, when delegation should become mandatory
// (see docs/IDE_COPILOT_BACKEND_SDK.md).
const (
	CapabilityReadContext = "read_context"
	CapabilityApplyPatch  = "apply_patch"
	CapabilityEditCode    = "edit_code"
	CapabilityDiffReview  = "diff_review"
	CapabilityPlanChanges = "plan_changes"
)

// StandardCapabilities is the full set of well-known capability identifiers.
var StandardCapabilities = []string{
	CapabilityReadContext,
	CapabilityApplyPatch,
	CapabilityEditCode,
	CapabilityDiffReview,
	CapabilityPlanChanges,
}

// NormalizeCapabilities lowercases, trims, dedupes, and sorts a capability list.
func NormalizeCapabilities(caps []string) []string {
	if len(caps) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(caps))
	normalized := make([]string, 0, len(caps))
	for _, capability := range caps {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability == "" || seen[capability] {
			continue
		}
		seen[capability] = true
		normalized = append(normalized, capability)
	}
	sort.Strings(normalized)
	return normalized
}

// SupportsCapabilities reports whether available satisfies every entry in required.
func SupportsCapabilities(available []string, required ...string) bool {
	if len(required) == 0 {
		return true
	}
	have := make(map[string]bool, len(available))
	for _, capability := range available {
		have[strings.ToLower(strings.TrimSpace(capability))] = true
	}
	for _, capability := range required {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability == "" {
			continue
		}
		if !have[capability] {
			return false
		}
	}
	return true
}

// ModifiedFile is one file changed by a delegated task.
type ModifiedFile struct {
	FilePath        string         `json:"file_path,omitempty"`
	CRUDOperation   string         `json:"crud_operation,omitempty"`
	DiffSummary     string         `json:"diff_summary,omitempty"`
	DiffDelta       string         `json:"diff_delta,omitempty"`
	DiffFormat      string         `json:"diff_format,omitempty"`
	DiffArtifactRef string         `json:"diff_artifact_ref,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// Artifact references a large diff or supporting file too big to inline.
type Artifact struct {
	ID          string         `json:"id,omitempty"`
	Kind        string         `json:"kind,omitempty"`
	Path        string         `json:"path,omitempty"`
	Description string         `json:"description,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ExecutionResult is the canonical structured payload a Backend should return, as
// JSON text from ExecuteTask/StartTask (directly, or as the terminal "done" event
// of an NDJSON progress stream), so AI Studio can automatically drive undo
// checkpoints and prompt/diff history without any backend-specific parsing code.
type ExecutionResult struct {
	Status        string         `json:"status,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	ModifiedFiles []ModifiedFile `json:"modified_files,omitempty"`
	Artifacts     []Artifact     `json:"artifacts,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// ResultContract lets Ask tell a backend how it prefers results to be shaped,
// e.g. bounding inline diff size or preferring artifact references for large diffs.
type ResultContract struct {
	StreamProgress       bool `json:"stream_progress,omitempty"`
	IncludeModifiedFiles bool `json:"include_modified_files,omitempty"`
	IncludeDiffDeltas    bool `json:"include_diff_deltas,omitempty"`
	IncludeMetadata      bool `json:"include_metadata,omitempty"`
	PreferArtifactRefs   bool `json:"prefer_artifact_refs,omitempty"`
	MaxInlineDiffBytes   int  `json:"max_inline_diff_bytes,omitempty"`
}

// DefaultResultContract is the recommended shape for new backend integrations:
// inline diffs and metadata, but fall back to artifact references past 32KB.
func DefaultResultContract() ResultContract {
	return ResultContract{
		IncludeModifiedFiles: true,
		IncludeDiffDeltas:    true,
		IncludeMetadata:      true,
		PreferArtifactRefs:   true,
		MaxInlineDiffBytes:   32 * 1024,
	}
}

// ParseExecutionResult parses raw text returned by Backend.ExecuteTask (or the
// final NDJSON "done" event of an async StartTask stream) into an ExecutionResult.
// ok is false when raw is not a recognizable structured result, so callers can
// fall back to treating it as plain text instead of losing the response.
func ParseExecutionResult(raw string) (result ExecutionResult, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ExecutionResult{}, false
	}
	if err := json.Unmarshal([]byte(trimmed), &result); err == nil {
		if result.Status != "" || result.Summary != "" || len(result.ModifiedFiles) > 0 || len(result.Artifacts) > 0 {
			return result, true
		}
	}
	var envelope struct {
		Type   string          `json:"type"`
		Result ExecutionResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err == nil && envelope.Type == "done" {
		return envelope.Result, true
	}
	return ExecutionResult{}, false
}
