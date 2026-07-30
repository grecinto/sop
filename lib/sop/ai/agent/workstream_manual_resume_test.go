package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/sharedcode/sop"
)

func TestPauseWorkStreamWithReason_MarksAsRequiringManualResume(t *testing.T) {
	thread := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "test query",
		StreamState: WorkStreamActive,
	}
	thread.InitializeWorkStream("planning")

	// Pause with manual resume required
	thread.PauseWorkStreamWithReason("User requested to pause and work on something else", true)

	if thread.StreamState != WorkStreamPaused {
		t.Errorf("expected stream state to be paused, got %s", thread.StreamState)
	}

	if !thread.StreamContext.RequiresManualResume {
		t.Error("expected RequiresManualResume to be true")
	}

	if thread.StreamContext.PauseReason != "User requested to pause and work on something else" {
		t.Errorf("expected pause reason to be set, got: %s", thread.StreamContext.PauseReason)
	}

	if thread.StreamContext.PausedAt == 0 {
		t.Error("expected PausedAt timestamp to be set")
	}
}

func TestPauseWorkStreamWithReason_CanPauseWithoutManualResume(t *testing.T) {
	thread := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "test query",
		StreamState: WorkStreamActive,
	}
	thread.InitializeWorkStream("planning")

	// Pause without manual resume required (auto-resume allowed)
	thread.PauseWorkStreamWithReason("Quick interruption", false)

	if thread.StreamState != WorkStreamPaused {
		t.Errorf("expected stream state to be paused, got %s", thread.StreamState)
	}

	if thread.StreamContext.RequiresManualResume {
		t.Error("expected RequiresManualResume to be false")
	}

	if thread.StreamContext.PauseReason != "Quick interruption" {
		t.Errorf("expected pause reason to be set, got: %s", thread.StreamContext.PauseReason)
	}
}

func TestCanAutoResume_AllowsAutoResumeWhenNotRequired(t *testing.T) {
	thread := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "test query",
		StreamState: WorkStreamActive,
	}
	thread.InitializeWorkStream("planning")

	// Pause without manual resume
	thread.PauseWorkStreamWithReason("Quick interruption", false)

	if !thread.CanAutoResume() {
		t.Error("expected CanAutoResume to return true for auto-resumable stream")
	}
}

func TestCanAutoResume_BlocksAutoResumeWhenRequired(t *testing.T) {
	thread := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "test query",
		StreamState: WorkStreamActive,
	}
	thread.InitializeWorkStream("planning")

	// Pause with manual resume required
	thread.PauseWorkStreamWithReason("User requested pause", true)

	if thread.CanAutoResume() {
		t.Error("expected CanAutoResume to return false for manual resume stream")
	}
}

func TestIsWaitingForManualResume_DetectsManualResumeState(t *testing.T) {
	thread := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "test query",
		StreamState: WorkStreamActive,
	}
	thread.InitializeWorkStream("planning")

	// Initially not waiting
	if thread.IsWaitingForManualResume() {
		t.Error("expected IsWaitingForManualResume to be false for active stream")
	}

	// Pause with manual resume
	thread.PauseWorkStreamWithReason("User pause", true)

	if !thread.IsWaitingForManualResume() {
		t.Error("expected IsWaitingForManualResume to be true after pause with manual resume")
	}
}

func TestResumeWorkStream_ClearsManualResumeFlag(t *testing.T) {
	thread := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "test query",
		StreamState: WorkStreamActive,
	}
	thread.InitializeWorkStream("planning")

	// Pause with manual resume
	thread.PauseWorkStreamWithReason("User pause", true)

	// Resume
	thread.ResumeWorkStream()

	if thread.StreamState != WorkStreamActive {
		t.Errorf("expected stream state to be active after resume, got %s", thread.StreamState)
	}

	if thread.StreamContext.RequiresManualResume {
		t.Error("expected RequiresManualResume to be false after resume")
	}

	if thread.StreamContext.PauseReason != "" {
		t.Error("expected PauseReason to be cleared after resume")
	}

	if thread.StreamContext.PausedAt != 0 {
		t.Error("expected PausedAt to be cleared after resume")
	}
}

