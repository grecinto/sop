# KB Quality Feature Flags & Staged Rollout

**Purpose:** Safe, incremental rollout of KB Quality features with ability to disable problematic components.

For the production validation checklist, see [KB_QUALITY_PRODUCTION_VALIDATION.md](KB_QUALITY_PRODUCTION_VALIDATION.md).

---

## Feature Flag Architecture

### Implementation Strategy

**Storage:** Environment variables + runtime config (no database dependency)

**Scope:** Process-level (daemon/httpserver) - restart required for changes

**Format:**
```bash
export KB_QUALITY_ENABLED=true
export KB_QUALITY_VALIDATION_ENABLED=true
export KB_QUALITY_STRICT_MODE=false
export KB_QUALITY_FEEDBACK_ENABLED=true
export KB_QUALITY_DASHBOARD_ENABLED=true
export KB_QUALITY_BACKGROUND_JOBS_ENABLED=true
```

### Flag Definitions

| Flag | Default | Phase | Description |
|------|---------|-------|-------------|
| `KB_QUALITY_ENABLED` | `true` | All | Master switch - disables all KB Quality features |
| `KB_QUALITY_VALIDATION_ENABLED` | `true` | Phase 1 | Enable post-generation validation |
| `KB_QUALITY_STRICT_MODE` | `false` | Phase 1 | Strict validation (reject on violations) vs lenient (warn) |
| `KB_QUALITY_CITATIONS_ENABLED` | `true` | Phase 1 | Add inline KB attribution comments |
| `KB_QUALITY_REVISION_ENABLED` | `true` | Phase 1 | Enable automatic revision loop |
| `KB_QUALITY_MAX_REVISIONS` | `2` | Phase 1 | Maximum revision attempts |
| `KB_QUALITY_TASK_CLASSIFIER_ENABLED` | `true` | Phase 2 | Automatic task classification |
| `KB_QUALITY_EFFECTIVENESS_TRACKING` | `true` | Phase 2 | Historical success/failure tracking |
| `KB_QUALITY_AUTO_SELECTION` | `true` | Phase 2 | Automatic KB selection based on task |
| `KB_QUALITY_FEEDBACK_ENABLED` | `true` | Phase 3 | User feedback collection |
| `KB_QUALITY_DASHBOARD_ENABLED` | `true` | Phase 3 | Quality dashboard UI |
| `KB_QUALITY_BACKGROUND_JOBS_ENABLED` | `true` | Phase 3 | Periodic quality analysis |
| `KB_QUALITY_AUTO_PRUNING_ENABLED` | `false` | Phase 3 | Automatic pruning of poor performers |
| `KB_QUALITY_EXPLANATION_ENABLED` | `true` | Phase 4 | Human-readable explanations |
| `KB_QUALITY_EXPLANATION_VERBOSITY` | `standard` | Phase 4 | `minimal`, `standard`, `detailed` |
| `KB_QUALITY_METADATA_EMBEDDING` | `true` | Phase 4 | Embed KB metadata in responses |

### Implementation Location

**File:** `lib/sop/ai/agent/codingworkflow/config.go` (NEW)

