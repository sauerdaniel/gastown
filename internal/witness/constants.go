package witness

import "time"

// Heartbeat timeout constants (P1: oc-hyor)
const (
	// MinHeartbeatTimeout is the minimum allowed heartbeat timeout in seconds.
	// Workers must heartbeat at least once per minute.
	MinHeartbeatTimeout = 60

	// MaxHeartbeatTimeout is the maximum allowed heartbeat timeout in seconds.
	// Workers must heartbeat at least once per hour.
	MaxHeartbeatTimeout = 3600

	// DefaultHeartbeatTimeout is the default timeout when not specified.
	DefaultHeartbeatTimeout = 180 // 3 minutes
)

// Health check timing constants
const (
	// StaleMultiplier determines when a worker is stale (timeout * multiplier)
	StaleMultiplier = 1.0

	// DeadMultiplier determines when a worker is dead (timeout * multiplier)
	DeadMultiplier = 2.0
)

// Timeouts for various operations
const (
	// DefaultCleanupTimeout is the default timeout for cleanup operations
	DefaultCleanupTimeout = 5 * time.Minute

	// DefaultNukeTimeout is the timeout for nuking a polecat worktree
	DefaultNukeTimeout = 2 * time.Minute

	// DefaultGitOperationTimeout is the timeout for git operations
	DefaultGitOperationTimeout = 30 * time.Second
)
