package generator

import (
	"fmt"
	"strings"

	"github.com/sharedcode/sop/ai"
)

const clarificationActionToolName = "request_clarification"

const clarificationActionToolSchema = `{"type":"object","properties":{"question":{"type":"string","description":"One short, concrete clarification question to ask the user."},"missing_fact":{"type":"string","description":"The single missing fact blocking further progress."},"reason":{"type":"string","description":"Why the clarification is required before continuing."},"target_query":{"type":"string","description":"Optional target ask being clarified."}},"required":["question"]}`

func appendClarificationActionTool(tools []ai.ToolDefinition) []ai.ToolDefinition {
	for _, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Name), clarificationActionToolName) {
			return tools
		}
	}
	cloned := append([]ai.ToolDefinition(nil), tools...)
	return append(cloned, ai.ToolDefinition{
		Name:        clarificationActionToolName,
		Description: "Ask the user for one missing fact and end the current loop turn instead of calling another tool.",
		Schema:      clarificationActionToolSchema,
	})
}

func clarificationRequestFromToolCalls(toolCalls []ai.ToolCall, fallbackTargetQuery string) (*ai.ClarificationRequest, bool) {
	if len(toolCalls) == 0 {
		return nil, false
	}
	toolCall := toolCalls[0]
	if !strings.EqualFold(strings.TrimSpace(toolCall.Name), clarificationActionToolName) {
		return nil, false
	}
	question := strings.TrimSpace(fmt.Sprintf("%v", toolCall.Args["question"]))
	if question == "" {
		return nil, false
	}
	targetQuery := strings.TrimSpace(fmt.Sprintf("%v", toolCall.Args["target_query"]))
	if targetQuery == "" {
		targetQuery = strings.TrimSpace(fallbackTargetQuery)
	}
	return &ai.ClarificationRequest{
		TargetQuery:       targetQuery,
		AssistantQuestion: question,
		MissingFact:       strings.TrimSpace(fmt.Sprintf("%v", toolCall.Args["missing_fact"])),
		Reason:            strings.TrimSpace(fmt.Sprintf("%v", toolCall.Args["reason"])),
		Source:            clarificationActionToolName,
		Status:            "pending",
	}, true
}
