package codingworkflow

import (
	"path/filepath"
	"strings"
)

// TaskType represents different categories of coding tasks
type TaskType string

const (
	TaskTypeImplementation TaskType = "implementation"
	TaskTypeDebugging      TaskType = "debugging"
	TaskTypeTesting        TaskType = "testing"
	TaskTypeRefactoring    TaskType = "refactoring"
	TaskTypeDocumentation  TaskType = "documentation"
	TaskTypeArchitecture   TaskType = "architecture"
	TaskTypeUnknown        TaskType = "unknown"
)

// KBCandidate represents a Knowledge Base candidate with confidence score
type KBCandidate struct {
	Name       string  // KB name or identifier
	Confidence float64 // 0.0-1.0 confidence score
	Reason     string  // Human-readable reason for selection
}

// TaskClassification represents the classification result
type TaskClassification struct {
	TaskType   TaskType      // Primary task type
	Confidence float64       // Confidence in classification (0.0-1.0)
	KBs        []KBCandidate // Ranked KB candidates
	Signals    []string      // Signals used for classification
}

// TaskClassifier classifies coding tasks and recommends relevant KBs
type TaskClassifier struct {
	// MinConfidence is the minimum confidence threshold for KB recommendations
	MinConfidence float64
}

// DefaultTaskClassifier returns a classifier with default settings
func DefaultTaskClassifier() *TaskClassifier {
	return &TaskClassifier{
		MinConfidence: 0.3, // Recommend KBs with 30%+ confidence
	}
}

// Classify analyzes a task and returns classification with KB recommendations
func (tc *TaskClassifier) Classify(taskDescription, activeFile string, workingDir string) TaskClassification {
	signals := []string{}
	
	// Extract file type signals
	fileExt := ""
	if activeFile != "" {
		fileExt = strings.ToLower(filepath.Ext(activeFile))
		if fileExt != "" {
			signals = append(signals, "file_ext:"+fileExt)
		}
	}
	
	// Normalize task description for analysis
	desc := strings.ToLower(taskDescription)
	words := strings.Fields(desc)
	
	// Extract keyword signals
	keywords := tc.extractKeywords(desc)
	for _, kw := range keywords {
		signals = append(signals, "keyword:"+kw)
	}
	
	// Classify task type based on signals
	taskType, confidence := tc.classifyTaskType(desc, fileExt, keywords)
	
	// Generate KB candidates based on task type and signals
	kbs := tc.recommendKBs(taskType, fileExt, keywords, words)
	
	// Filter by minimum confidence
	filteredKBs := []KBCandidate{}
	for _, kb := range kbs {
		if kb.Confidence >= tc.MinConfidence {
			filteredKBs = append(filteredKBs, kb)
		}
	}
	
	return TaskClassification{
		TaskType:   taskType,
		Confidence: confidence,
		KBs:        filteredKBs,
		Signals:    signals,
	}
}

// extractKeywords extracts significant keywords from task description
func (tc *TaskClassifier) extractKeywords(desc string) []string {
	keywords := []string{}
	
	// Task type keywords
	taskKeywords := map[string]bool{
		"implement": true, "add": true, "create": true, "build": true, "write": true,
		"fix": true, "debug": true, "solve": true, "repair": true,
		"test": true, "verify": true, "validate": true, "check": true,
		"refactor": true, "cleanup": true, "improve": true, "optimize": true,
		"document": true, "comment": true, "explain": true,
		"design": true, "architecture": true, "structure": true,
	}
	
	// Domain keywords
	domainKeywords := map[string]bool{
		"error": true, "handler": true, "api": true, "endpoint": true,
		"database": true, "sql": true, "query": true,
		"parser": true, "lexer": true, "ast": true,
		"http": true, "server": true, "client": true,
		"cache": true, "memory": true, "storage": true,
		"auth": true, "permission": true, "security": true,
		"concurrent": true, "parallel": true, "goroutine": true,
	}
	
	words := strings.Fields(desc)
	for _, word := range words {
		clean := strings.Trim(word, ".,;:!?")
		if taskKeywords[clean] || domainKeywords[clean] {
			keywords = append(keywords, clean)
		}
	}
	
	return keywords
}

// classifyTaskType determines the primary task type
func (tc *TaskClassifier) classifyTaskType(desc, fileExt string, keywords []string) (TaskType, float64) {
	scores := map[TaskType]float64{
		TaskTypeImplementation: 0.0,
		TaskTypeDebugging:      0.0,
		TaskTypeTesting:        0.0,
		TaskTypeRefactoring:    0.0,
		TaskTypeDocumentation:  0.0,
		TaskTypeArchitecture:   0.0,
	}
	
	// Score based on keywords
	for _, kw := range keywords {
		switch kw {
		case "implement", "add", "create", "build", "write":
			scores[TaskTypeImplementation] += 0.3
		case "fix", "debug", "solve", "repair":
			scores[TaskTypeDebugging] += 0.4
		case "test", "verify", "validate", "check":
			scores[TaskTypeTesting] += 0.4
		case "refactor", "cleanup", "improve", "optimize":
			scores[TaskTypeRefactoring] += 0.3
		case "document", "comment", "explain":
			scores[TaskTypeDocumentation] += 0.3
		case "design", "architecture", "structure":
			scores[TaskTypeArchitecture] += 0.3
		}
	}
	
	// Score based on file extension
	if fileExt == ".go" && strings.Contains(desc, "test") {
		scores[TaskTypeTesting] += 0.2
	}
	if strings.HasSuffix(fileExt, "_test.go") {
		scores[TaskTypeTesting] += 0.3
	}
	if fileExt == ".md" || fileExt == ".txt" {
		scores[TaskTypeDocumentation] += 0.2
	}
	
	// Score based on description patterns
	if strings.Contains(desc, "error") && (strings.Contains(desc, "fix") || strings.Contains(desc, "debug")) {
		scores[TaskTypeDebugging] += 0.2
	}
	if strings.Contains(desc, "update") || strings.Contains(desc, "modify") {
		scores[TaskTypeRefactoring] += 0.1
	}
	
	// Find highest score
	maxScore := 0.0
	maxType := TaskTypeUnknown
	for taskType, score := range scores {
		if score > maxScore {
			maxScore = score
			maxType = taskType
		}
	}
	
	// Default to implementation if unclear
	if maxScore < 0.3 {
		return TaskTypeImplementation, 0.5
	}
	
	// Normalize confidence to 0.0-1.0 range
	confidence := maxScore
	if confidence > 1.0 {
		confidence = 1.0
	}
	
	return maxType, confidence
}

