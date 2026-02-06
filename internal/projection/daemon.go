// Package projection provides daemon lifecycle management for projection sync.
package projection

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// State represents the daemon's persistent state.
type State struct {
	Running    bool      `json:"running"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	LastSync   time.Time `json:"last_sync"`
	SyncCount  int       `json:"sync_count"`
	ErrorCount int       `json:"error_count"`
}

var (
	pidFile      = "projection-sync.pid"
	lockFile     = "projection-sync.lock"
	stateFile    = "projection-sync.state"
	logFile      = "projection-sync.log"
)

// DefaultConfig creates a default configuration for the daemon.
func DefaultConfig(townRoot string, pollInterval time.Duration) Config {
	beadsDBPath := filepath.Join(townRoot, "..", "beads", ".beads", "beads.db")
	projDBPath := filepath.Join(townRoot, "cache", "projections.db")
	cacheDir := filepath.Join(townRoot, "cache")
	daemonDir := filepath.Join(townRoot, "daemon")

	return Config{
		BeadsDBPath:  beadsDBPath,
		ProjDBPath:   projDBPath,
		CacheDir:     cacheDir,
		PollInterval: pollInterval,
		Logger:      setupLogger(filepath.Join(daemonDir, logFile)),
	}
}

// IsRunning checks if the daemon is currently running.
func IsRunning(townRoot string) (bool, int, error) {
	daemonDir := filepath.Join(townRoot, "daemon")

	pidFile := filepath.Join(daemonDir, pidFile)
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}

	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		return false, 0, fmt.Errorf("parsing PID file: %w", err)
	}

	// Check if process exists
	if processExists(pid) {
		return true, pid, nil
	}

	// PID file exists but process is dead - stale
	_ = os.Remove(pidFile)
	return false, 0, nil
}

// StopDaemon stops a running daemon.
func StopDaemon(townRoot string) error {
	running, pid, err := IsRunning(townRoot)
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("daemon is not running")
	}

	// Send SIGTERM
	if err := terminateProcess(pid); err != nil {
		return fmt.Errorf("terminating process %d: %w", pid, err)
	}

	// Remove PID file
	daemonDir := filepath.Join(townRoot, "daemon")
	pidFile := filepath.Join(daemonDir, pidFile)
	_ = os.Remove(pidFile)

	return nil
}

// LoadState loads the daemon state from disk.
func LoadState(townRoot string) (*State, error) {
	daemonDir := filepath.Join(townRoot, "daemon")
	stateFile := filepath.Join(daemonDir, stateFile)

	stateBytes, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, err
	}

	var state State
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	return &state, nil
}

// SaveState saves the daemon state to disk.
func (d *SyncDaemon) SaveState() error {
	townRoot := d.cacheDir // Cache dir is inside town root
	for len(townRoot) > 0 && filepath.Base(townRoot) != "cache" {
		townRoot = filepath.Dir(townRoot)
	}
	townRoot = filepath.Dir(townRoot)

	daemonDir := filepath.Join(townRoot, "daemon")
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		return err
	}

	stateFile := filepath.Join(daemonDir, stateFile)

	d.mu.RLock()
	state := State{
		Running:    true,
		PID:        os.Getpid(),
		StartedAt:  time.Now(), // Simplified - should track actual start time
		LastSync:   d.lastSync,
		SyncCount:  d.syncCount,
		ErrorCount: d.errorCount,
	}
	d.mu.RUnlock()

	stateBytes, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return os.WriteFile(stateFile, stateBytes, 0644)
}

// setupLogger creates a file-based logger.
func setupLogger(logFile string) *log.Logger {
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		return log.New(os.Stderr, "[projection-sync] ", log.LstdFlags)
	}

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return log.New(os.Stderr, "[projection-sync] ", log.LstdFlags)
	}

	return log.New(file, "[projection-sync] ", log.LstdFlags)
}

// processExists checks if a process with the given PID exists.
func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists
	err = process.Signal(os.Signal(nil))
	if err != nil {
		return false
	}

	return true
}

// terminateProcess sends SIGTERM to a process.
func terminateProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	return process.Signal(os.Interrupt)
}
