package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

type AutomationPolicy struct {
	Enabled               bool
	AllowShellCommands    bool
	AllowEmail            bool
	RequireApproval       bool
	DefaultTimeoutSeconds int
	MaxTimeoutSeconds     int
}

func DefaultAutomationPolicy() AutomationPolicy {
	return AutomationPolicy{
		Enabled:               false,
		AllowShellCommands:    false,
		AllowEmail:            false,
		RequireApproval:       true,
		DefaultTimeoutSeconds: 600,
		MaxTimeoutSeconds:     1800,
	}
}

type LocalCommandRequest struct {
	Command        string
	Reasoning      string
	WorkingDir     string
	TimeoutSeconds int
	SessionID      string
}

var localCommandExecutor func(ctx context.Context, req LocalCommandRequest) (string, error)
var localDesktopAutomationEnabled atomic.Bool
var automationPolicy atomic.Value

func init() {
	automationPolicy.Store(DefaultAutomationPolicy())
}

func SetLocalCommandExecutor(fn func(ctx context.Context, req LocalCommandRequest) (string, error)) {
	localCommandExecutor = fn
}

func SetLocalDesktopAutomationEnabled(enabled bool) {
	localDesktopAutomationEnabled.Store(enabled)
}

func SetAutomationPolicy(policy AutomationPolicy) {
	automationPolicy.Store(policy)
}

func GetAutomationPolicy() AutomationPolicy {
	if policy, ok := automationPolicy.Load().(AutomationPolicy); ok {
		return policy
	}
	return DefaultAutomationPolicy()
}

func (p AutomationPolicy) isShellAutomationEnabled() bool {
	if p.Enabled {
		return p.AllowShellCommands
	}
	return localDesktopAutomationEnabled.Load()
}

func (p AutomationPolicy) isEmailAutomationEnabled() bool {
	if p.Enabled {
		return p.AllowEmail
	}
	return localDesktopAutomationEnabled.Load()
}

func (p AutomationPolicy) validateRequest(req LocalCommandRequest) error {
	if !p.isShellAutomationEnabled() {
		return fmt.Errorf("local automation is disabled")
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = p.DefaultTimeoutSeconds
	}
	if req.TimeoutSeconds > p.MaxTimeoutSeconds {
		return fmt.Errorf("requested timeout %d exceeds policy maximum %d", req.TimeoutSeconds, p.MaxTimeoutSeconds)
	}
	return nil
}

const executeLocalCommandArgsSchema = `{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute on the user's local machine after approval."},"reasoning":{"type":"string","description":"Short explanation shown to the user for why this local command is needed."},"working_dir":{"type":"string","description":"Optional working directory for repo-aware tasks such as code generation, tests, and debugging."},"timeout_seconds":{"type":"integer","description":"Optional timeout in seconds for long-running commands such as builds or tests."}},"required":["command"]}`

const searchRepoFilesArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository or directory path to inspect."},"pattern":{"type":"string","description":"Optional filename pattern such as *.go or *.md."},"max_results":{"type":"integer","description":"Optional maximum number of files to return."}},"required":["path"]}`

const grepRepoTextArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository or directory path to inspect."},"query":{"type":"string","description":"Text to search for within files."},"max_results":{"type":"integer","description":"Optional maximum number of matching lines to return."}},"required":["path","query"]}`

const runRepoTestsArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository or module directory to test."},"package":{"type":"string","description":"Optional Go package pattern such as ./... or ./cmd/.."},"pattern":{"type":"string","description":"Optional Go test name pattern."},"timeout_seconds":{"type":"integer","description":"Optional timeout in seconds for the test run."}},"required":["path"]}`

const readFileArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read."},"start_line":{"type":"integer","description":"Optional first line to include."},"end_line":{"type":"integer","description":"Optional last line to include."}},"required":["path"]}`

const writeFileArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write."},"content":{"type":"string","description":"Full content to write to the file."}},"required":["path","content"]}`

const patchFileArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to patch."},"old_string":{"type":"string","description":"Exact text to replace in the file."},"new_string":{"type":"string","description":"Replacement text to write."}},"required":["path","old_string","new_string"]}`

const createDirectoryArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Directory path to create."}},"required":["path"]}`

const renameFileArgsSchema = `{"type":"object","properties":{"old_path":{"type":"string","description":"Current file path."},"new_path":{"type":"string","description":"Destination file path."}},"required":["old_path","new_path"]}`

const deleteFileArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"File path to delete."}},"required":["path"]}`

const deleteDirectoryArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Directory path to delete."}},"required":["path"]}`

const copyFileArgsSchema = `{"type":"object","properties":{"source_path":{"type":"string","description":"Source file path to copy."},"destination_path":{"type":"string","description":"Destination file path."}},"required":["source_path","destination_path"]}`

const listDirectoryArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Directory path to inspect."}},"required":["path"]}`

const pathExistsArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"File or directory path to check."}},"required":["path"]}`

const statPathArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"File or directory path to inspect."}},"required":["path"]}`

const sendEmailArgsSchema = `{"type":"object","properties":{"to":{"type":"string","description":"Recipient email address."},"subject":{"type":"string","description":"Email subject line."},"body":{"type":"string","description":"Email body content."}},"required":["to"]}`

// toolExecuteLocalCommand equips the LLM to orchestrate shell commands on the user's local machine.
// Instead of running the command on the server, it returns an action payload that the UI interprets
// to communicate with the local SOP Desktop Companion daemon.

func (a *CopilotAgent) registerAutomationTools(ctx context.Context) {
	policy := GetAutomationPolicy()
	if !policy.Enabled && !localDesktopAutomationEnabled.Load() {
		return
	}

	if policy.isShellAutomationEnabled() {
		a.registry.Register("execute_local_command", "Executes a shell command on the user's local machine via the SOP Desktop Daemon. Use this for building code, git operations, local file inspection, or running local scripts. This will prompt the user for approval in the UI.", executeLocalCommandArgsSchema, wrapStringTool(a.toolExecuteLocalCommand))
		a.registry.Register("search_repo_files", "Searches a repository or directory for files matching a pattern. Useful for locating implementation files, docs, tests, or configs.", searchRepoFilesArgsSchema, wrapStringTool(a.toolSearchRepoFiles))
		a.registry.Register("grep_repo_text", "Searches repository text for a query and returns matching lines. Useful for finding TODOs, symbols, references, or debugging clues.", grepRepoTextArgsSchema, wrapStringTool(a.toolGrepRepoText))
		a.registry.Register("run_repo_tests", "Runs Go tests in a repository or module directory. Useful for validating changes, reproducing failures, and inspecting test output.", runRepoTestsArgsSchema, wrapStringTool(a.toolRunRepoTests))
		a.registry.Register("read_file", "Reads a text file from the local workspace and returns the requested line range. Useful for inspecting source, config, or documentation files.", readFileArgsSchema, wrapStringTool(a.toolReadFile))
		a.registry.Register("write_file", "Writes a complete file to the local workspace. Useful for creating or overwriting source, config, or documentation files with explicit content.", writeFileArgsSchema, wrapStringTool(a.toolWriteFile))
		a.registry.Register("patch_file", "Replaces an exact text fragment in a local file with new content. Useful for focused edits to source, config, or docs.", patchFileArgsSchema, wrapStringTool(a.toolPatchFile))
		a.registry.Register("create_directory", "Creates a directory and any missing parent directories in the local workspace.", createDirectoryArgsSchema, wrapStringTool(a.toolCreateDirectory))
		a.registry.Register("rename_file", "Renames or moves a file in the local workspace.", renameFileArgsSchema, wrapStringTool(a.toolRenameFile))
		a.registry.Register("delete_file", "Deletes a file from the local workspace.", deleteFileArgsSchema, wrapStringTool(a.toolDeleteFile))
		a.registry.Register("delete_directory", "Deletes a directory from the local workspace.", deleteDirectoryArgsSchema, wrapStringTool(a.toolDeleteDirectory))
		a.registry.Register("copy_file", "Copies a file from one path to another in the local workspace.", copyFileArgsSchema, wrapStringTool(a.toolCopyFile))
		a.registry.Register("list_directory", "Lists the contents of a directory in the local workspace for inspection before further edits.", listDirectoryArgsSchema, wrapStringTool(a.toolListDirectory))
		a.registry.Register("path_exists", "Checks whether a file or directory exists at the requested path in the local workspace.", pathExistsArgsSchema, wrapStringTool(a.toolPathExists))
		a.registry.Register("stat_path", "Returns file or directory metadata for the requested path in the local workspace.", statPathArgsSchema, wrapStringTool(a.toolStatPath))
	}

	if policy.isEmailAutomationEnabled() {
		a.registry.Register("send_email", "Sends an email.", sendEmailArgsSchema, wrapStringTool(a.toolSendEmail))
	}
}
func isSensitiveCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	sensitiveIndicators := []string{"sudo", "rm -rf", "chmod 777", "chown", "curl", "wget", "ssh ", "scp ", "docker", "kubectl", "systemctl", "service ", "apt-get", "brew install", "npm install", "pip install"}
	for _, indicator := range sensitiveIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

