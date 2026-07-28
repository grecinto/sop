# SOP Documentation Index

Comprehensive guide to SOP platform documentation. Last updated: 2026-07-26.

---

## 🚀 Getting Started

| Document | Description | Audience |
|----------|-------------|----------|
| [DEVELOPMENT_GUIDE.md](DEVELOPMENT_GUIDE.md) | Development setup, building, testing | Developers |
| [MAINTAINER_GUIDANCE.md](MAINTAINER_GUIDANCE.md) | Public/private positioning guidance | Maintainers |

---

## 📋 Product & Vision

| Document | Description |
|----------|-------------|
| [V1_DIRECTION_AND_POST_V1_VISION.md](V1_DIRECTION_AND_POST_V1_VISION.md) | **⭐ V1 strategy and post-v1 roadmap** |
| [CAPABILITY_ARCHITECTURE_ASSESSMENT.md](CAPABILITY_ARCHITECTURE_ASSESSMENT.md) | **🏗️ Capability system architecture and extensibility guide** |
| [CAPABILITY_PLUGIN_ARCHITECTURE_ASSESSMENT.md](CAPABILITY_PLUGIN_ARCHITECTURE_ASSESSMENT.md) | **🔌 Classification → Routing → Authorization → Orchestration pipeline analysis** |
| [CAPABILITY_GAPS_IMPLEMENTATION_PLAN.md](CAPABILITY_GAPS_IMPLEMENTATION_PLAN.md) | **🔧 Implementation plan for authorization gates, classification engine, lifecycle hooks, and metrics** |
| [WORKFLOWS.md](WORKFLOWS.md) | Consolidated workflow architecture, Ask/queue evolution, lifecycle hooks, and workflow-manager direction |
| [CLI_CODING_WORKFLOW_IMPLEMENTATION.md](CLI_CODING_WORKFLOW_IMPLEMENTATION.md) | Coding workflow implementation details and current architecture direction |

## 🏗️ Core Architecture

### Authentication & Security

| Document | Description |
|----------|-------------|
| [AUTHENTICATION.md](AUTHENTICATION.md) | Unified auth system, user store pattern, token management |
| [ENTITLEMENT_ARCHITECTURE.md](ENTITLEMENT_ARCHITECTURE.md) | Subscription enforcement and access control |
| [PAYMENTS.md](PAYMENTS.md) | Subscription and payment architecture |
| [SECURE_INPUT_HANDLING.md](SECURE_INPUT_HANDLING.md) | **🔒 Privacy-first secure input (passwords, tokens) - bypasses LLM, direct-to-tool routing** |

### Features

| Document | Description |
|----------|-------------|
| [AI_STUDIO.md](AI_STUDIO.md) | Local daemon integration, daemon mode authentication |
| [SPACES.md](SPACES.md) | Knowledge base spaces, vector-backed semantic memory, blob-to-space digest, expert implementation, and the current grounded model of Spaces / KBs as expertise sources with optional persona and tone guidance |
| [APPROVALS.md](APPROVALS.md) | Session-based auto-approval system |
| [TERMINAL_LAUNCH_FEATURE.md](TERMINAL_LAUNCH_FEATURE.md) | Terminal launch modes and execution |

---

## 🧠 AI & Intelligence

### Agent Architecture & Tooling

| Document | Description |
|----------|-------------|
| [FILE_BACKED_TOOL_RESULTS.md](FILE_BACKED_TOOL_RESULTS.md) | ✅ Production-ready: File-backed tool results for large outputs, strategic analysis tools |
| [SHELL_SESSION_PERSISTENCE.md](SHELL_SESSION_PERSISTENCE.md) | Persistent shell sessions for commands |
| [PHASE4_PARALLEL_EXECUTION.md](PHASE4_PARALLEL_EXECUTION.md) | Phase 4 parallel execution architecture, thread routing, PTY vs exec.Command |

### Knowledge Base Quality

| Document | Description |
|----------|-------------|
| [KB_QUALITY.md](KB_QUALITY.md) | Automated expertise enforcement, validation rules, task classification |
| [KB_QUALITY_DASHBOARD.md](KB_QUALITY_DASHBOARD.md) | Real-time expertise dashboard implementation |
| [OBSERVABILITY_KB_QUALITY.md](OBSERVABILITY_KB_QUALITY.md) | Prometheus metrics and Grafana dashboards for KB quality |
| [GO_BEST_PRACTICES_KB.md](GO_BEST_PRACTICES_KB.md) | Go coding standards knowledge base |

### Federation & Collaboration

