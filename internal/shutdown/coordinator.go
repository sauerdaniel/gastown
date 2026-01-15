// Package shutdown provides coordinated graceful shutdown for Gas Town agents.
//
// The shutdown coordinator ensures that all agents are notified before being
// terminated, giving them time to complete their current work and clean up.
//
// Shutdown Protocol:
// 1. Initiator (gt down) calls BeginGracefulShutdown() to signal shutdown start
// 2. Daemon detects shutdown in progress via InProgress() and stops auto-restarting agents
// 3. Agents can check for shutdown via InProgress() and initiate their own cleanup
// 4. Initiator stops agents in dependency order after grace period
// 5. Initiator calls EndGracefulShutdown() to cleanup
package shutdown

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultGracePeriod is how long agents get to shut down gracefully.
	DefaultGracePeriod = 30 * time.Second

	// MinGracePeriod is the minimum allowed grace period.
	MinGracePeriod = 5 * time.Second

	// MaxGracePeriod is the maximum allowed grace period.
	MaxGracePeriod = 5 * time.Minute
)

// Shutdown state filenames
const (
	signalFile     = "shutdown.lock"
	coordinatorFile = "shutdown.coordinator"
)

// Coordinator manages graceful shutdown coordination.
type Coordinator struct {
	townRoot    string
	gracePeriod time.Duration
}

// New creates a new shutdown coordinator for the given town root.
func New(townRoot string) *Coordinator {
	return &Coordinator{
		townRoot:    townRoot,
		gracePeriod: DefaultGracePeriod,
	}
}

// SetGracePeriod sets the grace period for graceful shutdown.
// The grace period is clamped to [MinGracePeriod, MaxGracePeriod].
func (c *Coordinator) SetGracePeriod(d time.Duration) {
	if d < MinGracePeriod {
		c.gracePeriod = MinGracePeriod
	} else if d > MaxGracePeriod {
		c.gracePeriod = MaxGracePeriod
	} else {
		c.gracePeriod = d
	}
}

// GracePeriod returns the configured grace period.
func (c *Coordinator) GracePeriod() time.Duration {
	return c.gracePeriod
}

// shutdownDir returns the shutdown directory path.
func (c *Coordinator) shutdownDir() string {
	return filepath.Join(c.townRoot, "daemon")
}

// signalPath returns the shutdown signal file path.
func (c *Coordinator) signalPath() string {
	return filepath.Join(c.shutdownDir(), signalFile)
}

// coordinatorPath returns the coordinator state file path.
func (c *Coordinator) coordinatorPath() string {
	return filepath.Join(c.shutdownDir(), coordinatorFile)
}

// InProgress checks if a graceful shutdown is currently in progress.
// This is checked by the daemon to skip heartbeat (prevent auto-restart).
func InProgress(townRoot string) bool {
	signalPath := filepath.Join(townRoot, "daemon", signalFile)
	_, err := os.Stat(signalPath)
	return err == nil
}

// BeginGracefulShutdown signals the start of a graceful shutdown.
// It creates the shutdown signal file that agents can check.
//
// Returns an error if shutdown is already in progress or if creating
// the signal file fails.
func (c *Coordinator) BeginGracefulShutdown() error {
	// Ensure shutdown directory exists
	if err := os.MkdirAll(c.shutdownDir(), 0755); err != nil {
		return fmt.Errorf("creating shutdown directory: %w", err)
	}

	// Check if shutdown is already in progress
	if InProgress(c.townRoot) {
		return fmt.Errorf("shutdown already in progress")
	}

	// Create the shutdown signal file
	signalFile := c.signalPath()
	if err := os.WriteFile(signalFile, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644); err != nil {
		return fmt.Errorf("creating shutdown signal file: %w", err)
	}

	return nil
}

// EndGracefulShutdown completes the graceful shutdown process.
// It removes the shutdown signal file and coordinator state.
func (c *Coordinator) EndGracefulShutdown() error {
	// Remove the shutdown signal file
	signalFile := c.signalPath()
	if err := os.Remove(signalFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing shutdown signal file: %w", err)
	}

	// Remove the coordinator state file
	coordinatorFile := c.coordinatorPath()
	if err := os.Remove(coordinatorFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing coordinator state file: %w", err)
	}

	return nil
}

// GetStartTime returns when the shutdown was initiated.
// Returns zero time if shutdown is not in progress or time cannot be read.
func (c *Coordinator) GetStartTime() time.Time {
	data, err := os.ReadFile(c.signalPath())
	if err != nil {
		return time.Time{}
	}

	var timestamp int64
	if _, err := fmt.Sscanf(string(data), "%d", &timestamp); err != nil {
		return time.Time{}
	}

	return time.Unix(timestamp, 0)
}

// Elapsed returns how long since shutdown was initiated.
// Returns zero duration if shutdown is not in progress.
func (c *Coordinator) Elapsed() time.Duration {
	startTime := c.GetStartTime()
	if startTime.IsZero() {
		return 0
	}
	return time.Since(startTime)
}

// Remaining returns how much time is left in the grace period.
// Returns zero if shutdown is not in progress or grace period has expired.
func (c *Coordinator) Remaining() time.Duration {
	elapsed := c.Elapsed()
	if elapsed == 0 {
		return 0
	}
	if elapsed >= c.gracePeriod {
		return 0
	}
	return c.gracePeriod - elapsed
}
