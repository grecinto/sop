# Space Federation - Operator Quick Reference

## Overview
Space Federation enables HQ to distribute expert knowledge Spaces to field daemons, and field daemons to contribute learned insights back to HQ.

## Shared Memory Concept

Think of shared memory as a special Space for daemon/HQ memory artifact sharing. It is the system-wide memory surface where compact lessons, patterns, and derived guidance can be accumulated over time and then re-shared across the fleet.

This is different from a normal authoring Space:
- Its purpose is memory propagation, not day-to-day content editing.
- Daemons write to it when they discover useful patterns during local work.
- HQ can ingest those artifacts, curate them, and redistribute them to other daemons.

## Quick Start

### HQ: Push Space to Single Daemon
```bash
curl -X POST http://hq.example.com/api/daemon/hq-push-space \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "TechPatterns",
    "database_name": "system",
    "daemon_id": "site-001",
    "version": 1
  }'
```

### HQ: Reshare Space to Multiple Daemons
```bash
curl -X POST http://hq.example.com/api/daemon/hq-reshare-space \
  -H "Content-Type: application/json" \
  -d '{
    "space_name": "TechPatterns",
    "database_name": "system",
    "daemon_ids": ["site-001", "site-002", "site-003"],
    "version": 1
  }'
```

### Check Federation Status
Query HQ logs or implement a status endpoint to check federation state:
- **pending**: Push initiated, waiting for delivery
- **delivered**: Successfully sent to daemon
- **acknowledged**: Daemon confirmed import
- **failed**: Push or import failed (check logs)

## Configuration

### HQ Setup
Add daemon URLs to HQ environment:
```bash
export SOP_DAEMON_SITE_001_URL=http://site-001.internal:9090
export SOP_DAEMON_SITE_002_URL=http://site-002.internal:9090
export SOP_DAEMON_SITE_003_URL=http://site-003.internal:9090
```

### Daemon Setup
Point daemons to HQ:
```bash
export SOP_HQ_BASE_URL=https://hq.example.com
```

## Common Operations

### Distribute New Space to Fleet
1. Create/curate Space at HQ
2. Verify Space content is complete
3. Use reshare endpoint to push to all daemons
4. Monitor logs for acknowledgments
5. Verify daemons can query the Space

### Update Existing Space
1. Modify Space content at HQ
2. Increment version number
3. Reshare with new version to daemons
4. Daemons will re-import (overwrites previous)

### Verify Daemon Received Space
SSH to daemon and check:
```bash
# Query daemon database
curl -X GET http://localhost:9090/api/spaces?database=system
```

Look for the pushed Space in the list.

### Check What Spaces Daemon Has
Query federated Spaces tracking (requires adding endpoint):
```go
// In daemon code
spaces := GetFederatedSpaces()
for _, space := range spaces {
    fmt.Printf("Space: %s, ReceivedAt: %s\n", space.SpaceName, space.ReceivedAt)
}
```

## Troubleshooting

### Space Push Fails
**Symptom**: HQ logs show "failed to push to daemon"

**Common Causes**:
- Daemon is offline or unreachable
- Network firewall blocking HQ→daemon traffic
- Wrong daemon URL in environment variable
- Daemon receive endpoint not configured

**Resolution**:
1. Verify daemon is running: `curl http://daemon:9090/health`
2. Check network connectivity from HQ to daemon
3. Verify SOP_DAEMON_*_URL is correct
4. Check daemon logs for errors
5. Retry push after fixing issue

### Daemon Doesn't Acknowledge
**Symptom**: Federation state stuck in "delivered", no "acknowledged"

**Common Causes**:
- Daemon couldn't import Space (corrupted data, DB error)
- Daemon can't reach HQ for acknowledgment
- HQ acknowledgment endpoint not configured

**Resolution**:
1. Check daemon logs for import errors
2. Verify daemon has SOP_HQ_BASE_URL set
3. Test daemon→HQ connectivity
4. Check HQ logs for acknowledgment arrival
5. May need to re-push Space

### Research Doesn't Include Federated Space
**Symptom**: Daemon answers don't reflect pushed Space content

**Common Causes**:
- Space import failed silently
- Research phase not calling retrieveFederatedSpaceContext
- Space content doesn't match query semantics

**Resolution**:
1. Verify Space is in daemon database
2. Check daemon logs during ask flow
3. Test direct query: search Space manually
4. Verify Space has relevant content for query
5. Check embedding/vector search configuration

### Derived Content Not Reaching HQ
**Symptom**: Daemon generates insights but HQ doesn't receive them

**Common Causes**:
- Daemon push logic not triggered
- Network issue daemon→HQ
- HQ not storing received content

