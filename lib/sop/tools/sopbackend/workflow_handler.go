package sopbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
	aidb "github.com/sharedcode/sop/ai/database"
	"github.com/sharedcode/sop/ai/memory"
)

// SaveWorkflowRequest represents a workflow save request
type SaveWorkflowRequest struct {
	Workflow *agent.WorkflowDefinition `json:"workflow"`
}

// SaveWorkflowResponse represents a workflow save response
type SaveWorkflowResponse struct {
	Success      bool                      `json:"success"`
	Message      string                    `json:"message,omitempty"`
	Workflow     *agent.WorkflowDefinition `json:"workflow,omitempty"`
	PublishState string                    `json:"publish_state,omitempty"`
	LastSavedAt  string                    `json:"last_saved_at,omitempty"`
}

// PublishWorkflowRequest represents a workflow publish request
type PublishWorkflowRequest struct {
	WorkflowID    string `json:"workflow_id"`
	CategoryPath  string `json:"category_path"`
	TargetVersion int    `json:"target_version"` // 0 = auto-increment, >0 = specific version
}

// PublishWorkflowResponse represents a workflow publish response
type PublishWorkflowResponse struct {
	Success           bool   `json:"success"`
	Message           string `json:"message,omitempty"`
	PublishedVersion  string `json:"published_version,omitempty"`
	FederationVersion int    `json:"federation_version,omitempty"`
	LastPublishedAt   string `json:"last_published_at,omitempty"`
}

// ListWorkflowsRequest represents a workflow list request
type ListWorkflowsRequest struct {
	CategoryPath string `json:"category_path,omitempty"` // Filter by category
	PublishState string `json:"publish_state,omitempty"` // Filter by state
}

// ListWorkflowsResponse represents a workflow list response
type ListWorkflowsResponse struct {
	Success   bool               `json:"success"`
	Message   string             `json:"message,omitempty"`
	Workflows []WorkflowListItem `json:"workflows,omitempty"`
}

// WorkflowListItem represents a workflow in the list
type WorkflowListItem struct {
	ID                   string `json:"workflow_id"`
	Name                 string `json:"name"`
	CategoryPath         string `json:"category_path"`
	Description          string `json:"description"`
	Version              string `json:"version"`
	PublishState         string `json:"publish_state"`
	LastPublishedVersion string `json:"last_published_version,omitempty"`
	LastPublishedAt      string `json:"last_published_at,omitempty"`
	LastUpdated          string `json:"last_updated,omitempty"`
}

// GetWorkflowRequest represents a single workflow get request
type GetWorkflowRequest struct {
	WorkflowID   string `json:"workflow_id,omitempty"`
	CategoryPath string `json:"category_path,omitempty"`
}

// GetWorkflowResponse represents a single workflow get response
type GetWorkflowResponse struct {
	Success  bool                      `json:"success"`
	Message  string                    `json:"message,omitempty"`
	Workflow *agent.WorkflowDefinition `json:"workflow,omitempty"`
}

