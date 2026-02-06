// Package daemon provides the lifecycle management for Gas Town workers.
package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
)

// WorkerLifecycleManager manages the lifecycle of ephemeral workers (polecats).
// It handles:
// - Health monitoring (detect crashed/stale workers)
// - Automatic crash recovery (respawn on same hook)
// - Cleanup of completed workers
// - Hook state persistence across crashes
type WorkerLifecycleManager struct {
	townRoot      string
	rigsConfig    *config.RigsConfig
	tmux          *tmux.Tmux
	checkInterval time.Duration
	staleThreshold time.Duration
	verbose       bool
}

// NewWorkerLifecycleManager creates a new worker lifecycle manager.
func NewWorkerLifecycleManager(townRoot string, rigsConfig *config.RigsConfig, checkInterval, staleThreshold time.Duration) *WorkerLifecycleManager {
	return &WorkerLifecycleManager{
		townRoot:       townRoot,
		rigsConfig:     rigsConfig,
		tmux:           tmux.NewTmux(),
		checkInterval:  checkInterval,
		staleThreshold: staleThreshold,
		verbose:        os.Getenv("GT_LIFECYCLE_DEBUG") == "1",
	}
}

// Run starts the lifecycle management loop.
func (wlm *WorkerLifecycleManager) Run(ctx context.Context) error {
	wlm.log("Worker lifecycle manager starting (check interval: %v, stale threshold: %v)", wlm.checkInterval, wlm.staleThreshold)

	ticker := time.NewTicker(wlm.checkInterval)
	defer ticker.Stop()

	// Run initial check immediately
	wlm.runCycle()

	for {
		select {
		case <-ticker.C:
			wlm.runCycle()
		case <-ctx.Done():
			wlm.log("Worker lifecycle manager shutting down")
			return ctx.Err()
		}
	}
}

// runCycle executes one lifecycle management cycle.
func (wlm *WorkerLifecycleManager) runCycle() {
	wlm.log("Running lifecycle check cycle")

	// Check health and recover stale workers
	recovered := wlm.checkHealthAndRecover()

	// Clean up completed workers
	cleaned := wlm.cleanupCompleted()

	if recovered > 0 || cleaned > 0 {
		wlm.log("Cycle complete: recovered %d, cleaned %d", recovered, cleaned)
	}
}

// checkHealthAndRecover checks worker health and recovers stale/crashed workers.
// Returns the number of workers recovered.
func (wlm *WorkerLifecycleManager) checkHealthAndRecover() int {
	recovered := 0

	g := git.NewGit(wlm.townRoot)
	rigMgr := rig.NewManager(wlm.townRoot, wlm.rigsConfig, g)

	// Get all rigs
	rigNames := wlm.rigsConfig.RigNames()

	for _, rigName := range rigNames {
		r, err := rigMgr.GetRig(rigName)
		if err != nil {
			wlm.log("Error getting rig %s: %v", rigName, err)
			continue
		}

		// Create polecat manager
		polecatGit := git.NewGit(r.Path)
		polecatMgr := polecat.NewManager(r, polecatGit, wlm.tmux)

		// Get all polecats
		polecats, err := polecatMgr.List()
		if err != nil {
			wlm.log("Error listing polecats for rig %s: %v", rigName, err)
			continue
		}

		// Check each polecat for staleness
		for _, p := range polecats {
			if wlm.isStale(r, p) {
				wlm.log("Detected stale polecat: %s/%s (state=%s)", rigName, p.Name, p.State)

				// Attempt recovery
				if err := wlm.recoverPolecat(r, polecatMgr, p); err != nil {
					wlm.log("Failed to recover polecat %s/%s: %v", rigName, p.Name, err)
				} else {
					wlm.log("Successfully recovered polecat %s/%s", rigName, p.Name)
					recovered++
				}
			}
		}
	}

	return recovered
}

// isStale checks if a polecat is stale (crashed or needs recovery).
// A polecat is stale if:
// - It has work assigned (Issue != "") AND
// - Its tmux session doesn't exist OR
// - Its agent state indicates a crash (working but no recent activity)
func (wlm *WorkerLifecycleManager) isStale(r *rig.Rig, p *polecat.Polecat) bool {
	// No work assigned = not stale (might be cleaned up separately)
	if p.Issue == "" {
		return false
	}

	// Check if tmux session exists
	sessionName := fmt.Sprintf("gt-%s-%s", r.Name, p.Name)
	hasSession, _ := wlm.tmux.HasSession(sessionName)

	// No session but has work = stale (crashed)
	if !hasSession {
		return true
	}

	// Session exists - check for stuck state via agent bead
	agentID := fmt.Sprintf("%s/polecats/%s", r.Name, p.Name)
	resolvedBeads := beads.ResolveBeadsDir(r.Path)
	beadsPath := filepath.Dir(resolvedBeads)
	bd := beads.NewWithBeadsDir(beadsPath, resolvedBeads)

	_, fields, err := bd.GetAgentBead(agentID)
	if err != nil {
		// Can't read agent bead - assume not stale
		return false
	}

	// Check for explicitly stuck states
	if fields.AgentState == "stuck" || fields.AgentState == "awaiting-gate" {
		return false // These are intentional pauses, not crashes
	}

	// TODO: Could add time-based staleness (no commits in last N minutes while state=working)
	// For now, rely on session existence check

	return false
}

