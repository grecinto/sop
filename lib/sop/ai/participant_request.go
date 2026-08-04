package ai

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const (
	ParticipantRequestIDVariableKey = "request_id"
	ParticipantTraceIDVariableKey   = "trace_id"
	ParticipantTraceIDPrefix        = "sop-participant-trace-"
)

// NewParticipantTraceID creates a SOP-owned trace id for delegated Ask-to-Ask
// flows. If this trace id comes back into SOP participant entrypoints, the
// request must be rejected to prevent recursive loops.
func NewParticipantTraceID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return ParticipantTraceIDPrefix + "fallback"
	}
	return ParticipantTraceIDPrefix + hex.EncodeToString(buf)
}

// IsSOPParticipantTraceID reports whether the trace id was minted by SOP for
// outbound Ask-to-Ask delegation.
func IsSOPParticipantTraceID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), ParticipantTraceIDPrefix)
}
