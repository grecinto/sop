package codingworkflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
)

// FormatCodeProposal creates a markdown string with before/after diff view.
// This format triggers the side-by-side diff UI with syntax highlighting.
//
// Example output:
//
//	The function needs better error handling.
//
//	**Before:**
//	```go
//	func parseInput(data string) *Result {
//	    return &Result{Data: data}
//	}
//	```
//
//	**After:**
//	```go
//	func parseInput(data string) (*Result, error) {
//	    if strings.TrimSpace(data) == "" {
//	        return nil, fmt.Errorf("empty input")
//	    }
//	    return &Result{Data: data}, nil
//	}
//	```
func FormatCodeProposal(description, beforeCode, afterCode, language string) string {
	return fmt.Sprintf(`%s

**Before:**
`+"```"+`%s
%s
`+"```"+`

**After:**
`+"```"+`%s
%s
`+"```", description, language, strings.TrimSpace(beforeCode), language, strings.TrimSpace(afterCode))
}

// FormatSingleCodeBlock creates a markdown string with syntax highlighting.
// Use this for new implementations, examples, or single code snippets.
//
// Example output:
//
//	Here's the implementation:
//
//	```go
//	func processData(input []byte) error {
//	    // ... code
//	}
//	```
func FormatSingleCodeBlock(description, code, language string) string {
	return fmt.Sprintf(`%s

`+"```"+`%s
%s
`+"```", description, language, strings.TrimSpace(code))
}

// FormatMultipleFiles creates markdown with multiple code blocks.
// Use this when showing changes to multiple files or providing multiple examples.
//
// Example output:
//
//	I've updated the following files:
//
//	**File: path/to/file1.go**
//	```go
//	// code here
//	```
//
//	**File: path/to/file2.go**
//	```go
//	// code here
//	```
func FormatMultipleFiles(description string, files map[string]string, language string) string {
	var builder strings.Builder
	builder.WriteString(description)
	builder.WriteString("\n\n")

	for filename, code := range files {
		builder.WriteString(fmt.Sprintf("**File: %s**\n", filename))
		builder.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", language, strings.TrimSpace(code)))
	}

	return builder.String()
}

// FormatArchitectureProposal creates markdown for architecture changes with examples.
// Includes both design description and code snippets to illustrate the architecture.
func FormatArchitectureProposal(designDescription string, patterns map[string]string, language string) string {
	var builder strings.Builder
	builder.WriteString(designDescription)
	builder.WriteString("\n\n")

	for patternName, code := range patterns {
		builder.WriteString(fmt.Sprintf("**Pattern: %s**\n", patternName))
		builder.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", language, strings.TrimSpace(code)))
	}

	return builder.String()
}

// EmitProgressMessage sends a progress message that appears with animation in the UI.
// Messages starting with "# " trigger the terminal sweep animation effect.
//
// Usage in orchestration workflows:
//
//	EmitProgressMessage(ctx, "Analyzing code structure")
//	EmitProgressMessage(ctx, "Generating architecture proposal")
//	EmitProgressMessage(ctx, "Validating against knowledge base rules")
//
// These appear in the chat UI with an animated progress indicator.
func EmitProgressMessage(ctx context.Context, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}

	// Add "# " prefix if not already present to trigger animation
	if !strings.HasPrefix(message, "# ") && !strings.HasPrefix(message, "$ ") {
		message = "# " + message
	}

	// Get the event streamer from context
	if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
		streamer("terminal_command", map[string]any{
			"text":               strings.TrimSpace(message),
			"display_as_command": true,
		})
	}
}

// EmitWorkflowPhase is a convenience wrapper for emitting step messages.
// It formats the step number and description consistently.
//
// Usage:
//
//	EmitWorkflowPhase(ctx, 1, "Analyzing current architecture")
//	EmitWorkflowPhase(ctx, 2, "Identifying design patterns")
//	EmitWorkflowPhase(ctx, 3, "Proposing architecture changes")
func EmitWorkflowPhase(ctx context.Context, step int, description string) {
	message := fmt.Sprintf("Step %d: %s", step, description)
	EmitProgressMessage(ctx, message)
}

// CodeFormatInstructions returns markdown formatting instructions for LLM responses.
// This should be included in BuildInstructions() to ensure proper code formatting.
const CodeFormatInstructions = `
## Code Formatting Guidelines

**CRITICAL**: Always format code responses using these patterns to trigger proper UI rendering:

### 1. Code Changes (Before/After Format)

When proposing code changes, modifications, or fixes, ALWAYS use this format:

**Before:**
` + "```" + `language
// original code
` + "```" + `

**After:**
` + "```" + `language
// modified code
` + "```" + `

This triggers a side-by-side diff view with:
- ❌ Before (red header) on left
- ✅ After (green header) on right  
- 📋 Copy button for new code
- Syntax highlighting on both sides

**Alternative keywords that work**:
- Before/After
- Original/Modified
- Old/New
- Current/Updated

### 2. New Implementation (Single Code Block)

For new implementations, examples, or snippets:

` + "```" + `language
// your code here
` + "```" + `

This triggers syntax highlighting only.

### 3. Multiple Files

When showing changes to multiple files:

**File: path/to/file1.go**
` + "```" + `go
// code here
` + "```" + `

**File: path/to/file2.go**
` + "```" + `go
// code here
` + "```" + `

### 4. Language Tags

ALWAYS specify the language in code blocks:
- ` + "`go`" + ` for Go
- ` + "`python`" + ` for Python
- ` + "`typescript`" + ` or ` + "`javascript`" + ` for TS/JS
- ` + "`java`" + `, ` + "`c`" + `, ` + "`cpp`" + `, ` + "`rust`" + `, etc.

### 5. Architecture Examples

For architecture proposals, include code examples of patterns:

**Pattern: Factory Method**
` + "```" + `go
type Factory interface {
    Create() Product
}
` + "```" + `

### 6. Test Code

Show test code with proper language tags:

` + "```" + `go
func TestParseInput(t *testing.T) {
    // test code
}
` + "```" + `

**Remember**: Code blocks without language tags will not get syntax highlighting!
`
