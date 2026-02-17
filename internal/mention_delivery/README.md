# Mention Delivery Worker

## Overview

The Mention Delivery Worker is a background service that monitors the Beads event stream for @mentions and subscription matches, then delivers notifications to subscribers via the OpenClaw message CLI.

## What It Does

The worker performs three main functions:

1. **Mention Detection**: Scans comments and descriptions in Beads events for `@username` mentions
2. **Subscription Matching**: Matches events against user subscriptions (beads, labels, assignees)
3. **Notification Delivery**: Sends notifications to subscribers via OpenClaw's message system

## Architecture

```
┌──────────────┐
│  Beads DB    │  (SQLite)
│  events      │  
│  table       │  
└──────┬───────┘
       │
       │ Poll every 10s
       │
┌──────▼──────────────┐
│ Mention Worker      │
│                     │
│ 1. Fetch new events │
│ 2. Parse @mentions  │
│ 3. Match subscriptions │
│ 4. Build messages   │
└──────┬──────────────┘
       │
       │ Send notifications
       │
┌──────▼──────────────┐
│ OpenClaw CLI        │
│ message send        │
│ --channel X         │
│ --target Y          │
└─────────────────────┘
```

## Current Status

### ✅ Implemented (POC Complete)

- [x] Database connection to beads.db (events & subscriptions tables)
- [x] Event polling loop (10s interval, incremental via lastEventID)
- [x] @mention detection & parsing in comments (`@username`)
- [x] Bead-level subscription matching (sub_type='bead')
- [x] Delivery prefs lookup per subscriber
- [x] Actual OpenClaw `message send` execution (not stub)
- [x] Graceful shutdown (SIGINT/SIGTERM via context)
- [x] Basic error logging

### ⚠️ Partially Implemented

- [ ] **Subscription matching** - TODO: Query subscriptions table
- [ ] **Channel/target lookup** - Currently hardcoded for demo
- [ ] **Error handling** - Missing retry logic and failure handling
- [ ] **Graceful shutdown** - No signal handling for clean exit

### Next Steps (Production Polish)

**High Priority:**
- [ ] bd CLI: `bd subscribe/unsubscribe bead/label/convoy <id> [channel target]`
- [ ] Expand subscription matching: labels, convoys, assignees
- [ ] Integrate into Gas Town daemon (gt mention-worker start/stop)
- [ ] Add unit/integration tests
- [ ] Retry logic & exponential backoff for failed deliveries
- [ ] Rate limiting & deduplication
- [ ] Config from ENV (BEADS_DB_PATH, OPENCLAW_CLI, POLL_INTERVAL)
- [ ] Structured logging & metrics

## How to Use

### Running the Worker

Currently runs as a standalone binary:

```bash
# Build
cd internal/mention_delivery
go build -o mention-worker worker.go

# Run
./mention-worker
```

### Expected Output

```
2026-02-06 10:00:00 Mention delivery worker started
2026-02-06 10:00:10 WOULD SEND: openclaw message send --channel webchat --target 5ti98... "Mentioned by james in bead oc-abc: @arthur can you review this?"
```

Note: Currently uses `log.Printf("WOULD SEND: ...")` instead of actually executing commands.

## Database Schema

### Events Table (Read)

```sql
CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  issue_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  actor TEXT NOT NULL,
  comment TEXT,
  created_at TEXT NOT NULL
);
```

### Subscriptions Table (Not Yet Created)

Proposed schema:

```sql
CREATE TABLE subscriptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  subscriber TEXT NOT NULL,      -- Username
  channel TEXT NOT NULL,          -- openclaw, telegram, webchat, etc.
  target TEXT NOT NULL,           -- Channel/user ID for delivery
  sub_type TEXT NOT NULL,         -- 'bead', 'label', 'assignee', 'all'
  sub_id TEXT,                    -- ID of subscribed item (bead ID, label, etc.)
  created_at TEXT NOT NULL
);
```

