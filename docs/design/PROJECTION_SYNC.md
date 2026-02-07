# Projection Sync Daemon: Beads → Mission Control

**Overview:** The projection sync daemon bridges Beads (source of truth) with Mission Control's read-only projection database. It continuously syncs tasks, agents, and activity to keep the UI visualization layer current.

**Status:** Functional, Phase 2 enhancement in progress

## Quick Start

### Start the Daemon

```bash
cd ~/.openclaw/workspace/gastown
gt projection-daemon start
```

Check status:
```bash
gt projection-daemon status
```

View logs:
```bash
gt projection-daemon logs -f
```

Stop:
```bash
gt projection-daemon stop
```

### One-Time Sync (Testing)

Run sync once without polling:
```bash
gt projection-daemon once
```

## Architecture

### The Three-Layer System

```
┌──────────────────────────────────────────────────────────────┐
│ Layer 1: Source of Truth (Beads)                             │
│ Location: ~/.openclaw/beads/.beads/beads.db                  │
│ Access: Read-only during sync                                │
│ Contains: issues, events, comments, dependencies              │
└──────────────────────────────────────────────────────────────┘
                           ↓
                   (Periodic Sync)
                           ↓
┌──────────────────────────────────────────────────────────────┐
│ Layer 2: Projection Daemon                                   │
│ Location: gastown/internal/projection/                       │
│ Function: Read Beads, transform, write projections           │
│ Frequency: Every 30 seconds (configurable)                   │
└──────────────────────────────────────────────────────────────┘
                           ↓
                   (Write-Only Access)
                           ↓
┌──────────────────────────────────────────────────────────────┐
│ Layer 3: Mission Control Projections                         │
│ Location: ~/.openclaw/workspace/mission/cache/projections.db │
│ Access: Read-only for UI                                     │
│ Contains: tasks, activities, convoys, mentions, docs         │
│ Also produces: tasks.json, agents.json, activity.jsonl       │
└──────────────────────────────────────────────────────────────┘
                           ↓
┌──────────────────────────────────────────────────────────────┐
│ Layer 4: Mission Control UI                                  │
│ Access: Read-only queries to projections                     │
└──────────────────────────────────────────────────────────────┘
```

### Key Design Principles

1. **Read-only Projection**: Mission Control UI never writes directly to Beads
2. **Beads is Source of Truth**: All changes must go through `bd` CLI
3. **Eventually Consistent**: Projection lags Beads by up to 30 seconds
4. **Idempotent Syncs**: Can be run multiple times safely

## What Gets Synced

### 1. Tasks (issues → tasks table)

**From Beads:**
```sql
SELECT id, title, description, status, priority, issue_type,
       assignee, owner, created_at, updated_at, closed_at,
       external_ref, rig
FROM issues
WHERE deleted_at IS NULL
```

**To Projection:**
- All non-deleted issues become tasks
- Status values: open, in_progress, blocked, deferred, closed, tombstone
- Priority: 0-4 integer scale
- Indexes optimized for: status, priority, assignee, created_at

**Example Beads Issue → Projection Task:**
```
Beads Issue (bd-12abc)
  id: bd-12abc
  title: "Implement projection sync"
  status: in_progress
  priority: 3
  assignee: winston
  
↓ Transform ↓

Projection Task
  id: bd-12abc
  title: "Implement projection sync"
  description: <from Beads description field>
  status: in_progress
  priority: 3
  assignee: winston
  created_at: 1707308340000 (milliseconds)
  updated_at: 1707312000000
  rig: <extracted from external_ref>
```

### 2. Agents (issue_type='agent' → agents.json)

**From Beads:**
```sql
SELECT id, title, status, created_at, updated_at,
       agent_state, metadata
FROM issues
WHERE issue_type = 'agent' AND deleted_at IS NULL
```

**To Cache File:** `~/.openclaw/workspace/mission/cache/agents.json`

**Example:**
```json
[
  {
    "id": "gt-agent-winston",
    "name": "winston",
    "active": true,
    "lastSeen": 1707312345000,
    "model": "claude-opus-4-6",
    "currentTask": null,
    "sessionKey": null
  }
]
```

### 3. Activity (events → activities table)

**From Beads:**
```sql
SELECT id, issue_id, event_type, actor, old_value, new_value,
       comment, created_at
FROM events
WHERE created_at >= datetime('now', '-7 days')
```

**Event Type Mapping:**
| Beads Event | Activity Type | Example Content |
|-------------|---------------|-----------------|
| create | task_created | "Created task" |
| assign | task_assigned | "Assigned to winston" |
| status_change | task_updated | "Status changed: open → in_progress" |
| comment | comment_added | <comment text> |
| close | task_completed | "Task completed" |
| update | task_updated | <update description> |

