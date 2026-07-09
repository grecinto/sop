package codingworkflow

import (
	"testing"
)

func TestTaskClassifier_Classify_Implementation(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("implement user authentication handler", "auth.go", "/project")
	
	if result.TaskType != TaskTypeImplementation {
		t.Errorf("expected task type implementation, got %s", result.TaskType)
	}
	if result.Confidence < 0.3 {
		t.Errorf("expected confidence >= 0.3, got %.2f", result.Confidence)
	}
	if len(result.KBs) == 0 {
		t.Error("expected at least one KB recommendation")
	}
	// Should recommend go_best_practices
	foundGoBP := false
	for _, kb := range result.KBs {
		if kb.Name == "go_best_practices" {
			foundGoBP = true
			if kb.Confidence < 0.8 {
				t.Errorf("expected high confidence for go_best_practices, got %.2f", kb.Confidence)
			}
		}
	}
	if !foundGoBP {
		t.Error("expected go_best_practices in KB recommendations")
	}
}

func TestTaskClassifier_Classify_Debugging(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("fix error in connection handling", "server.go", "/project")
	
	if result.TaskType != TaskTypeDebugging {
		t.Errorf("expected task type debugging, got %s", result.TaskType)
	}
	if result.Confidence < 0.4 {
		t.Errorf("expected confidence >= 0.4, got %.2f", result.Confidence)
	}
	// Should recommend debugging_guide
	foundDebugGuide := false
	for _, kb := range result.KBs {
		if kb.Name == "debugging_guide" {
			foundDebugGuide = true
		}
	}
	if !foundDebugGuide {
		t.Error("expected debugging_guide in KB recommendations")
	}
}

func TestTaskClassifier_Classify_Testing(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("add test cases for validator", "validator_test.go", "/project")
	
	if result.TaskType != TaskTypeTesting {
		t.Errorf("expected task type testing, got %s", result.TaskType)
	}
	if result.Confidence < 0.4 {
		t.Errorf("expected confidence >= 0.4, got %.2f", result.Confidence)
	}
	// Should recommend testing_patterns
	foundTestPatterns := false
	for _, kb := range result.KBs {
		if kb.Name == "testing_patterns" {
			foundTestPatterns = true
			if kb.Confidence < 0.8 {
				t.Errorf("expected high confidence for testing_patterns, got %.2f", kb.Confidence)
			}
		}
	}
	if !foundTestPatterns {
		t.Error("expected testing_patterns in KB recommendations")
	}
}

func TestTaskClassifier_Classify_Refactoring(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("refactor error handling to improve readability", "handler.go", "/project")
	
	if result.TaskType != TaskTypeRefactoring {
		t.Errorf("expected task type refactoring, got %s", result.TaskType)
	}
	if result.Confidence < 0.3 {
		t.Errorf("expected confidence >= 0.3, got %.2f", result.Confidence)
	}
	// Should recommend refactoring_patterns
	foundRefactoring := false
	for _, kb := range result.KBs {
		if kb.Name == "refactoring_patterns" {
			foundRefactoring = true
		}
	}
	if !foundRefactoring {
		t.Error("expected refactoring_patterns in KB recommendations")
	}
}

func TestTaskClassifier_Classify_Architecture(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("design the overall system architecture", "design.md", "/project")
	
	if result.TaskType != TaskTypeArchitecture {
		t.Errorf("expected task type architecture, got %s", result.TaskType)
	}
	// Should recommend architecture_patterns with high confidence
	foundArchPatterns := false
	for _, kb := range result.KBs {
		if kb.Name == "architecture_patterns" {
			foundArchPatterns = true
			if kb.Confidence < 0.9 {
				t.Errorf("expected very high confidence for architecture_patterns, got %.2f", kb.Confidence)
			}
		}
	}
	if !foundArchPatterns {
		t.Error("expected architecture_patterns in KB recommendations")
	}
}

func TestTaskClassifier_Classify_APITask(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("implement REST API endpoint for user profile", "api.go", "/project")
	
	// Should recommend api_design_guide
	foundAPIGuide := false
	for _, kb := range result.KBs {
		if kb.Name == "api_design_guide" {
			foundAPIGuide = true
			if kb.Confidence < 0.7 {
				t.Errorf("expected high confidence for api_design_guide, got %.2f", kb.Confidence)
			}
		}
	}
	if !foundAPIGuide {
		t.Error("expected api_design_guide in KB recommendations for API task")
	}
}