```go
package codingworkflow

import (
    "os"
    "strconv"
    "strings"
)

// KBQualityConfig holds runtime feature flags for KB Quality system
type KBQualityConfig struct {
    Enabled                  bool
    ValidationEnabled        bool
    StrictMode               bool
    CitationsEnabled         bool
    RevisionEnabled          bool
    MaxRevisions             int
    TaskClassifierEnabled    bool
    EffectivenessTracking    bool
    AutoSelection            bool
    FeedbackEnabled          bool
    DashboardEnabled         bool
    BackgroundJobsEnabled    bool
    AutoPruningEnabled       bool
    ExplanationEnabled       bool
    ExplanationVerbosity     string
    MetadataEmbedding        bool
}

// DefaultKBQualityConfig returns production-ready defaults
func DefaultKBQualityConfig() *KBQualityConfig {
    return &KBQualityConfig{
        Enabled:                  true,
        ValidationEnabled:        true,
        StrictMode:               false, // lenient by default
        CitationsEnabled:         true,
        RevisionEnabled:          true,
        MaxRevisions:             2,
        TaskClassifierEnabled:    true,
        EffectivenessTracking:    true,
        AutoSelection:            true,
        FeedbackEnabled:          true,
        DashboardEnabled:         true,
        BackgroundJobsEnabled:    true,
        AutoPruningEnabled:       false, // safety: require manual pruning
        ExplanationEnabled:       true,
        ExplanationVerbosity:     "standard",
        MetadataEmbedding:        true,
    }
}

// LoadKBQualityConfig reads config from environment variables
func LoadKBQualityConfig() *KBQualityConfig {
    cfg := DefaultKBQualityConfig()
    
    // Master switch
    if v := os.Getenv("KB_QUALITY_ENABLED"); v != "" {
        cfg.Enabled = parseBool(v, true)
    }
    
    // Phase 1: Validation
    if v := os.Getenv("KB_QUALITY_VALIDATION_ENABLED"); v != "" {
        cfg.ValidationEnabled = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_STRICT_MODE"); v != "" {
        cfg.StrictMode = parseBool(v, false)
    }
    if v := os.Getenv("KB_QUALITY_CITATIONS_ENABLED"); v != "" {
        cfg.CitationsEnabled = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_REVISION_ENABLED"); v != "" {
        cfg.RevisionEnabled = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_MAX_REVISIONS"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n >= 0 {
            cfg.MaxRevisions = n
        }
    }
    
    // Phase 2: Selection
    if v := os.Getenv("KB_QUALITY_TASK_CLASSIFIER_ENABLED"); v != "" {
        cfg.TaskClassifierEnabled = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_EFFECTIVENESS_TRACKING"); v != "" {
        cfg.EffectivenessTracking = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_AUTO_SELECTION"); v != "" {
        cfg.AutoSelection = parseBool(v, true)
    }
    
    // Phase 3: Feedback & Learning
    if v := os.Getenv("KB_QUALITY_FEEDBACK_ENABLED"); v != "" {
        cfg.FeedbackEnabled = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_DASHBOARD_ENABLED"); v != "" {
        cfg.DashboardEnabled = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_BACKGROUND_JOBS_ENABLED"); v != "" {
        cfg.BackgroundJobsEnabled = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_AUTO_PRUNING_ENABLED"); v != "" {
        cfg.AutoPruningEnabled = parseBool(v, false) // safety default
    }
    
    // Phase 4: UX
    if v := os.Getenv("KB_QUALITY_EXPLANATION_ENABLED"); v != "" {
        cfg.ExplanationEnabled = parseBool(v, true)
    }
    if v := os.Getenv("KB_QUALITY_EXPLANATION_VERBOSITY"); v != "" {
        v = strings.ToLower(v)
        if v == "minimal" || v == "standard" || v == "detailed" {
            cfg.ExplanationVerbosity = v
        }
    }
    if v := os.Getenv("KB_QUALITY_METADATA_EMBEDDING"); v != "" {
        cfg.MetadataEmbedding = parseBool(v, true)
    }
    
    return cfg
}

func parseBool(s string, defaultVal bool) bool {
    s = strings.ToLower(strings.TrimSpace(s))
    if s == "true" || s == "1" || s == "yes" || s == "on" {
        return true
    }
    if s == "false" || s == "0" || s == "no" || s == "off" {
        return false
    }
    return defaultVal
}
```

### Integration Points

**Orchestrator Initialization:**
```go
// In orchestrator.go Execute()
func (o *Orchestrator) Execute(task Task) (ExecutionResult, error) {
    cfg := LoadKBQualityConfig()
    
    // Skip KB Quality entirely if disabled
    if !cfg.Enabled {
        return o.executeWithoutKBQuality(task)
    }
    
    // Conditional initialization based on flags
    if cfg.ValidationEnabled && o.ValidationOrchestrator == nil {
        o.ValidationOrchestrator = NewValidationOrchestrator(
            NewValidator(ValidatorConfig{StrictMode: cfg.StrictMode}),
            CitationConfig{Enabled: cfg.CitationsEnabled},
        )
    }
    
    if cfg.TaskClassifierEnabled && o.TaskClassifier == nil {
        o.TaskClassifier = NewTaskClassifier()
    }
    
    // ... rest of Execute logic with conditional checks
}
```

