package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sharedcode/sop/ai"
)

type AutomationPolicy struct {
	Enabled                   bool
	ShellAutomationEnabled    *bool
	GitAutomationEnabled      *bool
	PostgresAutomationEnabled *bool
	SplunkAutomationEnabled   *bool
	AllowShellCommands        bool
	AllowEmail                bool
	AllowPostgresRead         bool
	AllowPostgresWrite        bool
	AllowSplunkRead           bool
	AllowSplunkWrite          bool
	RequireApproval           bool
	DefaultTimeoutSeconds     int
	MaxTimeoutSeconds         int
}

func DefaultAutomationPolicy() AutomationPolicy {
	shellEnabled := true
	gitEnabled := true
	postgresEnabled := true
	splunkEnabled := true
	return AutomationPolicy{
		Enabled:                   false,
		ShellAutomationEnabled:    &shellEnabled,
		GitAutomationEnabled:      &gitEnabled,
		PostgresAutomationEnabled: &postgresEnabled,
		SplunkAutomationEnabled:   &splunkEnabled,
		AllowShellCommands:        false,
		AllowEmail:                false,
		RequireApproval:           true,
		DefaultTimeoutSeconds:     600,
		MaxTimeoutSeconds:         1800,
	}
}

// LocalCommandRequest specifies parameters for executing local commands with optional
// real-time streaming and stdin support.
//
// Basic Usage:
//
//	req := LocalCommandRequest{
//	    Command: "ls -la",
//	    WorkingDir: "/home/user",
//	    TimeoutSeconds: 30,
//	}
//
// Real-Time Streaming (Phase 1 Enhancement):
//
//	req := LocalCommandRequest{
//	    Command: "go test ./...",
//	    StreamStdout: func(line string) {
//	        // Receives each line as it's produced
//	        fmt.Print(line)
//	    },
//	    StreamStderr: func(line string) {
//	        // Receives errors separately with ⚠️ prefix
//	        fmt.Printf("ERROR: %s", line)
//	    },
//	}
//
// Interactive Commands with Stdin:
//
//	req := LocalCommandRequest{
//	    Command: "cat",
//	    Stdin: "hello from AI\n",
//	    StreamStdout: func(line string) {
//	        fmt.Println(line)  // Receives "hello from AI"
//	    },
//	}
//
// Completion Callback:
//
//	req := LocalCommandRequest{
//	    Command: "npm install",
//	    OnComplete: func(stdout, stderr string, exitCode int) {
//	        if exitCode == 0 {
//	            log.Println("Success!")
//	        } else {
//	            log.Printf("Failed with code %d", exitCode)
//	        }
//	    },
//	}
//
// Backward Compatibility:
// StreamOutput callback is deprecated but still supported. If both StreamOutput and
// StreamStdout/StreamStderr are provided, the new callbacks take precedence.
type LocalCommandRequest struct {
	Command        string // Command to execute (e.g., "ls -la", "npm install")
	Reasoning      string // Optional: Explanation of why this command is being run
	WorkingDir     string // Working directory for command execution
	TimeoutSeconds int    // Maximum execution time (default: 600s)
	SessionID      string // Session ID for persistent working directory across commands

	// Deprecated: Use StreamStdout/StreamStderr for real-time streaming
	StreamOutput func(string)

	// Phase 1 Enhancements: Real-Time I/O
	Stdin        string                                    // Input to send to command's stdin
	StreamStdout func(string)                              // Called for each stdout line in real-time
	StreamStderr func(string)                              // Called for each stderr line in real-time
	OnComplete   func(stdout, stderr string, exitCode int) // Called when command finishes

	// Phase 2 Enhancements: Process Control
	ProcessGroup   bool          // Create new process group for signal isolation
	OnProcessStart func(pid int) // Called when process starts with PID

	// Phase 3 Enhancements: Background Jobs
	Background bool   // Run in background, return immediately with JobID
	JobID      string // Optional: Specify custom job ID (auto-generated if empty)
}

// BackgroundJob represents a command running in background
type BackgroundJob struct {
	JobID     string
	Command   string
	PID       int
	StartTime time.Time
	Stdout    strings.Builder
	Stderr    strings.Builder
	ExitCode  *int // nil if still running
	Error     string
	mu        sync.Mutex
}

var localCommandExecutor func(ctx context.Context, req LocalCommandRequest) (string, error)
var localSendSignalToProcess func(pid int, signal os.Signal) error
var localGetBackgroundJob func(jobID string) (*BackgroundJob, error)
var localKillBackgroundJob func(jobID string) error
var localDesktopAutomationEnabled atomic.Bool
var automationPolicy atomic.Value
var storesFeatureEnabled atomic.Bool
var kbWorkflowsEnabled atomic.Bool

func init() {
	automationPolicy.Store(DefaultAutomationPolicy())
	// Stores enabled by default so the runtime exposes the capability out of the box.
	storesFeatureEnabled.Store(true)
	// KB workflows disabled by default for v1 - needs infrastructure development
	kbWorkflowsEnabled.Store(false)
}

func SetLocalCommandExecutor(fn func(ctx context.Context, req LocalCommandRequest) (string, error)) {
	localCommandExecutor = fn
}

// Phase 2: Signal control
func SetLocalSendSignalToProcess(fn func(pid int, signal os.Signal) error) {
	localSendSignalToProcess = fn
}

// Phase 3: Background job control
func SetLocalGetBackgroundJob(fn func(jobID string) (*BackgroundJob, error)) {
	localGetBackgroundJob = fn
}

func SetLocalKillBackgroundJob(fn func(jobID string) error) {
	localKillBackgroundJob = fn
}

func SetLocalDesktopAutomationEnabled(enabled bool) {
	localDesktopAutomationEnabled.Store(enabled)
}

func SetAutomationPolicy(policy AutomationPolicy) {
	automationPolicy.Store(policy)
}

// SetStoresFeatureEnabled controls whether Stores capability and tools are available.
// Disabled by default for v1 (focused on AI Copilot), can be enabled for v1.1+.
func SetStoresFeatureEnabled(enabled bool) {
	storesFeatureEnabled.Store(enabled)
}

// StoresFeatureEnabled returns true if Stores capability should be active.
func StoresFeatureEnabled() bool {
	return storesFeatureEnabled.Load()
}

// SetKBWorkflowsEnabled enables or disables KB-driven workflows
func SetKBWorkflowsEnabled(enabled bool) {
	kbWorkflowsEnabled.Store(enabled)
}

// KBWorkflowsEnabled returns whether KB-driven workflows are enabled
func KBWorkflowsEnabled() bool {
	return kbWorkflowsEnabled.Load()
}

// formatCommandOutputForDisplay formats command output for better readability.
// For list-like outputs (e.g., ls), formats into two aligned columns.
func formatCommandOutputForDisplay(command, output string) string {
	// Only format if it's a list command (ls, dir, etc.) and output looks like a simple list
	cmdLower := strings.ToLower(strings.TrimSpace(command))
	isListCommand := strings.HasPrefix(cmdLower, "ls") ||
		strings.HasPrefix(cmdLower, "dir") ||
		strings.HasPrefix(cmdLower, "ll")

	if !isListCommand {
		return output
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Need at least 4 items to make two columns worthwhile
	if len(lines) < 4 {
		return output
	}

	// Check if lines are simple (no tabs, not overly complex formatting)
	for _, line := range lines {
		// Skip already formatted output (with multiple spaces, tabs, or very long lines)
		if strings.Contains(line, "\t") || strings.Contains(line, "  ") || len(line) > 50 {
			return output
		}
	}

	// Format into two columns
	var sb strings.Builder
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}

	// Add padding for column alignment
	colWidth := maxLen + 4

	for i := 0; i < len(lines); i += 2 {
		left := lines[i]
		if i+1 < len(lines) {
			right := lines[i+1]
			sb.WriteString(fmt.Sprintf("%-*s  %s\n", colWidth, left, right))
		} else {
			// Odd number of items, last one goes on its own line
			sb.WriteString(left + "\n")
		}
	}

	return sb.String()
}

func GetAutomationPolicy() AutomationPolicy {
	if policy, ok := automationPolicy.Load().(AutomationPolicy); ok {
		return policy
	}
	return DefaultAutomationPolicy()
}

func boolPtr(v bool) *bool {
	return &v
}

func boolValue(v *bool, fallback bool) bool {
	if v != nil {
		return *v
	}
	return fallback
}

func (p AutomationPolicy) IsFamilyRegistered(family string) bool {
	if !p.Enabled && !localDesktopAutomationEnabled.Load() {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(family)) {
	case "shell":
		return boolValue(p.ShellAutomationEnabled, true)
	case "git":
		return boolValue(p.GitAutomationEnabled, true)
	case "postgres":
		return boolValue(p.PostgresAutomationEnabled, true)
	case "splunk":
		return boolValue(p.SplunkAutomationEnabled, true)
	default:
		return false
	}
}