## Configuration

Currently hardcoded in `NewWorker()`:

```go
beadsDB := "/home/dsauer/.openclaw/beads/.beads/beads.db"
openclaw := "openclaw"
```

Should be moved to environment variables or config file:

```bash
export BEADS_DB_PATH="/path/to/beads.db"
export OPENCLAW_CLI="openclaw"
export POLL_INTERVAL="10s"
```

## Next Steps

### Phase 1: Core Functionality (High Priority)

1. **Implement subscription matching**
   - Query subscriptions table when events arrive
   - Match against sub_type and sub_id
   - Build targeted notification messages

2. **Fix channel/target lookup**
   - Query user preferences for delivery channels
   - Look up OpenClaw target IDs from subscriber usernames
   - Support multiple channels per user

3. **Add error handling**
   - Wrap database operations in error handlers
   - Implement retry logic for failed deliveries
   - Log errors with context

4. **Execute actual notifications**
   - Replace `log.Printf("WOULD SEND")` with `exec.Command()`
   - Capture command output and errors
   - Track delivery status

### Phase 2: Reliability (Medium Priority)

5. **Graceful shutdown**
   - Handle SIGTERM/SIGINT signals
   - Flush pending notifications
   - Close database connections cleanly

6. **Delivery tracking**
   - Create `mention_deliveries` table
   - Record sent notifications (event_id, subscriber, sent_at)
   - Prevent duplicate deliveries

7. **Rate limiting**
   - Limit notifications per user per time window
   - Batch multiple mentions into digest messages
   - Implement quiet hours

### Phase 3: User Experience (Lower Priority)

8. **User preferences**
   - Allow users to configure notification channels
   - Support notification frequency (immediate, hourly, daily digest)
   - Enable/disable by mention type

9. **Delivery feedback**
   - Track successful vs failed deliveries
   - Retry failed deliveries
   - Alert admins on persistent failures

10. **Daemon integration**
    - Integrate with Gas Town daemon lifecycle
    - Add `gt mention-worker start/stop/status` commands
    - Add health checks and monitoring

## Testing

No tests currently exist. Recommended test coverage:

- [x] Mention regex parsing (`@username` extraction)
- [ ] Event processing logic
- [ ] Subscription matching
- [ ] Notification message formatting
- [ ] Database queries
- [ ] Error handling paths

## Known Issues

1. **Hardcoded paths**: Database path and OpenClaw CLI location are hardcoded
2. **No subscription support**: Subscription matching is stubbed out
3. **No delivery execution**: Commands are logged but not executed
4. **Memory leak potential**: `lastEventID` only increases, no cleanup of processed events
5. **Single-threaded**: Processes events sequentially, could block on slow deliveries

## Security Considerations

- **SQL injection**: Using parameterized queries (✅)
- **Command injection**: Need to sanitize user input before exec (⚠️)
- **Access control**: No verification that subscriber should see mentioned bead (❌)
- **Notification spam**: No rate limiting or abuse prevention (❌)

## Performance Notes

- Polls every 10 seconds regardless of activity (could use event triggers)
- Loads all events since `lastEventID` on each poll (could be thousands)
- No connection pooling for database
- Synchronous notification sending could block polling

## Dependencies

- `github.com/mattn/go-sqlite3` - SQLite driver
- OpenClaw CLI (`openclaw` binary must be in PATH)
- Beads database (must be readable)

## Integration with Gas Town

To integrate with Gas Town daemon:

1. Create `internal/cmd/mention_worker.go` command
2. Add lifecycle management (start/stop/status)
3. Add daemon configuration (`mention_worker.poll_interval`, etc.)
4. Add health checks to daemon monitoring
5. Document in main Gas Town README

## Contributing

When working on this module:

- Add error handling to all database operations
- Write unit tests for new functionality
- Update this README with implementation status
- Consider backwards compatibility with existing Beads schema

## License

Same as Gas Town parent project.