func requiresPasswordPrompt(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	return strings.Contains(lower, "sudo") || strings.Contains(lower, "ssh ") || strings.Contains(lower, "scp ")
}

func sessionIDFromArgs(args map[string]any) string {
	if raw, ok := args["session_id"]; ok {
		if sessionID, ok := raw.(string); ok {
			return sessionID
		}
	}
	return ""
}

func determineApprovalReason(command, reasoning string, requiresApproval, requiresPasswordPrompt bool) string {
	if requiresPasswordPrompt {
		return "This command may require elevated access or credentials, so it needs explicit user approval and a password prompt before execution."
	}
	if requiresApproval {
		return "This command performs a local action that could affect the workspace or system, so it requires explicit user approval."
	}
	if reasoning != "" {
		return reasoning
	}
	return "This command will run on the local machine and needs user confirmation before execution."
}

func (a *CopilotAgent) toolExecuteLocalCommand(ctx context.Context, args map[string]any) (string, error) {
	commandRaw, ok := args["command"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'command'")
	}
	command, ok := commandRaw.(string)
	if !ok {
		return "", fmt.Errorf("'command' must be a string")
	}
	return a.runLocalAutomationCommand(ctx, args, command, "", "", 600)
}

func (a *CopilotAgent) toolSearchRepoFiles(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		path = "."
	}

	patternRaw, _ := args["pattern"]
	pattern, _ := patternRaw.(string)
	if pattern == "" {
		pattern = "*"
	}

	maxResults := 50
	if maxResultsRaw, ok := args["max_results"]; ok {
		switch v := maxResultsRaw.(type) {
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

	command := fmt.Sprintf("find %s -type f -name %s | head -n %d", shellQuote(path), shellQuote(pattern), maxResults)
	return a.runLocalAutomationCommand(ctx, args, command, "Search the repository for files matching the requested pattern.", "", 60)
}

func (a *CopilotAgent) toolGrepRepoText(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		path = "."
	}

	queryRaw, ok := args["query"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'query'")
	}
	query, ok := queryRaw.(string)
	if !ok {
		return "", fmt.Errorf("'query' must be a string")
	}

	maxResults := 50
	if maxResultsRaw, ok := args["max_results"]; ok {
		switch v := maxResultsRaw.(type) {
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

	command := fmt.Sprintf("grep -R -n -H --binary-files=without-match %s %s | head -n %d", shellQuote(query), shellQuote(path), maxResults)
	return a.runLocalAutomationCommand(ctx, args, command, "Search the repository for the requested text and return matching lines.", "", 60)
}

func (a *CopilotAgent) toolRunRepoTests(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		path = "."
	}

	pkgRaw, _ := args["package"]
	pkg, _ := pkgRaw.(string)
	if pkg == "" {
		pkg = "./..."
	}

	patternRaw, _ := args["pattern"]
	pattern, _ := patternRaw.(string)

	command := "go test " + pkg
	if pattern != "" {
		command += " -run " + shellQuote(pattern)
	}

	return a.runLocalAutomationCommand(ctx, args, command, "Run the relevant Go tests for the requested repository path.", path, 120)
}

func (a *CopilotAgent) toolReadFile(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		return "", fmt.Errorf("'path' must not be empty")
	}

	startLine := 1
	if startLineRaw, ok := args["start_line"]; ok {
		switch v := startLineRaw.(type) {
		case int:
			startLine = v
		case float64:
			startLine = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				startLine = int(parsed)
			}
		}
	}

	endLine := 200
	if endLineRaw, ok := args["end_line"]; ok {
		switch v := endLineRaw.(type) {
		case int:
			endLine = v
		case float64:
			endLine = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				endLine = int(parsed)
			}
		}
	}
	if endLine < startLine {
		endLine = startLine
	}

	command := fmt.Sprintf("sed -n '%d,%dp' %s", startLine, endLine, shellQuote(path))
	return a.runLocalAutomationCommand(ctx, args, command, "Read the requested file range from the local workspace.", "", 60)
}

