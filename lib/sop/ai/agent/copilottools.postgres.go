package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sharedcode/sop/ai"
)

type postgresConfig struct {
	ConnectionString string
	Host             string
	Port             string
	Database         string
	User             string
	Password         string
}

var postgresQueryExecutor func(ctx context.Context, cfg postgresConfig, query string, allowWrite bool) (string, error)

func buildPostgresConfig(args map[string]any) postgresConfig {
	connectionString := firstNonEmptyString(args["connection_string"], os.Getenv("POSTGRES_CONNECTION_STRING"), "")
	if connectionString != "" {
		return postgresConfig{ConnectionString: connectionString}
	}
	return postgresConfig{
		Host:     firstNonEmptyString(args["host"], os.Getenv("POSTGRES_HOST"), "localhost"),
		Port:     firstNonEmptyString(args["port"], os.Getenv("POSTGRES_PORT"), "5432"),
		Database: firstNonEmptyString(args["database"], os.Getenv("POSTGRES_DATABASE"), "postgres"),
		User:     firstNonEmptyString(args["user"], os.Getenv("POSTGRES_USER"), "postgres"),
		Password: firstNonEmptyString(args["password"], os.Getenv("POSTGRES_PASSWORD"), ""),
	}
}

func postgresNativeAPIEnabled(args map[string]any) bool {
	if boolValueFromArgs(args, "use_native_api") {
		return true
	}
	return envEnabled("SOP_POSTGRES_NATIVE_API_ENABLED") || envEnabled("SOP_POSTGRES_API_ENABLED")
}

func applyPostgresQueryLimit(query string, limit int) string {
	trimmed := strings.TrimSpace(query)
	if limit <= 0 {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, " limit ") || strings.HasSuffix(lower, " limit") {
		return trimmed
	}
	if strings.HasSuffix(lower, ";") {
		trimmed = strings.TrimSuffix(trimmed, ";")
	}
	return fmt.Sprintf("%s LIMIT %d", trimmed, limit)
}

func executePostgresQueryNative(ctx context.Context, cfg postgresConfig, query string, allowWrite bool) (string, error) {
	if postgresQueryExecutor != nil {
		result, err := postgresQueryExecutor(ctx, cfg, query, allowWrite)
		if err == nil {
			// Emit result as content event for test compatibility
			emitAutomationToolStreamContent(ctx, result)
		}
		return result, err
	}
	if cfg.ConnectionString == "" {
		cfg.ConnectionString = fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	}
	if cfg.ConnectionString == "" {
		return "", fmt.Errorf("postgres connection config is not set")
	}
	db, err := sql.Open("postgres", cfg.ConnectionString)
	if err != nil {
		return "", err
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return "", err
	}

	if allowWrite {
		result, err := db.ExecContext(ctx, query)
		if err != nil {
			return "", err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return "", err
		}
		payload := map[string]any{"status": "ok", "rows_affected": affected, "mode": "write"}
		return marshalJSON(payload)
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(`{"status":"ok","columns":`)
	columnsJSON, _ := json.Marshal(columns)
	sb.Write(columnsJSON)
	sb.WriteString(`,"rows":[`)
	rowIndex := 0
	for rows.Next() {
		values := make([]any, len(columns))
		for i := range values {
			values[i] = new(sql.RawBytes)
		}
		if err := rows.Scan(values...); err != nil {
			return "", err
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			if value, ok := values[i].(*sql.RawBytes); ok {
				row[column] = string(*value)
			} else {
				row[column] = nil
			}
		}
		if rowIndex > 0 {
			sb.WriteByte(',')
		}
		rowJSON, _ := json.Marshal(row)
		sb.Write(rowJSON)
		if streamerEnabled := ctx.Value(ai.CtxKeyEventStreamer); streamerEnabled != nil {
			emitAutomationToolStreamContent(ctx, string(rowJSON))
		}
		rowIndex++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	sb.WriteString(`],"mode":"read"}`)
	return sb.String(), nil
}

func isPostgresWriteQuery(query string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(query))
	for _, prefix := range []string{"insert", "update", "delete", "create", "alter", "drop", "truncate", "grant", "revoke", "copy", "with"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
