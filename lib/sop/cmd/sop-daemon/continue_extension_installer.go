package main

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// continueExtensionID is the AI Studio fork of Continue's VS Code extension
// (northstar.ai-studio), not the public Continue marketplace listing.
const continueExtensionID = "northstar.ai-studio"

// continueVsixEnvVar overrides the bundled vsix path for release packaging/testing.
const continueVsixEnvVar = "SOP_CONTINUE_VSIX_PATH"

// ensureContinueExtensionInstalledAsync runs the install check in the background so
// it never delays or blocks daemon startup. Failures are logged only; the daemon
// remains fully functional without VS Code or the extension present.
func ensureContinueExtensionInstalledAsync() {
	go func() {
		if err := ensureContinueExtensionInstalled(); err != nil {
			logVerbose("Continue extension install skipped: %s", err.Error())
		}
	}()
}

func ensureContinueExtensionInstalled() error {
	vsixPath, err := resolveContinueVsixPath()
	if err != nil {
		return err
	}

	codeCliPath, err := findCodeCLI()
	if err != nil {
		return err
	}

	installed, err := isContinueExtensionInstalled(codeCliPath)
	if err != nil {
		return err
	}
	if installed {
		logVerbose("AI Studio Continue extension already installed")
		return nil
	}

	cmd := exec.Command(codeCliPath, "--install-extension", vsixPath, "--force")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errWithOutput("install AI Studio Continue extension", err, output)
	}
	log.Printf("Installed AI Studio Continue extension for VS Code from %s", vsixPath)
	return nil
}

// resolveContinueVsixPath looks for the bundled vsix next to the running daemon
// binary (release packaging places it there), or at SOP_CONTINUE_VSIX_PATH.
func resolveContinueVsixPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(continueVsixEnvVar)); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", err
		}
		return override, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(filepath.Dir(exePath), "continue-ai-studio.vsix")
	if _, err := os.Stat(candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func findCodeCLI() (string, error) {
	if path, err := exec.LookPath("code"); err == nil {
		return path, nil
	}
	for _, candidate := range candidateCodeCLIPaths() {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}

func candidateCodeCLIPaths() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/usr/local/bin/code",
			"/opt/homebrew/bin/code",
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
			filepath.Join(home, "Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"),
		}
	case "windows":
		return []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Microsoft VS Code", "bin", "code.cmd"),
			filepath.Join(os.Getenv("PROGRAMFILES"), "Microsoft VS Code", "bin", "code.cmd"),
		}
	default:
		return []string{
			"/usr/bin/code",
			"/usr/share/code/bin/code",
			filepath.Join(home, ".local/share/code/bin/code"),
		}
	}
}

func isContinueExtensionInstalled(codeCliPath string) (bool, error) {
	cmd := exec.Command(codeCliPath, "--list-extensions")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, errWithOutput("list VS Code extensions", err, output)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		if strings.EqualFold(strings.TrimSpace(scanner.Text()), continueExtensionID) {
			return true, nil
		}
	}
	return false, nil
}

func errWithOutput(action string, err error, output []byte) error {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return err
	}
	return &continueExtensionInstallError{action: action, cause: err, output: trimmed}
}

type continueExtensionInstallError struct {
	action string
	cause  error
	output string
}

func (e *continueExtensionInstallError) Error() string {
	return e.action + ": " + e.cause.Error() + ": " + e.output
}

func (e *continueExtensionInstallError) Unwrap() error {
	return e.cause
}
