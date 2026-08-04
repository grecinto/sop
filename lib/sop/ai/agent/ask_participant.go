package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/sharedcode/sop/ai"
)

const participantNDJSONContentType = "application/x-ndjson"

// AskParticipant exposes the typed Ask contract for controller-to-participant
// delegation without forcing callers through the generic Agent interface.
type AskParticipant interface {
	AskWithRequest(ctx context.Context, req AskRequest) (AskResponse, error)
}

// ParticipantSession is the JSON-safe session shape for controllers that use
// SOP Ask as a delegated participant. It intentionally carries only portable
// session data, not process-local transaction handles.
type ParticipantSession struct {
	CurrentDB        string                 `json:"current_db,omitempty"`
	CurrentUserQuery string                 `json:"current_user_query,omitempty"`
	UserID           string                 `json:"user_id,omitempty"`
	ClientID         string                 `json:"client_id,omitempty"`
	SessionID        string                 `json:"session_id,omitempty"`
	AvatarScope      string                 `json:"avatar_scope,omitempty"`
	AgentID          string                 `json:"agent_id,omitempty"`
	ActiveDomain     string                 `json:"active_domain,omitempty"`
	ActiveRepo       string                 `json:"active_repo,omitempty"`
	SelectedKBs      []ai.ArtifactReference `json:"selected_kbs,omitempty"`
	Variables        map[string]any         `json:"variables,omitempty"`
}

// ParticipantRequest is the transport-safe Ask payload another controller can
// send when SOP Ask is acting as a participant.
type ParticipantRequest struct {
	RequestID        string              `json:"request_id,omitempty"`
	TraceID          string              `json:"trace_id,omitempty"`
	RootRequestID    string              `json:"root_request_id,omitempty"`
	Query            string              `json:"query"`
	Session          *ParticipantSession `json:"session,omitempty"`
	ProviderOverride *ProviderDetails    `json:"provider_override,omitempty"`
	ThinkingLevel    string              `json:"thinking_level,omitempty"`
	DefaultFormat    string              `json:"default_format,omitempty"`
	IsNewTopic       bool                `json:"is_new_topic,omitempty"`
	Options          map[string]any      `json:"options,omitempty"`
}

// ParticipantResponse is the structured Ask result returned to an external
// controller using SOP Ask as a participant.
type ParticipantResponse struct {
	FinalText      string              `json:"final_text"`
	UpdatedSession *ParticipantSession `json:"updated_session,omitempty"`
	ToolCalls      []ai.ToolCall       `json:"tool_calls,omitempty"`
	OutcomeFacts   []string            `json:"outcome_facts,omitempty"`
	OutcomeRecipes []ai.LearnedRecipe  `json:"outcome_recipes,omitempty"`
	Error          string              `json:"error,omitempty"`
}

// ParticipantStreamEvent is the NDJSON event envelope emitted when another
// controller requests streaming participant output from SOP Ask.
type ParticipantStreamEvent struct {
	Type     string               `json:"type"`
	Message  string               `json:"message,omitempty"`
	Data     any                  `json:"data,omitempty"`
	Response *ParticipantResponse `json:"response,omitempty"`
	Error    string               `json:"error,omitempty"`
}

type participantErrorResponse struct {
	Error string `json:"error"`
}

func (r ParticipantRequest) toAskRequest() AskRequest {
	var cfg *ai.ConfigMap
	if len(r.Options) > 0 || r.IsNewTopic {
		cfg = ai.NewConfigMap()
		for key, value := range r.Options {
			cfg.Set(key, value)
		}
		if r.IsNewTopic {
			cfg.Set("is_new_topic", true)
		}
	}

	traceID := strings.TrimSpace(r.TraceID)
	if traceID == "" {
		traceID = strings.TrimSpace(r.RootRequestID)
	}

	return AskRequest{
		Query:            r.Query,
		RequestID:        strings.TrimSpace(r.RequestID),
		TraceID:          traceID,
		Session:          participantSessionToAI(r.Session),
		ProviderOverride: r.ProviderOverride,
		ThinkingLevel:    r.ThinkingLevel,
		DefaultFormat:    r.DefaultFormat,
		Options:          cfg,
	}
}