// recommendKBs generates ranked KB recommendations
func (tc *TaskClassifier) recommendKBs(taskType TaskType, fileExt string, keywords []string, words []string) []KBCandidate {
	candidates := []KBCandidate{}
	
	// Always recommend language-specific best practices
	if fileExt == ".go" || containsString(words, "go") || containsString(words, "golang") {
		candidates = append(candidates, KBCandidate{
			Name:       "go_best_practices",
			Confidence: 0.9,
			Reason:     "Go language file or task",
		})
	}
	
	// Task-type specific recommendations
	switch taskType {
	case TaskTypeImplementation:
		candidates = append(candidates, KBCandidate{
			Name:       "architecture_patterns",
			Confidence: 0.7,
			Reason:     "Implementation task benefits from architectural guidance",
		})
		if fileExt == ".go" {
			candidates = append(candidates, KBCandidate{
				Name:       "go_error_handling",
				Confidence: 0.8,
				Reason:     "Go implementation requires proper error handling",
			})
		}
	
	case TaskTypeDebugging:
		candidates = append(candidates, KBCandidate{
			Name:       "debugging_guide",
			Confidence: 0.8,
			Reason:     "Debugging task requires systematic approach",
		})
		if containsAnyString(keywords, []string{"error", "panic", "crash"}) {
			candidates = append(candidates, KBCandidate{
				Name:       "go_error_handling",
				Confidence: 0.9,
				Reason:     "Error-related debugging",
			})
		}
	
	case TaskTypeTesting:
		candidates = append(candidates, KBCandidate{
			Name:       "testing_patterns",
			Confidence: 0.9,
			Reason:     "Testing task requires test patterns and practices",
		})
		if fileExt == ".go" {
			candidates = append(candidates, KBCandidate{
				Name:       "go_testing_guide",
				Confidence: 0.85,
				Reason:     "Go testing best practices",
			})
		}
	
	case TaskTypeRefactoring:
		candidates = append(candidates, KBCandidate{
			Name:       "refactoring_patterns",
			Confidence: 0.8,
			Reason:     "Refactoring task benefits from established patterns",
		})
		candidates = append(candidates, KBCandidate{
			Name:       "architecture_patterns",
			Confidence: 0.6,
			Reason:     "Refactoring may involve architectural improvements",
		})
	
	case TaskTypeArchitecture:
		candidates = append(candidates, KBCandidate{
			Name:       "architecture_patterns",
			Confidence: 0.95,
			Reason:     "Architecture task requires design patterns",
		})
		candidates = append(candidates, KBCandidate{
			Name:       "system_design_guide",
			Confidence: 0.8,
			Reason:     "System design guidance for architecture work",
		})
	}
	
	// Domain-specific recommendations based on keywords
	if containsAnyString(keywords, []string{"api", "endpoint", "http", "server"}) {
		candidates = append(candidates, KBCandidate{
			Name:       "api_design_guide",
			Confidence: 0.75,
			Reason:     "API-related task",
		})
	}
	
	if containsAnyString(keywords, []string{"database", "sql", "query"}) {
		candidates = append(candidates, KBCandidate{
			Name:       "database_patterns",
			Confidence: 0.75,
			Reason:     "Database-related task",
		})
	}
	
	if containsAnyString(keywords, []string{"parser", "lexer", "ast"}) {
		candidates = append(candidates, KBCandidate{
			Name:       "parser_implementation_guide",
			Confidence: 0.8,
			Reason:     "Parser/compiler task",
		})
	}
	
	if containsAnyString(keywords, []string{"concurrent", "parallel", "goroutine"}) {
		candidates = append(candidates, KBCandidate{
			Name:       "concurrency_patterns",
			Confidence: 0.8,
			Reason:     "Concurrency-related task",
		})
	}
	
	if containsAnyString(keywords, []string{"auth", "permission", "security"}) {
		candidates = append(candidates, KBCandidate{
			Name:       "security_guide",
			Confidence: 0.8,
			Reason:     "Security-related task",
		})
	}
	
	// Sort by confidence (highest first)
	sortKBCandidates(candidates)
	
	return candidates
}

// Helper functions

func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsAnyString(keywords []string, targets []string) bool {
	for _, kw := range keywords {
		for _, target := range targets {
			if kw == target {
				return true
			}
		}
	}
	return false
}

func sortKBCandidates(candidates []KBCandidate) {
	// Simple bubble sort - fine for small lists
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Confidence > candidates[i].Confidence {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
}