func (p AutomationPolicy) isShellAutomationEnabled() bool {
	if !p.IsFamilyRegistered("shell") {
		return false
	}
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

func (p AutomationPolicy) isPostgresReadEnabled() bool {
	enabled := p.Enabled || localDesktopAutomationEnabled.Load()
	if !enabled || !p.IsFamilyRegistered("postgres") {
		return false
	}
	return p.AllowPostgresRead || p.AllowPostgresWrite || (!p.AllowPostgresRead && !p.AllowPostgresWrite)
}

func (p AutomationPolicy) isPostgresWriteEnabled() bool {
	enabled := p.Enabled || localDesktopAutomationEnabled.Load()
	if !enabled || !p.IsFamilyRegistered("postgres") {
		return false
	}
	return p.AllowPostgresWrite
}

func (p AutomationPolicy) isSplunkReadEnabled() bool {
	enabled := p.Enabled || localDesktopAutomationEnabled.Load()
	if !enabled || !p.IsFamilyRegistered("splunk") {
		return false
	}
	return p.AllowSplunkRead || p.AllowSplunkWrite || (!p.AllowSplunkRead && !p.AllowSplunkWrite)
}

func (p AutomationPolicy) isSplunkWriteEnabled() bool {
	enabled := p.Enabled || localDesktopAutomationEnabled.Load()
	if !enabled || !p.IsFamilyRegistered("splunk") {
		return false
	}
	return p.AllowSplunkWrite
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

const executeLocalCommandArgsSchema = `{"type":"object","properties":{"command":{"type":"string","description":"Shell command, shell script, or runnable executable path to execute on the user's local machine after approval."},"reasoning":{"type":"string","description":"Short explanation shown to the user for why this local command is needed."},"working_dir":{"type":"string","description":"Optional working directory for repo-aware tasks such as code generation, tests, and debugging."},"timeout_seconds":{"type":"integer","description":"Optional timeout in seconds for long-running commands such as builds or tests."},"background":{"type":"boolean","description":"Run command in background (non-blocking). Default: false (sequential execution). Only set to true when user explicitly requests parallel execution with phrases like 'in background', 'in parallel', 'while', 'simultaneously'. When unclear, ask user before setting to true."}},"required":["command"]}`

const searchRepoFilesArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository or directory path to inspect."},"pattern":{"type":"string","description":"Optional filename pattern such as *.go or *.md."},"max_results":{"type":"integer","description":"Optional maximum number of files to return."}},"required":["path"]}`

const grepRepoTextArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository or directory path to inspect."},"query":{"type":"string","description":"Text to search for within files."},"max_results":{"type":"integer","description":"Optional maximum number of matching lines to return."}},"required":["path","query"]}`

const runRepoTestsArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository or module directory to test."},"package":{"type":"string","description":"Optional Go package pattern such as ./... or ./cmd/.."},"pattern":{"type":"string","description":"Optional Go test name pattern."},"timeout_seconds":{"type":"integer","description":"Optional timeout in seconds for the test run."}},"required":["path"]}`

const readFileArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read."},"start_line":{"type":"integer","description":"Optional first line to include."},"end_line":{"type":"integer","description":"Optional last line to include."}},"required":["path"]}`

const listFolderStoreFilesArgsSchema = `{"type":"object","properties":{"database":{"type":"string","description":"Optional database override. Defaults to the active session database."},"store":{"type":"string","description":"Name of the folder-backed source store to inspect."}},"required":["store"]}`

const readFolderStoreFileArgsSchema = `{"type":"object","properties":{"database":{"type":"string","description":"Optional database override. Defaults to the active session database."},"store":{"type":"string","description":"Name of the folder-backed source store to inspect."},"path":{"type":"string","description":"Relative path to the file inside the source store."}},"required":["store","path"]}`

const listRepoStoreFilesArgsSchema = `{"type":"object","properties":{"database":{"type":"string","description":"Optional database override. Defaults to the active session database."},"store":{"type":"string","description":"Name of the repository-backed source store to inspect."}},"required":["store"]}`

const readRepoStoreFileArgsSchema = `{"type":"object","properties":{"database":{"type":"string","description":"Optional database override. Defaults to the active session database."},"store":{"type":"string","description":"Name of the repository-backed source store to inspect."},"path":{"type":"string","description":"Relative path to the file inside the repository-backed source store."}},"required":["store","path"]}`

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

const gitStatusArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository path to inspect."},"short":{"type":"boolean","description":"Return a concise status summary."}}}`
const gitDiffArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository path to inspect."},"staged":{"type":"boolean","description":"Show staged changes only."}}}`
const gitLogArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository path to inspect."},"max_count":{"type":"integer","description":"Maximum number of commits to return."}}}`
const gitBranchArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository path to inspect."}}}`
const gitCheckoutArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository path to inspect."},"ref":{"type":"string","description":"Branch, tag, or commit to switch to."}},"required":["path","ref"]}`
const gitCommitArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository path to inspect."},"message":{"type":"string","description":"Commit message."},"files":{"type":"array","items":{"type":"string"},"description":"Optional list of specific files to stage before committing. If omitted and 'all' is true, stages all modified files."},"all":{"type":"boolean","description":"Stage all modified and deleted files before committing. Ignored if 'files' is specified."}},"required":["path","message"]}`
const gitPullArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository path to inspect."},"rebase":{"type":"boolean","description":"Use rebase instead of merge when pulling."}}}`
const gitPushArgsSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Repository path to inspect."},"remote":{"type":"string","description":"Remote name to push to."},"branch":{"type":"string","description":"Branch name to push."}}}`

// Phase 2.3: Tool Result Analysis
const listToolResultsArgsSchema = `{"type":"object","properties":{}}`
const readToolResultArgsSchema = `{"type":"object","properties":{"file_path":{"type":"string","description":"Path to the stored tool result file."},"start_line":{"type":"integer","description":"Starting line number (1-indexed)."},"end_line":{"type":"integer","description":"Ending line number (inclusive)."}},"required":["file_path"]}`
const grepToolResultArgsSchema = `{"type":"object","properties":{"file_path":{"type":"string","description":"Path to the stored tool result file."},"pattern":{"type":"string","description":"Text pattern to search for within the file."},"max_matches":{"type":"integer","description":"Maximum number of matching lines to return (default: 100)."}},"required":["file_path","pattern"]}`
const analyzeToolResultArgsSchema = `{"type":"object","properties":{"file_path":{"type":"string","description":"Path to the stored tool result file."},"operation":{"type":"string","description":"Analysis operation to run: grep, awk, sed, head, tail, wc, or cat."},"pattern":{"type":"string","description":"Text pattern to search for when using grep or awk."},"start_line":{"type":"integer","description":"First line number to include for sed operations (1-indexed)."},"end_line":{"type":"integer","description":"Last line number to include for sed operations (inclusive)."},"max_matches":{"type":"integer","description":"Maximum number of lines to return for grep/awk/head/tail operations."}},"required":["file_path","operation"]}`

const postgresQueryArgsSchema = `{"type":"object","properties":{"query":{"type":"string","description":"Read-only SQL query to run against PostgreSQL."},"connection_string":{"type":"string","description":"Optional PostgreSQL connection string such as postgresql://user:pass@host:5432/dbname."},"host":{"type":"string","description":"Optional PostgreSQL host name."},"port":{"type":"string","description":"Optional PostgreSQL port."},"database":{"type":"string","description":"Optional PostgreSQL database name."},"user":{"type":"string","description":"Optional PostgreSQL user name."},"password":{"type":"string","description":"Optional PostgreSQL password."}},"required":["query"]}`

const postgresExecuteArgsSchema = `{"type":"object","properties":{"statement":{"type":"string","description":"Write-capable SQL statement for PostgreSQL such as INSERT, UPDATE, DELETE, CREATE, ALTER, or DROP."},"connection_string":{"type":"string","description":"Optional PostgreSQL connection string such as postgresql://user:pass@host:5432/dbname."},"host":{"type":"string","description":"Optional PostgreSQL host name."},"port":{"type":"string","description":"Optional PostgreSQL port."},"database":{"type":"string","description":"Optional PostgreSQL database name."},"user":{"type":"string","description":"Optional PostgreSQL user name."},"password":{"type":"string","description":"Optional PostgreSQL password."}},"required":["statement"]}`

const postgresListTablesArgsSchema = `{"type":"object","properties":{"connection_string":{"type":"string","description":"Optional PostgreSQL connection string such as postgresql://user:pass@host:5432/dbname."},"host":{"type":"string","description":"Optional PostgreSQL host name."},"port":{"type":"string","description":"Optional PostgreSQL port."},"database":{"type":"string","description":"Optional PostgreSQL database name."},"user":{"type":"string","description":"Optional PostgreSQL user name."},"password":{"type":"string","description":"Optional PostgreSQL password."}}}`

const splunkSearchArgsSchema = `{"type":"object","properties":{"query":{"type":"string","description":"Splunk search query or SPL statement."},"host":{"type":"string","description":"Optional Splunk host or base URL."},"port":{"type":"string","description":"Optional Splunk management port."},"username":{"type":"string","description":"Optional Splunk username."},"password":{"type":"string","description":"Optional Splunk password or token."},"index":{"type":"string","description":"Optional Splunk index to scope the search to."},"earliest":{"type":"string","description":"Optional earliest time expression such as -15m or 2024-01-01T00:00:00."},"latest":{"type":"string","description":"Optional latest time expression."},"max_results":{"type":"integer","description":"Optional maximum number of events to return."}},"required":["query"]}`

const splunkListSavedSearchesArgsSchema = `{"type":"object","properties":{"host":{"type":"string","description":"Optional Splunk host or base URL."},"port":{"type":"string","description":"Optional Splunk management port."},"username":{"type":"string","description":"Optional Splunk username."},"password":{"type":"string","description":"Optional Splunk password or token."}}}`

const splunkCreateSavedSearchArgsSchema = `{"type":"object","properties":{"name":{"type":"string","description":"Name for the saved search."},"search":{"type":"string","description":"Splunk search query to save."},"host":{"type":"string","description":"Optional Splunk host or base URL."},"port":{"type":"string","description":"Optional Splunk management port."},"username":{"type":"string","description":"Optional Splunk username."},"password":{"type":"string","description":"Optional Splunk password or token."}},"required":["name","search"]}`

const splunkDeleteSavedSearchArgsSchema = `{"type":"object","properties":{"name":{"type":"string","description":"Name of the saved search to delete."},"host":{"type":"string","description":"Optional Splunk host or base URL."},"port":{"type":"string","description":"Optional Splunk management port."},"username":{"type":"string","description":"Optional Splunk username."},"password":{"type":"string","description":"Optional Splunk password or token."}},"required":["name"]}`

const sendEmailArgsSchema = `{"type":"object","properties":{"to":{"type":"string","description":"Recipient email address."},"subject":{"type":"string","description":"Email subject line."},"body":{"type":"string","description":"Email body content."}},"required":["to"]}`

// toolExecuteLocalCommand equips the LLM to orchestrate shell commands on the user's local machine.
// Instead of running the command on the server, it returns an action payload that the UI interprets
// to communicate with the local SOP Desktop Companion daemon.

func (a *CopilotAgent) registerAutomationTools(ctx context.Context) {
	if !a.hasRegisteredCapability("Desktop", "Desktop Automation", "desktop") {
		return
	}

	a.registry.Register("list_folder_store_files", "Lists files inside a folder-backed source store so the agent can inspect them before curating into a Space.", listFolderStoreFilesArgsSchema, wrapStringTool(a.toolListFolderStoreFiles))
	a.registry.Register("read_folder_store_file", "Reads a file from a folder-backed source store so the agent can inspect its contents before curating into a Space.", readFolderStoreFileArgsSchema, wrapStringTool(a.toolReadFolderStoreFile))
	a.registry.Register("list_repo_store_files", "Lists files inside a repository-backed source store so the agent can inspect them before curating into a Space.", listRepoStoreFilesArgsSchema, wrapStringTool(a.toolListRepoStoreFiles))
	a.registry.Register("read_repo_store_file", "Reads a file from a repository-backed source store so the agent can inspect its contents before curating into a Space.", readRepoStoreFileArgsSchema, wrapStringTool(a.toolReadRepoStoreFile))

	policy := GetAutomationPolicy()
	if !policy.Enabled && !localDesktopAutomationEnabled.Load() {
		return
	}

	if policy.isShellAutomationEnabled() {
		a.registry.Register("execute_local_command", "Executes a shell command on the user's local machine via the SOP Desktop Daemon. Use this for building code, git operations, local file inspection, or running local scripts. Commands run in a persistent shell session that maintains working directory and environment variables across calls. Runs sequentially by default (safe). Only set background=true when user explicitly requests parallel execution. This will prompt the user for approval in the UI.", executeLocalCommandArgsSchema, wrapStringTool(a.toolExecuteLocalCommand))
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
		a.registry.Register("git_status", "Shows the working tree status for a repository, including modified, staged, and untracked files.", gitStatusArgsSchema, wrapStringTool(a.toolGitStatus))
		a.registry.Register("git_diff", "Shows the current diff for a repository, or staged changes when requested.", gitDiffArgsSchema, wrapStringTool(a.toolGitDiff))
		a.registry.Register("git_log", "Shows recent commit history for a repository.", gitLogArgsSchema, wrapStringTool(a.toolGitLog))
		a.registry.Register("git_branch", "Lists branches for a repository.", gitBranchArgsSchema, wrapStringTool(a.toolGitBranch))
		a.registry.Register("git_checkout", "Switches a repository to a different branch, tag, or commit.", gitCheckoutArgsSchema, wrapStringTool(a.toolGitCheckout))
		a.registry.Register("git_commit", "Creates a git commit for a repository. Can stage specific files or all changes in one operation. Use 'files' parameter to stage only relevant project files (excluding binaries, build artifacts, temp files).", gitCommitArgsSchema, wrapStringTool(a.toolGitCommit))
		a.registry.Register("git_pull", "Pulls changes from a remote into a repository, optionally using rebase.", gitPullArgsSchema, wrapStringTool(a.toolGitPull))
		a.registry.Register("git_push", "Pushes a branch to a remote repository.", gitPushArgsSchema, wrapStringTool(a.toolGitPush))

		// Phase 2.3: Tool Result Analysis - efficiently process large tool outputs
		a.registry.Register("list_tool_results", "Lists all stored tool result files from the current session. When a tool produces large output (>20KB), it's stored to a file. Use this to see what result files are available for analysis.", listToolResultsArgsSchema, wrapStringTool(a.toolListToolResults))
		a.registry.Register("read_tool_result", "Reads a specific line range from a stored tool result file. Use this after identifying line numbers with grep_tool_result, or to view specific sections of large test/build outputs.", readToolResultArgsSchema, wrapStringTool(a.toolReadToolResult))
		a.registry.Register("grep_tool_result", "Searches for a text pattern within a stored tool result file and returns matching lines with line numbers. Use this to find errors, failures, or specific patterns in large outputs.", grepToolResultArgsSchema, wrapStringTool(a.toolGrepToolResult))
		a.registry.Register("analyze_tool_result", "Analyzes a stored tool result file using a lightweight operation such as grep, awk, sed, head, tail, wc, or cat. This is the preferred path for targeted inspection when the LLM wants a slice of the output without forcing the full temp file into context.", analyzeToolResultArgsSchema, wrapStringTool(a.toolAnalyzeToolResult))

		if policy.isPostgresReadEnabled() {
			a.registry.Register("postgres_query", "Runs a read-only SQL query against PostgreSQL through the local psql CLI using a connection string or host/database/user/password fields.", postgresQueryArgsSchema, wrapStringTool(a.toolPostgresQuery))
			a.registry.Register("postgres_list_tables", "Lists PostgreSQL tables from the configured database for inspection and debugging.", postgresListTablesArgsSchema, wrapStringTool(a.toolPostgresListTables))
		}
		if policy.isPostgresWriteEnabled() {
			a.registry.Register("postgres_execute", "Executes a write-capable SQL statement against PostgreSQL such as INSERT, UPDATE, DELETE, CREATE, ALTER, or DROP.", postgresExecuteArgsSchema, wrapStringTool(a.toolPostgresExecute))
		}
		if policy.isSplunkReadEnabled() {
			a.registry.Register("splunk_search", "Runs a Splunk search using the local splunk CLI or a REST-compatible shell wrapper and returns the matching events.", splunkSearchArgsSchema, wrapStringTool(a.toolSplunkSearch))
			a.registry.Register("splunk_list_saved_searches", "Lists existing Splunk saved searches from the configured host.", splunkListSavedSearchesArgsSchema, wrapStringTool(a.toolSplunkListSavedSearches))
		}
		if policy.isSplunkWriteEnabled() {
			a.registry.Register("splunk_create_saved_search", "Creates a Splunk saved search from a name and search string.", splunkCreateSavedSearchArgsSchema, wrapStringTool(a.toolSplunkCreateSavedSearch))
			a.registry.Register("splunk_update_saved_search", "Updates an existing Splunk saved search.", splunkCreateSavedSearchArgsSchema, wrapStringTool(a.toolSplunkUpdateSavedSearch))
			a.registry.Register("splunk_delete_saved_search", "Deletes an existing Splunk saved search.", splunkDeleteSavedSearchArgsSchema, wrapStringTool(a.toolSplunkDeleteSavedSearch))
		}
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

// sessionIDFromContext extracts SessionID from context (same approach as desktop capability)
func sessionIDFromContext(ctx context.Context) string {
	if payload, ok := ctx.Value("session_payload").(*ai.SessionPayload); ok && payload != nil {
		if payload.SessionID != "" {
			return payload.SessionID
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

// rawLocalCommandExecutor executes commands for direct /c prefix calls
// Does NOT emit tool result events (HandlePrefix will format and emit)
func (a *CopilotAgent) rawLocalCommandExecutor(ctx context.Context, args map[string]any) (string, error) {
	commandRaw, ok := args["command"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'command'")
	}
	command, ok := commandRaw.(string)
	if !ok {
		return "", fmt.Errorf("'command' must be a string")
	}

	if localCommandExecutor == nil {
		return "", fmt.Errorf("local command execution not available")
	}

	req := LocalCommandRequest{
		Command:        command,
		Reasoning:      "Direct local command execution requested by user.",
		TimeoutSeconds: 600,
		SessionID:      sessionIDFromContext(ctx),
	}

	return localCommandExecutor(ctx, req)
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

	// Phase 1: Always use -v flag for test debugging
	command := "go test -v " + pkg
	if pattern != "" {
		command += " -run " + shellQuote(pattern)
	}

	result, err := a.runLocalAutomationCommand(ctx, args, command, "Run the relevant Go tests for the requested repository path.", path, 120)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(result) == "" {
		return fmt.Sprintf("Command: %s", command), nil
	}
	return fmt.Sprintf("Command: %s\n\n%s", command, result), nil
}

func (a *CopilotAgent) toolListFolderStoreFiles(ctx context.Context, args map[string]any) (string, error) {
	db, err := a.getDBForSpaceContext(ctx, args)
	if err != nil {
		return "", err
	}
	storeNameRaw, ok := args["store"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'store'")
	}
	storeName, ok := storeNameRaw.(string)
	if !ok || strings.TrimSpace(storeName) == "" {
		return "", fmt.Errorf("'store' must be a non-empty string")
	}
	files, err := db.ListFolderStoreFiles(ctx, storeName)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "No files found.", nil
	}
	return strings.Join(files, "\n"), nil
}

func (a *CopilotAgent) toolReadFolderStoreFile(ctx context.Context, args map[string]any) (string, error) {
	db, err := a.getDBForSpaceContext(ctx, args)
	if err != nil {
		return "", err
	}
	storeNameRaw, ok := args["store"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'store'")
	}
	storeName, ok := storeNameRaw.(string)
	if !ok || strings.TrimSpace(storeName) == "" {
		return "", fmt.Errorf("'store' must be a non-empty string")
	}
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok || strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("'path' must be a non-empty string")
	}
	return db.ReadFolderStoreFile(ctx, storeName, path)
}

func (a *CopilotAgent) toolListRepoStoreFiles(ctx context.Context, args map[string]any) (string, error) {
	db, err := a.getDBForSpaceContext(ctx, args)
	if err != nil {
		return "", err
	}
	storeNameRaw, ok := args["store"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'store'")
	}
	storeName, ok := storeNameRaw.(string)
	if !ok || strings.TrimSpace(storeName) == "" {
		return "", fmt.Errorf("'store' must be a non-empty string")
	}
	files, err := db.ListRepoStoreFiles(ctx, storeName)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "No files found.", nil
	}
	return strings.Join(files, "\n"), nil
}

func (a *CopilotAgent) toolReadRepoStoreFile(ctx context.Context, args map[string]any) (string, error) {
	db, err := a.getDBForSpaceContext(ctx, args)
	if err != nil {
		return "", err
	}
	storeNameRaw, ok := args["store"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'store'")
	}
	storeName, ok := storeNameRaw.(string)
	if !ok || strings.TrimSpace(storeName) == "" {
		return "", fmt.Errorf("'store' must be a non-empty string")
	}
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'path'")
	}
	path, ok := pathRaw.(string)
	if !ok || strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("'path' must be a non-empty string")
	}
	return db.ReadRepoStoreFile(ctx, storeName, path)
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
	path := "."
	if ok {
		if pathValue, ok := pathRaw.(string); ok {
			path = pathValue
		} else {
			return "", fmt.Errorf("'path' must be a string")
		}
	}
	if path == "" {
		path = "."
	}

	// Note: Directory listing output is NOT streamed to chat history
	// It's returned as the tool result only

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}

	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	header := fmt.Sprintf("Directory contents for %s:\n", path)
	sb.WriteString(header)

	for _, name := range names {
		line := fmt.Sprintf("- %s\n", name)
		sb.WriteString(line)
	}

	formatted := strings.TrimSpace(sb.String())
	return formatted, nil
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

func (a *CopilotAgent) toolGitStatus(ctx context.Context, args map[string]any) (string, error) {
	path := repoPathFromArgs(args)
	short := boolFlagFromArgs(args, "short")
	command := fmt.Sprintf("git --no-pager -C %s status", shellQuote(path))
	if short {
		command += " --short"
	}
	return a.runLocalAutomationCommand(ctx, args, command, "Inspect the repository status before making changes.", path, 60)
}

func (a *CopilotAgent) toolGitDiff(ctx context.Context, args map[string]any) (string, error) {
	path := repoPathFromArgs(args)
	staged := boolFlagFromArgs(args, "staged")
	command := fmt.Sprintf("git --no-pager -C %s diff", shellQuote(path))
	if staged {
		command += " --staged"
	}
	return a.runLocalAutomationCommand(ctx, args, command, "Inspect repository changes before committing or rebasing.", path, 60)
}

func (a *CopilotAgent) toolGitLog(ctx context.Context, args map[string]any) (string, error) {
	path := repoPathFromArgs(args)
	count := 10
	if countRaw, ok := args["max_count"]; ok {
		switch v := countRaw.(type) {
		case int:
			count = v
		case float64:
			count = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				count = int(parsed)
			}
		}
	}
	if count <= 0 {
		count = 10
	}
	command := fmt.Sprintf("git --no-pager -C %s log --oneline -n %d", shellQuote(path), count)
	return a.runLocalAutomationCommand(ctx, args, command, "Inspect recent repository history.", path, 60)
}

func (a *CopilotAgent) toolGitBranch(ctx context.Context, args map[string]any) (string, error) {
	path := repoPathFromArgs(args)
	command := fmt.Sprintf("git --no-pager -C %s branch --all", shellQuote(path))
	return a.runLocalAutomationCommand(ctx, args, command, "Inspect repository branches.", path, 60)
}

func (a *CopilotAgent) toolGitCheckout(ctx context.Context, args map[string]any) (string, error) {
	path := repoPathFromArgs(args)
	refRaw, ok := args["ref"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'ref'")
	}
	ref, ok := refRaw.(string)
	if !ok || strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("'ref' must be a non-empty string")
	}
	command := fmt.Sprintf("git --no-pager -C %s checkout %s", shellQuote(path), shellQuote(ref))
	return a.runLocalAutomationCommand(ctx, args, command, "Switch the repository to another branch or revision.", path, 60)
}