// recoverPolecat attempts to recover a stale polecat by respawning it with the same hook.
func (wlm *WorkerLifecycleManager) recoverPolecat(r *rig.Rig, polecatMgr *polecat.Manager, p *polecat.Polecat) error {
	wlm.log("Recovering polecat %s/%s with hook_bead=%s", r.Name, p.Name, p.Issue)

	// Get the hook bead from the agent bead (more reliable than Issue field)
	agentID := fmt.Sprintf("%s/polecats/%s", r.Name, p.Name)
	resolvedBeads := beads.ResolveBeadsDir(r.Path)
	beadsPath := filepath.Dir(resolvedBeads)
	bd := beads.NewWithBeadsDir(beadsPath, resolvedBeads)

	_, fields, err := bd.GetAgentBead(agentID)
	if err != nil {
		return fmt.Errorf("reading agent bead: %w", err)
	}

	hookBead := fields.HookBead
	if hookBead == "" {
		hookBead = p.Issue // Fallback to Issue field if hook_bead not set
	}

	if hookBead == "" {
		return fmt.Errorf("no hook_bead found for recovery")
	}

	// Kill stale session if it exists
	sessionName := fmt.Sprintf("gt-%s-%s", r.Name, p.Name)
	_ = wlm.tmux.KillSessionWithProcesses(sessionName)

	// Repair the worktree (creates fresh from latest origin, preserves identity)
	opts := polecat.AddOptions{
		HookBead: hookBead, // Preserve hook assignment
	}

	polecat, err := polecatMgr.RepairWorktreeWithOptions(p.Name, true, opts)
	if err != nil {
		return fmt.Errorf("repairing worktree: %w", err)
	}

	// Start the session
	accountsPath := filepath.Join(wlm.townRoot, "mayor", "accounts")
	claudeConfigDir, _, err := config.ResolveAccountConfigDir(accountsPath, "")
	if err != nil {
		return fmt.Errorf("resolving account: %w", err)
	}

	polecatSessMgr := polecat.NewSessionManager(wlm.tmux, r)
	startOpts := polecat.SessionStartOptions{
		RuntimeConfigDir: claudeConfigDir,
	}

	if err := polecatSessMgr.Start(p.Name, startOpts); err != nil {
		return fmt.Errorf("starting session: %w", err)
	}

	// Update agent state to working
	if err := polecatMgr.SetAgentState(p.Name, "working"); err != nil {
		wlm.log("Warning: could not update agent state: %v", err)
	}

	// Update issue status to in_progress if it was stuck in hooked state
	status := "in_progress"
	if err := bd.Update(hookBead, beads.UpdateOptions{Status: &status}); err != nil {
		wlm.log("Warning: could not update issue status: %v", err)
	}

	wlm.log("Polecat %s/%s recovered successfully", r.Name, polecat.Name)
	return nil
}

// cleanupCompleted removes polecats that have finished their work.
// Returns the number of polecats cleaned up.
func (wlm *WorkerLifecycleManager) cleanupCompleted() int {
	cleaned := 0

	g := git.NewGit(wlm.townRoot)
	rigMgr := rig.NewManager(wlm.townRoot, wlm.rigsConfig, g)

	rigNames := wlm.rigsConfig.RigNames()

	for _, rigName := range rigNames {
		r, err := rigMgr.GetRig(rigName)
		if err != nil {
			continue
		}

		polecatGit := git.NewGit(r.Path)
		polecatMgr := polecat.NewManager(r, polecatGit, wlm.tmux)

		polecats, err := polecatMgr.List()
		if err != nil {
			continue
		}

		for _, p := range polecats {
			if wlm.shouldCleanup(r, p) {
				wlm.log("Cleaning up completed polecat: %s/%s", rigName, p.Name)

				// Remove the polecat (with safety checks)
				if err := polecatMgr.Remove(p.Name, false); err != nil {
					wlm.log("Failed to cleanup polecat %s/%s: %v", rigName, p.Name, err)
				} else {
					wlm.log("Successfully cleaned up polecat %s/%s", rigName, p.Name)
					cleaned++
				}
			}
		}
	}

	return cleaned
}

