package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if err := runSite(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "site launch failed: %v\n", err)
		os.Exit(1)
	}
}

func runSite(args []string) error {
	cmd := exec.Command("go", buildSiteArgs(args)...)
	cmd.Dir = repoRoot()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildSiteArgs(args []string) []string {
	fullArgs := []string{"run", "./src/backend/site"}
	fullArgs = append(fullArgs, args...)
	return fullArgs
}

func repoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "src", "backend", "site")
		if _, err := os.Stat(candidate); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
	}
}
