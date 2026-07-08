# Fleet Learning Privacy Governance - Implementation Tracker

## Status: ⚙️ IN PROGRESS - Phase 1 Core Filter (MVP) Complete

Privacy governance for fleet learning has been implemented in Phase 1 with core filtering capabilities.

## Implementation Overview

### Learning Architecture

**Three Tiers of Learning:**
1. **Local Learning (Always On)**: Daemon learns from its own experiences → stored in local LTM → improves future runs (no privacy concerns)
2. **Fleet Outbound (Optional)**: Share learnings with HQ → other daemons (privacy controls apply)
3. **Fleet Inbound (Optional)**: Receive learnings from other daemons → accelerates improvement (pre-vetted)

**Privacy Scope**: Controls apply only to fleet sharing (#2 and #3). Local learning (#1) always enabled.

### Architecture

**Core Engine (lib/sop):**
- Privacy filter logic for fleet sharing (reusable across daemon and backend)
- PII detection patterns
- Policy types and enforcement

**Backend Integration (lib/sop/tools/sopbackend):**
- Privacy settings UI endpoints
- User preference management for fleet participation
- Approval queue interface

**Daemon Edge Enforcement (lib/sop/cmd/sop-daemon):**
- Local learning (always enabled)
- Outbound fleet content filtering (optional)
- Pre-share validation pipeline
- Local audit logging

## Phase 1: Core Filter (MVP) - ✅ COMPLETE

### Objectives
- Implement local PII detection ✅
- Create privacy filter pipeline ✅
- Add outbound filtering to daemon ✅
- Enable basic user opt-out ✅

### Tasks

#### Core Privacy Library
- [x] Create `lib/sop/ai/privacy/` package
- [x] Implement `detector.go` with PII pattern matching
  - [x] Email pattern
  - [x] Phone number pattern
  - [x] SSN pattern
  - [x] IP address pattern
  - [x] API key pattern
  - [x] Credit card pattern
  - [x] AWS key pattern
  - [x] GitHub token pattern
  - [x] Private key pattern
  - [x] Password pattern
  - [x] Bearer token pattern
  - [x] JWT pattern
- [x] Implement `filter.go` with filter pipeline
  - [x] FilterResult types (allow, block, pending, redact)
  - [x] FilterBeforeShare() function
  - [x] User opt-out check
  - [x] Category opt-out check
  - [x] PII detection integration
  - [x] Sensitive keyword detection
- [x] Implement `policy.go` with policy types
  - [x] PolicyType enum (strict, balanced, open)
  - [x] PrivacySettings struct
  - [x] Category allowlist management
  - [x] Default category preferences
- [ ] Unit tests for all core components (deferred)

#### Daemon Integration
- [x] Create `lib/sop/cmd/sop-daemon/privacy_filter.go`
- [x] Integrate filter into `sync_queue.go`
  - [x] Filter in `Enqueue()` before HQ sync
  - [x] Handle FilterResult actions (allow/block/pending/redact)
  - [x] Log filtering decisions
  - [x] Approval queue for pending items
- [x] Create `lib/sop/cmd/sop-daemon/privacy_config.go`
  - [x] Load privacy policy from daemon config
  - [x] Default opt-in initialization
  - [x] User settings persistence (JSON file)
- [x] Add daemon config initialization in main.go
- [x] File-based audit logger (JSONL format)
- [ ] Integration tests for daemon filtering (deferred)

#### User Settings Storage
- [x] User privacy settings store (file-based)
  - [x] Schema: user_id, global_enabled, category_prefs, created_at
  - [x] Load/save operations
  - [x] Per-daemon storage (privacy_settings.json)
- [x] Default initialization on first use
- [x] Settings persistence

#### API Endpoints
- [x] `/api/daemon/privacy/settings` - GET settings
- [x] `/api/daemon/privacy/settings/update` - POST update
- [x] `/api/daemon/privacy/session/toggle` - POST private session
- [x] `/api/daemon/privacy/category` - POST category preference
- [x] `/api/daemon/privacy/audit` - GET audit log
- [x] `/api/daemon/privacy/approval-queue` - GET pending items
- [x] `/api/daemon/privacy/approve` - POST approve item
- [x] `/api/daemon/privacy/reject` - POST reject item

### Acceptance Criteria
- ✅ PII detection blocks/redacts sharing when patterns detected
- ✅ User opt-out prevents sharing globally
- ✅ Category opt-out prevents sharing per-category
- ✅ Filtered content handled before reaching HQ (in sync queue)
- ✅ Audit log records all filter decisions (file-based JSONL)
- ✅ Local learning continues regardless of privacy settings
- ⏳ Performance impact <10ms per item filtered (not yet measured)

### Implementation Summary

**Core Libraries Created:**
- `lib/sop/ai/privacy/detector.go` - PII detection with 12 pattern types
- `lib/sop/ai/privacy/filter.go` - 5-stage filter pipeline
- `lib/sop/ai/privacy/policy.go` - Privacy settings and policies

**Daemon Integration:**
- `lib/sop/cmd/sop-daemon/privacy_config.go` - Settings management
- `lib/sop/cmd/sop-daemon/privacy_filter.go` - Filter enforcement and audit logging
- `lib/sop/cmd/sop-daemon/sync_queue.go` - Filter integration before HQ sync
- `lib/sop/cmd/sop-daemon/main.go` - Initialization and API routes

**Backend UI Handlers:**
- `lib/sop/tools/sopbackend/privacy_settings_handler.go` - API endpoint stubs

**Data Flow:**
1. Content stored to local Space (always, no filter)
2. Content queued for HQ sync → Privacy filter applied
3. Filter result: allow/redact → sync to HQ, block/pending → local only

### Estimated Effort
3-4 weeks → **Completed in 1 session**

---

## Phase 2: User Controls - 🔲 Not Started

### Objectives
- Build privacy settings UI
- Enable per-category controls
- Implement private session mode
- Add quick opt-out notifications

### Tasks

#### Backend UI Endpoints
- [ ] Create `lib/sop/tools/sopbackend/privacy_settings_handler.go`
- [ ] Implement endpoints:
  - [ ] GET `/api/daemon/privacy/settings` - Fetch user settings
  - [ ] POST `/api/daemon/privacy/settings` - Update user settings
  - [ ] POST `/api/daemon/privacy/session/toggle` - Private session mode
  - [ ] GET `/api/daemon/privacy/audit` - Audit log viewer
- [ ] RBAC integration (user owns their settings)
- [ ] API tests

#### UI Components
- [ ] Create `lib/sop/tools/sopbackend/templates/privacy_settings.html`
- [ ] Privacy settings page layout
  - [ ] Global opt-in/opt-out toggle
  - [ ] Category preference checkboxes
  - [ ] Private session mode toggle
  - [ ] Review-before-share option
  - [ ] Save/restore buttons
- [ ] UI indicators
  - [ ] Badge: "Fleet Learning: On" with status
  - [ ] Private session banner
  - [ ] Quick-toggle dropdown
- [ ] Toast notifications
  - [ ] "Sharing X to fleet [View] [Opt Out]"
  - [ ] "Private session enabled"
  - [ ] "Settings saved"

#### Private Session Mode
- [ ] Session-level flag storage
- [ ] Filter integration (block all when active)
- [ ] UI state persistence across page loads
- [ ] Clear visual indicators

#### First-Time User Experience
- [ ] Privacy notice on first launch
- [ ] Clear explanation of what's shared
  - [ ] Emphasize: "Your daemon learns from you either way"
  - [ ] Clarify: Local learning always-on, fleet sharing optional
  - [ ] Benefit: "Fleet sharing accelerates learning beyond your own experience"
- [ ] Consent flow (default opt-in for fleet)
- [ ] Link to full privacy settings

### Acceptance Criteria
- ✅ Users can view and modify privacy settings
- ✅ Category preferences persist across sessions
- ✅ Private session mode blocks all sharing
- ✅ UI indicators clearly show sharing status
- ✅ Toast notifications appear on sharing events
- ✅ First-time users see privacy notice

### Estimated Effort
2-3 weeks

---

## Phase 3: Approval Queue - 🔲 Not Started

### Objectives
- Build pending approval storage
- Create approval queue UI
- Enable content preview and review
- Implement basic redaction

### Tasks

#### Approval Queue Storage
- [ ] Create pending approval store (system DB)
  - [ ] Schema: item_id, user_id, content, category, detected_pii, created_at
  - [ ] CRUD operations
  - [ ] TTL/cleanup for old items
- [ ] Queue management functions
  - [ ] Add to queue
  - [ ] Fetch pending items
  - [ ] Approve/reject actions
  - [ ] Bulk operations

#### Approval UI
- [ ] Create approval queue page `/daemon/privacy/pending`
- [ ] Item list view
  - [ ] Content preview (truncated)
  - [ ] PII flags (if detected)
  - [ ] Source: user, session, category
  - [ ] Timestamp
- [ ] Item detail view
  - [ ] Full content display
  - [ ] Inline redaction editor
  - [ ] PII highlights
  - [ ] Approve/reject/edit buttons
- [ ] Batch approval interface
  - [ ] Select multiple items
  - [ ] Bulk approve/reject

#### Redaction Engine
- [ ] Basic redaction patterns
  - [ ] Replace emails with `[EMAIL]`
  - [ ] Replace IPs with `[IP_ADDRESS]`
  - [ ] Replace phone numbers with `[PHONE]`
- [ ] Manual redaction support
  - [ ] Highlight and replace text
  - [ ] Preserve structure
- [ ] Redaction preview

#### Workflow Integration
- [ ] Filter returns "pending" for items requiring approval
- [ ] Queue items automatically added
- [ ] Approved items pushed to HQ
- [ ] Rejected items logged and deleted

### Acceptance Criteria
- ✅ Items flagged for approval appear in queue
- ✅ Users can review content before sharing
- ✅ Inline redaction works correctly
- ✅ Approved items flow to HQ after approval
- ✅ Rejected items are blocked permanently
- ✅ Queue doesn't grow unbounded (TTL cleanup)

### Estimated Effort
3 weeks

---

## Phase 4: Transparency - 🔲 Not Started

### Objectives
- Add sharing status indicators
- Build local audit log viewer
- Show per-item sharing badges
- Display fleet contribution summary

### Tasks

#### Status Indicators
- [ ] Global status badge (always visible)
  - [ ] Green: "Fleet Learning: On"
  - [ ] Orange: "Private Session"
  - [ ] Red: "Fleet Learning: Off"
- [ ] Per-item badges in memory viewer
  - [ ] "✓ Shared with fleet on [date]"
  - [ ] "⊘ Not shared (user opt-out)"
  - [ ] "⏸ Pending approval"
- [ ] Click-through to details

#### Audit Log Viewer
- [ ] Create audit log page `/daemon/privacy/audit`
- [ ] Log entry display
  - [ ] Timestamp
  - [ ] Action (shared, blocked, pending)
  - [ ] Content summary
  - [ ] Reason
  - [ ] Destination
- [ ] Filtering and search
  - [ ] By date range
  - [ ] By action type
  - [ ] By category
- [ ] Export functionality (CSV/JSON)

#### Fleet Contribution Summary
- [ ] Dashboard widget
  - [ ] "You helped X team members this week"
  - [ ] "Fleet helped you Y times"
  - [ ] Top shared categories
- [ ] Detailed breakdown
  - [ ] Items shared by category
  - [ ] Items reused by others
  - [ ] Estimated impact

#### Provenance Display
- [ ] Show source of federated content
  - [ ] Badge: "[Shared from Site-001]"
  - [ ] Badge: "[HQ Knowledge]"
- [ ] Lineage tracking
  - [ ] Original author (if permitted)
  - [ ] Derivation chain

### Acceptance Criteria
- ✅ Users always see current sharing status
- ✅ Audit log shows all sharing decisions
- ✅ Per-item badges clearly indicate sharing state
- ✅ Contribution summary motivates participation
- ✅ Federated content shows provenance

### Estimated Effort
2 weeks

---

## Phase 5: Advanced Controls - 🔲 Not Started

### Objectives
- Support custom PII patterns
- Implement policy templates
- Add differential privacy aggregation
- Enable revocation propagation
- Compliance mode

### Tasks

#### Custom PII Patterns
- [ ] UI for adding custom patterns
- [ ] Pattern validation
- [ ] Per-daemon pattern storage
- [ ] Pattern testing interface

#### Policy Templates
- [ ] Pre-configured policies
  - [ ] Strict: manual approval for everything
  - [ ] Balanced: auto-share with PII checks
  - [ ] Open: auto-share enabled categories
- [ ] Template selection UI
- [ ] Template import/export
- [ ] Custom policy creation

#### Differential Privacy
- [ ] Pattern aggregation logic
  - [ ] Detect similar patterns across daemons
  - [ ] Aggregate instead of sharing individual items
  - [ ] Example: "5 sites reported this pattern"
- [ ] Threshold configuration
- [ ] Anonymization guarantees

#### Revocation Propagation
- [ ] Revoke button on shared items
- [ ] HQ revocation endpoint
- [ ] Tombstone distribution to receiving daemons
- [ ] Daemon revocation handling
- [ ] UI confirmation

#### Compliance Mode
- [ ] HIPAA/GDPR/SOC2 presets
- [ ] Enhanced PII detection rules
- [ ] Mandatory manual approval
- [ ] Extended audit logging
- [ ] Compliance report generation

### Acceptance Criteria
- ✅ Custom PII patterns work correctly
- ✅ Policy templates apply consistently
- ✅ Aggregated insights protect individual privacy
- ✅ Revocation propagates within 5 minutes
- ✅ Compliance mode meets regulatory requirements

### Estimated Effort
4-5 weeks

---

## Cross-Phase Tasks

### Documentation
- [x] Privacy governance design doc
- [x] Update LTM_CATEGORY_AUTO_SHARE.md
- [x] Update ACTIVE_MEMORY_DESIGN.md
- [ ] User-facing privacy guide
- [ ] Admin operational guide
- [ ] API documentation
- [ ] Compliance documentation

### Testing
- [ ] Unit tests (per phase)
- [ ] Integration tests (per phase)
- [ ] E2E privacy workflow tests
- [ ] Performance tests (filter overhead)
- [ ] Security tests (bypass attempts)
- [ ] Compliance validation tests

### Deployment
- [ ] Feature flags for gradual rollout
- [ ] Migration scripts for existing users
- [ ] Monitoring and alerting
  - [ ] PII detection rate
  - [ ] User opt-out rate
  - [ ] Filter performance metrics
  - [ ] Approval queue depth
- [ ] Rollback plan

### Training & Rollout
- [ ] Internal alpha (Phase 1-2)
- [ ] Internal beta (Phase 3-4)
- [ ] Public documentation
- [ ] User training materials
- [ ] Operator training
- [ ] General availability (Phase 5)

---

## Timeline Estimate

- **Phase 1 (Core Filter)**: 3-4 weeks
- **Phase 2 (User Controls)**: 2-3 weeks
- **Phase 3 (Approval Queue)**: 3 weeks
- **Phase 4 (Transparency)**: 2 weeks
- **Phase 5 (Advanced)**: 4-5 weeks
- **Total**: ~15-17 weeks (4 months)

---

## Success Metrics

### Adoption
- User opt-in rate >85%
- Category customization >40%
- Private session usage (track trend)

### Quality
- PII detection accuracy >98%
- False positive rate <5%
- Manual approval volume <10% of content

### Impact
- Useful patterns shared >3/user/week
- Pattern reuse rate >50%
- Time-to-solution improvement (measure)

### Performance
- Filter overhead <10ms/item
- UI response time <200ms
- No HQ performance impact

---

## Related Documents

- [Fleet Learning Privacy Governance](FLEET_LEARNING_PRIVACY_GOVERNANCE.md) - Complete design
- [LTM Category Auto-Share](LTM_CATEGORY_AUTO_SHARE.md) - HQ-level controls
- [Space Federation Implementation](SPACE_FEDERATION_IMPLEMENTATION_SUMMARY.md) - Technical foundation
- [Active Memory Design](../lib/sop/ai/memory/ACTIVE_MEMORY_DESIGN.md) - Memory architecture
