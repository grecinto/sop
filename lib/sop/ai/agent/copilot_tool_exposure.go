package agent

import (
	"context"
	"strings"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
)

var nativeSpaceToolNames = map[string]bool{
	"mint_to_space":              true,
	"delete_space":               true,
	"enrich_space":               true,
	"update_space_config":        true,
	"read_space_config":          true,
	"vectorize_space":            true,
	"vectorize_space_categories": true,
	"vectorize_space_items":      true,
	"search_space":               true,
	"list_space_categories":      true,
	"list_space_items":           true,
	"delete_space_items":         true,
	"delete_space_categories":    true,
}

var nativeStoreToolNames = map[string]bool{
	"execute_script":     true,
	"list_stores":        true,
	"begin_tx":           true,
	"commit_tx":          true,
	"rollback_tx":        true,
	"open_store":         true,
	"select":             true,
	"join":               true,
	"join_right":         true,
	"explain_join":       true,
	"add":                true,
	"update":             true,
	"delete":             true,
	"manage_transaction": true,
	"scan":               true,
	"filter":             true,
	"sort":               true,
	"project":            true,
	"limit":              true,
}

var gitToolNames = map[string]bool{
	"git_status":   true,
	"git_diff":     true,
	"git_log":      true,
	"git_branch":   true,
	"git_checkout": true,
	"git_commit":   true,
	"git_pull":     true,
	"git_push":     true,
}

var commandLineToolNames = map[string]bool{
	"execute_local_command": true,
	"list_directory":        true,
	"read_file":             true,
	"write_file":            true,
	"delete_file":           true,
	"copy_file":             true,
	"rename_file":           true,
	"create_directory":      true,
	"delete_directory":      true,
	"path_exists":           true,
	"stat_path":             true,
}

var postgresToolNames = map[string]bool{
	"postgres_query":       true,
	"postgres_list_tables": true,
	"postgres_execute":     true,
}

var splunkToolNames = map[string]bool{
	"splunk_search":              true,
	"splunk_list_saved_searches": true,
	"splunk_create_saved_search": true,
	"splunk_update_saved_search": true,
	"splunk_delete_saved_search": true,
}

var alwaysExposedTools = map[string]bool{
	"list_tools": true,
	"help":       true,
}

type nativeToolExposure struct {
	restrict bool
	allowed  map[string]bool
	domain   string // Track domain for logging
}

func activeRoutingState(ctx context.Context) *TaskContextClassification {
	if p := ai.GetSessionPayload(ctx); p != nil && p.Variables != nil {
		if rs, ok := p.Variables["RoutingState"].(*TaskContextClassification); ok {
			return rs
		}
	}
	return nil
}