**Backend Initialization:**
```go
// In sopbackend/backend.go
func (b *Backend) initKBQuality() error {
    cfg := codingworkflow.LoadKBQualityConfig()
    
    if !cfg.Enabled {
        return nil // skip initialization
    }
    
    if cfg.EffectivenessTracking {
        b.KBEffectivenessTracker = memory.NewKBEffectivenessTracker(b.getKBEffectivenessPath())
    }
    
    if cfg.DashboardEnabled {
        // Register dashboard routes
    }
    
    if cfg.BackgroundJobsEnabled {
        // Start background quality check job
    }
    
    return nil
}
```

---

## Staged Rollout Plan

### Phase 0: Pre-Rollout (Week -1)

**Goals:**
- Final testing in staging environment
- Documentation complete and reviewed
- Monitoring infrastructure ready

**Tasks:**
- [ ] Deploy to staging with all flags enabled
- [ ] Run acceptance tests (90+ test suite)
- [ ] Review user guide with team
- [ ] Set up monitoring dashboards (Grafana)
- [ ] Create rollback procedures
- [ ] Prepare user communication

**Success Criteria:**
- All tests passing ✅
- No performance degradation (< 50ms overhead)
- Documentation reviewed by 3+ team members
- Monitoring dashboards operational

**Configuration:**
```bash
# Staging environment
export KB_QUALITY_ENABLED=true
export KB_QUALITY_VALIDATION_ENABLED=true
export KB_QUALITY_STRICT_MODE=false  # lenient during testing
# All other flags: true (except AUTO_PRUNING)
```

---

### Stage 1: Silent Rollout (Week 1-2)

**Audience:** Internal team only (5-10 users)

**Goal:** Validate core functionality without user impact

**Flags:**
```bash
export KB_QUALITY_ENABLED=true
export KB_QUALITY_VALIDATION_ENABLED=true
export KB_QUALITY_STRICT_MODE=false           # lenient mode only
export KB_QUALITY_CITATIONS_ENABLED=false     # no visible changes yet
export KB_QUALITY_REVISION_ENABLED=true
export KB_QUALITY_TASK_CLASSIFIER_ENABLED=true
export KB_QUALITY_EFFECTIVENESS_TRACKING=true
export KB_QUALITY_AUTO_SELECTION=true
export KB_QUALITY_FEEDBACK_ENABLED=false      # not visible yet
export KB_QUALITY_DASHBOARD_ENABLED=true      # admin only
export KB_QUALITY_BACKGROUND_JOBS_ENABLED=true
export KB_QUALITY_EXPLANATION_ENABLED=true
export KB_QUALITY_METADATA_EMBEDDING=false    # not needed without feedback UI
```

**What's Active:**
- ✅ Validation (lenient warnings only)
- ✅ Automatic revision loop
- ✅ Task classification
- ✅ Auto KB selection
- ✅ Effectiveness tracking (learning mode)
- ✅ Quality dashboard (for monitoring)

**What's Hidden:**
- ❌ Citations (no visible code changes)
- ❌ Feedback buttons (not shown to users)
- ❌ Explanations (generated but not displayed)

**Monitoring:**
- Validation pass/fail rates
- Revision loop frequency
- Task classification accuracy
- Performance overhead (< 50ms target)
- Error rates

**Success Criteria:**
- Zero production incidents
- Validation overhead < 50ms average
- Task classifier accuracy > 80%
- No user complaints about slow responses

**Communication:**
```
Subject: [Internal] KB Quality Beta - Week 1

We've enabled KB Quality in silent mode. You won't see any changes, but the 
system is learning in the background.

What to watch for:
- Code generation speed (should be unchanged)
- Any unexpected errors in AI responses
- Check /kb-quality dashboard for metrics

Report issues: #ai-feedback channel
```

---

### Stage 2: Feedback Collection (Week 3-4)

**Audience:** Internal team (5-10 users) + early adopters (10-15 users)

