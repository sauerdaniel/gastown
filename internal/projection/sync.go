// Package projection handles syncing Beads data to Mission Control projections.
package projection

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// beadsTask represents a task from Beads issues table.
type beadsTask struct {
	ID        string
	Title     string
	Desc      string
	Status    string
	Priority  int
	Type      string
	Assignee  string
	Owner     string
	CreatedAt time.Time
	UpdatedAt time.Time
	ClosedAt  sql.NullTime
	Labels    string
	External  string
	Rig       string
	Epic      string
	Project   string
}

// beadsAgent represents an agent from Beads issues table.
type beadsAgent struct {
	ID         string
	Title      string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	AgentState string
	Metadata   string
}

// beadsEvent represents an event from Beads events table.
type beadsEvent struct {
	ID        int64
	IssueID   string
	Type      string
	Actor     string
	OldValue  string
	NewValue  string
	Comment   string
	CreatedAt time.Time
}

// SyncDaemon handles periodic syncing from Beads to Mission Control projections.
type SyncDaemon struct {
	beadsDBPath        string
	projDBPath         string
	cacheDir           string
	logger             *log.Logger
	pollInterval       time.Duration
	ctx                context.Context
	cancel             context.CancelFunc
	mu                 sync.RWMutex
	lastSync           time.Time
	syncCount          int
	errorCount         int
	lastEventID        int64
	lastTaskUpdate     int64
	lastCommentSync    int64
	incrementalEnabled bool
}

// Config holds the daemon configuration.
type Config struct {
	BeadsDBPath  string // Path to beads.db
	ProjDBPath   string // Path to projections.db
	CacheDir     string // Path to cache directory
	PollInterval time.Duration
	Logger       *log.Logger
}

// New creates a new sync daemon.
func New(cfg Config) *SyncDaemon {
	ctx, cancel := context.WithCancel(context.Background())

	if cfg.PollInterval == 0 {
		cfg.PollInterval = 30 * time.Second // Default: sync every 30 seconds
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "[projection-sync] ", log.LstdFlags)
	}

	daemon := &SyncDaemon{
		beadsDBPath:        cfg.BeadsDBPath,
		projDBPath:         cfg.ProjDBPath,
		cacheDir:           cfg.CacheDir,
		logger:             cfg.Logger,
		pollInterval:       cfg.PollInterval,
		ctx:                ctx,
		cancel:             cancel,
		incrementalEnabled: false,
	}

	// Try to load persisted state for incremental sync
	townRoot := cfg.CacheDir
	for len(townRoot) > 0 && filepath.Base(townRoot) != "cache" {
		townRoot = filepath.Dir(townRoot)
	}
	townRoot = filepath.Dir(townRoot)

	if state, err := LoadState(townRoot); err == nil && state != nil {
		daemon.lastEventID = state.LastEventID
		daemon.lastTaskUpdate = state.LastTaskUpdate
		daemon.lastCommentSync = state.LastCommentSync
		daemon.incrementalEnabled = state.IncrementalSynced
		daemon.logger.Printf("Loaded persisted state: lastEventID=%d, incrementalEnabled=%v", daemon.lastEventID, daemon.incrementalEnabled)
	}

	return daemon
}

// Run starts the sync daemon loop.
func (d *SyncDaemon) Run() error {
	d.logger.Println("Starting projection sync daemon")

	// Verify database paths exist
	if _, err := os.Stat(d.beadsDBPath); err != nil {
		return fmt.Errorf("beads database not found at %s: %w", d.beadsDBPath, err)
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(d.cacheDir, 0755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	// Initial sync with retry
	maxRetries := 3
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := d.Sync(); err != nil {
			d.logger.Printf("Initial sync attempt %d/%d failed: %v", i+1, maxRetries, err)
			lastErr = err
			d.errorCount++
			time.Sleep(time.Second * time.Duration(i+1)) // Exponential backoff
			continue
		}
		lastErr = nil
		break
	}
	
	if lastErr != nil {
		return fmt.Errorf("initial sync failed after %d attempts: %w", maxRetries, lastErr)
	}

	// Sync loop
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			d.logger.Println("Shutdown requested, stopping sync daemon")
			return nil
		case <-ticker.C:
			if err := d.Sync(); err != nil {
				d.logger.Printf("Sync failed: %v", err)
				d.errorCount++
				
				// Log if error rate is high
				d.mu.RLock()
				errorRate := float64(d.errorCount) / float64(d.syncCount+d.errorCount)
				d.mu.RUnlock()
				if errorRate > 0.5 {
					d.logger.Printf("WARNING: High error rate (%.1f%%), check database connectivity", errorRate*100)
				}
			} else {
				d.mu.Lock()
				d.syncCount++
				d.mu.Unlock()
			}
		}
	}
}