func (a *CopilotAgent) toolGitCommit(ctx context.Context, args map[string]any) (string, error) {
	path := repoPathFromArgs(args)
	messageRaw, ok := args["message"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'message'")
	}
	message, ok := messageRaw.(string)
	if !ok || strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("'message' must be a non-empty string")
	}

	// Check if specific files are provided
	var command string
	if filesRaw, ok := args["files"]; ok {
		if filesArray, ok := filesRaw.([]any); ok && len(filesArray) > 0 {
			// Stage specific files
			files := make([]string, 0, len(filesArray))
			for _, f := range filesArray {
				if fileStr, ok := f.(string); ok && strings.TrimSpace(fileStr) != "" {
					files = append(files, shellQuote(fileStr))
				}
			}
			if len(files) > 0 {
				filesList := strings.Join(files, " ")
				command = fmt.Sprintf("git --no-pager -C %s add %s && git --no-pager -C %s commit -m %s",
					shellQuote(path), filesList, shellQuote(path), shellQuote(message))
			} else {
				return "", fmt.Errorf("'files' array contains no valid file paths")
			}
		} else {
			return "", fmt.Errorf("'files' must be an array of strings")
		}
	} else {
		// Fall back to 'all' parameter behavior
		command = fmt.Sprintf("git --no-pager -C %s add -A && git --no-pager -C %s commit -m %s", shellQuote(path), shellQuote(path), shellQuote(message))
		if allRaw, ok := args["all"]; ok {
			if all, ok := allRaw.(bool); ok && !all {
				command = fmt.Sprintf("git --no-pager -C %s commit -m %s", shellQuote(path), shellQuote(message))
			}
		}
	}

	return a.runLocalAutomationCommand(ctx, args, command, "Create a git commit for the requested repository change.", path, 60)
}