// shouldCleanup determines if a polecat should be cleaned up.
// A polecat should be cleaned up if:
// - State is Done (no work assigned) AND
// - cleanup_status is "clean" (self-reported safe) AND
// - No active tmux session
func (wlm *WorkerLifecycleManager) shouldCleanup(r *rig.Rig, p *polecat.Polecat) bool {
	// Only cleanup if state is Done (no work assigned)
	if p.State != polecat.StateDone {
		return false
	}

	// Check tmux session status
	sessionName := fmt.Sprintf("gt-%s-%s", r.Name, p.Name)
	hasSession, _ := wlm.tmux.HasSession(sessionName)

	// Don't cleanup if session is still running
	if hasSession {
		return false
	}

	// Check cleanup_status from agent bead (ZFC self-report)
	agentID := fmt.Sprintf("%s/polecats/%s", r.Name, p.Name)
	resolvedBeads := beads.ResolveBeadsDir(r.Path)
	beadsPath := filepath.Dir(resolvedBeads)
	bd := beads.NewWithBeadsDir(beadsPath, resolvedBeads)

	_, fields, err := bd.GetAgentBead(agentID)
	if err != nil {
		// Can't read cleanup status - be conservative, don't cleanup
		return false
	}

	cleanupStatus := polecat.CleanupStatus(fields.CleanupStatus)

	// Only cleanup if explicitly reported as clean
	if cleanupStatus != polecat.CleanupClean && cleanupStatus != polecat.CleanupUnknown {
		wlm.log("Polecat %s/%s has cleanup_status=%s, skipping cleanup", r.Name, p.Name, cleanupStatus)
		return false
	}

	return true
}

// log logs a message if verbose mode is enabled.
func (wlm *WorkerLifecycleManager) log(format string, args ...interface{}) {
	if wlm.verbose {
		timestamp := time.Now().Format("15:04:05")
		prefix := style.Subtle.Render(fmt.Sprintf("[lifecycle %s]", timestamp))
		fmt.Printf("%s %s\n", prefix, fmt.Sprintf(format, args...))
	}
}

// LifecycleConfig holds configuration for worker lifecycle management.
type LifecycleConfig struct {
	Enabled          bool          `json:"enabled"`           // Enable lifecycle management
	AutoCleanup      bool          `json:"auto_cleanup"`      // Automatically cleanup completed workers
	AutoRecovery     bool          `json:"auto_recovery"`     // Automatically recover crashed workers
	CheckInterval    time.Duration `json:"check_interval"`    // How often to check worker health
	StaleThreshold   time.Duration `json:"stale_threshold"`   // How long before considering a worker stale
	MaxPolecats      int           `json:"max_polecats"`      // Maximum polecats per rig (0 = unlimited)
}

// DefaultLifecycleConfig returns the default lifecycle configuration.
func DefaultLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		Enabled:        true,
		AutoCleanup:    true,
		AutoRecovery:   true,
		CheckInterval:  30 * time.Second,
		StaleThreshold: 5 * time.Minute,
		MaxPolecats:    10,
	}
}

// LoadLifecycleConfig loads lifecycle configuration from a rig's settings.
func LoadLifecycleConfig(rigPath string) LifecycleConfig {
	settingsPath := filepath.Join(rigPath, "settings", "config.json")
	settings, err := config.LoadRigSettings(settingsPath)
	if err != nil {
		return DefaultLifecycleConfig()
	}

	cfg := DefaultLifecycleConfig()

	// Parse lifecycle settings from rig config
	if settings.Lifecycle != nil {
		if enabled, ok := settings.Lifecycle["enabled"].(bool); ok {
			cfg.Enabled = enabled
		}
		if autoCleanup, ok := settings.Lifecycle["auto_cleanup"].(bool); ok {
			cfg.AutoCleanup = autoCleanup
		}
		if autoRecovery, ok := settings.Lifecycle["auto_recovery"].(bool); ok {
			cfg.AutoRecovery = autoRecovery
		}
		if interval, ok := settings.Lifecycle["check_interval"].(string); ok {
			if d, err := time.ParseDuration(interval); err == nil {
				cfg.CheckInterval = d
			}
		}
		if threshold, ok := settings.Lifecycle["stale_threshold"].(string); ok {
			if d, err := time.ParseDuration(threshold); err == nil {
				cfg.StaleThreshold = d
			}
		}
		if max, ok := settings.Lifecycle["max_polecats"].(float64); ok {
			cfg.MaxPolecats = int(max)
		}
	}

	return cfg
}

// GetPolecatSessionName returns the tmux session name for a polecat.
func GetPolecatSessionName(rigName, polecatName string) string {
	return fmt.Sprintf("gt-%s-%s", rigName, polecatName)
}

// ParsePolecatSessionName parses a tmux session name into rig and polecat names.
// Returns empty strings if the session name doesn't match the expected format.
func ParsePolecatSessionName(sessionName string) (rigName, polecatName string) {
	// Expected format: gt-<rig>-<polecat>
	// Example: gt-gastown-polecat-01 or gt-gastown-toast

	if !strings.HasPrefix(sessionName, "gt-") {
		return "", ""
	}

	parts := strings.SplitN(sessionName, "-", 3)
	if len(parts) < 3 {
		return "", ""
	}

	return parts[1], parts[2]
}
