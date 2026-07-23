# Space Federation API

This document describes the Space federation endpoints that enable HQ to push Spaces to daemons, and daemons to push derived content back to HQ.

## Shared Memory as a Special Federated Space

Shared memory is a special knowledge surface in the system. It is not a normal user-facing Space for everyday authoring; it is the application-wide memory lane used by daemons and HQ to store compact, privacy-safe "memory artifacts" that may later be reused, summarized, and re-shared across the fleet.

In this model:
- Daemons can promote useful observations, patterns, and derived guidance into shared memory.
- HQ can receive those artifacts, curate them, and re-share them with other daemons.
- Shared memory acts as the federated memory artifact channel for the daemon/HQ network.

Think of it as a special Space whose purpose is memory exchange rather than ordinary content authoring.

## HQ-side Endpoints

### Push Space to Daemon
**POST /api/daemon/hq-push-space**

Push a full Space from HQ to a specific daemon.

Request:
```json
{
  "space_name": "TechPatterns",
  "database_name": "system",
  "daemon_id": "daemon-001",
  "version": 1
}
```

Response:
```json
{
  "status": "delivered",
  "message": "Space TechPatterns pushed to daemon daemon-001",
  "timestamp": "2026-07-06T10:00:00Z"
}
```

### Reshare Space to Multiple Daemons
**POST /api/daemon/hq-reshare-space**

Push a Space from HQ to multiple daemons concurrently.

Request:
```json
{
  "space_name": "TechPatterns",
  "database_name": "system",
  "daemon_ids": ["daemon-001", "daemon-002", "daemon-003"],
  "version": 1
}
```

Response:
```json
{
  "status": "completed",
  "results": {
    "daemon-001": "delivered",
    "daemon-002": "delivered",
    "daemon-003": "failed: connection timeout"
  },
  "timestamp": "2026-07-06T10:00:00Z"
}
```

### Acknowledge Space Receipt
**POST /api/daemon/acknowledge-space**

Receive acknowledgment from daemon that a Space was successfully imported.

Request:
```json
{
  "space_name": "TechPatterns",
  "database_name": "system",
  "daemon_id": "daemon-001",
  "status": "acknowledged",
  "timestamp": "2026-07-06T10:00:01Z"
}
```

Response:
```json
{
  "status": "recorded",
  "timestamp": "2026-07-06T10:00:02Z"
}
```

## Daemon-side Endpoints

### Receive Space from HQ
**POST /api/daemon/receive-space**

Receive and import a Space pushed from HQ.

Request:
```json
{
  "space_name": "TechPatterns",
  "database_name": "system",
  "space_data": "{...exported Space JSON...}",
  "timestamp": "2026-07-06T10:00:00Z"
}
```

Response:
```json
{
  "status": "imported",
  "message": "Space TechPatterns imported successfully",
  "timestamp": "2026-07-06T10:00:01Z"
}
```

## Federation Flow

### HQ → Daemon Push Flow
1. HQ exports the Space to JSON
2. HQ calls daemon's `/api/daemon/receive-space` endpoint
3. Daemon imports the Space locally
4. Daemon sends acknowledgment to HQ's `/api/daemon/acknowledge-space`
5. HQ updates federation state to "acknowledged"

### Daemon Research Integration
- When daemon performs research (handleAsk), it queries:
  - Local shared_memory artifacts from the special federated memory Space
  - Federated Space content received from HQ
- Results are combined and injected into the prompt context

### Daemon → HQ Content Push Flow
1. Daemon generates useful content based on federated Space
2. Daemon pushes derived content to HQ via `/api/daemon/hq-sync`
3. HQ stores the artifact in the special shared_memory federated memory Space
4. HQ can later reshare to other daemons

### HQ Reshare Flow
1. HQ mints or receives new content
2. HQ determines target daemons (based on policy/needs)
3. HQ calls `/api/daemon/hq-push-space` for each daemon
4. Tracks delivery status via federation state
5. Retries failed deliveries

## Federation State Tracking

Each Space-daemon pair has a state:
- **pending**: HQ initiated push, waiting for delivery
- **delivered**: Space successfully sent to daemon
- **acknowledged**: Daemon confirmed import
- **failed**: Push or import failed

State includes:
- Space name and database
- Daemon ID
- Status
- Push timestamp
- Update timestamp
- Version number
- Metadata (error details, retry count, etc.)

## Environment Configuration

### HQ Configuration
```bash
# Daemon registry URLs (for push)
export SOP_DAEMON_DAEMON_001_URL=http://daemon-001.internal:9090
export SOP_DAEMON_DAEMON_002_URL=http://daemon-002.internal:9090
```

### Daemon Configuration
```bash
# HQ endpoint for acknowledgments and content push
export SOP_HQ_BASE_URL=https://hq.example.com
```

## Use Cases

### 1. Expert Knowledge Distribution
- HQ maintains curated Spaces (e.g., TechPatterns, SecurityPlaybooks)
- HQ pushes updates to all daemons
- Daemons use federated Spaces during research phase
- Users get consistent expert context across fleet

### 2. Learned Pattern Propagation
- Daemon discovers useful pattern during local work
- Daemon pushes derived content to HQ
- HQ mints into shared Space
- HQ reshares to other daemons
- Fleet learns collectively

### 3. Site-specific Customization
- HQ maintains general best practices
- Site daemon receives base Spaces
- Daemon extends with site-specific context
- Daemon pushes site-specific learnings to HQ
- HQ can selectively reshare to similar sites