func buildNativeToolExposure(routingState *TaskContextClassification) nativeToolExposure {
	if routingState == nil {
		return nativeToolExposure{}
	}

	allowed := make(map[string]bool)
	exposedDomains := []string{}

	// Always expose core tools
	for name := range alwaysExposedTools {
		allowed[name] = true
	}

	// CROSS-DOMAIN SUPPORT: Additively expose tools based on artifacts and families
	// This allows queries like "mint to space and commit to git" to work

	// 1. Expose Space tools if SpacesArtifacts present
	if len(routingState.SpacesArtifacts) > 0 || routingState.Domain == SpacesDomain {
		allowedSpacesTools, _ := allowedNativeDomainTools(routingState)
		for name := range allowedSpacesTools {
			allowed[name] = true
		}
		exposedDomains = append(exposedDomains, "Spaces")
	}

	// 2. Expose Store tools if StoresArtifacts present
	if len(routingState.StoresArtifacts) > 0 || routingState.Domain == StoresDomain {
		_, allowedStoresTools := allowedNativeDomainTools(routingState)
		for name := range allowedStoresTools {
			allowed[name] = true
		}
		exposedDomains = append(exposedDomains, "Stores")
	}

	// 3. Expose automation tools based on AutomationFamilies (additive)
	if len(routingState.AutomationFamilies) > 0 {
		for _, family := range routingState.AutomationFamilies {
			switch family {
			case "git":
				for name := range gitToolNames {
					allowed[name] = true
				}
				exposedDomains = append(exposedDomains, "Git")
			case "postgres":
				for name := range postgresToolNames {
					allowed[name] = true
				}
				exposedDomains = append(exposedDomains, "Postgres")
			case "splunk":
				for name := range splunkToolNames {
					allowed[name] = true
				}
				exposedDomains = append(exposedDomains, "Splunk")
			case "shell":
				for name := range commandLineToolNames {
					allowed[name] = true
				}
				exposedDomains = append(exposedDomains, "Shell")
			}
		}
	}

	// 4. Domain-specific fallback for domain-only routing when the classifier
	// did not surface explicit artifacts but still set a domain.
	if len(exposedDomains) == 0 {
		switch routingState.Domain {
		case GitDomain:
			for name := range gitToolNames {
				allowed[name] = true
			}
			allowed["list_directory"] = true
			allowed["read_file"] = true
			allowed["write_file"] = true
			exposedDomains = append(exposedDomains, "Git")

		case CommandLineDomain:
			for name := range commandLineToolNames {
				allowed[name] = true
			}
			for name := range gitToolNames {
				allowed[name] = true
			}
			exposedDomains = append(exposedDomains, "CommandLine")

		case AutomationDomain:
			for name := range commandLineToolNames {
				allowed[name] = true
			}
			exposedDomains = append(exposedDomains, "Automation")

		case GeneralDomain, "":
			// General/unclassified: expose most tools
			for name := range nativeStoreToolNames {
				allowed[name] = true
			}
			for name := range nativeSpaceToolNames {
				allowed[name] = true
			}
			for name := range commandLineToolNames {
				allowed[name] = true
			}
			for name := range gitToolNames {
				allowed[name] = true
			}
			exposedDomains = append(exposedDomains, "General")

		default:
			// Unknown domain - use legacy artifact-based exposure
			allowedSpacesTools, allowedStoresTools := allowedNativeDomainTools(routingState)
			for name := range allowedSpacesTools {
				allowed[name] = true
			}
			for name := range allowedStoresTools {
				allowed[name] = true
			}
			exposedDomains = append(exposedDomains, "Legacy")
		}
	}

	// Always add essential file tools if any automation tools are exposed
	if len(routingState.AutomationFamilies) > 0 || routingState.Domain == GitDomain || routingState.Domain == CommandLineDomain {
		allowed["list_directory"] = true
		allowed["read_file"] = true
		allowed["write_file"] = true
	}

	log.Info("Tool exposure built",
		"domain", routingState.Domain,
		"exposed_domains", exposedDomains,
		"automation_families", routingState.AutomationFamilies,
		"spaces_artifacts", len(routingState.SpacesArtifacts),
		"stores_artifacts", len(routingState.StoresArtifacts),
		"tool_count", len(allowed))

	return nativeToolExposure{restrict: true, allowed: allowed, domain: routingState.Domain}
}