func (a *CopilotAgent) toolWriteFile(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		return "", fmt.Errorf("'path' must not be empty")
	}

	contentRaw, ok := args["content"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'content'")
	}
	content, ok := contentRaw.(string)
	if !ok {
		return "", fmt.Errorf("'content' must be a string")
	}

	command := fmt.Sprintf("mkdir -p %s && cat <<'EOF' > %s\n%s\nEOF", shellQuote(pathDir(path)), shellQuote(path), content)
	return a.runLocalAutomationCommand(ctx, args, command, "Write the requested content to the local workspace file.", "", 60)
}

func (a *CopilotAgent) toolPatchFile(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		return "", fmt.Errorf("'path' must not be empty")
	}

	oldStringRaw, ok := args["old_string"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'old_string'")
	}
	oldString, ok := oldStringRaw.(string)
	if !ok {
		return "", fmt.Errorf("'old_string' must be a string")
	}

	newStringRaw, ok := args["new_string"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'new_string'")
	}
	newString, ok := newStringRaw.(string)
	if !ok {
		return "", fmt.Errorf("'new_string' must be a string")
	}

	pythonScript := fmt.Sprintf(`import pathlib, sys; p=pathlib.Path(%q); data=p.read_text(); old=%q; new=%q; assert old in data, 'old_string not found'; p.write_text(data.replace(old, new, 1))`, path, oldString, newString)
	command := fmt.Sprintf("python3 - <<'PY'\n%s\nPY", pythonScript)
	return a.runLocalAutomationCommand(ctx, args, command, "Patch the requested text in the local workspace file.", "", 60)
}

func (a *CopilotAgent) toolCreateDirectory(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		return "", fmt.Errorf("'path' must not be empty")
	}

	command := fmt.Sprintf("mkdir -p %s", shellQuote(path))
	return a.runLocalAutomationCommand(ctx, args, command, "Create the requested directory in the local workspace.", "", 60)
}

func (a *CopilotAgent) toolRenameFile(ctx context.Context, args map[string]any) (string, error) {
	oldPathRaw, ok := args["old_path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'old_path'")
	}
	oldPath, ok := oldPathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'old_path' must be a string")
	}
	if oldPath == "" {
		return "", fmt.Errorf("'old_path' must not be empty")
	}

	newPathRaw, ok := args["new_path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'new_path'")
	}
	newPath, ok := newPathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'new_path' must be a string")
	}
	if newPath == "" {
		return "", fmt.Errorf("'new_path' must not be empty")
	}

	command := fmt.Sprintf("mv %s %s", shellQuote(oldPath), shellQuote(newPath))
	return a.runLocalAutomationCommand(ctx, args, command, "Rename or move the requested file in the local workspace.", "", 60)
}

func (a *CopilotAgent) toolDeleteFile(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		return "", fmt.Errorf("'path' must not be empty")
	}

	command := fmt.Sprintf("rm -f %s", shellQuote(path))
	return a.runLocalAutomationCommand(ctx, args, command, "Delete the requested file from the local workspace.", "", 60)
}

func (a *CopilotAgent) toolDeleteDirectory(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		return "", fmt.Errorf("'path' must not be empty")
	}

	command := fmt.Sprintf("rm -rf %s", shellQuote(path))
	return a.runLocalAutomationCommand(ctx, args, command, "Delete the requested directory from the local workspace.", "", 60)
}

