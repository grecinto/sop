package capabilities

import (
	"context"
	"fmt"
	log "log/slog"
)

// LifecycleAware extends Capability with lifecycle hooks for resource management
// Capabilities implementing this interface can perform setup, cleanup, and error handling
type LifecycleAware interface {
	Capability

	// OnEnable is called when the capability is enabled
	OnEnable(ctx context.Context) error

	// OnDisable is called when the capability is disabled
	OnDisable(ctx context.Context) error

	// OnError is called when the capability encounters an error
	OnError(ctx context.Context, err error)

	// OnShutdown is called during graceful shutdown
	OnShutdown(ctx context.Context) error
}

// LifecycleManager manages capability lifecycle events
type LifecycleManager struct {
	registry *Registry
	enabled  map[string]bool
}

// NewLifecycleManager creates a new lifecycle manager for the given registry
func NewLifecycleManager(registry *Registry) *LifecycleManager {
	return &LifecycleManager{
		registry: registry,
		enabled:  make(map[string]bool),
	}
}

// EnableCapability enables a capability and calls OnEnable hook if implemented
func (lm *LifecycleManager) EnableCapability(ctx context.Context, capabilityID string) error {
	caps := lm.registry.ListAll()
	for _, cap := range caps {
		if cap.ID() == capabilityID {
			// Call OnEnable hook if capability implements LifecycleAware
			if lifecycleAware, ok := cap.(LifecycleAware); ok {
				if err := lifecycleAware.OnEnable(ctx); err != nil {
					return fmt.Errorf("failed to enable capability %s: %w", capabilityID, err)
				}
			}
			lm.enabled[capabilityID] = true
			log.Info("Capability enabled", "id", capabilityID)
			return nil
		}
	}
	return fmt.Errorf("capability not found: %s", capabilityID)
}

// DisableCapability disables a capability and calls OnDisable hook if implemented
func (lm *LifecycleManager) DisableCapability(ctx context.Context, capabilityID string) error {
	caps := lm.registry.ListAll()
	for _, cap := range caps {
		if cap.ID() == capabilityID {
			// Call OnDisable hook if capability implements LifecycleAware
			if lifecycleAware, ok := cap.(LifecycleAware); ok {
				if err := lifecycleAware.OnDisable(ctx); err != nil {
					return fmt.Errorf("failed to disable capability %s: %w", capabilityID, err)
				}
			}
			lm.enabled[capabilityID] = false
			log.Info("Capability disabled", "id", capabilityID)
			return nil
		}
	}
	return fmt.Errorf("capability not found: %s", capabilityID)
}

// HandleError notifies capability of error via OnError hook if implemented
func (lm *LifecycleManager) HandleError(ctx context.Context, capability Capability, err error) {
	if lifecycleAware, ok := capability.(LifecycleAware); ok {
		lifecycleAware.OnError(ctx, err)
	}
	log.Error("Capability error", "id", capability.ID(), "error", err)
}

// ShutdownAll calls OnShutdown on all lifecycle-aware capabilities
func (lm *LifecycleManager) ShutdownAll(ctx context.Context) error {
	caps := lm.registry.ListAll()
	var errors []error

	for _, cap := range caps {
		if lifecycleAware, ok := cap.(LifecycleAware); ok {
			if err := lifecycleAware.OnShutdown(ctx); err != nil {
				errors = append(errors, fmt.Errorf("shutdown failed for %s: %w", cap.ID(), err))
			} else {
				log.Info("Capability shut down successfully", "id", cap.ID())
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	log.Info("All capabilities shut down successfully")
	return nil
}

// IsEnabled returns whether a capability is currently enabled
func (lm *LifecycleManager) IsEnabled(capabilityID string) bool {
	return lm.enabled[capabilityID]
}

// GetEnabledCapabilities returns a list of all enabled capability IDs
func (lm *LifecycleManager) GetEnabledCapabilities() []string {
	var enabled []string
	for id, isEnabled := range lm.enabled {
		if isEnabled {
			enabled = append(enabled, id)
		}
	}
	return enabled
}