| Document | Description |
|----------|-------------|
| [FEDERATION.md](FEDERATION.md) | Central memory federation, poll+stream architecture |
| [FEDERATED_CONTENT_STRATEGY.md](FEDERATED_CONTENT_STRATEGY.md) | Content federation strategy |
| [FLEET_LEARNING.md](FLEET_LEARNING.md) | Privacy-preserving fleet learning and governance |

### Memory & Context

| Document | Description |
|----------|-------------|
| [LONG_TERM_MEMORY.md](LONG_TERM_MEMORY.md) | Hybrid memory system, category auto-share |
| [STREAMING_DOCUMENT_STORAGE.md](STREAMING_DOCUMENT_STORAGE.md) | Vector/tensor-native document storage for space items, first-class Space UI surfaces, DocID-backed references, and document-card/detail-view browsing |

---

## 🛠️ Implementation Guides

### Workflows

| Document | Description |
|----------|-------------|
| [WORKFLOWS.md](WORKFLOWS.md) | ⭐ **Complete Guide**: Workflow architecture, lifecycle, implementation status, tool specifications, and best practices. Covers KB-based storage, multi-step orchestration, and automatic ingestion. |
| [../lib/sop/ai/memory/CATEGORY_HIERARCHY_INTEGRITY.md](../lib/sop/ai/memory/CATEGORY_HIERARCHY_INTEGRITY.md) | ⚠️ **Critical**: Semantic Path Search requires complete category hierarchies. Documents EnsureCategory fix for workflow category storage. |
| [CLI_CODING_WORKFLOW_IMPLEMENTATION.md](CLI_CODING_WORKFLOW_IMPLEMENTATION.md) | Terminal-first coding workflow implementation |

### Platform

| Document | Description |
|----------|-------------|
| [DESKTOP_APP.md](DESKTOP_APP.md) | Desktop application architecture |
| [LOCAL_RAG_APP_STORE_CONCEPT.md](LOCAL_RAG_APP_STORE_CONCEPT.md) | Local RAG app store concept |
| [CLOUDFLARE_PRIVATE_DOWNLOADS.md](CLOUDFLARE_PRIVATE_DOWNLOADS.md) | Private download infrastructure |

---

## 📊 Operations

### Observability

| Document | Description |
|----------|-------------|
| [OBSERVABILITY_PLAN.md](OBSERVABILITY_PLAN.md) | General observability strategy (Prometheus, Grafana) |
| [OBSERVABILITY_KB_QUALITY.md](OBSERVABILITY_KB_QUALITY.md) | KB quality-specific metrics and dashboards |

---

## 💼 Business

### Sales & Marketing

| Document | Description |
|----------|-------------|
| [NORTHSTAR_SDK_PRICING.md](NORTHSTAR_SDK_PRICING.md) | SDK pricing strategy |
| [NORTHSTAR_SDK_SALES_GUIDE.md](NORTHSTAR_SDK_SALES_GUIDE.md) | SDK sales guide |
| [MARKETING_KB_QUALITY_UPDATES.md](MARKETING_KB_QUALITY_UPDATES.md) | Marketing materials for KB quality features |

---

## 📋 Documentation Organization

### Consolidation Summary (2026-07-15)

**Before**: 60+ fragmented documents  
**After**: 26 comprehensive documents

### Major Consolidations

1. **AI Studio** (4 → 1): Merged bug fixes, quick refs, and specs into single comprehensive guide
2. **Authentication** (6 → 1): Unified all auth docs including architecture, fixes, and design
3. **KB Quality** (12 → 2): Consolidated features, trackers, validators into main docs + observability
4. **Federation** (4 → 1): Merged design, implementation, and throttling docs
5. **Spaces** (4 → 1): Combined implementation guide, user guide, and test plans
6. **Fleet Learning** (2 → 1): Merged governance and implementation tracker
7. **Long-Term Memory** (2 → 1): Combined category auto-share and hybrid memory plans
8. **Approvals** (2 → 1): Merged feature enhancement and quick reference

### Removed Obsolete Docs

- Implementation trackers for completed features
- Completed fix action plans and summaries
- Obsolete architectural assessments
- Redundant quick reference guides
- Obsolete design proposals (categorizer v1, central brain, etc.)
- Site implementation plans (site is now live)

---

## 🔍 Quick Links by Role

### For Developers

**Getting Started**:
1. [DEVELOPMENT_GUIDE.md](DEVELOPMENT_GUIDE.md) - Setup and build
2. [AUTHENTICATION.md](AUTHENTICATION.md) - Auth system
3. [AI_STUDIO.md](AI_STUDIO.md) - Local daemon mode

