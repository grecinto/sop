package codingworkflow

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileSearchAdapter offers a lightweight repository search facade.
// Guard rail: keep search operations read-only and explicit. Future copilots should be
// able to gather repository context without silently mutating anything.
type FileSearchAdapter struct{}

func (f *FileSearchAdapter) Search(task Task, state RunState, query string) ([]string, error) {
	_ = state
	if task.WorkingDir == "" {
		return nil, fmt.Errorf("working dir is required")
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if rgPath, err := exec.LookPath("rg"); err == nil {
		cmd := exec.Command(rgPath, "--files", "--glob", "*.go", "--glob", "*.md", "--glob", "*.txt", "-e", query, task.WorkingDir)
		output, err := cmd.CombinedOutput()
		if err == nil {
			matches := []string{}
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				matches = append(matches, strings.TrimPrefix(line, task.WorkingDir+string(os.PathSeparator)))
			}
			return matches, nil
		}
	}

	matches := []string{}
	walkErr := filepath.Walk(task.WorkingDir, func(path string, info os.FileInfo, err error) error {
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
