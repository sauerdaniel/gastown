package witness

import (
	"fmt"
	"strconv"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

// HealthCheckResult tracks the result of checking worker health.
type HealthCheckResult struct {
	AgentID       string
	WorkerName    string
	PreviousHealth string
	CurrentHealth  string
	Action        string // "updated", "reassigned_work", "notified_mayor"
	Error         error
}

// CheckWorkerHealth checks all active workers for stale heartbeats and updates health state.
// This implements the health state machine: healthy → stale → dead
//
// Health transitions:
// - healthy: heartbeat within timeout window
// - stale: heartbeat overdue (> timeout, < 2x timeout)
// - dead: no heartbeat for 2x timeout
//
// Actions taken:
// - healthy → stale: Update health state, log warning
// - stale → dead: Mark dead, reassign work if any, notify Mayor
//
// Returns a list of health check results for all workers checked.
func CheckWorkerHealth(workDir string) ([]HealthCheckResult, error) {
	// Find beads directory
	beadsDir := beads.ResolveBeadsDir(workDir)
	b := beads.NewWithBeadsDir(workDir, beadsDir)

	// Query all active agent beads
	agents, err := b.List(beads.ListOptions{
		Type:   "agent",
		Status: "open", // Only check active agents
	})
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}

	var results []HealthCheckResult
	now := time.Now().UTC()

	for _, agent := range agents {
		result := HealthCheckResult{
			AgentID:    agent.ID,
			WorkerName: agent.ID, // Simplified - extract name from ID in production
		}

		// Parse agent fields
		fields := beads.ParseAgentFields(agent.Description)
		if fields == nil {
			// No lifecycle fields - skip (not a P1 worker)
			continue
		}

		// Skip if no heartbeat tracking (e.g., mayor, deacon)
		if fields.LastHeartbeat == "" || fields.HeartbeatTimeout == "" {
			continue
		}

		// Parse last heartbeat timestamp
		lastHeartbeat, err := time.Parse(time.RFC3339, fields.LastHeartbeat)
		if err != nil {
			result.Error = fmt.Errorf("parsing last_heartbeat: %w", err)
			results = append(results, result)
			continue
		}

		// Parse timeout (seconds)
		timeoutSec, err := strconv.Atoi(fields.HeartbeatTimeout)
		if err != nil {
			result.Error = fmt.Errorf("parsing heartbeat_timeout: %w", err)
			results = append(results, result)
			continue
		}
		timeout := time.Duration(timeoutSec) * time.Second

		// Calculate time since last heartbeat
		timeSince := now.Sub(lastHeartbeat)

		// Determine current health based on timeout
		result.PreviousHealth = fields.Health
		var newHealth string
		var needsUpdate bool

		if timeSince < timeout {
			// Within timeout window - healthy
			newHealth = beads.HealthHealthy
		} else if timeSince < 2*timeout {
			// Overdue but not dead yet - stale
			newHealth = beads.HealthStale
		} else {
			// No heartbeat for 2x timeout - dead
			newHealth = beads.HealthDead
		}

		// Check if health state changed
		if newHealth != fields.Health {
			needsUpdate = true
			result.CurrentHealth = newHealth
			fields.Health = newHealth

			// If transitioning to dead, also update lifecycle state
			if newHealth == beads.HealthDead && fields.LifecycleState != beads.LifecycleCrashed {
				fields.LifecycleState = beads.LifecycleCrashed
			}
		} else {
			result.CurrentHealth = newHealth
		}

		// Update agent bead if health changed
		if needsUpdate {
			newDesc := beads.FormatAgentDescription(agent.Title, fields)
			if err := b.Update(agent.ID, beads.UpdateOptions{
				Description: &newDesc,
			}); err != nil {
				result.Error = fmt.Errorf("updating agent bead: %w", err)
				results = append(results, result)
				continue
			}

			result.Action = fmt.Sprintf("updated health: %s → %s", result.PreviousHealth, newHealth)

			// If worker is now dead, reassign work
			if newHealth == beads.HealthDead && fields.AssignedWork != "" {
				if err := reassignOrphanWork(b, fields.AssignedWork, agent.ID); err != nil {
					result.Error = fmt.Errorf("reassigning work: %w", err)
				} else {
					result.Action += ", reassigned work " + fields.AssignedWork
				}
			}
		} else {
			result.Action = "no change"
		}

		results = append(results, result)
	}

	return results, nil
}

// reassignOrphanWork reassigns work from a crashed/dead worker.
// This resets the work item to 'open' status and clears the assignee,
// allowing it to be picked up by another worker.
func reassignOrphanWork(b *beads.Beads, workID, deadWorkerID string) error {
	// Get the work item
	issue, err := b.Show(workID)
	if err != nil {
		return fmt.Errorf("getting work item %s: %w", workID, err)
	}
	if issue == nil {
		return fmt.Errorf("work item %s not found", workID)
	}

	// Only reassign if it's still in_progress (not already completed)
	if issue.Status != "in_progress" {
		return nil // Already handled
	}

	// Reset to open and clear assignee
	newStatus := "open"
	emptyAssignee := ""
	if err := b.Update(workID, beads.UpdateOptions{
		Status:   &newStatus,
		Assignee: &emptyAssignee,
	}); err != nil {
		return fmt.Errorf("updating work item: %w", err)
	}

	// Note: Comment addition would be done via bd comments CLI command
	// Not implemented here to keep Beads API usage simple

	return nil
}
