package codingworkflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop/ai/agent/capabilities"
)

// TestResearchStepsCreation verifies research step factories
func TestResearchStepsCreation(t *testing.T) {
	tests := []struct {
		name        string
		factory     func(map[string]interface{}) Step
		context     map[string]interface{}
		expectedID  string
		expectedTag string
	}{
		{
			name:        "ComplexityAnalysisStep",
			factory:     NewComplexityAnalysisStep,
			context:     map[string]interface{}{"threshold": "high"},
			expectedID:  "research_complexity_analysis",
			expectedTag: "research",
		},
		{
			name:        "SecurityValidationStep",
			factory:     NewSecurityValidationStep,
			context:     map[string]interface{}{"sensitive_areas": []string{"auth"}},
			expectedID:  "research_security_validation",
			expectedTag: "security",
		},
		{
			name:        "ArchitectureResearchStep",
			factory:     NewArchitectureResearchStep,
			context:     map[string]interface{}{"focus": "microservices"},
			expectedID:  "research_architecture",
			expectedTag: "architecture",
		},
		{
			name:        "DependencyResearchStep",
			factory:     NewDependencyResearchStep,
			context:     map[string]interface{}{"language": "python"},
			expectedID:  "research_dependencies",
			expectedTag: "dependencies",
		},
		{
			name:        "TestCoverageResearchStep",
			factory:     NewTestCoverageResearchStep,
			context:     map[string]interface{}{"target_path": "./lib"},
			expectedID:  "research_test_coverage",
			expectedTag: "testing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := tt.factory(tt.context)
			metadata := step.Metadata()

			if metadata.ID != tt.expectedID {
				t.Errorf("Expected ID %s, got %s", tt.expectedID, metadata.ID)
			}

			foundTag := false
			for _, tag := range metadata.Tags {
				if tag == tt.expectedTag {
					foundTag = true
					break
				}
			}
			if !foundTag {
				t.Errorf("Expected tag %s not found in %v", tt.expectedTag, metadata.Tags)
			}
		})
	}
}

// TestComplexityAnalysisStepPrompt verifies prompt generation
func TestComplexityAnalysisStepPrompt(t *testing.T) {
	step := NewComplexityAnalysisStep(map[string]interface{}{
		"threshold": "high",
	})

	routingState := capabilities.RoutingState{}
	prompt, err := step.OnBeforePrompt(context.Background(), "test", routingState)
	if err != nil {
		t.Fatalf("OnBeforePrompt failed: %v", err)
	}

	if prompt == "" {
		t.Error("Expected non-empty prompt")
	}

	// Check that prompt mentions complexity
	if !strings.Contains(prompt, "complexity") {
		t.Error("Prompt should mention complexity")
	}
}

// TestSignalCacheBasicOperations verifies cache operations
func TestSignalCacheBasicOperations(t *testing.T) {
	cache := NewSignalCache()

	// Test Set and Get
	data := map[string]interface{}{
		"result": "test data",
	}
	cache.Set("key1", data)

	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}

	retrieved, found := cache.Get("key1", 5*time.Minute)
	if !found {
		t.Fatal("Expected to find cached entry")
	}

	if retrieved["result"] != "test data" {
		t.Errorf("Expected 'test data', got %v", retrieved["result"])
	}

	// Test non-existent key
	_, found = cache.Get("nonexistent", 5*time.Minute)
	if found {
		t.Error("Should not find non-existent key")
	}

	// Test Clear
	cache.Clear()
	if cache.Size() != 0 {
		t.Errorf("Expected cache size 0 after clear, got %d", cache.Size())
	}
}

// TestSignalCacheExpiration verifies TTL expiration
func TestSignalCacheExpiration(t *testing.T) {
	cache := NewSignalCache()

	data := map[string]interface{}{"value": 123}
	cache.Set("key1", data)

	// Should be retrievable with long TTL
	_, found := cache.Get("key1", 1*time.Hour)
	if !found {
		t.Error("Should find entry with long TTL")
	}

	// Should expire with very short TTL
	time.Sleep(10 * time.Millisecond)
	_, found = cache.Get("key1", 1*time.Millisecond)
	if found {
		t.Error("Should not find expired entry")
	}
}

// TestSignalCachePrune verifies pruning of expired entries
func TestSignalCachePrune(t *testing.T) {
	cache := NewSignalCache()

	// Add multiple entries
	cache.Set("key1", map[string]interface{}{"v": 1})
	cache.Set("key2", map[string]interface{}{"v": 2})
	cache.Set("key3", map[string]interface{}{"v": 3})

	if cache.Size() != 3 {
		t.Fatalf("Expected cache size 3, got %d", cache.Size())
	}

	// Wait and prune with very short TTL
	time.Sleep(10 * time.Millisecond)
	removed := cache.Prune(1 * time.Millisecond)

	if removed != 3 {
		t.Errorf("Expected to prune 3 entries, got %d", removed)
	}

	if cache.Size() != 0 {
		t.Errorf("Expected cache size 0 after prune, got %d", cache.Size())
	}
}

