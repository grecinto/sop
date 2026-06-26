package agent

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// nativeDataAdapter is the shared contract for backend-specific native integrations.
type nativeDataAdapter interface {
	enabled(args map[string]any) bool
	execute(ctx context.Context, args map[string]any) (string, error)
}

func envEnabled(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return strings.EqualFold(value, "true") || strings.EqualFold(value, "1") || strings.EqualFold(value, "yes")
	}
	return parsed
}

func boolValueFromArgs(args map[string]any, key string) bool {
	if raw, ok := args[key]; ok {
		switch value := raw.(type) {
		case bool:
			return value
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(value))
			if err == nil {
				return parsed
			}
		}
	}
	return false
}

func marshalJSON(payload any) (string, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