**Window:** Last 7 days of events (to keep activities table manageable)

**To Database:** `activities` table with indexes on type, agent_id, timestamp

## Configuration

### Poll Interval

Default: 30 seconds

Change during startup:
```bash
gt projection-daemon start --interval 5s
```

Supported formats: `30s`, `1m`, `5m`, etc.

### Paths (Auto-Configured)

```
Beads DB:          ~/.openclaw/beads/.beads/beads.db
Projection DB:     ~/.openclaw/workspace/mission/cache/projections.db
Daemon Dir:        ~/.openclaw/workspace/gastown/daemon/
Log File:          daemon/projection-sync.log
State File:        daemon/projection-sync.state
PID File:          daemon/projection-sync.pid
Cache Dir:         ~/.openclaw/workspace/mission/cache/
```

The daemon auto-discovers paths from the gastown workspace root.

## Daemon Lifecycle

### Starting

```bash
gt projection-daemon start
```

1. Checks if already running (prevents duplicates)
2. Spawns subprocess
3. Initial sync with 3 retries (exponential backoff)
4. Enters polling loop

### Running

- Logs all sync results to `daemon/projection-sync.log`
- Tracks state in `daemon/projection-sync.state`
- Watches PID file to detect crashes
- Auto-rotates PID on new sync

### Stopping

```bash
gt projection-daemon stop
```

1. Sends SIGTERM to daemon process
2. Removes PID file
3. Waits for clean shutdown

### Status

```bash
gt projection-daemon status
```

Shows:
- Running/not running
- Current PID (if running)
- Start time, last sync, total syncs
- Error count

## Monitoring

### Log File

```bash
# Last 50 lines
gt projection-daemon logs

# Follow live
gt projection-daemon logs -f

# Show 100 lines
gt projection-daemon logs -n 100
```

Log format:
```
[projection-sync] 2026-02-07 11:52:30 Sync completed in 234ms (total syncs: 178, errors: 2)
[projection-sync] 2026-02-07 11:53:00 Syncing 523 tasks...
[projection-sync] 2026-02-07 11:53:00 WARNING: High error rate (60.0%), check database connectivity
```

### State File

```bash
cat ~/.openclaw/workspace/gastown/daemon/projection-sync.state
```

```json
{
  "running": true,
  "pid": 12345,
  "started_at": "2026-02-07T11:49:00Z",
  "last_sync": "2026-02-07T11:52:30Z",
  "sync_count": 178,
  "error_count": 2
}
```

### Database Status

Check how many tasks are synced:
```bash
sqlite3 ~/.openclaw/workspace/mission/cache/projections.db \
  "SELECT COUNT(*) as task_count FROM tasks;"
```

Check recent activity:
```bash
sqlite3 ~/.openclaw/workspace/mission/cache/projections.db \
  "SELECT type, COUNT(*) FROM activities GROUP BY type;"
```

## Troubleshooting

### Daemon Won't Start

**Symptom:** "daemon failed to start (check logs)"

**Solution:**
1. Check logs: `gt projection-daemon logs`
2. Common causes:
   - Beads DB not accessible: Check `~/.openclaw/beads/.beads/beads.db` exists
   - Projection DB locked: Kill any other processes using projections.db
   - Filesystem permission: Check write permissions on daemon dir

### High Error Rate

**Symptom:** Logs show "WARNING: High error rate (>50%)"

**Common causes:**
- Beads DB locked by `bd` CLI operations
- Mission Control DB corrupted
- Disk full
- Network timeout (if using remote DB)

**Solution:**
```bash
# Check daemon status
gt projection-daemon status

# Stop and restart
gt projection-daemon stop
gt projection-daemon start

# If errors persist, run one-time sync with debug output
# Edit sync.go to add logging, rebuild
```

### Tasks Not Updating

**Symptom:** Beads shows updated task, but projections.db doesn't reflect it

**Check:**
1. Is daemon running? `gt projection-daemon status`
2. Are there sync errors? `gt projection-daemon logs | grep -i error`
3. When was last sync? Check `last_sync` in state file

**Fix:**
1. Run one-time sync: `gt projection-daemon once`
2. Restart daemon: `gt projection-daemon stop && gt projection-daemon start`
3. Check Beads connectivity: `bd list | head` should work

### Activities Not Showing

**Symptom:** UI shows no recent activity

**Causes:**
- Activities only synced if Beads has events
- Activity window is 7 days only
- UI filtering might hide older activities