// Stop stops the daemon.
func (d *SyncDaemon) Stop() {
	d.cancel()
}

// Sync performs a full sync from Beads to Mission Control.
func (d *SyncDaemon) Sync() error {
	startTime := time.Now()

	// Connect to Beads database with timeout
	beadsDB, err := sql.Open("sqlite3", d.beadsDBPath+"?_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("opening beads database: %w", err)
	}
	defer func() {
		if err := beadsDB.Close(); err != nil {
			d.logger.Printf("Warning: error closing beads database: %v", err)
		}
	}()

	// Verify beads connection
	if err := beadsDB.Ping(); err != nil {
		return fmt.Errorf("beads database connection failed: %w", err)
	}

	// Verify Beads schema
	if err := d.verifyBeadsSchema(beadsDB); err != nil {
		d.logger.Printf("Warning: schema verification failed: %v", err)
		// Non-fatal, continue with sync
	}

	// Ensure projection database directory exists
	if err := os.MkdirAll(filepath.Dir(d.projDBPath), 0755); err != nil {
		return fmt.Errorf("creating projection database directory: %w", err)
	}

	// Connect to projection database with timeout
	projDB, err := sql.Open("sqlite3", d.projDBPath+"?_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("opening projection database: %w", err)
	}
	defer func() {
		if err := projDB.Close(); err != nil {
			d.logger.Printf("Warning: error closing projection database: %v", err)
		}
	}()

	// Verify projection connection
	if err := projDB.Ping(); err != nil {
		return fmt.Errorf("projection database connection failed: %w", err)
	}

	// Sync tasks (incremental if possible)
	if d.lastTaskUpdate > 0 && d.incrementalEnabled {
		if err := d.syncTasksIncremental(beadsDB, projDB); err != nil {
			d.logger.Printf("Incremental task sync failed, falling back to full sync: %v", err)
			if err := d.syncTasks(beadsDB, projDB); err != nil {
				return fmt.Errorf("syncing tasks: %w", err)
			}
		}
	} else {
		if err := d.syncTasks(beadsDB, projDB); err != nil {
			return fmt.Errorf("syncing tasks: %w", err)
		}
		d.mu.Lock()
		d.incrementalEnabled = true
		d.mu.Unlock()
	}

	// Sync agents
	if err := d.syncAgents(beadsDB, projDB); err != nil {
		return fmt.Errorf("syncing agents: %w", err)
	}

	// Sync activity (incremental if possible)
	if d.lastEventID > 0 && d.incrementalEnabled {
		if err := d.syncActivityIncremental(beadsDB, projDB); err != nil {
			d.logger.Printf("Incremental activity sync failed, falling back to full sync: %v", err)
			if err := d.syncActivity(beadsDB, projDB); err != nil {
				return fmt.Errorf("syncing activity: %w", err)
			}
		}
	} else {
		if err := d.syncActivity(beadsDB, projDB); err != nil {
			return fmt.Errorf("syncing activity: %w", err)
		}
	}

	// Sync comments
	if err := d.syncComments(beadsDB, projDB); err != nil {
		return fmt.Errorf("syncing comments: %w", err)
	}

	// Update state with mutex protection
	d.mu.Lock()
	d.lastSync = time.Now()
	d.mu.Unlock()

	// Save state to disk
	if err := d.SaveState(); err != nil {
		d.logger.Printf("Warning: failed to save state: %v", err)
		// Non-fatal, continue
	}

	duration := time.Since(startTime)
	d.mu.RLock()
	syncCount := d.syncCount
	errorCount := d.errorCount
	d.mu.RUnlock()
	
	d.logger.Printf("Sync completed in %v (total syncs: %d, errors: %d)", duration, syncCount, errorCount)

	return nil
}