**Goal:** Collect user feedback to train effectiveness tracker

**Flags:**
```bash
export KB_QUALITY_ENABLED=true
export KB_QUALITY_VALIDATION_ENABLED=true
export KB_QUALITY_STRICT_MODE=false
export KB_QUALITY_CITATIONS_ENABLED=false     # still hidden
export KB_QUALITY_REVISION_ENABLED=true
export KB_QUALITY_TASK_CLASSIFIER_ENABLED=true
export KB_QUALITY_EFFECTIVENESS_TRACKING=true
export KB_QUALITY_AUTO_SELECTION=true
export KB_QUALITY_FEEDBACK_ENABLED=true       # ← NEW: feedback buttons visible
export KB_QUALITY_DASHBOARD_ENABLED=true
export KB_QUALITY_BACKGROUND_JOBS_ENABLED=true
export KB_QUALITY_EXPLANATION_ENABLED=true
export KB_QUALITY_METADATA_EMBEDDING=true     # ← NEW: needed for feedback UI
```

**What's New:**
- ✅ Feedback buttons (👍/👎) appear when Spaces are used
- ✅ KB metadata embedded in responses
- ✅ Explanations available (collapsible)

**User Instructions:**
- Share [SPACES_USER_GUIDE.md](SPACES_USER_GUIDE.md)
- Request feedback on 50%+ of generations
- Weekly check-in meetings for feedback

**Monitoring:**
- Feedback submission rate (target: 50%+)
- Positive vs negative feedback ratio
- Effectiveness score trends
- User engagement metrics

**Success Criteria:**
- 50%+ of early adopters submit feedback regularly
- Feedback submission rate > 30% overall
- Effectiveness scores improving week-over-week
- No usability complaints about feedback UI

**Communication:**
```
Subject: KB Quality Beta - Feedback Collection Phase

Spaces are now learning from your feedback! 

New features visible:
- 👍/👎 feedback buttons when Spaces are used
- "How I used expertise" explanation section
- Space quality dashboard at /kb-quality

Please rate code quality frequently - it helps the system learn your preferences.

User Guide: docs/SPACES_USER_GUIDE.md
```

---

### Stage 3: Full Visibility (Week 5-6)

**Audience:** All users (beta program)

**Goal:** Make Spaces fully visible with all features

**Flags:**
```bash
export KB_QUALITY_ENABLED=true
export KB_QUALITY_VALIDATION_ENABLED=true
export KB_QUALITY_STRICT_MODE=false
export KB_QUALITY_CITATIONS_ENABLED=true      # ← NEW: citations visible
export KB_QUALITY_REVISION_ENABLED=true
export KB_QUALITY_TASK_CLASSIFIER_ENABLED=true
export KB_QUALITY_EFFECTIVENESS_TRACKING=true
export KB_QUALITY_AUTO_SELECTION=true
export KB_QUALITY_FEEDBACK_ENABLED=true
export KB_QUALITY_DASHBOARD_ENABLED=true
export KB_QUALITY_BACKGROUND_JOBS_ENABLED=true
export KB_QUALITY_EXPLANATION_ENABLED=true
export KB_QUALITY_METADATA_EMBEDDING=true
```

**What's New:**
- ✅ Inline citations show which Spaces influenced code
- ✅ Full transparency into KB selection

**User Instructions:**
- Citations show as comments: `// Space Patterns Applied: ...`
- Press Ctrl+Shift+K to toggle citation visibility (coming soon)
- Review explanations to understand Space impact

**Monitoring:**
- Citation feedback (too noisy? helpful?)
- User engagement with explanations
- Quality dashboard usage
- Support ticket volume

**Success Criteria:**
- < 5% users request to disable citations
- Explanation section viewed in 30%+ of generations
- Quality dashboard accessed by 20%+ of users
- Support tickets < 2 per week

**Communication:**
```
Subject: KB Quality Beta - Full Feature Rollout

All Space features are now visible!

What's new:
- Inline citations show which Spaces influenced your code
- Full explanations available in "How I used expertise"
- Quality dashboard shows Space performance over time

Toggle citations: Ctrl+Shift+K (coming soon)
Disable auto-selection: Settings > Spaces

User Guide: docs/SPACES_USER_GUIDE.md
Feedback: #ai-feedback channel
```