// TestWorkflowManagerCacheConfiguration verifies cache config methods
func TestWorkflowManagerCacheConfiguration(t *testing.T) {
	orchestrator := &Orchestrator{}
	wm := NewWorkflowManager(orchestrator)

	// Check default state
	if !wm.cacheEnabled {
		t.Error("Cache should be enabled by default")
	}

	if wm.cacheTTL != 5*time.Minute {
		t.Errorf("Expected default TTL of 5 minutes, got %v", wm.cacheTTL)
	}

	// Test enable/disable
	wm.EnableCache(false)
	if wm.cacheEnabled {
		t.Error("Cache should be disabled")
	}

	wm.EnableCache(true)
	if !wm.cacheEnabled {
		t.Error("Cache should be enabled")
	}

	// Test TTL configuration
	wm.SetCacheTTL(10 * time.Minute)
	if wm.cacheTTL != 10*time.Minute {
		t.Errorf("Expected TTL of 10 minutes, got %v", wm.cacheTTL)
	}

	// Test clear
	wm.signalCache.Set("test", map[string]interface{}{})
	if wm.GetCacheSize() != 1 {
		t.Error("Expected cache size 1")
	}

	wm.ClearCache()
	if wm.GetCacheSize() != 0 {
		t.Error("Expected cache size 0 after clear")
	}
}

// TestSignalCacheKeyGeneration verifies cache key generation
func TestSignalCacheKeyGeneration(t *testing.T) {
	signal1 := StepSignal{
		Type: SignalResearchBestPractices,
		Context: map[string]interface{}{
			"language": "go",
		},
	}

	signal2 := StepSignal{
		Type: SignalResearchBestPractices,
		Context: map[string]interface{}{
			"language": "go",
		},
	}

	signal3 := StepSignal{
		Type: SignalResearchBestPractices,
		Context: map[string]interface{}{
			"language": "python", // Different context
		},
	}

	key1 := generateSignalCacheKey(signal1)
	key2 := generateSignalCacheKey(signal2)
	key3 := generateSignalCacheKey(signal3)

	// Same signal should generate same key
	if key1 != key2 {
		t.Error("Identical signals should generate same cache key")
	}

	// Different context should generate different key
	if key1 == key3 {
		t.Error("Different signals should generate different cache keys")
	}
}

// TestBuiltInResearchStepFactory verifies built-in step creation
func TestBuiltInResearchStepFactory(t *testing.T) {
	orchestrator := &Orchestrator{}
	wm := NewWorkflowManager(orchestrator)

	tests := []struct {
		signalType SignalType
		shouldWork bool
	}{
		{SignalAnalyzeComplexity, true},
		{SignalValidateSecurity, true},
		{SignalResearchArchitecture, true},
		{SignalResearchDependencies, true},
		{SignalResearchTestCoverage, true},
		{SignalResearchBestPractices, false}, // Function call only
		{SignalResearchCodePatterns, false},  // Function call only
	}

	for _, tt := range tests {
		t.Run(string(tt.signalType), func(t *testing.T) {
			signal := StepSignal{
				Type:    tt.signalType,
				Context: map[string]interface{}{},
			}

			step := wm.createBuiltInResearchStep(signal)

			if tt.shouldWork && step == nil {
				t.Errorf("Expected step for %s, got nil", tt.signalType)
			}

			if !tt.shouldWork && step != nil {
				t.Errorf("Expected nil for %s, got step", tt.signalType)
			}
		})
	}
}

// TestCachingInProcessFunctionCallSignal verifies caching in function calls
func TestCachingInProcessFunctionCallSignal(t *testing.T) {
	orchestrator := &Orchestrator{}
	wm := NewWorkflowManager(orchestrator)

	// Create a signal with custom handler that tracks executions
	executionCount := 0
	signal := StepSignal{
		Type: SignalResearchBestPractices,
		Mode: SignalModeFunctionCall,
		Handler: func(ctx context.Context, infra *SharedInfrastructure, sig StepSignal) (map[string]interface{}, error) {
			executionCount++
			return map[string]interface{}{
				"execution": executionCount,
			}, nil
		},
		Context: map[string]interface{}{
			"language": "go",
		},
	}

	ws := &WorkflowState{
		ResearchData: make(map[string]interface{}),
	}

	// First execution
	err := wm.processFunctionCallSignal(context.Background(), signal, ws)
	if err != nil {
		t.Fatalf("First execution failed: %v", err)
	}

	if executionCount != 1 {
		t.Errorf("Expected 1 execution, got %d", executionCount)
	}

	// Second execution with same signal - should use cache
	err = wm.processFunctionCallSignal(context.Background(), signal, ws)
	if err != nil {
		t.Fatalf("Second execution failed: %v", err)
	}

	if executionCount != 1 {
		t.Errorf("Expected still 1 execution (cached), got %d", executionCount)
	}

	// Verify cache size
	if wm.GetCacheSize() != 1 {
		t.Errorf("Expected cache size 1, got %d", wm.GetCacheSize())
	}
}

// TestCacheDisabled verifies behavior when cache is disabled
func TestCacheDisabled(t *testing.T) {
	orchestrator := &Orchestrator{}
	wm := NewWorkflowManager(orchestrator)
	wm.EnableCache(false)

	executionCount := 0
	signal := StepSignal{
		Type: SignalResearchBestPractices,
		Mode: SignalModeFunctionCall,
		Handler: func(ctx context.Context, infra *SharedInfrastructure, sig StepSignal) (map[string]interface{}, error) {
			executionCount++
			return map[string]interface{}{
				"execution": executionCount,
			}, nil
		},
	}

	ws := &WorkflowState{
		ResearchData: make(map[string]interface{}),
	}

	// First execution
	wm.processFunctionCallSignal(context.Background(), signal, ws)

	// Second execution - should NOT use cache
	wm.processFunctionCallSignal(context.Background(), signal, ws)

	if executionCount != 2 {
		t.Errorf("Expected 2 executions (cache disabled), got %d", executionCount)
	}
}
