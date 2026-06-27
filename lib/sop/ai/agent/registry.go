package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sharedcode/sop/inmemory"
)

// ToolHandler is the function signature for a tool execution.
type ToolHandler func(ctx context.Context, args map[string]any) (any, error)

func wrapStringTool(fn func(context.Context, map[string]any) (string, error)) ToolHandler {
	return func(ctx context.Context, args map[string]any) (any, error) {
		return fn(ctx, args)
	}
}

func formatToolResult(ctx context.Context, res any) (string, error) {
	switch v := res.(type) {
	case string:
		return v, nil
	case nil:
		return "", nil
	default:
		return serializeResult(ctx, v)
	}
}

// ToolDefinition defines a tool's metadata and handler.
type ToolDefinition struct {
	Name             string
	Description      string
	ShortDescription string
	ArgsSchema       string // JSON schema or description of args
	Handler          ToolHandler
	Hidden           bool
}

// Registry manages the available tools.
type Registry struct {
	tools      inmemory.BtreeInterface[int, ToolDefinition]
	orderByKey map[string]int
	nextOrder  int
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:      inmemory.NewBtree[int, ToolDefinition](true),
		orderByKey: make(map[string]int),
	}
}

func (r *Registry) upsertTool(name string, def ToolDefinition) {
	if existingOrder, ok := r.orderByKey[name]; ok {
		r.tools.Upsert(existingOrder, def)
		return
	}

	r.nextOrder++
	order := r.nextOrder
	r.orderByKey[name] = order
	r.tools.Upsert(order, def)
}

// Register adds a tool to the registry.
func (r *Registry) Register(name, description, argsSchema string, handler ToolHandler) {
	r.upsertTool(name, ToolDefinition{
		Name:        name,
		Description: description,
		ArgsSchema:  argsSchema,
		Handler:     handler,
		Hidden:      false,
	})
}

// RegisterWithUI adds a tool to the registry with a short, user-friendly UI description.
func (r *Registry) RegisterWithUI(name, shortDesc, description, argsSchema string, handler ToolHandler) {
	r.upsertTool(name, ToolDefinition{
		Name:             name,
		Description:      description,
		ShortDescription: shortDesc,
		ArgsSchema:       argsSchema,
		Handler:          handler,
		Hidden:           false,
	})
}

// RegisterHidden adds a hidden tool to the registry (not shown in prompt).
func (r *Registry) RegisterHidden(name, description, argsSchema string, handler ToolHandler) {
	r.upsertTool(name, ToolDefinition{
		Name:        name,
		Description: description,
		ArgsSchema:  argsSchema,
		Handler:     handler,
		Hidden:      true,
	})
}

// Get retrieves a tool definition by name.
func (r *Registry) Get(name string) (ToolDefinition, bool) {
	order, ok := r.orderByKey[name]
	if !ok {
		return ToolDefinition{}, false
	}
	if !r.tools.Find(order, true) {
		return ToolDefinition{}, false
	}
	return r.tools.GetCurrentValue(), true
}

// List returns all registered tools in registration order.
func (r *Registry) List() []ToolDefinition {
	if r.tools.Count() == 0 {
		return nil
	}

	list := make([]ToolDefinition, 0, r.tools.Count())
	if r.tools.First() {
		for {
			list = append(list, r.tools.GetCurrentValue())
			if !r.tools.Next() {
				break
			}
		}
	}
	return list
}

// GeneratePrompt generates the tools description for the LLM prompt.
func (r *Registry) GeneratePrompt() string {
	var sb strings.Builder
	sb.WriteString("You have access to the following tools via the native tools platform to help answer the user's question.\n")
	sb.WriteString("Tools:\n")

	for i, t := range r.List() {
		if t.Hidden {
			continue
		}
		schemaStr := t.ArgsSchema
		if len(schemaStr) > 0 && schemaStr[0] == '{' {
			schemaStr = "(args: " + schemaStr + ")"
		}
		sb.WriteString(fmt.Sprintf("%d. %s%s - %s\n", i+1, t.Name, schemaStr, t.Description))
	}
	return sb.String()
}
