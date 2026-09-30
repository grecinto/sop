# Federated Content Strategy (Historical Snapshot)

This document formerly contained a detailed category catalogue, seed-pattern counts, rollout status, and a production-readiness claim from July 2026. Those counts and statuses are historical and should not be used to infer what is currently seeded, queued, or shared.

The current runtime state is consolidated in [FEDERATION.md](FEDERATION.md): startup seeding and local retrieval are active, the configured HQ sync loop is present, and automatic Ask-derived enqueue/promotion is disabled in the current path.

The implementation sources that define current categories and behavior are `lib/sop/cmd/sop-daemon/federation_content.go`, `lib/sop/cmd/sop-daemon/main.go`, `lib/sop/cmd/sop-daemon/rag.go`, and `lib/sop/cmd/sop-daemon/auth.go`. Recheck them and their tests before changing category policy or describing fleet learning as an automatic production feature.
