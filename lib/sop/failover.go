package sop

import (
	"context"
	"errors"
	"strings"
	"syscall"
)

// IsFailoverQualifiedIOError reports whether an error indicates the active drive/filesystem
// is unhealthy in a way that warrants immediate failover to the passive drive.
//
// This is distinct from ShouldRetry: retryable/transient errors should be retried first,
// while this function targets permanent/media/FS/device conditions where staying on the
// current drive is counterproductive.
//
// Notes:
//   - It includes common POSIX errno values available on macOS/Linux/BSD.
//   - For Linux-specific errno that may not exist on other platforms as named constants,
//     numeric values are used via syscall.Errno to remain portable (they will simply never
//     match on platforms that don't produce them).
//   - EFBIG is intentionally excluded per current SOP usage (registry/store repo use small files).
func IsFailoverQualifiedIOError(err error) bool {
	if err == nil {
		return false
	}

	// Context cancellations/timeouts are permanent from the caller's POV.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Always-failover: device/media/FS corruption or unavailability.
	always := []syscall.Errno{
		syscall.EIO,    // generic I/O error (bad sector, writeback/fsync failure)
		syscall.ENODEV, // no such device
		syscall.ENXIO,  // no such device or address
		syscall.EROFS,  // filesystem turned read-only (often due to prior errors)
		syscall.ENOSPC, // no space left on device
		syscall.EDQUOT, // disk quota exceeded
	}

	for _, code := range always {
		if errors.Is(err, code) {
			return true
		}
	}

	// Linux-specific serious conditions (numeric errno values for portability):
	linuxSpecific := []syscall.Errno{
		121, // EREMOTEIO: remote I/O error
		117, // EUCLEAN: structure needs cleaning (filesystem corruption)
		123, // ENOMEDIUM: no medium found
		124, // EMEDIUMTYPE: wrong medium type
	}
	for _, code := range linuxSpecific {
		if errors.Is(err, code) {
			return true
		}
	}

	// String fallback for cross-platform/read-only messages when errno mapping is opaque.
	// Keep this conservative and specific.
	s := err.Error()
	if strings.Contains(s, "read-only file system") || strings.Contains(s, "readonly file system") {
		return true
	}

	return false
}

// ShouldTriggerFailover reports whether a failover-qualified storage error should
// escalate to a failover decision. Repeated qualified failures are gated by the
// shared storage-health policy, while irreparable root causes (for example,
// read-only or device-unavailable conditions) bypass that threshold and trigger
// immediately.
func ShouldTriggerFailover(err error, operation, targetPath string) bool {
	if err == nil {
		return false
	}
	if !IsFailoverQualifiedIOError(err) {
		return false
	}
	if IsIrreparableFailoverError(err) {
		return true
	}

	policy := DefaultStorageHealthPolicy()
	if policy == nil {
		return false
	}
	if strings.TrimSpace(operation) != "" && strings.TrimSpace(targetPath) != "" {
		state := policy.StoragePathState(operation, targetPath)
		return state.Count >= 3
	}

	states := policy.StoragePathStates()
	for _, state := range states {
		if state.Status != StorageHealthHealthy && state.Count >= 3 {
			return true
		}
	}
	return false
}

// IsIrreparableFailoverError reports whether an error is so severe that it
// should bypass any thresholding and trigger failover immediately.
func IsIrreparableFailoverError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return true
	}
	if errors.Is(err, syscall.ENODEV) || errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.EIO) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "read-only file system") || strings.Contains(msg, "readonly file system") ||
		strings.Contains(msg, "no such device") || strings.Contains(msg, "device or resource busy")
}
