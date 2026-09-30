# Fleet Learning Privacy Governance

> **Status note:** This document contains historical implementation detail and privacy design. For federation context, see [FEDERATION.md](FEDERATION.md); the dated checklists below are not a live implementation tracker.

## Overview

Fleet learning enables operational knowledge sharing across daemons while preserving user privacy through daemon-level governance controls. Privacy enforcement happens at the edge (daemon) before any content reaches HQ, ensuring no performance impact on central infrastructure.

### Learning Architecture: Local + Fleet

The daemon has **three tiers of learning**:

1. **Local Learning (Always On)**: Daemon learns from its own experiences and stores in local LTM
   - Always enabled, regardless of fleet settings
   - User always benefits from their daemon's self-improvement
   - No privacy concerns (data never leaves daemon)
   - Improves future runs automatically

2. **Fleet Sharing Outbound (Optional)**: Share local learnings with HQ → other daemons
   - **Privacy controls apply here** (user opt-out, PII filtering, approval queue)
   - User can disable without affecting local learning (#1)

3. **Fleet Sharing Inbound (Optional)**: Receive pre-vetted learnings from other daemons via HQ
   - Accelerates learning beyond local experience
   - User can disable inbound separately

**Key Insight**: Opting out of fleet learning only disables cross-daemon sharing (#2 and #3). The daemon continues to learn from its own activity (#1) and improves future runs. Privacy governance only applies to optional fleet participation.

## Architecture Principle

**Filter at the edge, not at the hub.**

Each daemon decides what it shares before transmission. HQ trusts daemon filtering and only receives pre-vetted content.

## Implementation Structure

### Core Libraries (lib/sop)

**lib/sop/ai/privacy/filter.go**
- Core privacy filter logic
- PII detection patterns
- Redaction engine
- Reusable across daemon and backend

**lib/sop/ai/privacy/policy.go**
- Share policy types: strict, balanced, open
- Category allowlist management
- User preference storage schema

**lib/sop/ai/privacy/detector.go**
- Local PII detection (no external calls)
- Pattern matching for: emails, phones, IPs, API keys, SSNs, credit cards
- Configurable custom patterns

### Backend Integration (lib/sop/tools/sopbackend)

**lib/sop/tools/sopbackend/privacy_settings_handler.go**
- User privacy settings endpoints
- Category preference management
- Approval queue UI endpoints

**lib/sop/tools/sopbackend/templates/privacy_settings.html**
- User privacy control panel
- Category opt-in/opt-out toggles
- Private session mode controls

### Daemon Edge Enforcement (lib/sop/cmd/sop-daemon)

**lib/sop/cmd/sop-daemon/privacy_filter.go**
- Outbound content filtering
- Pre-share validation pipeline
- Integration with space_federation.go

**lib/sop/cmd/sop-daemon/privacy_config.go**
- Load privacy policy from daemon config
- User settings cache
- Default opt-in configuration

## Default Behavior: Opt-In by Default

### User Privacy Model

**On First Launch:**
- Fleet learning **enabled by default**
- User shown privacy notice with clear controls
- Transparent about what's shared and what's not

**Default Category Settings:**
```go
CategoryPreferences: map[string]bool{
    "workflow_pattern":         true,  // enabled
    "anti_pattern":             true,  // enabled
    "repair_strategy":          true,  // enabled
    "tool_preference":          true,  // enabled
    "performance_optimization": true,  // enabled
    "testing_strategy":         true,  // enabled
    "error_resolution":         true,  // enabled
    "security_pattern":         false, // disabled (sensitive)
    "code_quality":             true,  // enabled
    "api_design":               true,  // enabled
}
```

### What Gets Shared (By Default)

**Always Learned Locally (Never Leaves Daemon):**
- Everything the daemon experiences is learned locally
- Stored in local LTM for future improvement
- Available immediately in future sessions
- No privacy concerns - stays on daemon

**Optionally Shared to Fleet (After Privacy Filter):**
- Workflow patterns and solutions
- Anti-patterns and lessons learned
- Repair strategies
- Tool preferences and configurations
- Performance optimization insights
- Testing approaches
- Code quality guidance

**Never Shared to Fleet:**
- Conversation history or transcripts
- Personal information (PII)
- Private session content
- User-specific data
- Raw prompts and responses

**Opt-Out Impact**: If user opts out of fleet learning, local learning continues. The daemon still improves from its own experience, just doesn't participate in cross-daemon knowledge sharing.

## Privacy Filter Pipeline

### Filter Execution Order

```go
func (pf *PrivacyFilter) FilterBeforeShare(ctx context.Context, item *memory.Item) (FilterResult, error) {
    // 1. Check global opt-out (highest priority)
    if !settings.GlobalShareEnabled {
        return FilterResult{Action: "block", Reason: "user opted out globally"}, nil
    }
    
    // 2. Check category-specific opt-out
    if !settings.CategoryPreferences[item.Category] {
        return FilterResult{Action: "block", Reason: "category opted out"}, nil
    }
    
    // 3. Check private session flag
    if settings.PrivateSessionMode {
        return FilterResult{Action: "block", Reason: "private session active"}, nil
    }
    
    // 4. PII detection (always runs)
    if pf.PIIDetector.ContainsSensitiveData(item.Text) {
        if settings.BlockOnPII {
            return FilterResult{Action: "block", Reason: "PII detected"}, nil
        }
        return FilterResult{Action: "pending", Reason: "PII requires review"}, nil
    }
    
    // 5. Approval requirement (per-user setting)
    if settings.RequireApproval {
        return FilterResult{Action: "pending", Reason: "user approval required"}, nil
    }
    
    // DEFAULT: Allow sharing
    return FilterResult{Action: "allow"}, nil
}
```

### PII Detection Patterns

**Built-in Patterns:**
- Email addresses: `[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}`
- Phone numbers: `\d{3}[-.]?\d{3}[-.]?\d{4}`
- Social Security Numbers: `\d{3}-\d{2}-\d{4}`
- IP addresses: `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`
- API keys: `[A-Za-z0-9_-]{32,}`
- Credit cards: `\d{4}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}`

**Custom Patterns:**
- Configurable per-daemon
- Customer-specific identifiers
- Internal system references

## User Controls

### Settings Page (/daemon/settings/privacy)

**Global Toggle:**
```
✓ Contribute to fleet learning
  Help improve the platform by sharing operational patterns
```

**Category Controls:**
```
Fine-grained controls:
☑ workflow_pattern          Share helpful workflow patterns
☑ anti_pattern              Share mistakes to help others avoid them
☐ error_resolution          [You opted out]
☐ security_pattern          [You opted out - sensitive]
```

**Session Controls:**
```
□ Private session mode      Nothing shared while enabled
☑ Review before share       Approve items before sharing
```

### Inline Controls

**Quick Opt-Out Toast:**
When item is about to be shared:
```
🔄 Sharing workflow pattern to fleet
   [View] [Opt Out] [Don't show again]
```

**Contextual Prompt (First Share):**
```
🎯 You discovered a helpful pattern!

We'd like to share this with your team:
"Use jq instead of grep for JSON parsing"

[Share This Time]  [Always Share]  [Never Share]
```

## Transparency Features

### Sharing Status Indicators

**Active Indicator:**
- Badge in daemon UI: `🌐 Fleet Learning: On` (green)
- Click to quick-toggle or open settings

**Private Session Mode:**
- Banner: `🔒 Private Session - Nothing being shared`
- Different UI color scheme

**Per-Item Status:**
```
[workflow_pattern] Use batch processing for large datasets
Status: ✓ Shared with fleet on 2026-07-05
Recipients: 5 daemons
[Actions: Revoke Share | View Usage]
```

### Local Audit Log

```
Fleet Sharing Activity

2026-07-08 14:23  ✓ Shared workflow_pattern "batch processing"     → HQ
2026-07-08 13:45  ⊘ Blocked error_resolution (user opt-out)        → [blocked]
2026-07-08 12:10  ⏸ Pending repair_strategy (PII detected)         → [pending approval]

[Export Audit Log] [Privacy Settings]
```

## Integration Points

### Daemon Outbound (space_federation.go)

**Before Pushing to HQ:**
```go
func pushDerivedContentToHQ(ctx context.Context, insights []Insight) error {
    privacyFilter := getPrivacyFilter()
    
    var approvedInsights []Insight
    for _, insight := range insights {
        result, err := privacyFilter.FilterBeforeShare(ctx, insight.ToItem())
        // Handle result.Action: allow/block/pending
    }
    
    return sendToHQ(ctx, approvedInsights)
}
```

### Backend Category Auto-Share (ltm_category_auto_share.go)

**Category Toggle Integration:**
```go
func HandleSetCategoryAutoShare(w http.ResponseWriter, r *http.Request) {
    // Existing HQ-level category auto-share toggle
    // Now respects daemon-level user opt-outs
    // Filter ensures only user-approved content reaches HQ
}
```

## Configuration

### Daemon Config (daemon-config.json)

```json
{
  "privacy": {
    "share_policy": "balanced",
    "pii_detection": {
      "enabled": true,
      "block_on_detection": true,
      "custom_patterns": []
    },
    "approval_required": {
      "new_categories": true,
      "security_pattern": true,
      "error_resolution": false
    },
    "user_controls": {
      "allow_opt_out": true,
      "allow_private_sessions": true,
      "default_enabled": true
    },
    "redaction": {
      "auto_redact_emails": true,
      "auto_redact_ips": true,
      "preserve_structure": true
    }
  }
}
```

## Trust Model

### Governance Hierarchy

1. **User opt-out** (highest priority)
2. **Session privacy flag**
3. **PII detection**
4. **Category policy**
5. **Approval requirement**
6. **Auto-approve with redaction**

### Daemon-Level Responsibilities

**Local Learning (Always):**
- Learn from all local experiences
- Store in local LTM (memory_<user_id>)
- Apply learnings immediately in future sessions
- No privacy filtering needed (data never leaves daemon)

**Fleet Sharing Outbound (Optional):**
- Filter all outbound content through privacy pipeline
- Enforce user preferences (opt-out, category controls)
- Detect and block PII
- Queue items requiring approval
- Maintain local audit log

**Fleet Sharing Inbound (Optional):**
- Receive pre-vetted fleet knowledge from HQ
- Apply to local LTM for accelerated learning
- No filtering needed (already vetted at source)

### HQ Responsibilities

- Aggregate pre-vetted content from daemons
- Redistribute to other daemons (inbound fleet sharing)
- Track usage and provenance
- Handle revocation requests
- No content validation (trusts daemon filtering)

**Key Insight**: HQ only sees content that passed daemon privacy filters. Local learning happens entirely on-daemon without HQ involvement.

## Implementation Phases

### Phase 1: Core Filter (MVP)
- [ ] Implement PIIDetector with core patterns
- [ ] Create PrivacyFilter pipeline
- [ ] Add FilterBeforeShare to daemon outbound paths
- [ ] Block on PII detection (strict mode)
- [ ] Basic user opt-out storage

### Phase 2: User Controls
- [ ] Privacy settings page in backend UI
- [ ] Category preference toggles
- [ ] Private session mode
- [ ] Opt-in by default initialization
- [ ] Quick opt-out toast notifications

### Phase 3: Approval Queue
- [ ] Pending approval storage
- [ ] Approval queue UI panel
- [ ] Inline content preview
- [ ] Approve/reject actions
- [ ] Redaction editor (basic)

### Phase 4: Transparency
- [ ] Sharing status indicators in UI
- [ ] Per-item sharing badges
- [ ] Local audit log viewer
- [ ] Usage telemetry display
- [ ] Fleet contribution summary

### Phase 5: Advanced Controls
- [ ] Custom PII patterns
- [ ] Policy templates (strict/balanced/open)
- [ ] Differential privacy aggregation
- [ ] Revocation propagation
- [ ] Compliance mode

## Privacy Guarantees

### What We Guarantee

✅ **User control**: Every user can opt out globally or per-category  
✅ **PII protection**: Automatic detection and blocking of sensitive data  
✅ **Session privacy**: Private sessions never contribute to fleet  
✅ **Transparency**: Full visibility into what's shared and with whom  
✅ **Local enforcement**: Privacy filtering happens at daemon, not HQ  
✅ **Audit trail**: Immutable log of all sharing decisions  

### What We Don't Guarantee

❌ **Perfect PII detection**: Regex patterns can miss novel formats  
❌ **Semantic sensitivity**: Cannot detect "sensitive by context"  
❌ **Cross-user learning prevention**: Shared patterns may reveal team practices  
❌ **Revocation immediacy**: Revoked content may take time to propagate  

## Compliance Considerations

### GDPR Alignment

- User consent: Opt-in by default with clear controls
- Right to be forgotten: Global opt-out + revocation
- Data minimization: Only operational patterns, not personal data
- Transparency: Full audit log and sharing visibility

### HIPAA Considerations

- PHI detection patterns for healthcare deployments
- Compliance mode: manual approval for everything
- No automatic sharing in regulated environments
- Enhanced PII patterns for medical data

### SOC2 Controls

- Access control: User-level permissions
- Audit logging: Immutable sharing trail
- Data classification: Automatic sensitivity tagging
- Change management: Policy version tracking

## Testing Strategy

### Unit Tests

- PII detection accuracy
- Filter pipeline logic
- User preference enforcement
- Category allowlist validation

### Integration Tests

- Daemon outbound filtering
- Backend settings management
- Approval queue workflow
- Audit log accuracy

### Privacy Tests

- Opt-out effectiveness
- PII blocking coverage
- Private session isolation
- Revocation propagation

## Rollout Plan

### Internal Alpha (Phase 1)

- Deploy to internal test fleet
- Validate core filtering
- Collect PII detection edge cases
- Refine opt-out UX

### Internal Beta (Phase 2-3)

- Add user controls and approval queue
- Test with real operational patterns
- Measure sharing volume and quality
- Gather user feedback

### General Availability (Phase 4-5)

- Full transparency features
- Production-ready audit logging
- Documentation and training
- Monitoring and alerting

## Success Metrics

### Adoption Metrics

- % of users with fleet learning enabled (target: >85%)
- % of users who customize category preferences (target: >40%)
- Number of private sessions per user (track trend)

### Quality Metrics

- PII detection accuracy (target: >98%)
- False positive rate (target: <5%)
- Manual approval queue volume (target: <10% of content)
- User opt-out rate (target: <15%)

### Impact Metrics

- Useful patterns shared per user per week (target: >3)
- Patterns reused by other users (target: >50% reuse)
- Time to solution improvement (track before/after)

## See Also

- [LTM Category Auto-Share](LONG_TERM_MEMORY.md) - HQ-level category controls
- [Space Federation Implementation](FEDERATION.md) - Technical implementation
- [LONG_TERM_MEMORY.md](LONG_TERM_MEMORY.md) - Current memory lifecycle and category controls
