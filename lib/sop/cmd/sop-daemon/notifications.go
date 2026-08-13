package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"

	sharedagent "github.com/sharedcode/sop/ai/agent"
)

func installDefaultLTMPressureNotifier() {
	sharedagent.SetDefaultLTMPressureNotifier(func(notification sharedagent.LTMPressureNotification) {
		sharedagent.EmitLTMPressureNotification(notification)
		title := strings.TrimSpace(notification.Title)
		if title == "" {
			title = "Memory cleanup"
		}
		message := strings.TrimSpace(notification.Message)
		if message == "" {
			message = "Local long-term memory cleanup triggered."
		}
		if err := emitDesktopNotification(title, message); err != nil {
			log.Printf("LTM desktop notification unavailable: %v", err)
		}
	})
}

func emitDesktopNotification(title, message string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("osascript", "-e", fmt.Sprintf(`display notification %q with title %q`, message, title))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("osascript notification failed: %w", err)
		}
		return nil
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			cmd := exec.Command("notify-send", "--urgency=normal", title, message)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("notify-send failed: %w", err)
			}
			return nil
		}
		return fmt.Errorf("notify-send not available")
	case "windows":
		if _, err := exec.LookPath("powershell.exe"); err == nil {
			cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", fmt.Sprintf("[System.Windows.Forms.MessageBox]::Show(%q, %q, 'OK', 'Information')", message, title))
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("powershell notification failed: %w", err)
			}
			return nil
		}
		return fmt.Errorf("powershell not available")
	default:
		return fmt.Errorf("desktop notifications unsupported on %s", runtime.GOOS)
	}
}