func (a *CopilotAgent) toolGitPull(ctx context.Context, args map[string]any) (string, error) {
	path := repoPathFromArgs(args)
	command := fmt.Sprintf("git --no-pager -C %s pull", shellQuote(path))
	if rebase := boolFlagFromArgs(args, "rebase"); rebase {
		command = fmt.Sprintf("git --no-pager -C %s pull --rebase", shellQuote(path))
	}
	return a.runLocalAutomationCommand(ctx, args, command, "Pull the latest changes for the repository.", path, 60)
}

func (a *CopilotAgent) toolGitPush(ctx context.Context, args map[string]any) (string, error) {
	path := repoPathFromArgs(args)
	remote := "origin"
	if remoteRaw, ok := args["remote"]; ok {
		if value, ok := remoteRaw.(string); ok && strings.TrimSpace(value) != "" {
			remote = value
		}
	}
	branch := ""
	if branchRaw, ok := args["branch"]; ok {
		if value, ok := branchRaw.(string); ok && strings.TrimSpace(value) != "" {
			branch = value
		}
	}
	command := fmt.Sprintf("git --no-pager -C %s push %s", shellQuote(path), remote)
	if branch != "" {
		command += " " + shellQuote(branch)
	}
	return a.runLocalAutomationCommand(ctx, args, command, "Push repository changes to the requested remote branch.", path, 60)
}