func TestGetManualResumeWorkStreams_FiltersCorrectly(t *testing.T) {
	stm := NewShortTermMemory()

	// Create several threads with different states
	thread1 := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "manual resume thread",
		StreamState: WorkStreamActive,
	}
	thread1.InitializeWorkStream("planning")
	thread1.PauseWorkStreamWithReason("User pause", true)
	stm.AddThread(thread1)

	thread2 := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "auto resume thread",
		StreamState: WorkStreamActive,
	}
	thread2.InitializeWorkStream("planning")
	thread2.PauseWorkStreamWithReason("Quick interrupt", false)
	stm.AddThread(thread2)

	thread3 := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "active thread",
		StreamState: WorkStreamActive,
	}
	thread3.InitializeWorkStream("planning")
	stm.AddThread(thread3)

	thread4 := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "another manual resume",
		StreamState: WorkStreamActive,
	}
	thread4.InitializeWorkStream("execution")
	thread4.PauseWorkStreamWithReason("Working on bug", true)
	stm.AddThread(thread4)

	// Get manual resume streams
	manualResumeStreams := stm.GetManualResumeWorkStreams()

	if len(manualResumeStreams) != 2 {
		t.Errorf("expected 2 manual resume streams, got %d", len(manualResumeStreams))
	}

	// Verify correct threads are returned
	foundThread1 := false
	foundThread4 := false
	for _, thread := range manualResumeStreams {
		if thread.ID == thread1.ID {
			foundThread1 = true
		}
		if thread.ID == thread4.ID {
			foundThread4 = true
		}
	}

	if !foundThread1 || !foundThread4 {
		t.Error("expected to find thread1 and thread4 in manual resume streams")
	}
}

func TestFormatPausedWorkStreamsReminder_CreatesUserFriendlyMessage(t *testing.T) {
	stm := NewShortTermMemory()

	// Create paused threads
	thread1 := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "Fix authentication bug",
		Label:       "Auth Bug Fix",
		StreamState: WorkStreamActive,
	}
	thread1.InitializeWorkStream("investigation")
	thread1.PauseWorkStreamWithReason("User requested to work on payments first", true)
	stm.AddThread(thread1)

	thread2 := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "Add Redis caching",
		Label:       "Redis Cache",
		StreamState: WorkStreamActive,
	}
	thread2.InitializeWorkStream("planning")
	thread2.PauseWorkStreamWithReason("Waiting for design review", true)
	stm.AddThread(thread2)

	// Get reminder
	reminder := stm.FormatPausedWorkStreamsReminder()

	// Verify content
	if !strings.Contains(reminder, "Paused Work Streams") {
		t.Error("expected reminder to contain 'Paused Work Streams' header")
	}

	if !strings.Contains(reminder, "Auth Bug Fix") {
		t.Error("expected reminder to contain thread1 label")
	}

	if !strings.Contains(reminder, "Redis Cache") {
		t.Error("expected reminder to contain thread2 label")
	}

	if !strings.Contains(reminder, "investigation phase") {
		t.Error("expected reminder to show thread1's current phase")
	}

	if !strings.Contains(reminder, "planning phase") {
		t.Error("expected reminder to show thread2's current phase")
	}

	if !strings.Contains(reminder, "User requested to work on payments first") {
		t.Error("expected reminder to show thread1's pause reason")
	}

	if !strings.Contains(reminder, "Waiting for design review") {
		t.Error("expected reminder to show thread2's pause reason")
	}

	if !strings.Contains(reminder, "resume") {
		t.Error("expected reminder to include resume instruction")
	}
}

func TestFormatPausedWorkStreamsReminder_ReturnsEmptyForNoStreams(t *testing.T) {
	stm := NewShortTermMemory()

	reminder := stm.FormatPausedWorkStreamsReminder()

	if reminder != "" {
		t.Errorf("expected empty reminder when no paused streams, got: %s", reminder)
	}
}

func TestFormatPausedWorkStreamsReminder_ShowsTimeSincePause(t *testing.T) {
	stm := NewShortTermMemory()

	thread := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "Test task",
		Label:       "Test",
		StreamState: WorkStreamActive,
	}
	thread.InitializeWorkStream("planning")

	// Manually set paused timestamp to 2 minutes ago
	thread.StreamState = WorkStreamPaused
	thread.StreamContext.PausedAt = time.Now().Unix() - 120
	thread.StreamContext.RequiresManualResume = true
	thread.StreamContext.PauseReason = "Test pause"

	stm.AddThread(thread)

	reminder := stm.FormatPausedWorkStreamsReminder()

	// Should show "2m ago" or similar
	if !strings.Contains(reminder, "ago") {
		t.Errorf("expected reminder to show time since pause, got: %s", reminder)
	}
}

func TestGetPausedWorkStreams_ReturnsAllPausedThreads(t *testing.T) {
	stm := NewShortTermMemory()

	// Create threads with different states
	pausedManual := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "paused manual",
		StreamState: WorkStreamActive,
	}
	pausedManual.InitializeWorkStream("planning")
	pausedManual.PauseWorkStreamWithReason("Manual", true)
	stm.AddThread(pausedManual)

	pausedAuto := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "paused auto",
		StreamState: WorkStreamActive,
	}
	pausedAuto.InitializeWorkStream("planning")
	pausedAuto.PauseWorkStreamWithReason("Auto", false)
	stm.AddThread(pausedAuto)

	active := &ConversationThread{
		ID:          sop.NewUUID(),
		RootPrompt:  "active",
		StreamState: WorkStreamActive,
	}
	active.InitializeWorkStream("planning")
	stm.AddThread(active)

	paused := stm.GetPausedWorkStreams()

	if len(paused) != 2 {
		t.Errorf("expected 2 paused streams (manual + auto), got %d", len(paused))
	}
}
