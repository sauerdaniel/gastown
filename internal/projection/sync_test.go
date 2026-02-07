package projection

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestSyncComments tests the comment sync functionality.
func TestSyncComments(t *testing.T) {
	// Create temporary test databases
	tempDir := t.TempDir()
	beadsDBPath := filepath.Join(tempDir, "beads_test.db")
	projDBPath := filepath.Join(tempDir, "proj_test.db")

	// Create test Beads database with schema and sample comments
	beadsDB, err := setupTestBeadsDB(beadsDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Beads DB: %v", err)
	}
	defer beadsDB.Close()

	// Create test Projection database with schema
	projDB, err := setupTestProjDB(projDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Proj DB: %v", err)
	}
	defer projDB.Close()

	// Create daemon
	cfg := Config{
		BeadsDBPath:  beadsDBPath,
		ProjDBPath:   projDBPath,
		CacheDir:     filepath.Join(tempDir, "cache"),
		PollInterval: 1 * time.Second,
	}
	daemon := New(cfg)

	// Run comment sync
	if err := daemon.syncComments(beadsDB, projDB); err != nil {
		t.Fatalf("syncComments failed: %v", err)
	}

	// Verify comments were synced
	rows, err := projDB.Query("SELECT COUNT(*) FROM task_comments")
	if err != nil {
		t.Fatalf("Failed to query task_comments: %v", err)
	}
	defer rows.Close()

	var count int
	if !rows.Next() {
		t.Fatal("No rows returned from task_comments count")
	}
	if err := rows.Scan(&count); err != nil {
		t.Fatalf("Failed to scan comment count: %v", err)
	}

	if count == 0 {
		t.Fatal("No comments synced to projection DB")
	}

	t.Logf("✓ Successfully synced %d comments", count)

	// Verify a specific comment
	rows, err = projDB.Query("SELECT id, task_id, author, content FROM task_comments LIMIT 1")
	if err != nil {
		t.Fatalf("Failed to query task_comments details: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("No comments found in projection DB")
	}

	var id, taskID, author, content string
	if err := rows.Scan(&id, &taskID, &author, &content); err != nil {
		t.Fatalf("Failed to scan comment details: %v", err)
	}

	if id == "" || taskID == "" || content == "" {
		t.Fatalf("Comment has empty fields: id=%s, taskID=%s, content=%s", id, taskID, content)
	}

	t.Logf("✓ Sample comment verified: id=%s, taskID=%s, author=%s", id, taskID, author)
}

// TestSchemaVerification tests the schema verification functionality.
func TestSchemaVerification(t *testing.T) {
	// Create temporary test database
	tempDir := t.TempDir()
	beadsDBPath := filepath.Join(tempDir, "beads_test.db")

	// Create test Beads database
	beadsDB, err := setupTestBeadsDB(beadsDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Beads DB: %v", err)
	}
	defer beadsDB.Close()

	// Create daemon
	cfg := Config{
		BeadsDBPath:  beadsDBPath,
		ProjDBPath:   filepath.Join(tempDir, "proj_test.db"),
		CacheDir:     filepath.Join(tempDir, "cache"),
		PollInterval: 1 * time.Second,
	}
	daemon := New(cfg)

	// Run schema verification (should not error)
	if err := daemon.verifyBeadsSchema(beadsDB); err != nil {
		t.Fatalf("verifyBeadsSchema failed: %v", err)
	}

	t.Log("✓ Schema verification passed")
}

// setupTestBeadsDB creates a test Beads database with schema and sample data.
func setupTestBeadsDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// Create issues table
	if _, err := db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			status TEXT DEFAULT 'open',
			priority INTEGER DEFAULT 2,
			issue_type TEXT DEFAULT 'task',
			assignee TEXT,
			owner TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			closed_at DATETIME,
			external_ref TEXT,
			deleted_at DATETIME
		)
	`); err != nil {
		return nil, err
	}

	// Create comments table
	if _, err := db.Exec(`
		CREATE TABLE comments (
			id INTEGER PRIMARY KEY,
			issue_id TEXT NOT NULL,
			author TEXT NOT NULL,
			text TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return nil, err
	}

	// Create events table
	if _, err := db.Exec(`
		CREATE TABLE events (
			id INTEGER PRIMARY KEY,
			issue_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			actor TEXT NOT NULL,
			old_value TEXT,
			new_value TEXT,
			comment TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return nil, err
	}

	// Insert sample issue
	if _, err := db.Exec(
		"INSERT INTO issues (id, title, description) VALUES (?, ?, ?)",
		"oc-test-1", "Test Issue", "This is a test issue",
	); err != nil {
		return nil, err
	}

	// Insert sample comments
	for i := 1; i <= 3; i++ {
		if _, err := db.Exec(
			"INSERT INTO comments (issue_id, author, text) VALUES (?, ?, ?)",
			"oc-test-1", "user123", "Test comment "+string(rune(48+i)),
		); err != nil {
			return nil, err
		}
	}

	// Insert sample events
	for i := 1; i <= 5; i++ {
		if _, err := db.Exec(
			"INSERT INTO events (issue_id, event_type, actor, old_value, new_value, comment, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			"oc-test-1", "update", "user123", "open", "in_progress", "Updated status", time.Now(),
		); err != nil {
			return nil, err
		}
	}

	return db, nil
}

// setupTestProjDB creates a test Projection database with schema.
func setupTestProjDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// Create tasks table
	if _, err := db.Exec(`
		CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			status TEXT,
			priority INTEGER,
			issue_type TEXT,
			assignee TEXT,
			owner TEXT,
			created_at INTEGER,
			updated_at INTEGER,
			closed_at INTEGER,
			labels TEXT,
			external_ref TEXT,
			rig TEXT,
			epic TEXT,
			project TEXT,
			has_comments BOOLEAN,
			comment_count INTEGER,
			has_artifacts BOOLEAN,
			has_hooks BOOLEAN,
			source_repo TEXT,
			indexed_at INTEGER
		)
	`); err != nil {
		return nil, err
	}

	// Create task_comments table
	if _, err := db.Exec(`
		CREATE TABLE task_comments (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			author TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER
		)
	`); err != nil {
		return nil, err
	}

	// Create activities table
	if _, err := db.Exec(`
		CREATE TABLE activities (
			type TEXT,
			agent_id TEXT,
			task_id TEXT,
			content TEXT,
			timestamp INTEGER
		)
	`); err != nil {
		return nil, err
	}

	return db, nil
}

// TestSyncActivityIncremental tests incremental activity sync using event ID tracking.
func TestSyncActivityIncremental(t *testing.T) {
	// Create temporary test databases
	tempDir := t.TempDir()
	beadsDBPath := filepath.Join(tempDir, "beads_test.db")
	projDBPath := filepath.Join(tempDir, "proj_test.db")

	// Create test Beads database with schema and sample events
	beadsDB, err := setupTestBeadsDB(beadsDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Beads DB: %v", err)
	}
	defer beadsDB.Close()

	// Create test Projection database with schema
	projDB, err := setupTestProjDB(projDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Proj DB: %v", err)
	}
	defer projDB.Close()

	// Create daemon
	cfg := Config{
		BeadsDBPath:  beadsDBPath,
		ProjDBPath:   projDBPath,
		CacheDir:     filepath.Join(tempDir, "cache"),
		PollInterval: 1 * time.Second,
	}
	daemon := New(cfg)

	// First sync - full sync (lastEventID = 0)
	if err := daemon.syncActivityIncremental(beadsDB, projDB); err != nil {
		t.Fatalf("First syncActivityIncremental failed: %v", err)
	}

	// Count initial events
	var initialCount int
	if err := projDB.QueryRow("SELECT COUNT(*) FROM activities").Scan(&initialCount); err != nil {
		t.Fatalf("Failed to count initial activities: %v", err)
	}
	if initialCount == 0 {
		t.Fatal("No activities synced in first incremental sync")
	}
	t.Logf("✓ First sync: %d activities synced", initialCount)

	// Verify lastEventID was updated
	if daemon.lastEventID == 0 {
		t.Fatal("lastEventID not updated after incremental sync")
	}
	t.Logf("✓ lastEventID updated to: %d", daemon.lastEventID)

	// Add more events to Beads
	if _, err := beadsDB.Exec(`
		INSERT INTO events (issue_id, event_type, actor, old_value, new_value, comment, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "oc-999", "update", "arthur", "open", "closed", "Updated task", time.Now()); err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	// Second incremental sync
	oldEventID := daemon.lastEventID
	if err := daemon.syncActivityIncremental(beadsDB, projDB); err != nil {
		t.Fatalf("Second syncActivityIncremental failed: %v", err)
	}

	// Count final events
	var finalCount int
	if err := projDB.QueryRow("SELECT COUNT(*) FROM activities").Scan(&finalCount); err != nil {
		t.Fatalf("Failed to count final activities: %v", err)
	}

	if finalCount != initialCount+1 {
		t.Fatalf("Expected %d activities, got %d", initialCount+1, finalCount)
	}
	t.Logf("✓ Second sync: %d new activity added (total: %d)", finalCount-initialCount, finalCount)

	if daemon.lastEventID <= oldEventID {
		t.Fatalf("lastEventID not advanced: was %d, now %d", oldEventID, daemon.lastEventID)
	}
	t.Logf("✓ lastEventID advanced to: %d", daemon.lastEventID)
}