// Phase 2.3: Tool Result Analysis Tools

func (a *CopilotAgent) toolListToolResults(ctx context.Context, args map[string]any) (string, error) {
	storage := getOrCreateToolResultStorage(ctx)
	if storage == nil {
		return "No tool result storage available for this session.", nil
	}

	results, err := storage.ListStoredResults()
	if err != nil {
		return "", fmt.Errorf("failed to list tool results: %w", err)
	}

	if len(results) == 0 {
		return "No stored tool results found. Large tool outputs (>20KB) are automatically stored for analysis.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d stored tool result file(s):\n\n", len(results)))

	for i, result := range results {
		sizeKB := float64(result.Size) / 1024.0
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, result.FileName))
		sb.WriteString(fmt.Sprintf("   Path: %s\n", result.FilePath))
		sb.WriteString(fmt.Sprintf("   Size: %.1fKB\n", sizeKB))
		sb.WriteString(fmt.Sprintf("   Time: %s\n\n", result.Timestamp.Format("2006-01-02 15:04:05")))
	}

	sb.WriteString("Use read_tool_result() or grep_tool_result() to analyze these files.\n")
	return sb.String(), nil
}

func (a *CopilotAgent) toolReadToolResult(ctx context.Context, args map[string]any) (string, error) {
	filePathRaw, ok := args["file_path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'file_path'")
	}
	filePath, ok := filePathRaw.(string)
	if !ok || strings.TrimSpace(filePath) == "" {
		return "", fmt.Errorf("'file_path' must be a non-empty string")
	}

	storage := getOrCreateToolResultStorage(ctx)
	if storage == nil {
		return "", fmt.Errorf("no tool result storage available")
	}

	startLine := 1
	endLine := -1 // Read to end

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

	content, err := storage.ReadResultLines(filePath, startLine, endLine)
	if err != nil {
		return "", fmt.Errorf("failed to read tool result: %w", err)
	}

	lineCount := strings.Count(content, "\n")
	return fmt.Sprintf("[Lines %d-%d from %s]\n\n%s", startLine, startLine+lineCount, filepath.Base(filePath), content), nil
}

func (a *CopilotAgent) toolGrepToolResult(ctx context.Context, args map[string]any) (string, error) {
	filePathRaw, ok := args["file_path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'file_path'")
	}
	filePath, ok := filePathRaw.(string)
	if !ok || strings.TrimSpace(filePath) == "" {
		return "", fmt.Errorf("'file_path' must be a non-empty string")
	}

	patternRaw, ok := args["pattern"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'pattern'")
	}
	pattern, ok := patternRaw.(string)
	if !ok || strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("'pattern' must be a non-empty string")
	}

	storage := getOrCreateToolResultStorage(ctx)
	if storage == nil {
		return "", fmt.Errorf("no tool result storage available")
	}

	maxMatches := 100
	if maxMatchesRaw, ok := args["max_matches"]; ok {
		switch v := maxMatchesRaw.(type) {
		case int:
			maxMatches = v
		case float64:
			maxMatches = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				maxMatches = int(parsed)
			}
		}
	}

	matches, err := storage.GrepResult(filePath, pattern, maxMatches)
	if err != nil {
		return "", fmt.Errorf("failed to grep tool result: %w", err)
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No matches found for pattern '%s' in %s", pattern, filepath.Base(filePath)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d match(es) for '%s' in %s:\n\n", len(matches), pattern, filepath.Base(filePath)))

	for _, match := range matches {
		sb.WriteString(fmt.Sprintf("Line %d: %s\n", match.LineNumber, match.Content))
	}

	if maxMatches > 0 && len(matches) >= maxMatches {
		sb.WriteString(fmt.Sprintf("\n(Showing first %d matches. Use max_matches parameter to see more.)\n", maxMatches))
	}

	return sb.String(), nil
}

func (a *CopilotAgent) toolAnalyzeToolResult(ctx context.Context, args map[string]any) (string, error) {
	filePathRaw, ok := args["file_path"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'file_path'")
	}
	filePath, ok := filePathRaw.(string)
	if !ok || strings.TrimSpace(filePath) == "" {
		return "", fmt.Errorf("'file_path' must be a non-empty string")
	}

	operationRaw, ok := args["operation"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'operation'")
	}
	operation, ok := operationRaw.(string)
	if !ok || strings.TrimSpace(operation) == "" {
		return "", fmt.Errorf("'operation' must be a non-empty string")
	}

	storage := getOrCreateToolResultStorage(ctx)
	if storage == nil {
		return "", fmt.Errorf("no tool result storage available")
	}

	pattern := ""
	if patternRaw, ok := args["pattern"]; ok {
		if value, ok := patternRaw.(string); ok {
			pattern = value
		}
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

	endLine := -1
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

	maxMatches := 50
	if maxMatchesRaw, ok := args["max_matches"]; ok {
		switch v := maxMatchesRaw.(type) {
		case int:
			maxMatches = v
		case float64:
			maxMatches = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				maxMatches = int(parsed)
			}
		}
	}

	result, err := storage.AnalyzeResult(filePath, operation, pattern, startLine, endLine, maxMatches)
	if err != nil {
		return "", fmt.Errorf("failed to analyze tool result: %w", err)
	}

	if strings.TrimSpace(result) == "" {
		return fmt.Sprintf("No content returned for operation '%s' on %s", operation, filepath.Base(filePath)), nil
	}

	prefix := strings.ToLower(strings.TrimSpace(operation))
	switch prefix {
	case "grep", "awk":
		return fmt.Sprintf("[%s result for %s]\n\n%s", prefix, filepath.Base(filePath), result), nil
	case "sed":
		return fmt.Sprintf("[sed result for %s]\n\n%s", filepath.Base(filePath), result), nil
	case "head", "tail", "cat":
		return fmt.Sprintf("[%s result for %s]\n\n%s", prefix, filepath.Base(filePath), result), nil
	case "wc":
		return fmt.Sprintf("[wc result for %s]\n\n%s", filepath.Base(filePath), result), nil
	default:
		return result, nil
	}
}

func (a *CopilotAgent) toolPostgresQuery(ctx context.Context, args map[string]any) (string, error) {
	queryRaw, ok := args["query"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'query'")
	}
	query, ok := queryRaw.(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("'query' must be a non-empty string")
	}
	if isPostgresWriteQuery(query) {
		return "", fmt.Errorf("postgres query is read-only only; write statements are not allowed")
	}

	limit := 0
	if raw, ok := args["limit"]; ok {
		switch v := raw.(type) {
		case int:
			limit = v
		case float64:
			limit = int(v)
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				limit = int(parsed)
			}
		}
	}
	query = applyPostgresQueryLimit(query, limit)

	if postgresNativeAPIEnabled(args) {
		cfg := buildPostgresConfig(args)
		result, err := executePostgresQueryNative(ctx, cfg, query, false)
		if err == nil {
			emitAutomationToolResult(ctx, "postgres_query", result)
		}
		return result, err
	}

	connectionString := ""
	if raw, ok := args["connection_string"]; ok {
		if value, ok := raw.(string); ok {
			connectionString = strings.TrimSpace(value)
		}
	}

	if connectionString == "" {
		host := firstNonEmptyString(args["host"], os.Getenv("POSTGRES_HOST"), "localhost")
		port := firstNonEmptyString(args["port"], os.Getenv("POSTGRES_PORT"), "5432")
		database := firstNonEmptyString(args["database"], os.Getenv("POSTGRES_DATABASE"), "postgres")
		user := firstNonEmptyString(args["user"], os.Getenv("POSTGRES_USER"), "postgres")
		password := firstNonEmptyString(args["password"], os.Getenv("POSTGRES_PASSWORD"), "")
		connectionString = fmt.Sprintf("postgresql://%s:%s@%s:%s/%s", user, password, host, port, database)
	}

	command := fmt.Sprintf("psql %s -c %s", shellQuote(connectionString), shellQuote(query))
	return a.runLocalAutomationCommand(ctx, args, command, "Run the requested PostgreSQL query through the local psql CLI.", "", 60)
}