---

### Stage 4: General Availability (Week 7+)

**Audience:** All users (production)

**Goal:** Production-ready with monitoring and support

**Flags:**
```bash
# All features enabled (production defaults)
export KB_QUALITY_ENABLED=true
export KB_QUALITY_VALIDATION_ENABLED=true
export KB_QUALITY_STRICT_MODE=false
export KB_QUALITY_CITATIONS_ENABLED=true
export KB_QUALITY_REVISION_ENABLED=true
export KB_QUALITY_MAX_REVISIONS=2
export KB_QUALITY_TASK_CLASSIFIER_ENABLED=true
export KB_QUALITY_EFFECTIVENESS_TRACKING=true
export KB_QUALITY_AUTO_SELECTION=true
export KB_QUALITY_FEEDBACK_ENABLED=true
export KB_QUALITY_DASHBOARD_ENABLED=true
export KB_QUALITY_BACKGROUND_JOBS_ENABLED=true
export KB_QUALITY_AUTO_PRUNING_ENABLED=false  # manual pruning only
export KB_QUALITY_EXPLANATION_ENABLED=true
export KB_QUALITY_EXPLANATION_VERBOSITY=standard
export KB_QUALITY_METADATA_EMBEDDING=true
```

**Ongoing:**
- Weekly quality dashboard reviews
- Monthly Space pruning (manual review)
- Quarterly effectiveness analysis
- Continuous user feedback collection

**Success Criteria:**
- User satisfaction > 4/5 stars
- Feedback submission rate > 40%
- Support ticket volume stable
- Quality metrics improving month-over-month

**Communication:**
```
Subject: Spaces Feature Now Available to Everyone

We're excited to announce Spaces are now available to all users!

Spaces automatically apply proven patterns from your knowledge bases to improve 
code quality and reduce iteration time.

Getting started:
1. Read the User Guide: docs/SPACES_USER_GUIDE.md
2. Give feedback (👍/👎) to help Spaces learn
3. Check /kb-quality dashboard to see Space performance

Questions? Visit #ai-support
```

---

## Rollback Procedures

### Emergency Rollback (Critical Issue)

**Trigger:** Production incident, data loss, severe performance degradation

**Action:**
```bash
# Disable all KB Quality features immediately
export KB_QUALITY_ENABLED=false

# Restart services
systemctl restart sop-daemon
systemctl restart sop-httpserver
```

**Recovery Time:** < 5 minutes

**Impact:** System reverts to pre-KB-Quality behavior

---

### Selective Rollback (Feature-Specific Issue)

**Scenario:** Validation too strict, feedback UI broken, dashboard errors

**Action:** Disable specific feature flags

**Examples:**

**Validation causing false positives:**
```bash
export KB_QUALITY_VALIDATION_ENABLED=false
# or switch to lenient mode
export KB_QUALITY_STRICT_MODE=false
```

**Feedback UI broken:**
```bash
export KB_QUALITY_FEEDBACK_ENABLED=false
export KB_QUALITY_METADATA_EMBEDDING=false
```

**Dashboard errors:**
```bash
export KB_QUALITY_DASHBOARD_ENABLED=false
export KB_QUALITY_BACKGROUND_JOBS_ENABLED=false
```

**Recovery Time:** < 5 minutes per service restart

---

### Partial Rollback (Gradual Degradation)

**Scenario:** Performance overhead accumulating, user complaints increasing

**Action:** Reduce feature scope incrementally

**Step 1:** Disable expensive features
```bash
export KB_QUALITY_CITATIONS_ENABLED=false     # reduce code generation overhead
export KB_QUALITY_EXPLANATION_ENABLED=false   # reduce computation
```

**Step 2:** Reduce revision attempts
```bash
export KB_QUALITY_MAX_REVISIONS=1  # or 0 to disable
```

**Step 3:** Disable background jobs
```bash
export KB_QUALITY_BACKGROUND_JOBS_ENABLED=false
```