// TestSyncTasksIncremental tests incremental task sync using dirty_issues table.
func TestSyncTasksIncremental(t *testing.T) {
	// Create temporary test databases
	tempDir := t.TempDir()
	beadsDBPath := filepath.Join(tempDir, "beads_test.db")
	projDBPath := filepath.Join(tempDir, "proj_test.db")

	// Create test Beads database
	beadsDB, err := setupTestBeadsDB(beadsDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Beads DB: %v", err)
	}
	defer beadsDB.Close()

	// Create dirty_issues table for incremental testing
	if _, err := beadsDB.Exec(`
		CREATE TABLE dirty_issues (
			issue_id TEXT PRIMARY KEY,
			updated_at INTEGER
		)
	`); err != nil {
		t.Fatalf("Failed to create dirty_issues table: %v", err)
	}

	// Create test Projection database
	projDB, err := setupTestProjDB(projDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Proj DB: %v", err)
	}
	defer projDB.Close()

	// Create daemon
	cfg := Config{
		BeadsDBPath:  beadsDBPath,
		ProjDBPath:   projDBPath,
		CacheDir:     filepath.Join(tempDir, "cache"),
		PollInterval: 1 * time.Second,
	}
	daemon := New(cfg)

	// Insert some test tasks into Beads
	now := time.Now()
	for i := 1; i <= 3; i++ {
		id := "oc-incr-" + string(rune(48+i))
		if _, err := beadsDB.Exec(`
			INSERT INTO issues (id, title, description, status, priority, issue_type, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, id, "Test Task "+string(rune(48+i)), "Description", "open", 2, "task", now, now); err != nil {
			t.Fatalf("Failed to insert test issue: %v", err)
		}

		// Mark as dirty
		if _, err := beadsDB.Exec(`
			INSERT INTO dirty_issues (issue_id, updated_at)
			VALUES (?, ?)
		`, id, now.UnixMilli()); err != nil {
			t.Fatalf("Failed to mark issue dirty: %v", err)
		}
	}

	// First incremental sync (should process dirty issues)
	if err := daemon.syncTasksIncremental(beadsDB, projDB); err != nil {
		t.Fatalf("syncTasksIncremental failed: %v", err)
	}

	// Count synced tasks
	var taskCount int
	if err := projDB.QueryRow("SELECT COUNT(*) FROM tasks WHERE id LIKE 'oc-incr-%'").Scan(&taskCount); err != nil {
		t.Fatalf("Failed to count synced tasks: %v", err)
	}

	if taskCount != 3 {
		t.Fatalf("Expected 3 tasks, got %d", taskCount)
	}
	t.Logf("✓ Synced %d dirty tasks", taskCount)

	// Verify dirty_issues table was cleared
	var dirtyCount int
	if err := beadsDB.QueryRow("SELECT COUNT(*) FROM dirty_issues").Scan(&dirtyCount); err != nil {
		t.Fatalf("Failed to count dirty issues: %v", err)
	}

	if dirtyCount != 0 {
		t.Fatalf("Expected 0 dirty issues after sync, got %d", dirtyCount)
	}
	t.Logf("✓ dirty_issues table cleared after sync")

	// Verify lastTaskUpdate was set
	if daemon.lastTaskUpdate == 0 {
		t.Fatal("lastTaskUpdate not set after incremental sync")
	}
	t.Logf("✓ lastTaskUpdate: %d", daemon.lastTaskUpdate)
}

// TestIncrementalStatePreservation tests that incremental sync state is preserved.
func TestIncrementalStatePreservation(t *testing.T) {
	tempDir := t.TempDir()
	beadsDBPath := filepath.Join(tempDir, "beads_test.db")
	projDBPath := filepath.Join(tempDir, "proj_test.db")

	// Create test databases
	beadsDB, err := setupTestBeadsDB(beadsDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Beads DB: %v", err)
	}
	defer beadsDB.Close()

	projDB, err := setupTestProjDB(projDBPath)
	if err != nil {
		t.Fatalf("Failed to setup test Proj DB: %v", err)
	}
	defer projDB.Close()

	// Create daemon and update state
	cfg := Config{
		BeadsDBPath:  beadsDBPath,
		ProjDBPath:   projDBPath,
		CacheDir:     filepath.Join(tempDir, "cache"),
		PollInterval: 1 * time.Second,
	}
	daemon := New(cfg)
	
	// Manually set incremental state
	daemon.lastEventID = 12345
	daemon.lastTaskUpdate = 67890
	daemon.incrementalEnabled = true

	// Simulate SaveState
	if err := daemon.SaveState(); err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Create new daemon and verify state was loaded
	daemon2 := New(cfg)

	if daemon2.lastEventID != daemon.lastEventID {
		t.Fatalf("lastEventID not preserved: expected %d, got %d", daemon.lastEventID, daemon2.lastEventID)
	}

	if daemon2.lastTaskUpdate != daemon.lastTaskUpdate {
		t.Fatalf("lastTaskUpdate not preserved: expected %d, got %d", daemon.lastTaskUpdate, daemon2.lastTaskUpdate)
	}

	if daemon2.incrementalEnabled != daemon.incrementalEnabled {
		t.Fatalf("incrementalEnabled not preserved: expected %v, got %v", daemon.incrementalEnabled, daemon2.incrementalEnabled)
	}

	t.Logf("✓ State preserved: lastEventID=%d, lastTaskUpdate=%d, incrementalEnabled=%v",
		daemon2.lastEventID, daemon2.lastTaskUpdate, daemon2.incrementalEnabled)
}
