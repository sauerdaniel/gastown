package projection

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// BenchmarkSyncTasksFull benchmarks full task sync with varying dataset sizes.
func BenchmarkSyncTasksFull(b *testing.B) {
	benchmarkSyncTasksFullWithSize(b, 100, "100-tasks")
	benchmarkSyncTasksFullWithSize(b, 1000, "1k-tasks")
	benchmarkSyncTasksFullWithSize(b, 5000, "5k-tasks")
}

func benchmarkSyncTasksFullWithSize(b *testing.B, taskCount int, label string) {
	b.Run(label, func(b *testing.B) {
		tempDir := b.TempDir()
		beadsDBPath := filepath.Join(tempDir, "beads_bench.db")
		projDBPath := filepath.Join(tempDir, "proj_bench.db")

		// Setup databases with task dataset
		beadsDB, err := setupBenchBeadsDBWithTasks(beadsDBPath, taskCount)
		if err != nil {
			b.Fatalf("Failed to setup Beads DB: %v", err)
		}
		defer beadsDB.Close()

		projDB, err := setupTestProjDB(projDBPath)
		if err != nil {
			b.Fatalf("Failed to setup Proj DB: %v", err)
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

		// Reset timer to exclude setup time
		b.ResetTimer()

		// Run benchmark
		for i := 0; i < b.N; i++ {
			// Clear projection tasks before each iteration
			_, _ = projDB.Exec("DELETE FROM tasks")

			if err := daemon.syncTasks(beadsDB, projDB); err != nil {
				b.Fatalf("syncTasks failed: %v", err)
			}
		}

		// Report metrics
		b.StopTimer()
		if b.N > 0 {
			avgNs := b.Elapsed().Nanoseconds() / int64(b.N)
			b.ReportMetric(float64(taskCount), "tasks")
			b.ReportMetric(float64(avgNs)/1e6, "ms-per-iteration")
		}
	})
}

// BenchmarkSyncTasksIncremental benchmarks incremental task sync with varying dirty set sizes.
func BenchmarkSyncTasksIncremental(b *testing.B) {
	benchmarkSyncTasksIncrementalWithSize(b, 1000, 50, "1k-tasks-5%-dirty")
	benchmarkSyncTasksIncrementalWithSize(b, 1000, 100, "1k-tasks-10%-dirty")
	benchmarkSyncTasksIncrementalWithSize(b, 5000, 50, "5k-tasks-1%-dirty")
	benchmarkSyncTasksIncrementalWithSize(b, 5000, 250, "5k-tasks-5%-dirty")
}

func benchmarkSyncTasksIncrementalWithSize(b *testing.B, totalTasks, dirtyTasks int, label string) {
	b.Run(label, func(b *testing.B) {
		tempDir := b.TempDir()
		beadsDBPath := filepath.Join(tempDir, "beads_bench.db")
		projDBPath := filepath.Join(tempDir, "proj_bench.db")

		// Setup databases with task dataset
		beadsDB, err := setupBenchBeadsDBWithTasksAndDirtyIssues(beadsDBPath, totalTasks, dirtyTasks)
		if err != nil {
			b.Fatalf("Failed to setup Beads DB: %v", err)
		}
		defer beadsDB.Close()

		projDB, err := setupTestProjDB(projDBPath)
		if err != nil {
			b.Fatalf("Failed to setup Proj DB: %v", err)
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

		// Reset timer to exclude setup time
		b.ResetTimer()

		// Run benchmark
		for i := 0; i < b.N; i++ {
			// Re-populate dirty_issues before each iteration
			_, _ = beadsDB.Exec("DELETE FROM dirty_issues")
			for j := 0; j < dirtyTasks; j++ {
				taskID := fmt.Sprintf("oc-bench-%d", j)
				_, _ = beadsDB.Exec(
					"INSERT INTO dirty_issues (issue_id, updated_at) VALUES (?, ?)",
					taskID, time.Now().UnixMilli(),
				)
			}

			if err := daemon.syncTasksIncremental(beadsDB, projDB); err != nil {
				b.Fatalf("syncTasksIncremental failed: %v", err)
			}
		}

		// Report metrics
		b.StopTimer()
		if b.N > 0 {
			avgNs := b.Elapsed().Nanoseconds() / int64(b.N)
			b.ReportMetric(float64(totalTasks), "total-tasks")
			b.ReportMetric(float64(dirtyTasks), "dirty-tasks")
			b.ReportMetric(float64(avgNs)/1e6, "ms-per-iteration")
		}
	})
}

// BenchmarkSyncActivityFull benchmarks full activity sync with varying dataset sizes.
func BenchmarkSyncActivityFull(b *testing.B) {
	benchmarkSyncActivityFullWithSize(b, 100, "100-events")
	benchmarkSyncActivityFullWithSize(b, 1000, "1k-events")
	benchmarkSyncActivityFullWithSize(b, 5000, "5k-events")
}

func benchmarkSyncActivityFullWithSize(b *testing.B, eventCount int, label string) {
	b.Run(label, func(b *testing.B) {
		tempDir := b.TempDir()
		beadsDBPath := filepath.Join(tempDir, "beads_bench.db")
		projDBPath := filepath.Join(tempDir, "proj_bench.db")

		// Setup databases with events
		beadsDB, err := setupBenchBeadsDBWithEvents(beadsDBPath, eventCount)
		if err != nil {
			b.Fatalf("Failed to setup Beads DB: %v", err)
		}
		defer beadsDB.Close()

		projDB, err := setupTestProjDB(projDBPath)
		if err != nil {
			b.Fatalf("Failed to setup Proj DB: %v", err)
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

		// Reset timer to exclude setup time
		b.ResetTimer()

		// Run benchmark
		for i := 0; i < b.N; i++ {
			// Clear projection activities before each iteration
			_, _ = projDB.Exec("DELETE FROM activities")
			daemon.lastEventID = 0 // Reset for full sync

			if err := daemon.syncActivity(beadsDB, projDB); err != nil {
				b.Fatalf("syncActivity failed: %v", err)
			}
		}

		// Report metrics
		b.StopTimer()
		if b.N > 0 {
			avgNs := b.Elapsed().Nanoseconds() / int64(b.N)
			b.ReportMetric(float64(eventCount), "events")
			b.ReportMetric(float64(avgNs)/1e6, "ms-per-iteration")
		}
	})
}

// BenchmarkSyncActivityIncremental benchmarks incremental activity sync with varying event volumes.
func BenchmarkSyncActivityIncremental(b *testing.B) {
	benchmarkSyncActivityIncrementalWithSize(b, 1000, 10, "1k-events-1%-new")
	benchmarkSyncActivityIncrementalWithSize(b, 1000, 50, "1k-events-5%-new")
	benchmarkSyncActivityIncrementalWithSize(b, 5000, 25, "5k-events-0.5%-new")
	benchmarkSyncActivityIncrementalWithSize(b, 5000, 250, "5k-events-5%-new")
}

func benchmarkSyncActivityIncrementalWithSize(b *testing.B, totalEvents, newEvents int, label string) {
	b.Run(label, func(b *testing.B) {
		tempDir := b.TempDir()
		beadsDBPath := filepath.Join(tempDir, "beads_bench.db")
		projDBPath := filepath.Join(tempDir, "proj_bench.db")

		// Setup databases with events (but only track some)
		beadsDB, err := setupBenchBeadsDBWithEvents(beadsDBPath, totalEvents)
		if err != nil {
			b.Fatalf("Failed to setup Beads DB: %v", err)
		}
		defer beadsDB.Close()

		projDB, err := setupTestProjDB(projDBPath)
		if err != nil {
			b.Fatalf("Failed to setup Proj DB: %v", err)
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

		// Simulate prior syncs: set lastEventID to skip most events
		daemon.lastEventID = int64(totalEvents - newEvents - 1)

		// Reset timer to exclude setup time
		b.ResetTimer()

		// Run benchmark
		for i := 0; i < b.N; i++ {
			if err := daemon.syncActivityIncremental(beadsDB, projDB); err != nil {
				b.Fatalf("syncActivityIncremental failed: %v", err)
			}
		}

		// Report metrics
		b.StopTimer()
		if b.N > 0 {
			avgNs := b.Elapsed().Nanoseconds() / int64(b.N)
			b.ReportMetric(float64(totalEvents), "total-events")
			b.ReportMetric(float64(newEvents), "new-events")
			b.ReportMetric(float64(avgNs)/1e6, "ms-per-iteration")
		}
	})
}

// BenchmarkSyncComments benchmarks comment sync with varying comment volumes.
func BenchmarkSyncComments(b *testing.B) {
	benchmarkSyncCommentsWithSize(b, 100, "100-comments")
	benchmarkSyncCommentsWithSize(b, 500, "500-comments")
	benchmarkSyncCommentsWithSize(b, 1000, "1k-comments")
}

func benchmarkSyncCommentsWithSize(b *testing.B, commentCount int, label string) {
	b.Run(label, func(b *testing.B) {
		tempDir := b.TempDir()
		beadsDBPath := filepath.Join(tempDir, "beads_bench.db")
		projDBPath := filepath.Join(tempDir, "proj_bench.db")

		// Setup databases with comments
		beadsDB, err := setupBenchBeadsDBWithComments(beadsDBPath, commentCount)
		if err != nil {
			b.Fatalf("Failed to setup Beads DB: %v", err)
		}
		defer beadsDB.Close()

		projDB, err := setupTestProjDB(projDBPath)
		if err != nil {
			b.Fatalf("Failed to setup Proj DB: %v", err)
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

		// Reset timer to exclude setup time
		b.ResetTimer()

		// Run benchmark
		for i := 0; i < b.N; i++ {
			// Clear projection comments before each iteration
			_, _ = projDB.Exec("DELETE FROM task_comments")

			if err := daemon.syncComments(beadsDB, projDB); err != nil {
				b.Fatalf("syncComments failed: %v", err)
			}
		}

		// Report metrics
		b.StopTimer()
		if b.N > 0 {
			avgNs := b.Elapsed().Nanoseconds() / int64(b.N)
			b.ReportMetric(float64(commentCount), "comments")
			b.ReportMetric(float64(avgNs)/1e6, "ms-per-iteration")
		}
	})
}

// Helper functions for benchmark setup

// setupBenchBeadsDBWithTasks creates a Beads DB with specified number of tasks.
func setupBenchBeadsDBWithTasks(path string, taskCount int) (*sql.DB, error) {
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
			rig TEXT,
			deleted_at DATETIME
		)
	`); err != nil {
		return nil, err
	}

	// Insert tasks
	now := time.Now()
	for i := 0; i < taskCount; i++ {
		if _, err := db.Exec(
			`INSERT INTO issues (id, title, description, status, priority, issue_type, assignee, owner, created_at, updated_at, external_ref, rig)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("oc-bench-%d", i),
			fmt.Sprintf("Task %d", i),
			fmt.Sprintf("Description for task %d", i),
			"open",
			2,
			"task",
			"user123",
			"owner456",
			now,
			now,
			"",
			"",
		); err != nil {
			return nil, err
		}
	}

	return db, nil
}

// setupBenchBeadsDBWithTasksAndDirtyIssues creates a Beads DB with tasks and dirty_issues tracking.
func setupBenchBeadsDBWithTasksAndDirtyIssues(path string, totalTasks, dirtyCount int) (*sql.DB, error) {
	db, err := setupBenchBeadsDBWithTasks(path, totalTasks)
	if err != nil {
		return nil, err
	}

	// Create dirty_issues table
	if _, err := db.Exec(`
		CREATE TABLE dirty_issues (
			issue_id TEXT PRIMARY KEY,
			updated_at INTEGER
		)
	`); err != nil {
		return nil, err
	}

	// Insert dirty issue entries
	now := time.Now().UnixMilli()
	for i := 0; i < dirtyCount; i++ {
		if _, err := db.Exec(
			`INSERT INTO dirty_issues (issue_id, updated_at) VALUES (?, ?)`,
			fmt.Sprintf("oc-bench-%d", i),
			now,
		); err != nil {
			return nil, err
		}
	}

	return db, nil
}

// setupBenchBeadsDBWithEvents creates a Beads DB with specified number of events.
func setupBenchBeadsDBWithEvents(path string, eventCount int) (*sql.DB, error) {
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
			rig TEXT,
			deleted_at DATETIME
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

	// Insert test issue with all fields
	now := time.Now()
	if _, err := db.Exec(
		`INSERT INTO issues (id, title, description, status, priority, issue_type, assignee, owner, created_at, updated_at, external_ref, rig)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"oc-test-1", "Test Issue", "This is a test issue", "open", 2, "task", "user123", "owner456", now, now, "", "",
	); err != nil {
		return nil, err
	}

	// Insert events
	for i := 0; i < eventCount; i++ {
		if _, err := db.Exec(
			`INSERT INTO events (issue_id, event_type, actor, old_value, new_value, comment, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"oc-test-1",
			"update",
			"user123",
			"old",
			"new",
			fmt.Sprintf("Comment %d", i),
			now.Add(time.Duration(i)*time.Second),
		); err != nil {
			return nil, err
		}
	}

	return db, nil
}

// setupBenchBeadsDBWithComments creates a Beads DB with specified number of comments.
func setupBenchBeadsDBWithComments(path string, commentCount int) (*sql.DB, error) {
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
			rig TEXT,
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

	// Insert test issue with all fields
	now := time.Now()
	if _, err := db.Exec(
		`INSERT INTO issues (id, title, description, status, priority, issue_type, assignee, owner, created_at, updated_at, external_ref, rig)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"oc-test-1", "Test Issue", "This is a test issue", "open", 2, "task", "user123", "owner456", now, now, "", "",
	); err != nil {
		return nil, err
	}

	// Insert comments
	for i := 0; i < commentCount; i++ {
		if _, err := db.Exec(
			`INSERT INTO comments (issue_id, author, text, created_at) VALUES (?, ?, ?, ?)`,
			"oc-test-1",
			fmt.Sprintf("user%d", i),
			fmt.Sprintf("This is comment number %d with some content", i),
			now.Add(time.Duration(i)*time.Second),
		); err != nil {
			return nil, err
		}
	}

	return db, nil
}