**Step 4:** Full rollback if issues persist
```bash
export KB_QUALITY_ENABLED=false
```

---

## Monitoring & Alerting

### Key Metrics

**Performance:**
- P50/P95/P99 validation latency (target: <50ms)
- Revision loop frequency (target: <20% of generations)
- Dashboard load time (target: <2s)

**Quality:**
- Validation pass rate (target: >90%)
- Feedback positive rate (target: >70%)
- Effectiveness score trends (target: improving)

**Engagement:**
- Feedback submission rate (target: >40%)
- Dashboard views per week (target: >20% users)
- Explanation section expansion rate (target: >30%)

**Errors:**
- KB Quality error rate (target: <0.1%)
- Background job failures (target: 0)
- API endpoint errors (target: <1%)

### Alert Thresholds

| Metric | Warning | Critical | Action |
|--------|---------|----------|--------|
| Validation latency P95 | >100ms | >200ms | Investigate, consider rollback |
| Validation pass rate | <85% | <75% | Review validator rules |
| Feedback positive rate | <60% | <50% | Review KB content quality |
| Error rate | >1% | >5% | Immediate investigation |
| Background job failures | >1/day | >3/day | Fix job logic, restart |

### Dashboard URLs

**Production:**
- Grafana: `http://grafana.example.com/d/kb-quality`
- Prometheus: `http://prometheus.example.com/graph`
- KB Quality: `http://127.0.0.1:9090/kb-quality`

**Staging:**
- Grafana: `http://staging-grafana.example.com/d/kb-quality`
- KB Quality: `http://127.0.0.1:9090/kb-quality`

---

## Success Metrics Summary

| Stage | Duration | Key Metric | Target | Actual |
|-------|----------|------------|--------|--------|
| Stage 1 | Week 1-2 | Zero incidents | 0 | _TBD_ |
| Stage 2 | Week 3-4 | Feedback rate | >30% | _TBD_ |
| Stage 3 | Week 5-6 | Citation acceptance | >95% | _TBD_ |
| Stage 4 | Week 7+ | User satisfaction | >4/5 | _TBD_ |

**Overall Success Criteria:**
- ✅ Zero data loss incidents
- ✅ Performance overhead < 50ms
- ✅ User satisfaction > 4/5 stars
- ✅ Feedback submission rate > 40%
- ✅ Support ticket volume < 5/week
- ✅ Effectiveness scores improving month-over-month

---

## Post-Rollout Review

**Schedule:** Week 8 (after 1 week of GA)

**Attendees:** Engineering team, product, support

**Agenda:**
1. Review success metrics vs targets
2. Analyze user feedback themes
3. Identify top pain points
4. Prioritize enhancements (from KB_QUALITY_REMAINING_ENHANCEMENTS.md)
5. Plan next iteration

**Deliverables:**
- Rollout retrospective document
- Enhancement priority list
- Resource allocation for follow-up work

---

## Appendix: Configuration Examples

### Development Environment
```bash
# Maximum observability, minimal restrictions
export KB_QUALITY_ENABLED=true
export KB_QUALITY_STRICT_MODE=false
export KB_QUALITY_EXPLANATION_VERBOSITY=detailed
export KB_QUALITY_MAX_REVISIONS=3
```

### Testing Environment
```bash
# All features on for comprehensive testing
export KB_QUALITY_ENABLED=true
export KB_QUALITY_STRICT_MODE=true  # test strict mode
export KB_QUALITY_AUTO_PRUNING_ENABLED=true  # test pruning logic
```

### Production Environment (Conservative)
```bash
# Safe defaults, manual control
export KB_QUALITY_ENABLED=true
export KB_QUALITY_STRICT_MODE=false
export KB_QUALITY_AUTO_PRUNING_ENABLED=false
export KB_QUALITY_EXPLANATION_VERBOSITY=standard
```

### Troubleshooting Environment
```bash
# Minimal features for debugging
export KB_QUALITY_ENABLED=true
export KB_QUALITY_VALIDATION_ENABLED=false
export KB_QUALITY_FEEDBACK_ENABLED=false
export KB_QUALITY_BACKGROUND_JOBS_ENABLED=false
```