// handleSaveWorkflow saves a workflow locally without publishing to HQ
func handleSaveWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SA authorization
	authCtx := sop.GetAuthFromContext(r.Context())
	isSA := false
	for _, role := range authCtx.Roles {
		if strings.EqualFold(role, sop.RoleSystemAdmin) {
			isSA = true
			break
		}
	}
	if !isSA {
		sendJSON(w, SaveWorkflowResponse{
			Success: false,
			Message: "Only SystemAdmin users can save workflows",
		}, http.StatusForbidden)
		return
	}

	var req SaveWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, SaveWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid request: %v", err),
		}, http.StatusBadRequest)
		return
	}

	if req.Workflow == nil {
		sendJSON(w, SaveWorkflowResponse{
			Success: false,
			Message: "Workflow is required",
		}, http.StatusBadRequest)
		return
	}

	// Get system database
	ctx := r.Context()
	db, llm, embedder, err := getSystemResources(ctx, r)
	if err != nil {
		sendJSON(w, SaveWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get system resources: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	// Save workflow
	if err := agent.SaveWorkflow(ctx, db, llm, embedder, req.Workflow); err != nil {
		log.Printf("Failed to save workflow: %v (workflow_id=%s)", err, req.Workflow.ID)
		sendJSON(w, SaveWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to save workflow: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	sendJSON(w, SaveWorkflowResponse{
		Success:      true,
		Message:      "Workflow saved successfully",
		Workflow:     req.Workflow,
		PublishState: req.Workflow.PublishState,
		LastSavedAt:  req.Workflow.Metadata.LastUpdated,
	})
}

// handlePublishWorkflow publishes a workflow to HQ with version control
func handlePublishWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SA authorization
	authCtx := sop.GetAuthFromContext(r.Context())
	isSA := false
	for _, role := range authCtx.Roles {
		if strings.EqualFold(role, sop.RoleSystemAdmin) {
			isSA = true
			break
		}
	}
	if !isSA {
		sendJSON(w, PublishWorkflowResponse{
			Success: false,
			Message: "Only SystemAdmin users can publish workflows to HQ",
		}, http.StatusForbidden)
		return
	}

	var req PublishWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, PublishWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid request: %v", err),
		}, http.StatusBadRequest)
		return
	}

	if req.WorkflowID == "" || req.CategoryPath == "" {
		sendJSON(w, PublishWorkflowResponse{
			Success: false,
			Message: "workflow_id and category_path are required",
		}, http.StatusBadRequest)
		return
	}

	// Get system database
	ctx := r.Context()
	db, llm, embedder, err := getSystemResources(ctx, r)
	if err != nil {
		sendJSON(w, PublishWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get system resources: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	// Load workflow from KB
	workflow, err := loadWorkflowFromKB(ctx, db, llm, embedder, req.CategoryPath)
	if err != nil {
		sendJSON(w, PublishWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to load workflow: %v", err),
		}, http.StatusNotFound)
		return
	}

	// Get daemon ID
	daemonID := os.Getenv("SOP_DAEMON_ID")
	if daemonID == "" {
		daemonID = "local-daemon"
	}

	// Publish to HQ
	if err := agent.PublishWorkflowToHQ(
		ctx,
		db,
		llm,
		embedder,
		workflow,
		daemonID,
		authCtx.UserID,
		req.TargetVersion,
	); err != nil {
		log.Printf("Failed to publish workflow: %v (workflow_id=%s)", err, req.WorkflowID)
		sendJSON(w, PublishWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to publish workflow: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	// Get federation version from workflow metadata
	federationVersion := 0
	if workflow.Metadata != nil {
		federationVersion = workflow.Metadata.FederationVersion
	}

	sendJSON(w, PublishWorkflowResponse{
		Success:           true,
		Message:           "Workflow published to HQ successfully",
		PublishedVersion:  workflow.LastPublishedVersion,
		FederationVersion: federationVersion,
		LastPublishedAt:   workflow.LastPublishedAt,
	}, http.StatusOK)
}

// handleListWorkflows lists workflows in Core Playbook KB
func handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SA authorization
	authCtx := sop.GetAuthFromContext(r.Context())
	isSA := false
	for _, role := range authCtx.Roles {
		if strings.EqualFold(role, sop.RoleSystemAdmin) {
			isSA = true
			break
		}
	}
	if !isSA {
		sendJSON(w, ListWorkflowsResponse{
			Success: false,
			Message: "Only SystemAdmin users can list workflows",
		}, http.StatusForbidden)
		return
	}

	var req ListWorkflowsRequest
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Ignore decode error for GET requests
		}
	}

	// Get system database
	ctx := r.Context()
	db, llm, embedder, err := getSystemResources(ctx, r)
	if err != nil {
		sendJSON(w, ListWorkflowsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get system resources: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	// List workflows from Core Playbook
	workflows, err := listWorkflowsFromKB(ctx, db, llm, embedder, req.CategoryPath, req.PublishState)
	if err != nil {
		log.Printf("Failed to list workflows: %v", err)
		sendJSON(w, ListWorkflowsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to list workflows: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	sendJSON(w, ListWorkflowsResponse{
		Success:   true,
		Workflows: workflows,
	}, http.StatusOK)
}

// handleGetWorkflow gets a single workflow by ID or category path
func handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check SA authorization
	authCtx := sop.GetAuthFromContext(r.Context())
	isSA := false
	for _, role := range authCtx.Roles {
		if strings.EqualFold(role, sop.RoleSystemAdmin) {
			isSA = true
			break
		}
	}
	if !isSA {
		sendJSON(w, GetWorkflowResponse{
			Success: false,
			Message: "Only SystemAdmin users can get workflows",
		}, http.StatusForbidden)
		return
	}

	var req GetWorkflowRequest
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, GetWorkflowResponse{
				Success: false,
				Message: fmt.Sprintf("Invalid request: %v", err),
			}, http.StatusBadRequest)
			return
		}
	} else {
		// GET method: read from query params
		req.WorkflowID = r.URL.Query().Get("workflow_id")
		req.CategoryPath = r.URL.Query().Get("category_path")
	}

	if req.WorkflowID == "" && req.CategoryPath == "" {
		sendJSON(w, GetWorkflowResponse{
			Success: false,
			Message: "workflow_id or category_path is required",
		}, http.StatusBadRequest)
		return
	}

	// Get system database
	ctx := r.Context()
	db, llm, embedder, err := getSystemResources(ctx, r)
	if err != nil {
		sendJSON(w, GetWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get system resources: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	// Load workflow
	categoryPath := req.CategoryPath
	if categoryPath == "" {
		// Need to search for workflow by ID (not implemented yet - would need full KB scan)
		sendJSON(w, GetWorkflowResponse{
			Success: false,
			Message: "Search by workflow_id not yet implemented, use category_path",
		}, http.StatusNotImplemented)
		return
	}

	workflow, err := loadWorkflowFromKB(ctx, db, llm, embedder, categoryPath)
	if err != nil {
		sendJSON(w, GetWorkflowResponse{
			Success: false,
			Message: fmt.Sprintf("Workflow not found: %v", err),
		}, http.StatusNotFound)
		return
	}

	sendJSON(w, GetWorkflowResponse{
		Success:  true,
		Workflow: workflow,
	}, http.StatusOK)
}

// Helper functions

func sendJSON(w http.ResponseWriter, data interface{}, statusCode ...int) {
	w.Header().Set("Content-Type", "application/json")
	if len(statusCode) > 0 {
		w.WriteHeader(statusCode[0])
	}
	json.NewEncoder(w).Encode(data)
}

func getSystemResources(ctx context.Context, r *http.Request) (*aidb.Database, ai.Generator, ai.Embeddings, error) {
	db, err := getSystemDB(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get system database: %w", err)
	}

	// Get configured LLM and embedder from request
	llm := GetConfiguredLLM(r)
	embedder := GetConfiguredEmbedder(r)

	return db, llm, embedder, nil
}

func loadWorkflowFromKB(ctx context.Context, db *aidb.Database, llm ai.Generator, embedder ai.Embeddings, categoryPath string) (*agent.WorkflowDefinition, error) {
	tx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, "core_playbook", tx, llm, embedder, false, false)
	if err != nil {
		return nil, fmt.Errorf("open core_playbook KB: %w", err)
	}

	// Search by category path
	results, err := kb.SearchByPath(ctx, []memory.PathSearchParam{
		{CategoryPath: categoryPath, SearchText: ""},
	})
	if err != nil {
		return nil, fmt.Errorf("search by path: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("workflow not found at path %s", categoryPath)
	}

	// Parse workflow from item data
	item := &results[0]
	workflowBytes, err := json.Marshal(item.Data)
	if err != nil {
		return nil, fmt.Errorf("marshal item data: %w", err)
	}

	var workflow agent.WorkflowDefinition
	if err := json.Unmarshal(workflowBytes, &workflow); err != nil {
		return nil, fmt.Errorf("unmarshal workflow: %w", err)
	}

	return &workflow, nil
}

func listWorkflowsFromKB(ctx context.Context, db *aidb.Database, llm ai.Generator, embedder ai.Embeddings, categoryPath, publishState string) ([]WorkflowListItem, error) {
	tx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, "core_playbook", tx, llm, embedder, false, false)
	if err != nil {
		return nil, fmt.Errorf("open core_playbook KB: %w", err)
	}

	// Search workflows - if no category path specified, search for all workflows
	searchPath := categoryPath
	if searchPath == "" {
		searchPath = "Playbooks/"
	}

	results, err := kb.SearchByPath(ctx, []memory.PathSearchParam{
		{CategoryPath: searchPath, SearchText: ""},
	})
	if err != nil {
		return nil, fmt.Errorf("search workflows: %w", err)
	}

	var workflows []WorkflowListItem
	for _, item := range results {
		// Parse workflow
		workflowBytes, err := json.Marshal(item.Data)
		if err != nil {
			continue
		}

		var workflow agent.WorkflowDefinition
		if err := json.Unmarshal(workflowBytes, &workflow); err != nil {
			continue
		}

		// Filter by publish state if specified
		if publishState != "" && workflow.PublishState != publishState {
			continue
		}

		// Get last updated from metadata
		lastUpdated := ""
		if workflow.Metadata != nil {
			lastUpdated = workflow.Metadata.LastUpdated
		}

		workflows = append(workflows, WorkflowListItem{
			ID:                   workflow.ID,
			Name:                 workflow.Name,
			CategoryPath:         workflow.CategoryPath,
			Description:          workflow.Description,
			Version:              workflow.Version,
			PublishState:         workflow.PublishState,
			LastPublishedVersion: workflow.LastPublishedVersion,
			LastPublishedAt:      workflow.LastPublishedAt,
			LastUpdated:          lastUpdated,
		})
	}

	return workflows, nil
}
