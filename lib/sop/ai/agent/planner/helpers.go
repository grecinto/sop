package planner

import "strings"

// Common helper functions used across all phase implementations

func readPhaseString(args map[string]any, key, fallback string) string {
	if args == nil {
		return fallback
	}
	if value, ok := args[key]; ok {
		if str, ok := value.(string); ok {
			return strings.TrimSpace(str)
		}
	}
	return fallback
}

func readPhaseStringSlice(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	if value, ok := args[key]; ok {
		switch typed := value.(type) {
		case []string:
			return typed
		case []any:
			items := make([]string, 0, len(typed))
			for _, item := range typed {
				if str, ok := item.(string); ok {
					items = append(items, strings.TrimSpace(str))
				}
			}
			return items
		}
	}
	return nil
}

func readPhaseBool(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	if value, ok := args[key]; ok {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return false
}