**Resolution**:
1. Check daemon logs for "daemon push" entries
2. Verify SOP_HQ_BASE_URL is correct
3. Test daemon→HQ connectivity
4. Check HQ hq-sync handler logs
5. Query HQ shared_memory for received content

## Monitoring

### Key Metrics to Track
- Space push success rate (target: >95%)
- Acknowledgment latency (target: <5 seconds)
- Federation state distribution (pending/delivered/acknowledged/failed)
- Space size over time (watch for growth)
- Derived content push rate from daemons

### Log Patterns to Watch

**HQ Logs**:
```
INFO  HQ push space: received request space_name=X daemon_id=Y
INFO  HQ push space: successfully pushed space_name=X daemon_id=Y
ERROR HQ push space: failed to push to daemon error=...
INFO  HQ acknowledge: received acknowledgment space_name=X daemon_id=Y
```

**Daemon Logs**:
```
INFO  daemon receive space: received Space from HQ space_name=X
INFO  daemon receive space: successfully imported space_name=X
INFO  daemon acknowledge: sent acknowledgment to HQ space_name=X
INFO  daemon federated retrieval: querying Space space_name=X
INFO  daemon push content: sent Space-derived content to HQ space_name=X
```

## Best Practices

### Space Design
- Keep Spaces focused on specific domains (e.g., "NetworkTroubleshooting", "SecurityPlaybooks")
- Use clear, searchable text in Space items
- Include category metadata for better routing
- Version Spaces when making significant updates
- Document Space purpose and expected use cases

### Push Strategy
- Start with pilot daemon before fleet-wide push
- Push during low-traffic periods
- Monitor first few acknowledgments before continuing
- Have rollback plan (previous Space version)
- Document what changed in version notes

### Fleet Management
- Maintain inventory of which daemons have which Spaces
- Track Space versions across fleet
- Schedule regular Space updates
- Remove obsolete Spaces from fleet
- Monitor daemon storage growth

### Security
- Ensure daemons authenticate to HQ (add auth tokens)
- Use TLS for HQ↔daemon communication
- Audit Space content before pushing
- Restrict Space push to authorized operators
- Log all federation operations

## Emergency Procedures

### Rollback Bad Space Push
1. Stop any in-progress reshare operations
2. Identify daemons that received bad Space
3. Push previous good version with rollback version number
4. Verify daemons import corrected Space
5. Document incident and root cause

### Daemon Fleet Out of Sync
1. Query each daemon for Space list and versions
2. Compare against HQ source of truth
3. Identify daemons with missing/stale Spaces
4. Batch-push correct Spaces to out-of-sync daemons
5. Verify all daemons reach consistent state

### HQ-Daemon Communication Failure
1. Verify network connectivity
2. Check firewall rules
3. Verify DNS resolution
4. Test with manual curl commands
5. Check for certificate expiration (if using TLS)
6. Review recent infrastructure changes

## Operational Checklist

### Pre-Deployment
- [ ] Configure SOP_DAEMON_*_URL for all daemons
- [ ] Configure SOP_HQ_BASE_URL on all daemons
- [ ] Test push to single daemon
- [ ] Verify daemon can acknowledge
- [ ] Test research includes federated content
- [ ] Set up monitoring/alerting

### Weekly
- [ ] Review federation state distribution
- [ ] Check for stuck "pending" or "failed" states
- [ ] Verify derived content flowing from daemons
- [ ] Monitor Space size growth
- [ ] Review error logs

### Per Space Update
- [ ] Document changes in version notes
- [ ] Test Space in staging environment
- [ ] Push to pilot daemon first
- [ ] Monitor acknowledgments
- [ ] Verify research queries work correctly
- [ ] Roll out to production fleet

### Monthly
- [ ] Audit Space inventory across fleet
- [ ] Review Space usage metrics
- [ ] Identify unused/stale Spaces
- [ ] Clean up obsolete Spaces
- [ ] Update documentation

## Support Contacts

For issues with Space Federation:
1. Check this guide first
2. Review logs (HQ and daemon)
3. Test with manual curl commands
4. Consult SPACE_FEDERATION_API.md for detailed API docs
5. Escalate to engineering if unresolved

## Additional Resources

- [SPACE_FEDERATION_API.md](SPACE_FEDERATION_API.md) - Complete API reference
- [SPACE_FEDERATION_IMPLEMENTATION_SUMMARY.md](SPACE_FEDERATION_IMPLEMENTATION_SUMMARY.md) - Technical details
- [OPERATIONS.md](OPERATIONS.md) - General operational procedures
- [ARCHITECTURE.md](ARCHITECTURE.md) - System architecture overview