**Feature Implementation**:
- [SPACES.md](SPACES.md) - Knowledge base implementation
- [KB_QUALITY.md](KB_QUALITY.md) - Quality validation system
- [TERMINAL_LAUNCH_FEATURE.md](TERMINAL_LAUNCH_FEATURE.md) - Terminal integration

**Observability**:
- [OBSERVABILITY_PLAN.md](OBSERVABILITY_PLAN.md) - Metrics and monitoring
- [OBSERVABILITY_KB_QUALITY.md](OBSERVABILITY_KB_QUALITY.md) - KB-specific metrics

### For Product Managers

**Feature Specs**:
- [AI_STUDIO.md](AI_STUDIO.md) - Local daemon integration
- [KB_QUALITY.md](KB_QUALITY.md) - Automated expertise enforcement
- [APPROVALS.md](APPROVALS.md) - Auto-approval workflows
- [FEDERATION.md](FEDERATION.md) - Memory federation

**Architecture**:
- [AUTHENTICATION.md](AUTHENTICATION.md) - Auth & security
- [FLEET_LEARNING.md](FLEET_LEARNING.md) - Privacy-preserving learning
- [LONG_TERM_MEMORY.md](LONG_TERM_MEMORY.md) - Memory systems

### For Sales & Marketing

**Sales Materials**:
- [NORTHSTAR_SDK_SALES_GUIDE.md](NORTHSTAR_SDK_SALES_GUIDE.md) - SDK sales guide
- [NORTHSTAR_SDK_PRICING.md](NORTHSTAR_SDK_PRICING.md) - Pricing strategy
- [MARKETING_KB_QUALITY_UPDATES.md](MARKETING_KB_QUALITY_UPDATES.md) - KB quality marketing

**Product Benefits**:
- [KB_QUALITY.md](KB_QUALITY.md) - Cost savings from automated validation
- [FLEET_LEARNING.md](FLEET_LEARNING.md) - Privacy-first approach

### For Operations

**Monitoring**:
- [OBSERVABILITY_PLAN.md](OBSERVABILITY_PLAN.md) - Metrics architecture
- [OBSERVABILITY_KB_QUALITY.md](OBSERVABILITY_KB_QUALITY.md) - KB quality dashboards

**Infrastructure**:
- [CLOUDFLARE_PRIVATE_DOWNLOADS.md](CLOUDFLARE_PRIVATE_DOWNLOADS.md) - Download infrastructure
- [PAYMENTS.md](PAYMENTS.md) - Payment processing

---

## 📝 Contributing to Documentation

### Guidelines

1. **Update existing docs** rather than creating new ones
2. **Use clear headings** and table of contents
3. **Include examples** and code snippets
4. **Add diagrams** for complex flows
5. **Update this index** when adding new docs

### When to Create New Docs

Only create a new document if:
- ✅ It covers a distinctly new feature area
- ✅ It's >500 lines and merging would be unwieldy
- ✅ It has a different target audience than existing docs

Otherwise, extend an existing document.

### Naming Conventions

**Documentation Files:**
- **Feature guides**: `FEATURE_NAME.md` (e.g., `AI_STUDIO.md`)
- **Architecture**: `ARCHITECTURE_AREA.md` (e.g., `AUTHENTICATION.md`)
- **Observability**: `OBSERVABILITY_SCOPE.md` (e.g., `OBSERVABILITY_KB_QUALITY.md`)
- **Business**: Descriptive names (e.g., `NORTHSTAR_SDK_PRICING.md`)

**Spaces and Stores:**
- User input with spaces is automatically normalized: `"My Space"` → stored as `"my_space"` → displayed as `"My Space"`
- Only alphanumeric, underscore, and hyphen allowed in storage: `[a-zA-Z0-9_-]`
- See [SPACES.md](SPACES.md#space-and-store-naming-conventions) for details

---

## 🔄 Recent Changes

### 2026-07-15: Major Documentation Consolidation

**Consolidated**:
- 60+ fragmented docs → 26 comprehensive docs
- Removed 34+ obsolete implementation trackers, assessments, and plans
- Created this master index

**New Organization**:
- Clear separation by role (Developer, Product, Sales, Ops)
- Comprehensive feature guides instead of fragmented specs
- Merged related concepts into single docs

**Benefits**:
- Easier to find information
- No duplicate/conflicting information
- Clear ownership and maintenance
- Better onboarding experience

---

*This index is maintained by the development team. Last major update: 2026-07-15*