func (a *CopilotAgent) toolPostgresExecute(ctx context.Context, args map[string]any) (string, error) {
	statementRaw, ok := args["statement"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'statement'")
	}
	statement, ok := statementRaw.(string)
	if !ok || strings.TrimSpace(statement) == "" {
		return "", fmt.Errorf("'statement' must be a non-empty string")
	}
	policy := GetAutomationPolicy()
	if !policy.isPostgresWriteEnabled() {
		return "", fmt.Errorf("postgres write access is not enabled")
	}
	if postgresNativeAPIEnabled(args) {
		cfg := buildPostgresConfig(args)
		result, err := executePostgresQueryNative(ctx, cfg, statement, true)
		if err == nil {
			emitAutomationToolResult(ctx, "postgres_execute", result)
		}
		return result, err
	}
	connectionString := ""
	if raw, ok := args["connection_string"]; ok {
		if value, ok := raw.(string); ok {
			connectionString = strings.TrimSpace(value)
		}
	}
	if connectionString == "" {
		host := firstNonEmptyString(args["host"], os.Getenv("POSTGRES_HOST"), "localhost")
		port := firstNonEmptyString(args["port"], os.Getenv("POSTGRES_PORT"), "5432")
		database := firstNonEmptyString(args["database"], os.Getenv("POSTGRES_DATABASE"), "postgres")
		user := firstNonEmptyString(args["user"], os.Getenv("POSTGRES_USER"), "postgres")
		password := firstNonEmptyString(args["password"], os.Getenv("POSTGRES_PASSWORD"), "")
		connectionString = fmt.Sprintf("postgresql://%s:%s@%s:%s/%s", user, password, host, port, database)
	}
	command := fmt.Sprintf("psql %s -c %s", shellQuote(connectionString), shellQuote(statement))
	return a.runLocalAutomationCommand(ctx, args, command, "Execute the requested PostgreSQL statement through the local psql CLI.", "", 60)
}

func (a *CopilotAgent) toolPostgresListTables(ctx context.Context, args map[string]any) (string, error) {
	if postgresNativeAPIEnabled(args) {
		cfg := buildPostgresConfig(args)
		result, err := executePostgresQueryNative(ctx, cfg, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name`, false)
		if err == nil {
			emitAutomationToolResult(ctx, "postgres_list_tables", result)
		}
		return result, err
	}
	connectionString := ""
	if raw, ok := args["connection_string"]; ok {
		if value, ok := raw.(string); ok {
			connectionString = strings.TrimSpace(value)
		}
	}
	if connectionString == "" {
		host := firstNonEmptyString(args["host"], os.Getenv("POSTGRES_HOST"), "localhost")
		port := firstNonEmptyString(args["port"], os.Getenv("POSTGRES_PORT"), "5432")
		database := firstNonEmptyString(args["database"], os.Getenv("POSTGRES_DATABASE"), "postgres")
		user := firstNonEmptyString(args["user"], os.Getenv("POSTGRES_USER"), "postgres")
		password := firstNonEmptyString(args["password"], os.Getenv("POSTGRES_PASSWORD"), "")
		connectionString = fmt.Sprintf("postgresql://%s:%s@%s:%s/%s", user, password, host, port, database)
	}
	command := fmt.Sprintf("psql %s -c %s", shellQuote(connectionString), shellQuote(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name`))
	return a.runLocalAutomationCommand(ctx, args, command, "List PostgreSQL tables through the local psql CLI.", "", 60)
}

func (a *CopilotAgent) toolPostgresDescribeTable(ctx context.Context, args map[string]any) (string, error) {
	tableRaw, ok := args["table"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'table'")
	}
	table, ok := tableRaw.(string)
	if !ok || strings.TrimSpace(table) == "" {
		return "", fmt.Errorf("'table' must be a non-empty string")
	}
	query := fmt.Sprintf(`SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_name = '%s' ORDER BY ordinal_position`, table)
	if postgresNativeAPIEnabled(args) {
		cfg := buildPostgresConfig(args)
		return executePostgresQueryNative(ctx, cfg, query, false)
	}
	connectionString := ""
	if raw, ok := args["connection_string"]; ok {
		if value, ok := raw.(string); ok {
			connectionString = strings.TrimSpace(value)
		}
	}
	if connectionString == "" {
		host := firstNonEmptyString(args["host"], os.Getenv("POSTGRES_HOST"), "localhost")
		port := firstNonEmptyString(args["port"], os.Getenv("POSTGRES_PORT"), "5432")
		database := firstNonEmptyString(args["database"], os.Getenv("POSTGRES_DATABASE"), "postgres")
		user := firstNonEmptyString(args["user"], os.Getenv("POSTGRES_USER"), "postgres")
		password := firstNonEmptyString(args["password"], os.Getenv("POSTGRES_PASSWORD"), "")
		connectionString = fmt.Sprintf("postgresql://%s:%s@%s:%s/%s", user, password, host, port, database)
	}
	command := fmt.Sprintf("psql %s -c %s", shellQuote(connectionString), shellQuote(query))
	return a.runLocalAutomationCommand(ctx, args, command, "Inspect a PostgreSQL table schema through the local psql CLI.", "", 60)
}

func (a *CopilotAgent) toolSplunkSearch(ctx context.Context, args map[string]any) (string, error) {
	queryRaw, ok := args["query"]
	if !ok {
		return "", fmt.Errorf("missing required argument 'query'")
	}
	query, ok := queryRaw.(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("'query' must be a non-empty string")
	}

	if splunkNativeAPIEnabled(args) {
		cfg := buildSplunkConfig(args)
		if savedSearchName, ok := args["saved_search_name"].(string); ok && strings.TrimSpace(savedSearchName) != "" {
			query = fmt.Sprintf("savedsearch %q", strings.TrimSpace(savedSearchName))
		}
		result, err := executeSplunkSearchNative(ctx, cfg, query)
		if err == nil {
			emitAutomationToolResult(ctx, "splunk_search", result)
		}
		return result, err
	}

	host := firstNonEmptyString(args["host"], os.Getenv("SPLUNK_HOST"), "localhost")
	port := firstNonEmptyString(args["port"], os.Getenv("SPLUNK_PORT"), "8089")
	username := firstNonEmptyString(args["username"], os.Getenv("SPLUNK_USERNAME"), "")
	password := firstNonEmptyString(args["password"], os.Getenv("SPLUNK_PASSWORD"), "")
	index := firstNonEmptyString(args["index"], os.Getenv("SPLUNK_INDEX"), "")
	earliest := stringArg(args, "earliest")
	latest := stringArg(args, "latest")
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

	flags := []string{"--output", "json"}
	if username != "" {
		flags = append(flags, "-auth", fmt.Sprintf("%s:%s", username, password))
	}
	if index != "" {
		flags = append(flags, "index", index)
	}
	if earliest != "" {
		flags = append(flags, "earliest", earliest)
	}
	if latest != "" {
		flags = append(flags, "latest", latest)
	}
	flags = append(flags, "count", strconv.Itoa(maxResults))

	command := fmt.Sprintf("splunk search %s --uri %s:%s %s", strings.Join(flags, " "), shellQuote(host), shellQuote(port), shellQuote(query))
	return a.runLocalAutomationCommand(ctx, args, command, "Run the requested Splunk search through the local Splunk CLI.", "", 60)
}

func (a *CopilotAgent) toolSplunkListSavedSearches(ctx context.Context, args map[string]any) (string, error) {
	if splunkNativeAPIEnabled(args) {
		cfg := buildSplunkConfig(args)
		result, err := executeSplunkSearchNative(ctx, cfg, "| rest /services/saved/searches | table title")
		if err == nil {
			emitAutomationToolResult(ctx, "splunk_list_saved_searches", result)
		}
		return result, err
	}
	return a.runLocalAutomationCommand(ctx, args, "splunk search '| rest /services/saved/searches | table title'", "List existing Splunk saved searches.", "", 60)
}

func (a *CopilotAgent) toolSplunkCreateSavedSearch(ctx context.Context, args map[string]any) (string, error) {
	if splunkNativeAPIEnabled(args) {
		cfg := buildSplunkConfig(args)
		result, err := executeSplunkSearchNative(ctx, cfg, fmt.Sprintf("| rest /services/saved/searches | search title=%q", args["name"]))
		if err == nil {
			emitAutomationToolResult(ctx, "splunk_create_saved_search", result)
		}
		return result, err
	}
	name, _ := args["name"].(string)
	search, _ := args["search"].(string)
	command := fmt.Sprintf("splunk create saved-search %s -search %s", shellQuote(name), shellQuote(search))
	command = strings.Replace(command, "splunk create saved-search", "splunk create saved/searches", 1)
	return a.runLocalAutomationCommand(ctx, args, command, "Create a saved Splunk search.", "", 60)
}