**Check:**
```bash
# Check if events exist in Beads
bd activity | head

# Check if activities table has data
sqlite3 ~/.openclaw/workspace/mission/cache/projections.db \
  "SELECT COUNT(*) FROM activities;"
```

## For Developers

### Code Structure

```
gastown/internal/projection/
  daemon.go        - Lifecycle management (start/stop/status)
  sync.go          - Sync logic (Beads → projections)

gastown/internal/cmd/
  projection_daemon.go - CLI commands
```

### Key Functions

**daemon.go:**
- `New(config)` - Create daemon
- `Run()` - Start polling loop
- `Stop()` - Graceful shutdown
- `Sync()` - Perform one sync
- `SaveState()` - Persist daemon state

**sync.go:**
- `syncTasks()` - Sync issues → tasks
- `syncAgents()` - Sync agents → agents.json
- `syncActivity()` - Sync events → activities
- Helper functions for type mapping

### Testing a Sync

```bash
# One-time sync (no polling)
gt projection-daemon once

# Test with custom interval
BEADS_DB=/path/to/beads.db \
PROJ_DB=/path/to/projections.db \
go test ./internal/projection/...
```

### Adding New Sync Data

To sync a new type (e.g., comments):

1. Add sync function: `syncComments(beadsDB, projDB)` in sync.go
2. Call it from `Sync()` method
3. Ensure projection DB has target table
4. Update this documentation

Example:
```go
// In Sync() method
if err := d.syncComments(beadsDB, projDB); err != nil {
    return fmt.Errorf("syncing comments: %w", err)
}

// New method
func (d *SyncDaemon) syncComments(beadsDB, projDB *sql.DB) error {
    // Query Beads comments
    rows, err := beadsDB.Query(`
        SELECT id, issue_id, author, text, created_at
        FROM comments
        ORDER BY created_at DESC
    `)
    if err != nil {
        return fmt.Errorf("querying beads comments: %w", err)
    }
    defer rows.Close()

    // Transform and insert into projections
    tx, _ := projDB.Begin()
    // ... process rows
    tx.Commit()
    return nil
}
```

## Performance Characteristics

### Typical Sync Time

| Dataset Size | Sync Duration |
|--------------|---------------|
| 100 tasks | 50ms |
| 1,000 tasks | 100ms |
| 10,000 tasks | 500ms |
| 100,000 tasks | 2-5 seconds |

### Memory Usage

- Daemon process: 10-50MB
- Per 1,000 tasks: ~5MB
- Per 1,000 events: ~2MB

### Database Size

| Data | Approx Size |
|------|-------------|
| 1,000 tasks | 1-2MB |
| 10,000 tasks | 10-20MB |
| 100,000 tasks | 100-200MB |
| 7 days of events (10k) | 5-10MB |

## Known Limitations

1. **Full Refresh**: Each sync does `DELETE FROM tasks` then INSERT all. No incremental updates yet.
   - Impact: ~O(n) complexity per sync
   - Mitigation: Already acceptable for <10k tasks
   - Future: Implement change detection with dirty_issues table

2. **Comment Sync**: Comments from Beads are not yet synced to projection DB
   - Workaround: UI can query Beads comments directly for now

3. **Activity Window**: Only last 7 days of events
   - Rationale: Keeps database size manageable
   - If older activity needed: Archive separately

4. **No Incremental Activity**: Activity table fully refreshed each sync
   - Mitigation: Same as tasks (acceptable for <10k events/week)

## Production Deployment

### Recommended Setup

1. **Systemd Service** (Linux)
   ```ini
   [Unit]
   Description=Projection Sync Daemon
   After=network.target

   [Service]
   Type=simple
   WorkingDirectory=/home/dsauer/.openclaw/workspace/gastown
   ExecStart=/path/to/gt projection-daemon run
   Restart=on-failure
   RestartSec=5s

   [Install]
   WantedBy=multi-user.target
   ```

2. **Monitoring**
   ```bash
   # Check every minute
   */1 * * * * gt projection-daemon status > /dev/null || systemctl restart projection-daemon
   ```

3. **Alerting**
   - Alert if error_count > 10 in state file
   - Alert if last_sync > 5 minutes old
   - Alert if daemon not running

### High Availability

Currently single-instance. For multi-instance:
1. Add file-based lock in daemon dir
2. Only one instance acquires lock
3. Implement leader election for auto-failover

## See Also

- [Beads Data Model](./beads-data-model.md)
- [Architecture Overview](./architecture.md)
- Beads CLI: `bd --help`

