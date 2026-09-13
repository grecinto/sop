package codingworkflow

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop/ai"
)

// FileSearchAdapter offers a lightweight repository search facade.
// Guard rail: keep search operations read-only and explicit. Future copilots should be
// able to gather repository context without silently mutating anything.
type FileSearchAdapter struct{}

func (f *FileSearchAdapter) Search(task Task, state RunState, query string) ([]string, error) {
	return f.SearchWithContext(context.Background(), task, state, query)
}

func (f *FileSearchAdapter) SearchWithContext(ctx context.Context, task Task, state RunState, query string) ([]string, error) {
	_ = state

	// Use task's working directory, ensuring it's absolute
	workingDir := task.WorkingDir
	log.Printf("[FileSearchAdapter] Initial workingDir from task=%q", workingDir)

	if workingDir == "" {
		if payload := ai.GetSessionPayload(ctx); payload != nil && payload.Variables != nil {
			if wd, ok := payload.Variables["working_dir"].(string); ok && strings.TrimSpace(wd) != "" {
				workingDir = strings.TrimSpace(wd)
				log.Printf("[FileSearchAdapter] Resolved empty working_dir from SessionPayload: %s", workingDir)
			}
		}
		if workingDir == "" {
			if cwd, err := os.Getwd(); err == nil {
				workingDir = cwd
				log.Printf("[FileSearchAdapter] Resolved empty working_dir from os.Getwd(): %s", workingDir)
			}
		}
		if workingDir == "" {
			return nil, fmt.Errorf("working dir is required (could not resolve from context or cwd)")
		}
	}

	// Ensure absolute path
	if !filepath.IsAbs(workingDir) {
		if absPath, err := filepath.Abs(workingDir); err == nil {
			workingDir = absPath
			log.Printf("[FileSearchAdapter] Converted relative to absolute: %s", workingDir)
		}
	}

	workingDir = strings.TrimSpace(workingDir)
	log.Printf("[FileSearchAdapter] Final workingDir=%q, query=%q", workingDir, query)
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	// Try multiple ripgrep locations: system PATH, VS Code's embedded version
	rgPaths := []string{}
	if rgPath, err := exec.LookPath("rg"); err == nil {
		rgPaths = append(rgPaths, rgPath)
	}
	// VS Code's embedded ripgrep for macOS ARM64
	vscodeRg := "/Applications/Visual Studio Code.app/Contents/Resources/app/node_modules.asar.unpacked/@vscode/ripgrep-universal/bin/darwin-arm64/rg"
	if _, err := os.Stat(vscodeRg); err == nil {
		rgPaths = append(rgPaths, vscodeRg)
	}

	if len(rgPaths) > 0 {
		rgPath := rgPaths[0]
		log.Printf("[FileSearchAdapter] Using ripgrep at: %s", rgPath)
		// Use ripgrep to search file content with line numbers and context
		// --line-number: show line numbers
		// --heading: group matches by file
		// --context 2: show 2 lines before/after match
		// --max-count 5: limit to 5 matches per file to avoid huge results
		// --color never: disable color output for clean parsing
		cmd := exec.Command(rgPath, "--hidden", "--no-ignore", "--line-number", "--heading", "--context", "2", "--max-count", "5",
			"--color", "never", "--glob", "*.go", "--glob", "*.md", "--glob", "*.txt", "--glob", "*.ts", "--glob", "*.tsx", "--glob", "*.js", "--glob", "*.jsx", "--glob", "*.json", "--glob", "*.yaml", "--glob", "*.yml", query, workingDir)
		output, cmdErr := cmd.CombinedOutput()

		// ripgrep exits with code 1 when no matches found, which is not an error for us
		if cmdErr != nil {
			if exitErr, ok := cmdErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				// No matches found - this is normal, return empty with helpful message
				log.Printf("[FileSearchAdapter] No matches found for query=%q in %s", query, workingDir)
				return []string{fmt.Sprintf("No matches found for pattern: %q\n\nTip: Use simple regex patterns (e.g., \"test.*setup\" or \"BillingWebhook\"), not boolean queries with OR.", query)}, nil
			}
			// Actual error (not just "no matches")
			log.Printf("[FileSearchAdapter] ripgrep error: %v, output: %s", cmdErr, string(output))
		}

		if len(output) > 0 {
			// Return raw ripgrep output which includes file:line:content format
			result := strings.TrimSpace(string(output))
			if result != "" {
				return []string{result}, nil
			}
		}
	} else {
		log.Printf("[FileSearchAdapter] ripgrep not found in PATH or VS Code location, falling back to file walking (note: regex patterns like '|' won't work)")
	}

	matches := []string{}
	walkErr := filepath.Walk(workingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".txt") {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				if strings.Contains(scanner.Text(), query) {
					matches = append(matches, strings.TrimPrefix(path, task.WorkingDir+string(os.PathSeparator)))
					break
				}
			}
			if err := scanner.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return matches, nil
}