func (a *CopilotAgent) toolSplunkUpdateSavedSearch(ctx context.Context, args map[string]any) (string, error) {
	if splunkNativeAPIEnabled(args) {
		cfg := buildSplunkConfig(args)
		result, err := executeSplunkSearchNative(ctx, cfg, fmt.Sprintf("| rest /services/saved/searches | search title=%q", args["name"]))
		if err == nil {
			emitAutomationToolResult(ctx, "splunk_update_saved_search", result)
		}
		return result, err
	}
	name, _ := args["name"].(string)
	search, _ := args["search"].(string)
	command := fmt.Sprintf("splunk edit saved-search %s -search %s", shellQuote(name), shellQuote(search))
	return a.runLocalAutomationCommand(ctx, args, command, "Update a saved Splunk search.", "", 60)
}

func (a *CopilotAgent) toolSplunkDeleteSavedSearch(ctx context.Context, args map[string]any) (string, error) {
	if splunkNativeAPIEnabled(args) {
		cfg := buildSplunkConfig(args)
		result, err := executeSplunkSearchNative(ctx, cfg, fmt.Sprintf("| rest /services/saved/searches | search title=%q", args["name"]))
		if err == nil {
			emitAutomationToolResult(ctx, "splunk_delete_saved_search", result)
		}
		return result, err
	}
	name, _ := args["name"].(string)
	command := fmt.Sprintf("splunk delete saved-search %s", shellQuote(name))
	return a.runLocalAutomationCommand(ctx, args, command, "Delete a saved Splunk search.", "", 60)
}

func emitAutomationToolChunk(ctx context.Context, toolName, chunk string) {
	if strings.TrimSpace(chunk) == "" {
		return
	}
	// if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
	// 	streamer("tool_stream_chunk", map[string]any{"tool": toolName, "text": chunk})
	// }
}

func shouldEmitAutomationProgressCommentary(command, workingDir string) bool {
	trimmedCommand := strings.TrimSpace(command)
	trimmedDir := strings.TrimSpace(workingDir)
	if trimmedCommand == "" {
		return false
	}
	if strings.Contains(trimmedCommand, "go test") || strings.Contains(trimmedCommand, "go test ./...") || strings.Contains(trimmedCommand, "go test") {
		return true
	}
	if trimmedDir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(trimmedDir, "go.mod")); err == nil {
		return strings.Contains(trimmedCommand, "go") || strings.Contains(trimmedCommand, "test")
	}
	return false
}

func wrapKnownCommandForTextAnalysis(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return trimmed, false
	}

	lower := strings.ToLower(trimmed)
	if isAlreadyWrappedCommand(lower) {
		return trimmed, false
	}
	if containsAnalysisPipeline(lower) {
		return trimmed, true
	}
	if strings.HasPrefix(lower, "go test") {
		pattern := `(^ok|^FAIL$|^FAIL\t|^--- FAIL:|^panic:|^Error:|^error:|^=== RUN)`
		return fmt.Sprintf("%s 2>&1 | grep -E '%s' || true", trimmed, pattern), true
	}
	if strings.HasPrefix(lower, "npm test") || strings.HasPrefix(lower, "yarn test") {
		pattern := `(failing|failed|passing|error|Error)`
		return fmt.Sprintf("%s 2>&1 | grep -E '%s' || true", trimmed, pattern), true
	}
	if strings.HasPrefix(lower, "pytest") {
		pattern := `(^FAILED|^ERROR|^PASSED|^passed|^failed|^error)`
		return fmt.Sprintf("%s 2>&1 | grep -E '%s' || true", trimmed, pattern), true
	}
	return trimmed, false
}

func isAlreadyWrappedCommand(command string) bool {
	for _, token := range []string{"2>&1 | grep -e", "2>&1 | grep -E", "| grep -e", "| grep -E", "| awk ", "| sed ", "| head ", "| tail ", "| wc "} {
		if strings.Contains(command, token) {
			return true
		}
	}
	return false
}

func containsAnalysisPipeline(command string) bool {
	for _, token := range []string{" | grep ", " | awk ", " | sed ", " | head ", " | tail ", " | wc ", "grep ", "awk ", "sed ", "head ", "tail ", "wc "} {
		if strings.Contains(command, token) {
			return true
		}
	}
	return false
}

func emitAutomationProgressCommentary(ctx context.Context, command, workingDir string) {
	if strings.TrimSpace(command) == "" {
		return
	}
	if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
		message := "Starting the requested automation now."
		if strings.Contains(strings.TrimSpace(command), "go test") {
			if _, err := os.Stat(filepath.Join(strings.TrimSpace(workingDir), "go.mod")); err == nil {
				message = "I’m running the test suite now; this workspace looks predominantly Go-based."
			} else {
				message = "I’m running the test suite now; this looks like a Go-based check."
			}
		}
		streamer(ai.ReasoningEventAssistantMessage, ai.BuildAssistantMessageEvent("commentary", message))
	}
}

func emitTerminalCommand(ctx context.Context, command string) {
	if strings.TrimSpace(command) == "" {
		return
	}
	if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
		streamer("terminal_command", map[string]any{
			"text":               strings.TrimSpace(command),
			"display_as_command": true,
		})
	}
}

func emitAutomationToolStreamContent(ctx context.Context, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
		streamer("content", content)
	}
}

func emitAutomationToolResult(ctx context.Context, toolName, result string) {
	if strings.TrimSpace(result) == "" {
		return
	}

	// Result is already unwrapped and processed by storage layer.
	// Emit it to the reasoning event stream as-is.
	if streamer, ok := ctx.Value(ai.CtxKeyEventStreamer).(func(string, any)); ok && streamer != nil {
		streamer(ai.ReasoningEventToolResult, map[string]any{
			"tool":   toolName,
			"result": strings.TrimSpace(result),
		})
	}
}

// parseAndEmitTestStatus analyzes test command output and streams immediate status
// Returns a status summary string to prepend to LLM output (empty if not a test or no status found)
// This ensures critical test outcomes are delivered to both UI and LLM
func parseAndEmitTestStatus(ctx context.Context, command, output string) string {
	// Only parse test commands
	isTestCommand := strings.Contains(command, "go test") ||
		strings.Contains(command, "npm test") ||
		strings.Contains(command, "yarn test") ||
		strings.Contains(command, "pytest") ||
		strings.Contains(command, "run_all_tests")

	if !isTestCommand {
		return ""
	}

	lines := strings.Split(output, "\n")
	status := "UNKNOWN"
	summary := ""
	keyLines := []string{}
	isGoTestCommand := strings.Contains(command, "go test") || strings.Contains(command, "run_all_tests")
	isNpmLikeCommand := strings.Contains(command, "npm test") || strings.Contains(command, "yarn test")
	isPytestCommand := strings.Contains(command, "pytest")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if isGoTestCommand {
			if trimmed == "FAIL" || strings.HasPrefix(trimmed, "FAIL\t") || strings.HasPrefix(trimmed, "--- FAIL:") || strings.HasPrefix(trimmed, "panic:") {
				status = "FAIL"
				summary = "❌ **Test Suite Failed**"
				if len(keyLines) < 8 {
					keyLines = append(keyLines, trimmed)
				}
				continue
			}

			if strings.HasPrefix(trimmed, "ok") {
				if len(trimmed) > 2 && (trimmed[2] == ' ' || trimmed[2] == '\t') {
					if status != "FAIL" {
						status = "PASS"
						summary = "✅ **Test Suite Passed**"
					}
					if len(keyLines) < 8 {
						keyLines = append(keyLines, trimmed)
					}
				}
			}
		}

		if isNpmLikeCommand {
			if strings.Contains(trimmed, "failing") || strings.Contains(trimmed, "failed") {
				status = "FAIL"
				summary = "❌ **Test Suite Failed**"
				if len(keyLines) < 8 {
					keyLines = append(keyLines, trimmed)
				}
			} else if strings.Contains(trimmed, "passing") {
				if status != "FAIL" {
					status = "PASS"
					summary = "✅ **Test Suite Passed**"
				}
				if len(keyLines) < 8 {
					keyLines = append(keyLines, trimmed)
				}
			}
		}

		if isPytestCommand {
			if strings.Contains(trimmed, "failed") || strings.Contains(trimmed, "error") {
				status = "FAIL"
				summary = "❌ **Test Suite Failed**"
				if len(keyLines) < 8 {
					keyLines = append(keyLines, trimmed)
				}
			} else if strings.Contains(trimmed, "passed") {
				if !strings.Contains(trimmed, "failed") && status != "FAIL" {
					status = "PASS"
					summary = "✅ **Test Suite Passed**"
				}
				if len(keyLines) < 8 {
					keyLines = append(keyLines, trimmed)
				}
			}
		}
	}

	// Emit immediate status if we found test results
	if status != "UNKNOWN" && summary != "" {
		// Don't stream to UI - let tool result handler decide
		// The full output is already in Terminal

		// Return status summary for LLM
		// This prevents LLM from looping - it sees clear PASS/FAIL status
		llmSummary := fmt.Sprintf("TEST STATUS: %s\n", status)
		if status == "FAIL" {
			llmSummary += "The test suite failed. Do not retry - report the failure to the user.\n"
			if len(keyLines) > 0 {
				llmSummary += "\nRelevant output:\n"
				for _, line := range keyLines {
					llmSummary += line + "\n"
				}
			}
		} else if status == "PASS" {
			llmSummary += "All tests passed successfully.\n"
			if len(keyLines) > 0 {
				llmSummary += "\nObserved result:\n"
				for _, line := range keyLines {
					llmSummary += line + "\n"
				}
			}
		}
		return llmSummary + "\n"
	}

	return ""
}

