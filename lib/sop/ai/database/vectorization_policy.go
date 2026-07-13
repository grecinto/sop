package database

import "sync"

// VectorizationFailurePolicy defines how the vectorization pipeline should react
// when semantic vectorization for a target fails. The policy receives explicit
// parameters for the target and the error so callers are not forced to push
// runtime state through context.
type VectorizationFailurePolicy interface {
	HandleFailure(kbName, targetType, targetID string, err error) (skip bool, escalate bool)
}

// CompositeVectorizationFailurePolicy runs multiple policies in order and
// aggregates the resulting skip/escalation decisions.
type CompositeVectorizationFailurePolicy []VectorizationFailurePolicy

func (p CompositeVectorizationFailurePolicy) HandleFailure(kbName, targetType, targetID string, err error) (skip bool, escalate bool) {
	for _, policy := range p {
		if policy == nil {
			continue
		}
		subSkip, subEscalate := policy.HandleFailure(kbName, targetType, targetID, err)
		if subSkip {
			skip = true
		}
		if subEscalate {
			escalate = true
		}
	}
	return skip, escalate
}

// NoopVectorizationFailurePolicy preserves the existing hard-fail behavior.
type NoopVectorizationFailurePolicy struct{}

func (NoopVectorizationFailurePolicy) HandleFailure(kbName, targetType, targetID string, err error) (bool, bool) {
	return false, false
}

var (
	vectorizationFailurePolicyMu sync.RWMutex
	vectorizationFailurePolicy   VectorizationFailurePolicy = NoopVectorizationFailurePolicy{}
)

// RegisterVectorizationFailurePolicy installs a policy for the current process.
// The returned function restores the previous policy.
func RegisterVectorizationFailurePolicy(policy VectorizationFailurePolicy) func() {
	vectorizationFailurePolicyMu.Lock()
	defer vectorizationFailurePolicyMu.Unlock()

	previous := vectorizationFailurePolicy
	if policy == nil {
		vectorizationFailurePolicy = NoopVectorizationFailurePolicy{}
	} else {
		vectorizationFailurePolicy = policy
	}

	return func() {
		vectorizationFailurePolicyMu.Lock()
		defer vectorizationFailurePolicyMu.Unlock()
		vectorizationFailurePolicy = previous
	}
}

func handleVectorizationFailure(kbName, targetType, targetID string, err error) (bool, bool) {
	if err == nil {
		return false, false
	}

	vectorizationFailurePolicyMu.RLock()
	policy := vectorizationFailurePolicy
	vectorizationFailurePolicyMu.RUnlock()

	if policy == nil {
		return false, false
	}

	return policy.HandleFailure(kbName, targetType, targetID, err)
}