// syncTasks syncs tasks from Beads issues to projections.
func (d *SyncDaemon) syncTasks(beadsDB, projDB *sql.DB) error {
	// Query Beads for all non-deleted issues
	rows, err := beadsDB.Query(`
		SELECT id, title, description, status, priority, issue_type,
		       assignee, owner, created_at, updated_at, closed_at,
		       external_ref, rig, '' AS labels, '' AS epic, '' AS project
		FROM issues
		WHERE deleted_at IS NULL
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return fmt.Errorf("querying beads issues: %w", err)
	}
	defer rows.Close()

	var tasks []beadsTask
	for rows.Next() {
		var t beadsTask
		err := rows.Scan(
			&t.ID, &t.Title, &t.Desc, &t.Status, &t.Priority, &t.Type,
			&t.Assignee, &t.Owner, &t.CreatedAt, &t.UpdatedAt, &t.ClosedAt,
			&t.Labels, &t.External, &t.Rig, &t.Epic, &t.Project,
		)
		if err != nil {
			return fmt.Errorf("scanning issue row: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating issue rows: %w", err)
	}

	// Begin transaction
	tx, err := projDB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear and rebuild tasks table
	if _, err := tx.Exec("DELETE FROM tasks"); err != nil {
		return fmt.Errorf("clearing tasks: %w", err)
	}

	// Insert tasks
	for _, t := range tasks {
		hasComments := false
		commentCount := 0
		// Check if task has comments (simplified - could be optimized)
		// For now, we'll update this in syncComments

		// Parse labels as JSON array
		var labels string
		if t.Labels != "" {
			labels = t.Labels
		}

		// Parse external_ref from metadata if needed
		external := t.External
		if external == "" {
			// Check metadata JSON for external_ref
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(t.Labels), &metadata); err == nil {
				if ext, ok := metadata["external_ref"].(string); ok {
					external = ext
				}
			}
		}

		// Insert task
		_, err := tx.Exec(`
			INSERT INTO tasks (
				id, title, description, status, priority, issue_type,
				assignee, owner, created_at, updated_at, closed_at,
				labels, external_ref, rig, epic, project,
				has_comments, comment_count, has_artifacts, has_hooks,
				source_repo, indexed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			t.ID, t.Title, t.Desc, t.Status, t.Priority, t.Type,
			nullString(t.Assignee), nullString(t.Owner),
			unixMillis(t.CreatedAt), unixMillis(t.UpdatedAt),
			nullTime(t.ClosedAt),
			labels, nullString(external), nullString(t.Rig),
			nullString(t.Epic), nullString(t.Project),
			hasComments, commentCount, false, false,
			".", time.Now().UnixMilli(),
		)
		if err != nil {
			return fmt.Errorf("inserting task %s: %w", t.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing tasks: %w", err)
	}

	// Write tasks.json cache file
	if err := d.writeTasksJSON(tasks); err != nil {
		d.logger.Printf("Warning: failed to write tasks.json: %v", err)
	}

	return nil
}

// syncAgents syncs agent status from Beads (issue_type=agent).
func (d *SyncDaemon) syncAgents(beadsDB, projDB *sql.DB) error {
	// Query Beads for agent-type issues
	rows, err := beadsDB.Query(`
		SELECT id, title, status, created_at, updated_at,
		       agent_state, metadata
		FROM issues
		WHERE issue_type = 'agent' AND deleted_at IS NULL
	`)
	if err != nil {
		return fmt.Errorf("querying beads agents: %w", err)
	}
	defer rows.Close()

	var agents []beadsAgent
	for rows.Next() {
		var a beadsAgent
		err := rows.Scan(
			&a.ID, &a.Title, &a.Status, &a.CreatedAt, &a.UpdatedAt,
			&a.AgentState, &a.Metadata,
		)
		if err != nil {
			return fmt.Errorf("scanning agent row: %w", err)
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating agent rows: %w", err)
	}

	// Write agents.json cache file
	if err := d.writeAgentsJSON(agents); err != nil {
		d.logger.Printf("Warning: failed to write agents.json: %v", err)
	}

	return nil
}

// syncActivity syncs activity from Beads events to projections.
func (d *SyncDaemon) syncActivity(beadsDB, projDB *sql.DB) error {
	// Query recent events from Beads (last 7 days)
	rows, err := beadsDB.Query(`
		SELECT id, issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM events
		WHERE created_at >= datetime('now', '-7 days')
		ORDER BY created_at DESC
	`)
	if err != nil {
		return fmt.Errorf("querying beads events: %w", err)
	}
	defer rows.Close()

	var events []beadsEvent
	for rows.Next() {
		var e beadsEvent
		err := rows.Scan(
			&e.ID, &e.IssueID, &e.Type, &e.Actor,
			&e.OldValue, &e.NewValue, &e.Comment, &e.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("scanning event row: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating event rows: %w", err)
	}

	// Begin transaction
	tx, err := projDB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear and rebuild activities table
	if _, err := tx.Exec("DELETE FROM activities"); err != nil {
		return fmt.Errorf("clearing activities: %w", err)
	}

	// Insert activities
	for _, e := range events {
		// Map Beads event types to MC activity types
		activityType := mapEventToActivity(e.Type)

		// Build content
		content := buildActivityContent(e)

		// Extract agent_id from issue_id if it's an agent
		agentID := e.Actor
		if strings.HasPrefix(e.IssueID, "oc-") || strings.HasPrefix(e.IssueID, "gt-") {
			agentID = e.Actor
		}

		_, err := tx.Exec(`
			INSERT INTO activities (type, agent_id, task_id, content, timestamp)
			VALUES (?, ?, ?, ?, ?)
		`,
			activityType, agentID, e.IssueID, content, unixMillis(e.CreatedAt),
		)
		if err != nil {
			return fmt.Errorf("inserting activity %d: %w", e.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing activities: %w", err)
	}

	// Write activity.jsonl cache file
	if err := d.writeActivityJSONL(events); err != nil {
		d.logger.Printf("Warning: failed to write activity.jsonl: %v", err)
	}

	return nil
}

// syncComments syncs comments from Beads to the projection database.
func (d *SyncDaemon) syncComments(beadsDB, projDB *sql.DB) error {
	// Query comments from Beads
	rows, err := beadsDB.Query(`
		SELECT id, issue_id, author, text, created_at
		FROM comments
		ORDER BY created_at DESC
		LIMIT 10000
	`)
	if err != nil {
		// If comments table doesn't exist, log warning and continue (not critical)
		d.logger.Printf("Warning: querying beads comments: %v", err)
		return nil
	}
	defer rows.Close()

	type beadsComment struct {
		ID        string
		IssueID   string
		Author    string
		Text      string
		CreatedAt time.Time
	}

	var comments []beadsComment
	for rows.Next() {
		var c beadsComment
		err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &c.CreatedAt)
		if err != nil {
			return fmt.Errorf("scanning comment row: %w", err)
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating comment rows: %w", err)
	}

	// Begin transaction
	tx, err := projDB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear and rebuild task_comments table
	if _, err := tx.Exec("DELETE FROM task_comments"); err != nil {
		return fmt.Errorf("clearing comments: %w", err)
	}

	// Insert comments
	for _, c := range comments {
		_, err := tx.Exec(`
			INSERT INTO task_comments (id, task_id, author, content, created_at)
			VALUES (?, ?, ?, ?, ?)
		`,
			c.ID, c.IssueID, nullString(c.Author), c.Text, unixMillis(c.CreatedAt),
		)
		if err != nil {
			return fmt.Errorf("inserting comment %s: %w", c.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing comments: %w", err)
	}

	d.logger.Printf("Synced %d comments", len(comments))

	return nil
}

// verifyBeadsSchema checks that Beads database has required tables and columns.
func (d *SyncDaemon) verifyBeadsSchema(db *sql.DB) error {
	// Define required tables and their columns
	requiredTables := map[string][]string{
		"issues": {"id", "title", "description", "status", "issue_type", "created_at", "updated_at", "deleted_at"},
		"events": {"id", "issue_id", "event_type", "actor", "created_at"},
		"comments": {"id", "issue_id", "author", "text", "created_at"},
	}

	for tableName, requiredCols := range requiredTables {
		// Get columns for this table
		rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
		if err != nil {
			return fmt.Errorf("checking table %s: %w", tableName, err)
		}
		defer rows.Close()

		columns := make(map[string]bool)
		for rows.Next() {
			var cid, notnull, pk int
			var name, ctype string
			var dfltValue sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
				return fmt.Errorf("scanning column info for %s: %w", tableName, err)
			}
			columns[name] = true
		}

		// Verify required columns exist
		for _, col := range requiredCols {
			if !columns[col] {
				d.logger.Printf("Warning: table %s missing column %s", tableName, col)
			}
		}
	}

	return nil
}

// writeTasksJSON writes the tasks cache file in Mission Control format.
func (d *SyncDaemon) writeTasksJSON(tasks interface{}) error {
	cacheFile := filepath.Join(d.cacheDir, "tasks.json")

	meta := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   time.Now().UnixMilli(),
		"count":     len(tasks.([]beadsTask)),
	}

	data := map[string]interface{}{
		"_meta": meta,
		"data":  tasks,
	}

	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		return err
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cacheFile, jsonData, 0644)
}

// writeAgentsJSON writes the agents cache file in Mission Control format.
func (d *SyncDaemon) writeAgentsJSON(agents []beadsAgent) error {
	cacheFile := filepath.Join(d.cacheDir, "agents.json")

	type mcAgent struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Active     bool   `json:"active"`
		LastSeen   int64  `json:"lastSeen"`
		Model      string `json:"model"`
		CurrentTask string `json:"currentTask,omitempty"`
		SessionKey string `json:"sessionKey,omitempty"`
	}

	var mcAgents []mcAgent
	for _, a := range agents {
		// Extract agent name from ID (e.g., "gt-agent-mort" -> "mort")
		name := a.Title
		if parts := strings.Split(a.ID, "-"); len(parts) > 0 {
			name = parts[len(parts)-1]
		}

		// Determine if active based on status and last activity
		active := a.Status == "open" || a.Status == "in_progress"

		mcAgents = append(mcAgents, mcAgent{
			ID:       a.ID,
			Name:     name,
			Active:   active,
			LastSeen: a.UpdatedAt.UnixMilli(),
			Model:    "claude-opus-4-6", // Default, could be parsed from metadata
		})
	}

	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		return err
	}

	jsonData, err := json.MarshalIndent(mcAgents, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cacheFile, jsonData, 0644)
}

// writeActivityJSONL writes the activity cache file in Mission Control format.
func (d *SyncDaemon) writeActivityJSONL(events []beadsEvent) error {
	cacheFile := filepath.Join(d.cacheDir, "activity.jsonl")

	type mcActivity struct {
		Type      string `json:"type"`
		AgentID   string `json:"agentId"`
		TaskID    string `json:"taskId,omitempty"`
		Content   string `json:"content,omitempty"`
		Timestamp int64  `json:"timestamp"`
	}

	var lines []string
	for _, e := range events {
		activityType := mapEventToActivity(e.Type)
		lines = append(lines, fmt.Sprintf(`{"type":"%s","agentId":"%s","taskId":"%s","content":%q,"timestamp":%d}`,
			activityType, e.Actor, e.IssueID, buildActivityContent(e), unixMillis(e.CreatedAt)))
	}

	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		return err
	}

	return os.WriteFile(cacheFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// Helper functions

func unixMillis(t time.Time) int64 {
	return t.UnixMilli()
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullTime(t sql.NullTime) sql.NullInt64 {
	if !t.Valid {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: t.Time.UnixMilli(), Valid: true}
}

func mapEventToActivity(eventType string) string {
	switch eventType {
	case "create":
		return "task_created"
	case "update", "status_change", "assign_change":
		return "task_updated"
	case "comment":
		return "comment_added"
	case "assign":
		return "task_assigned"
	case "close":
		return "task_completed"
	default:
		return "task_updated"
	}
}

func buildActivityContent(e beadsEvent) string {
	if e.Comment != "" {
		return e.Comment
	}
	switch e.Type {
	case "create":
		return fmt.Sprintf("Created task")
	case "assign":
		return fmt.Sprintf("Assigned to %s", e.NewValue)
	case "status_change":
		return fmt.Sprintf("Status changed: %s → %s", e.OldValue, e.NewValue)
	case "close":
		return "Task completed"
	default:
		return e.Type
	}
}

// syncTasksIncremental performs an incremental task sync using Beads dirty_issues table.
func (d *SyncDaemon) syncTasksIncremental(beadsDB, projDB *sql.DB) error {
	startTime := time.Now()

	// Check if dirty_issues table exists
	var tableExists bool
	err := beadsDB.QueryRow(`
		SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='dirty_issues')
	`).Scan(&tableExists)
	if err != nil || !tableExists {
		return fmt.Errorf("dirty_issues table not available, falling back to full sync")
	}

	// Query Beads for dirty (changed) issues
	rows, err := beadsDB.Query(`
		SELECT i.id, i.title, COALESCE(i.description, '') AS description,
		       i.status, i.priority, i.issue_type,
		       COALESCE(i.assignee, '') AS assignee, COALESCE(i.owner, '') AS owner,
		       i.created_at, i.updated_at, i.closed_at,
		       COALESCE(i.external_ref, '') AS external_ref,
		       '' AS rig, '' AS labels, '' AS epic, '' AS project
		FROM issues i
		INNER JOIN dirty_issues d ON i.id = d.issue_id
		WHERE i.deleted_at IS NULL
		ORDER BY i.updated_at DESC
	`)
	if err != nil {
		return fmt.Errorf("querying dirty issues: %w", err)
	}
	defer rows.Close()

	var tasks []beadsTask
	var maxUpdateTime int64
	for rows.Next() {
		var t beadsTask
		err := rows.Scan(
			&t.ID, &t.Title, &t.Desc, &t.Status, &t.Priority, &t.Type,
			&t.Assignee, &t.Owner, &t.CreatedAt, &t.UpdatedAt, &t.ClosedAt,
			&t.Labels, &t.External, &t.Rig, &t.Epic, &t.Project,
		)
		if err != nil {
			return fmt.Errorf("scanning issue row: %w", err)
		}
		tasks = append(tasks, t)
		
		// Track maximum update time for next incremental sync
		updateMs := t.UpdatedAt.UnixMilli()
		if updateMs > maxUpdateTime {
			maxUpdateTime = updateMs
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating issue rows: %w", err)
	}

	if len(tasks) == 0 {
		d.logger.Printf("No dirty issues to sync (incremental)")
		return nil
	}

	// Begin transaction
	tx, err := projDB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Update tasks (upsert pattern)
	for _, t := range tasks {
		hasComments := false
		commentCount := 0

		var labels string
		if t.Labels != "" {
			labels = t.Labels
		}

		external := t.External
		if external == "" {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(t.Labels), &metadata); err == nil {
				if ext, ok := metadata["external_ref"].(string); ok {
					external = ext
				}
			}
		}

		// Upsert: insert or update task
		_, err := tx.Exec(`
			INSERT INTO tasks (
				id, title, description, status, priority, issue_type,
				assignee, owner, created_at, updated_at, closed_at,
				labels, external_ref, rig, epic, project,
				has_comments, comment_count, has_artifacts, has_hooks,
				source_repo, indexed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				title=excluded.title,
				description=excluded.description,
				status=excluded.status,
				priority=excluded.priority,
				issue_type=excluded.issue_type,
				assignee=excluded.assignee,
				owner=excluded.owner,
				updated_at=excluded.updated_at,
				closed_at=excluded.closed_at,
				indexed_at=excluded.indexed_at
		`,
			t.ID, t.Title, t.Desc, t.Status, t.Priority, t.Type,
			nullString(t.Assignee), nullString(t.Owner),
			unixMillis(t.CreatedAt), unixMillis(t.UpdatedAt),
			nullTime(t.ClosedAt),
			labels, nullString(external), nullString(t.Rig),
			nullString(t.Epic), nullString(t.Project),
			hasComments, commentCount, false, false,
			".", time.Now().UnixMilli(),
		)
		if err != nil {
			return fmt.Errorf("upserting task %s: %w", t.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing tasks: %w", err)
	}

	// Clear dirty_issues table after successful sync
	if _, err := beadsDB.Exec("DELETE FROM dirty_issues"); err != nil {
		d.logger.Printf("Warning: failed to clear dirty_issues: %v", err)
	}

	// Update tracking state
	d.mu.Lock()
	d.lastTaskUpdate = maxUpdateTime
	d.mu.Unlock()

	duration := time.Since(startTime)
	d.logger.Printf("Incremental task sync completed in %v (synced %d dirty tasks)", duration, len(tasks))

	return nil
}

// syncActivityIncremental performs an incremental activity sync using event ID tracking.
func (d *SyncDaemon) syncActivityIncremental(beadsDB, projDB *sql.DB) error {
	startTime := time.Now()

	// Query events after lastEventID
	rows, err := beadsDB.Query(`
		SELECT id, issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM events
		WHERE id > ?
		ORDER BY id ASC
		LIMIT 10000
	`, d.lastEventID)
	if err != nil {
		return fmt.Errorf("querying beads events: %w", err)
	}
	defer rows.Close()

	var events []beadsEvent
	var maxEventID int64
	for rows.Next() {
		var e beadsEvent
		err := rows.Scan(
			&e.ID, &e.IssueID, &e.Type, &e.Actor,
			&e.OldValue, &e.NewValue, &e.Comment, &e.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("scanning event row: %w", err)
		}
		events = append(events, e)
		
		// Track maximum event ID for next incremental sync
		if e.ID > maxEventID {
			maxEventID = e.ID
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating event rows: %w", err)
	}

	if len(events) == 0 {
		d.logger.Printf("No new events to sync (last event ID: %d)", d.lastEventID)
		return nil
	}

	// Begin transaction
	tx, err := projDB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Append activities (don't clear, append only)
	for _, e := range events {
		activityType := mapEventToActivity(e.Type)
		content := buildActivityContent(e)
		agentID := e.Actor
		if strings.HasPrefix(e.IssueID, "oc-") || strings.HasPrefix(e.IssueID, "gt-") {
			agentID = e.Actor
		}

		_, err := tx.Exec(`
			INSERT INTO activities (type, agent_id, task_id, content, timestamp)
			VALUES (?, ?, ?, ?, ?)
		`,
			activityType, agentID, e.IssueID, content, unixMillis(e.CreatedAt),
		)
		if err != nil {
			return fmt.Errorf("inserting activity %d: %w", e.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing activities: %w", err)
	}

	// Update tracking state
	d.mu.Lock()
	d.lastEventID = maxEventID
	d.mu.Unlock()

	duration := time.Since(startTime)
	d.logger.Printf("Incremental activity sync completed in %v (synced %d events, last event ID: %d)", duration, len(events), maxEventID)

	return nil
}