func boolFlagFromArgs(args map[string]any, key string) bool {
	if raw, ok := args[key]; ok {
		if value, ok := raw.(bool); ok {
			return value
		}
	}
	return false
}

func repoPathFromArgs(args map[string]any) string {
	pathRaw, ok := args["path"]
	if !ok {
		return "."
	}
	path, ok := pathRaw.(string)
	if !ok || strings.TrimSpace(path) == "" {
		return "."
	}
	return path
}

func firstNonEmptyString(values ...interface{}) string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case nil:
			continue
		default:
			continue
		}
	}
	return ""
}

func stringArg(args map[string]any, key string) string {
	if raw, ok := args[key]; ok {
		if value, ok := raw.(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func discoverWorkingDirCandidates(startDir string) []string {
	if strings.TrimSpace(startDir) == "" {
		startDir = "."
	}

	seen := map[string]bool{}
	candidates := []string{}
	addCandidate := func(dir string) {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			return
		}
		cleaned := filepath.Clean(trimmed)
		if cleaned == "." {
			if cwd, err := os.Getwd(); err == nil {
				cleaned = filepath.Clean(cwd)
			}
		}
		if cleaned == "" || cleaned == string(filepath.Separator) {
			return
		}
		if seen[cleaned] {
			return
		}
		if info, err := os.Stat(filepath.Join(cleaned, ".git")); err == nil && info.IsDir() {
			seen[cleaned] = true
			candidates = append(candidates, cleaned)
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		addCandidate(cwd)
	}
	addCandidate(startDir)

	for _, seed := range []string{startDir, "."} {
		current := seed
		if strings.TrimSpace(current) == "" {
			current = "."
		}
		if current == "." {
			if cwd, err := os.Getwd(); err == nil {
				current = cwd
			}
		}
		for {
			addCandidate(current)
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			if entries, err := os.ReadDir(parent); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() || entry.Name() == ".git" {
						continue
					}
					addCandidate(filepath.Join(parent, entry.Name()))
				}
			}
			current = parent
		}
	}

	return candidates
}

// unwrapCommandExecutorResult extracts stdout from JSON command executor responses
// localCommandExecutor returns {"stdout":"...", "stderr":"...", "exit_code":0}
// This function extracts the actual stdout content for display
func unwrapCommandExecutorResult(result string) string {
	// Quick check - does it look like JSON?
	trimmed := strings.TrimSpace(result)
	if !strings.HasPrefix(trimmed, "{") {
		return result
	}

	var execResp struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}

	if err := json.Unmarshal([]byte(result), &execResp); err != nil {
		// Not a command result, return as-is
		return result
	}

	// Build readable output from stdout/stderr
	var output strings.Builder
	if execResp.Stdout != "" {
		output.WriteString(execResp.Stdout)
	}
	if execResp.Stderr != "" {
		if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n") {
			output.WriteString("\n")
		}
		output.WriteString(execResp.Stderr)
	}

	// If we got something, return it; otherwise return an empty string so the
	// caller can fall back to callback-captured streamed output if present.
	if output.Len() > 0 {
		return output.String()
	}
	return ""
}

func (a *CopilotAgent) runLocalAutomationCommand(ctx context.Context, args map[string]any, command, defaultReasoning, defaultWorkingDir string, defaultTimeoutSeconds int) (string, error) {
	reasoningRaw, _ := args["reasoning"]
	reasoning, _ := reasoningRaw.(string)
	if reasoning == "" {
		reasoning = defaultReasoning
	}

	workingDirRaw, _ := args["working_dir"]
	workingDir, _ := workingDirRaw.(string)
	workingDirPromptRequired := false
	candidateDirs := []string{}
	if strings.TrimSpace(workingDir) == "" {
		// Only resolve working dir in parallel execution mode (to avoid os.Getwd() fallback in tests)
		if isParallelExecution(ctx) {
			if ctxWorkingDir := resolveParallelWorkingDir(ctx, ""); strings.TrimSpace(ctxWorkingDir) != "" {
				workingDir = ctxWorkingDir
			}
		}
		if strings.TrimSpace(workingDir) == "" && strings.TrimSpace(defaultWorkingDir) != "" {
			workingDir = defaultWorkingDir
		}
		if strings.TrimSpace(workingDir) != "" {
			candidateDirs = discoverWorkingDirCandidates(workingDir)
			if len(candidateDirs) == 0 {
				workingDirPromptRequired = true
			} else if len(candidateDirs) > 1 {
				workingDirPromptRequired = true
			} else {
				workingDir = candidateDirs[0]
			}
		}
	} else {
		candidateDirs = discoverWorkingDirCandidates(workingDir)
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

	// Extract background flag
	background := false
	if backgroundRaw, ok := args["background"]; ok {
		if bgBool, ok := backgroundRaw.(bool); ok {
			background = bgBool
		}
	}

	// Phase 4: Force background execution for parallel threads
	sessionID := sessionIDFromContext(ctx)
	if isParallelExecution(ctx) {
		background = true
		sessionID = "" // Strip session to route to exec.Command instead of PTY
	}

	executedCommand := command
	if wrappedCommand, shouldWrap := wrapKnownCommandForTextAnalysis(command); shouldWrap {
		executedCommand = wrappedCommand
	}

	request := LocalCommandRequest{
		Command:        executedCommand,
		Reasoning:      reasoning,
		WorkingDir:     workingDir,
		TimeoutSeconds: timeoutSeconds,
		SessionID:      sessionID,
		Background:     background,
		// StreamStdout and StreamStderr are intentionally nil
		// Output is collected by the daemon but NOT streamed during execution
		// The Terminal pane displays the final result
		StreamStdout: nil,
		StreamStderr: nil,
	}

	policy := GetAutomationPolicy()
	if err := policy.validateRequest(request); err != nil {
		return "", err
	}

	// Emit the concrete shell command as a structured terminal event before execution.
	// The UI will unwrap this event and style it as the active command line.
	emitTerminalCommand(ctx, fmt.Sprintf("$ %s", command))

	if shouldEmitAutomationProgressCommentary(command, workingDir) {
		emitAutomationProgressCommentary(ctx, command, workingDir)
	}

	if localCommandExecutor != nil {
		result, err := localCommandExecutor(ctx, request)
		if err == nil {
			// Unwrap JSON response to get actual stdout content
			// localCommandExecutor returns {"stdout":"...", "stderr":"...", "exit_code":0}
			// but we want just the stdout for display/storage
			displayResult := unwrapCommandExecutorResult(result)

			statusSummary := parseAndEmitTestStatus(ctx, command, displayResult)

			// Prepend status summary if present
			// wrapToolResultWithStorage will handle file storage automatically
			if statusSummary != "" {
				displayResult = statusSummary + "\n\n" + displayResult
			}

			// Note: Command output is NOT streamed to chat history
			// It's only displayed in the Terminal window pane
			// The Terminal receives output through the PTY streaming mechanism

			return displayResult, nil
		}
		return result, err
	}

	requiresApproval := policy.RequireApproval || isSensitiveCommand(command)
	requiresPasswordPrompt := requiresPasswordPrompt(command)
	approvalReason := determineApprovalReason(command, reasoning, requiresApproval, requiresPasswordPrompt)

	payload := map[string]any{
		"action_type":                 "local_shell_execution",
		"command":                     command,
		"reasoning":                   reasoning,
		"working_dir":                 workingDir,
		"working_dir_candidates":      candidateDirs,
		"working_dir_prompt_required": workingDirPromptRequired,
		"timeout_seconds":             timeoutSeconds,
		"requires_approval":           requiresApproval,
		"requires_password_prompt":    requiresPasswordPrompt,
		"approval_reason":             approvalReason,
	}

	payloadBytes, _ := json.Marshal(payload)

	return fmt.Sprintf("<<<ACTION_REQUIRED:%s>>>", string(payloadBytes)), nil
}