func TestTaskClassifier_Classify_DatabaseTask(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("write SQL query to fetch user data", "query.go", "/project")
	
	// Should recommend database_patterns
	foundDBPatterns := false
	for _, kb := range result.KBs {
		if kb.Name == "database_patterns" {
			foundDBPatterns = true
		}
	}
	if !foundDBPatterns {
		t.Error("expected database_patterns in KB recommendations for database task")
	}
}

func TestTaskClassifier_Classify_ConcurrencyTask(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("add goroutine for parallel processing", "worker.go", "/project")
	
	// Should recommend concurrency_patterns
	foundConcurrency := false
	for _, kb := range result.KBs {
		if kb.Name == "concurrency_patterns" {
			foundConcurrency = true
			if kb.Confidence < 0.7 {
				t.Errorf("expected high confidence for concurrency_patterns, got %.2f", kb.Confidence)
			}
		}
	}
	if !foundConcurrency {
		t.Error("expected concurrency_patterns in KB recommendations for concurrency task")
	}
}

func TestTaskClassifier_Classify_SecurityTask(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("implement authentication and permission checks", "auth.go", "/project")
	
	// Should recommend security_guide
	foundSecurity := false
	for _, kb := range result.KBs {
		if kb.Name == "security_guide" {
			foundSecurity = true
		}
	}
	if !foundSecurity {
		t.Error("expected security_guide in KB recommendations for security task")
	}
}

func TestTaskClassifier_Classify_ConfidenceFiltering(t *testing.T) {
	tc := &TaskClassifier{
		MinConfidence: 0.8, // High threshold
	}
	
	result := tc.Classify("update code", "main.go", "/project")
	
	// With high threshold, should have fewer recommendations
	for _, kb := range result.KBs {
		if kb.Confidence < 0.8 {
			t.Errorf("KB %s has confidence %.2f, below threshold 0.8", kb.Name, kb.Confidence)
		}
	}
}

func TestTaskClassifier_Classify_SignalExtraction(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("implement error handler", "handler.go", "/project")
	
	if len(result.Signals) == 0 {
		t.Error("expected at least some signals to be extracted")
	}
	
	// Should have file extension signal
	foundFileExt := false
	for _, sig := range result.Signals {
		if sig == "file_ext:.go" {
			foundFileExt = true
		}
	}
	if !foundFileExt {
		t.Error("expected file_ext signal")
	}
	
	// Should have keyword signals
	foundKeyword := false
	for _, sig := range result.Signals {
		if sig == "keyword:implement" || sig == "keyword:error" || sig == "keyword:handler" {
			foundKeyword = true
			break
		}
	}
	if !foundKeyword {
		t.Error("expected at least one keyword signal")
	}
}

func TestTaskClassifier_Classify_KBRanking(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("implement API endpoint", "api.go", "/project")
	
	if len(result.KBs) < 2 {
		t.Skip("need at least 2 KBs to test ranking")
	}
	
	// KBs should be sorted by confidence (descending)
	for i := 1; i < len(result.KBs); i++ {
		if result.KBs[i].Confidence > result.KBs[i-1].Confidence {
			t.Errorf("KBs not properly sorted: KB[%d] (%.2f) > KB[%d] (%.2f)",
				i, result.KBs[i].Confidence, i-1, result.KBs[i-1].Confidence)
		}
	}
}

func TestTaskClassifier_Classify_NoActiveFile(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	// Should still work without active file
	result := tc.Classify("implement user service", "", "/project")
	
	if result.TaskType == TaskTypeUnknown {
		t.Error("should classify even without active file")
	}
	if len(result.KBs) == 0 {
		t.Error("should recommend at least some KBs even without active file")
	}
}

func TestTaskClassifier_Classify_VagueDescription(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("update code", "main.go", "/project")
	
	// Should default to implementation with moderate confidence
	if result.TaskType == TaskTypeUnknown {
		t.Error("should not return unknown for vague but valid description")
	}
	// Should still recommend go_best_practices for .go file
	foundGoBP := false
	for _, kb := range result.KBs {
		if kb.Name == "go_best_practices" {
			foundGoBP = true
		}
	}
	if !foundGoBP {
		t.Error("expected go_best_practices even for vague description with .go file")
	}
}

func TestTaskClassifier_Classify_ReasonProvided(t *testing.T) {
	tc := DefaultTaskClassifier()
	
	result := tc.Classify("implement parser", "parser.go", "/project")
	
	// Every KB candidate should have a reason
	for _, kb := range result.KBs {
		if kb.Reason == "" {
			t.Errorf("KB %s missing reason for recommendation", kb.Name)
		}
	}
}