func (a *CopilotAgent) toolCopyFile(ctx context.Context, args map[string]any) (string, error) {
	sourcePathRaw, ok := args["source_path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'source_path'")
	}
	sourcePath, ok := sourcePathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'source_path' must be a string")
	}
	if sourcePath == "" {
		return "", fmt.Errorf("'source_path' must not be empty")
	}

	destinationPathRaw, ok := args["destination_path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'destination_path'")
	}
	destinationPath, ok := destinationPathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'destination_path' must be a string")
	}
	if destinationPath == "" {
		return "", fmt.Errorf("'destination_path' must not be empty")
	}

	command := fmt.Sprintf("cp %s %s", shellQuote(sourcePath), shellQuote(destinationPath))
	return a.runLocalAutomationCommand(ctx, args, command, "Copy the requested file in the local workspace.", "", 60)
}

func (a *CopilotAgent) toolListDirectory(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		path = "."
	}

	command := fmt.Sprintf("ls -la %s", shellQuote(path))
	return a.runLocalAutomationCommand(ctx, args, command, "List the contents of the requested directory in the local workspace.", "", 60)
}

func (a *CopilotAgent) toolPathExists(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		return "", fmt.Errorf("'path' must not be empty")
	}

	command := fmt.Sprintf("test -e %s", shellQuote(path))
	return a.runLocalAutomationCommand(ctx, args, command, "Check whether the requested path exists in the local workspace.", "", 60)
}

func (a *CopilotAgent) toolStatPath(ctx context.Context, args map[string]any) (string, error) {
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' must be a string")
	}
	if path == "" {
		return "", fmt.Errorf("'path' must not be empty")
	}

	command := fmt.Sprintf("stat %s", shellQuote(path))
	return a.runLocalAutomationCommand(ctx, args, command, "Inspect the requested path metadata in the local workspace.", "", 60)
}

func pathDir(path string) string {
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash <= 0 {
		return "."
	}
	return path[:lastSlash]
}

func shellQuote(value string) string {
	return strconv.Quote(value)
}

func (a *CopilotAgent) runLocalAutomationCommand(ctx context.Context, args map[string]any, command, defaultReasoning, defaultWorkingDir string, defaultTimeoutSeconds int) (string, error) {
	reasoningRaw, _ := args["reasoning"]
	reasoning, _ := reasoningRaw.(string)
	if reasoning == "" {
		reasoning = defaultReasoning
	}

	workingDirRaw, _ := args["working_dir"]
	workingDir, _ := workingDirRaw.(string)
	if workingDir == "" {
		workingDir = defaultWorkingDir
	}

	timeoutSeconds := defaultTimeoutSeconds
	if timeoutSecondsRaw, ok := args["timeout_seconds"]; ok {
		switch v := timeoutSecondsRaw.(type) {
		case int:
			timeoutSeconds = v
		case float64:
			timeoutSeconds = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				timeoutSeconds = int(parsed)
			}
		}
	}

	request := LocalCommandRequest{
		Command:        command,
		Reasoning:      reasoning,
		WorkingDir:     workingDir,
		TimeoutSeconds: timeoutSeconds,
		SessionID:      sessionIDFromArgs(args),
	}

	policy := GetAutomationPolicy()
	if err := policy.validateRequest(request); err != nil {
		return "", err
	}

	if localCommandExecutor != nil {
		return localCommandExecutor(ctx, request)
	}

	requiresApproval := policy.RequireApproval || isSensitiveCommand(command)
	requiresPasswordPrompt := requiresPasswordPrompt(command)
	approvalReason := determineApprovalReason(command, reasoning, requiresApproval, requiresPasswordPrompt)

	payload := map[string]any{
		"action_type":              "local_shell_execution",
		"command":                  command,
		"reasoning":                reasoning,
		"working_dir":              workingDir,
		"timeout_seconds":          timeoutSeconds,
		"requires_approval":        requiresApproval,
		"requires_password_prompt": requiresPasswordPrompt,
		"approval_reason":          approvalReason,
	}

	payloadBytes, _ := json.Marshal(payload)

	return fmt.Sprintf("<<<ACTION_REQUIRED:%s>>>", string(payloadBytes)), nil
}
