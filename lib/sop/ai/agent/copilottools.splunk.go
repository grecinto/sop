package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type splunkConfig struct {
	BaseURL   string
	Username  string
	Password  string
	Token     string
	Index     string
	Earliest  string
	Latest    string
	MaxResult int
}

var splunkSearchExecutor func(ctx context.Context, cfg splunkConfig, query string) (string, error)

func buildSplunkConfig(args map[string]any) splunkConfig {
	maxResults := 50
	if raw, ok := args["max_results"]; ok {
		switch v := raw.(type) {
		case int:
			maxResults = v
		case float64:
			maxResults = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				maxResults = int(parsed)
			}
		}
	}
	if maxResults <= 0 {
		maxResults = 50
	}
	baseURL := firstNonEmptyString(args["base_url"], os.Getenv("SPLUNK_BASE_URL"), "")
	if baseURL == "" {
		host := firstNonEmptyString(args["host"], os.Getenv("SPLUNK_HOST"), "localhost")
		port := firstNonEmptyString(args["port"], os.Getenv("SPLUNK_PORT"), "8089")
		if strings.Contains(host, "://") {
			baseURL = host
		} else {
			baseURL = fmt.Sprintf("http://%s", host)
			if port != "" && port != "80" && port != "443" {
				baseURL = fmt.Sprintf("%s:%s", baseURL, port)
			}
		}
	}
	return splunkConfig{
		BaseURL:   baseURL,
		Username:  firstNonEmptyString(args["username"], os.Getenv("SPLUNK_USERNAME"), ""),
		Password:  firstNonEmptyString(args["password"], os.Getenv("SPLUNK_PASSWORD"), ""),
		Token:     firstNonEmptyString(args["token"], os.Getenv("SPLUNK_AUTH_TOKEN"), ""),
		Index:     firstNonEmptyString(args["index"], os.Getenv("SPLUNK_INDEX"), ""),
		Earliest:  stringArg(args, "earliest"),
		Latest:    stringArg(args, "latest"),
		MaxResult: maxResults,
	}
}

func splunkNativeAPIEnabled(args map[string]any) bool {
	if boolValueFromArgs(args, "use_native_api") {
		return true
	}
	return envEnabled("SOP_SPLUNK_NATIVE_API_ENABLED") || envEnabled("SOP_SPLUNK_API_ENABLED")
}

func executeSplunkSearchNative(ctx context.Context, cfg splunkConfig, query string) (string, error) {
	if splunkSearchExecutor != nil {
		result, err := splunkSearchExecutor(ctx, cfg, query)
		if err == nil {
			// Emit result as content event for test compatibility
			emitAutomationToolStreamContent(ctx, result)
		}
		return result, err
	}
	if cfg.BaseURL == "" {
		return "", fmt.Errorf("splunk base URL is not set")
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	u, err := url.Parse(baseURL + "/services/search/jobs/export")
	if err != nil {
		return "", err
	}
	params := url.Values{}
	params.Set("search", query)
	params.Set("output_mode", "json")
	params.Set("count", strconv.Itoa(cfg.MaxResult))
	if cfg.Index != "" {
		params.Set("index", cfg.Index)
	}
	if cfg.Earliest != "" {
		params.Set("earliest", cfg.Earliest)
	}
	if cfg.Latest != "" {
		params.Set("latest", cfg.Latest)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(""))
	if err != nil {
		return "", err
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	} else if cfg.Username != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("splunk request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	payload := strings.TrimSpace(string(body))
	if payload != "" {
		if strings.HasPrefix(payload, "[") {
			var items []json.RawMessage
			if err := json.Unmarshal([]byte(payload), &items); err == nil {
				for _, item := range items {
					emitAutomationToolStreamContent(ctx, string(item))
				}
				return payload, nil
			}
		}
		if strings.Contains(payload, "\n") {
			for _, line := range strings.Split(payload, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					emitAutomationToolStreamContent(ctx, trimmed)
				}
			}
			return payload, nil
		}
		emitAutomationToolStreamContent(ctx, payload)
	}
	return payload, nil
}