func participantResponseFromAsk(resp AskResponse) ParticipantResponse {
	return ParticipantResponse{
		FinalText:      resp.FinalText,
		UpdatedSession: participantSessionFromAI(resp.UpdatedSession),
		ToolCalls:      resp.ToolCalls,
		OutcomeFacts:   resp.OutcomeFacts,
		OutcomeRecipes: resp.OutcomeRecipes,
	}
}

func participantSessionToAI(session *ParticipantSession) *ai.SessionPayload {
	if session == nil {
		return nil
	}
	return &ai.SessionPayload{
		CurrentDB:        session.CurrentDB,
		CurrentUserQuery: session.CurrentUserQuery,
		UserID:           session.UserID,
		ClientID:         session.ClientID,
		SessionID:        session.SessionID,
		AvatarScope:      session.AvatarScope,
		AgentID:          session.AgentID,
		ActiveDomain:     session.ActiveDomain,
		ActiveRepo:       session.ActiveRepo,
		SelectedKBs:      session.SelectedKBs,
		Variables:        session.Variables,
	}
}

func participantSessionFromAI(session *ai.SessionPayload) *ParticipantSession {
	if session == nil {
		return nil
	}
	return &ParticipantSession{
		CurrentDB:        session.CurrentDB,
		CurrentUserQuery: session.CurrentUserQuery,
		UserID:           session.UserID,
		ClientID:         session.ClientID,
		SessionID:        session.SessionID,
		AvatarScope:      session.AvatarScope,
		AgentID:          session.AgentID,
		ActiveDomain:     session.ActiveDomain,
		ActiveRepo:       session.ActiveRepo,
		SelectedKBs:      session.SelectedKBs,
		Variables:        session.Variables,
	}
}

// NewParticipantHandler returns an HTTP handler that exposes SOP Ask as a
// typed participant endpoint for another controller.
func NewParticipantHandler(participant AskParticipant) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if participant == nil {
			w.Header().Set("Content-Type", "application/json")
			writeParticipantError(w, http.StatusServiceUnavailable, "ask participant is not configured")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			writeParticipantError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		defer r.Body.Close()
		var req ParticipantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			writeParticipantError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
			return
		}
		if strings.TrimSpace(req.Query) == "" {
			w.Header().Set("Content-Type", "application/json")
			writeParticipantError(w, http.StatusBadRequest, "query is required")
			return
		}
		traceID := strings.TrimSpace(req.TraceID)
		if traceID == "" {
			traceID = strings.TrimSpace(req.RootRequestID)
		}
		if ai.IsSOPParticipantTraceID(traceID) {
			w.Header().Set("Content-Type", "application/json")
			writeParticipantError(w, http.StatusConflict, fmt.Sprintf("refusing participant request because trace_id %q was generated by SOP and would recurse into Ask-to-Ask delegation", traceID))
			return
		}

		if participantAcceptsNDJSON(r) {
			handleParticipantStream(w, r, participant, req)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		resp, err := participant.AskWithRequest(r.Context(), req.toAskRequest())
		if err != nil {
			writeParticipantError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if err := json.NewEncoder(w).Encode(participantResponseFromAsk(resp)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

func participantAcceptsNDJSON(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), participantNDJSONContentType)
}

func handleParticipantStream(w http.ResponseWriter, r *http.Request, participant AskParticipant, req ParticipantRequest) {
	w.Header().Set("Content-Type", participantNDJSONContentType)
	streamer := newParticipantStreamWriter(w)
	askReq := req.toAskRequest()
	askReq.Writer = streamer.contentWriter()
	askReq.EventStreamer = func(eventType string, data any) {
		streamer.send(ParticipantStreamEvent{Type: eventType, Data: data})
	}
	askReq.ProgressSink = func(message string) {
		streamer.send(ParticipantStreamEvent{Type: "progress", Message: message})
	}

	resp, err := participant.AskWithRequest(r.Context(), askReq)
	if err != nil {
		streamer.send(ParticipantStreamEvent{Type: "participant_error", Error: err.Error()})
		streamer.send(ParticipantStreamEvent{Type: "done"})
		return
	}
	finalResp := participantResponseFromAsk(resp)
	streamer.send(ParticipantStreamEvent{Type: "final", Response: &finalResp})
	streamer.send(ParticipantStreamEvent{Type: "done"})
}

func writeParticipantError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(participantErrorResponse{Error: message})
}

type participantStreamWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	flush   func()
}

func newParticipantStreamWriter(w http.ResponseWriter) *participantStreamWriter {
	writer := &participantStreamWriter{encoder: json.NewEncoder(w)}
	if flusher, ok := w.(http.Flusher); ok {
		writer.flush = flusher.Flush
	}
	return writer
}

func (w *participantStreamWriter) send(event ParticipantStreamEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.encoder.Encode(event)
	if w.flush != nil {
		w.flush()
	}
}

func (w *participantStreamWriter) contentWriter() io.Writer {
	return participantContentWriter{stream: w}
}

type participantContentWriter struct {
	stream *participantStreamWriter
}

func (w participantContentWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if message != "" {
		w.stream.send(ParticipantStreamEvent{Type: "content", Message: message})
	}
	return len(p), nil
}

// ParticipantClient lets a higher-level controller invoke SOP Ask over HTTP
// while SOP remains a delegated participant runtime.
type ParticipantClient struct {
	URL        string
	HTTPClient *http.Client
	Headers    http.Header
}

// Ask sends a participant request to a remote SOP Ask endpoint and returns the
// structured response.
func (c *ParticipantClient) Ask(ctx context.Context, req ParticipantRequest) (ParticipantResponse, error) {
	if strings.TrimSpace(c.URL) == "" {
		return ParticipantResponse{}, fmt.Errorf("participant URL is required")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ParticipantResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return ParticipantResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, values := range c.Headers {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return ParticipantResponse{}, err
	}
	defer httpResp.Body.Close()

	var resp ParticipantResponse
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		var errResp participantErrorResponse
		if decodeErr := json.NewDecoder(httpResp.Body).Decode(&errResp); decodeErr == nil && strings.TrimSpace(errResp.Error) != "" {
			return ParticipantResponse{}, fmt.Errorf("participant ask failed: %s", errResp.Error)
		}
		return ParticipantResponse{}, fmt.Errorf("participant ask failed with status %d", httpResp.StatusCode)
	}

	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return ParticipantResponse{}, err
	}
	if strings.TrimSpace(resp.Error) != "" {
		return ParticipantResponse{}, fmt.Errorf("participant ask failed: %s", resp.Error)
	}
	return resp, nil
}

// AskStream invokes a remote SOP Ask participant endpoint in NDJSON mode,
// forwarding each streamed event to onEvent and returning the final response.
func (c *ParticipantClient) AskStream(ctx context.Context, req ParticipantRequest, onEvent func(ParticipantStreamEvent) error) (ParticipantResponse, error) {
	if strings.TrimSpace(c.URL) == "" {
		return ParticipantResponse{}, fmt.Errorf("participant URL is required")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ParticipantResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return ParticipantResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", participantNDJSONContentType)
	for key, values := range c.Headers {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return ParticipantResponse{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		var errResp participantErrorResponse
		if decodeErr := json.NewDecoder(httpResp.Body).Decode(&errResp); decodeErr == nil && strings.TrimSpace(errResp.Error) != "" {
			return ParticipantResponse{}, fmt.Errorf("participant ask failed: %s", errResp.Error)
		}
		return ParticipantResponse{}, fmt.Errorf("participant ask failed with status %d", httpResp.StatusCode)
	}

	var finalResp ParticipantResponse
	var streamErr string
	decoder := json.NewDecoder(bufio.NewReader(httpResp.Body))
	for {
		var event ParticipantStreamEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return finalResp, err
		}
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return finalResp, err
			}
		}
		switch event.Type {
		case "final":
			if event.Response != nil {
				finalResp = *event.Response
			}
		case "participant_error":
			streamErr = strings.TrimSpace(event.Error)
		}
	}
	if streamErr != "" {
		return finalResp, fmt.Errorf("participant ask failed: %s", streamErr)
	}
	return finalResp, nil
}