func allowedNativeDomainTools(routingState *TaskContextClassification) (map[string]bool, map[string]bool) {
	allowedSpacesTools := make(map[string]bool)
	allowedStoresTools := make(map[string]bool)
	if routingState == nil {
		return allowedSpacesTools, allowedStoresTools
	}

	allowAll := len(routingState.Layers) == 0
	hasCRUD := func(flag string) bool {
		if allowAll {
			return true
		}
		for _, layer := range routingState.Layers {
			for _, crud := range layer.CRUD {
				if strings.EqualFold(strings.TrimSpace(crud), flag) {
					return true
				}
			}
		}
		return false
	}

	allowRead := hasCRUD("R") || allowAll
	allowCreate := hasCRUD("C") || allowAll
	allowUpdate := hasCRUD("U") || allowAll
	allowDelete := hasCRUD("D") || allowAll

	for _, name := range []string{"execute_script", "list_stores", "begin_tx", "commit_tx", "rollback_tx", "open_store", "select", "join", "join_right", "explain_join", "manage_transaction", "scan", "filter", "sort", "project", "limit"} {
		allowedStoresTools[name] = true
	}
	if allowCreate {
		allowedStoresTools["add"] = true
	}
	if allowUpdate {
		allowedStoresTools["update"] = true
	}
	if allowDelete {
		allowedStoresTools["delete"] = true
	}
	if !allowRead && !allowCreate && !allowUpdate && !allowDelete {
		allowedStoresTools = map[string]bool{}
	}

	if allowRead {
		for _, name := range []string{"read_space_config", "search_space", "list_space_categories", "list_space_items"} {
			allowedSpacesTools[name] = true
		}
	}
	if allowCreate || allowUpdate {
		for _, name := range []string{"mint_to_space", "update_space_config", "enrich_space", "vectorize_space", "vectorize_space_categories", "vectorize_space_items"} {
			allowedSpacesTools[name] = true
		}
	}
	if allowDelete {
		for _, name := range []string{"delete_space", "delete_space_items", "delete_space_categories"} {
			allowedSpacesTools[name] = true
		}
	}

	return allowedSpacesTools, allowedStoresTools
}

func isNativeDomainTool(name string) bool {
	return nativeSpaceToolNames[name] || nativeStoreToolNames[name] ||
		gitToolNames[name] || commandLineToolNames[name] ||
		postgresToolNames[name] || splunkToolNames[name]
}

func shouldExposeNativeTool(name string, exposure nativeToolExposure) bool {
	if !exposure.restrict {
		return true
	}
	// Always expose core tools
	if alwaysExposedTools[name] {
		return true
	}
	// For domain tools, check if allowed
	if !isNativeDomainTool(name) {
		return true
	}
	return exposure.allowed[name]
}

func (a *CopilotAgent) listRegisteredTools(exposure nativeToolExposure) []ai.ToolDefinition {
	if a.registry == nil {
		log.Info("CopilotAgent.listRegisteredTools: no registry available")
		return nil
	}
	tools := make([]ai.ToolDefinition, 0)
	for _, tool := range a.registry.List() {
		// Skip hidden tools only when routing is restricted AND tool is not explicitly allowed
		if tool.Hidden && exposure.restrict && !exposure.allowed[tool.Name] {
			continue
		}
		if !shouldExposeNativeTool(tool.Name, exposure) {
			continue
		}
		tools = append(tools, ai.ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Schema:      tool.ArgsSchema,
		})
	}
	log.Info("CopilotAgent.listRegisteredTools: built exposure list", "restricted", exposure.restrict, "domain", exposure.domain, "allowed_count", len(exposure.allowed), "tool_count", len(tools))
	return tools
}

const storedScriptArgsSchema = `{"type": "object", "properties": {"database": {"type": "string", "description": "Target database constraint (optional)"}}}`

func (a *CopilotAgent) listStoredScriptTools(ctx context.Context) []ai.ToolDefinition {
	if !StoresFeatureEnabled() {
		return nil
	}
	if a.systemDB == nil {
		return nil
	}
	tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil
	}
	defer tx.Rollback(ctx)

	store, err := a.systemDB.OpenModelStore(ctx, "scripts", tx)
	if err != nil {
		return nil
	}

	keys, _ := store.List(ctx, ai.DefaultScriptCategory)
	tools := make([]ai.ToolDefinition, 0, len(keys))
	for _, scriptName := range keys {
		var script ai.Script
		if err := store.Load(ctx, ai.DefaultScriptCategory, scriptName, &script); err != nil {
			continue
		}
		desc := script.Description
		if desc == "" {
			desc = "Executes the script '" + scriptName + "'"
		}
		tools = append(tools, ai.ToolDefinition{
			Name:        scriptName,
			Description: "Execute pre-saved user script. " + desc,
			Schema:      storedScriptArgsSchema,
		})
	}
	log.Info("CopilotAgent.listStoredScriptTools: built script tool list", "tool_count", len(tools))
	return tools
}
