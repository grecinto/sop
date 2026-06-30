package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if err := runHTTPServer(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "httpserver launch failed: %v\n", err)
		os.Exit(1)
	}
}

func runHTTPServer(args []string) error {
	cmd := exec.Command("go", buildHTTPServerArgs(args)...)
	cmd.Dir = repoRoot()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildHTTPServerArgs(args []string) []string {
	fullArgs := []string{"run", "./lib/sop/tools/httpserver"}
	fullArgs = append(fullArgs, args...)
	return fullArgs
}

func repoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "lib", "sop", "tools", "httpserver")
		if _, err := os.Stat(candidate); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
	}
}
