// Package refinery provides the merge queue processing agent.
package refinery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/rig"
)

// MergeQueueConfig holds configuration for the merge queue processor.
type MergeQueueConfig struct {
	// Enabled controls whether the merge queue is active.
	Enabled bool `json:"enabled"`

	// TargetBranch is the default branch to merge to (e.g., "main").
	TargetBranch string `json:"target_branch"`

	// IntegrationBranches enables per-epic integration branches.
	IntegrationBranches bool `json:"integration_branches"`

	// OnConflict is the strategy for handling conflicts: "assign_back" or "auto_rebase".
	OnConflict string `json:"on_conflict"`

	// RunTests controls whether to run tests before merging.
	RunTests bool `json:"run_tests"`

	// TestCommand is the command to run for testing.
	TestCommand string `json:"test_command"`

	// DeleteMergedBranches controls whether to delete branches after merge.
	DeleteMergedBranches bool `json:"delete_merged_branches"`

	// RetryFlakyTests is the number of times to retry flaky tests.
	RetryFlakyTests int `json:"retry_flaky_tests"`

	// PollInterval is how often to check for new MRs.
	PollInterval time.Duration `json:"poll_interval"`

	// MaxConcurrent is the maximum number of MRs to process concurrently.
	MaxConcurrent int `json:"max_concurrent"`
}

// DefaultMergeQueueConfig returns sensible defaults for merge queue configuration.
func DefaultMergeQueueConfig() *MergeQueueConfig {
	return &MergeQueueConfig{
		Enabled:              true,
		TargetBranch:         "main",
		IntegrationBranches:  true,
		OnConflict:           "assign_back",
		RunTests:             true,
		TestCommand:          "",
		DeleteMergedBranches: true,
		RetryFlakyTests:      1,
		PollInterval:         30 * time.Second,
		MaxConcurrent:        1,
	}
}

// Engineer is the merge queue processor that polls for ready merge-requests
// and processes them according to the merge queue design.
type Engineer struct {
	rig     *rig.Rig
	beads   *beads.Beads
	config  *MergeQueueConfig
	workDir string

	// stopCh is used for graceful shutdown
	stopCh chan struct{}
}

// NewEngineer creates a new Engineer for the given rig.
func NewEngineer(r *rig.Rig) *Engineer {
	return &Engineer{
		rig:     r,
		beads:   beads.New(r.Path),
		config:  DefaultMergeQueueConfig(),
		workDir: r.Path,
		stopCh:  make(chan struct{}),
	}
}

// LoadConfig loads merge queue configuration from the rig's config.json.
func (e *Engineer) LoadConfig() error {
	configPath := filepath.Join(e.rig.Path, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Use defaults if no config file
			return nil
		}
		return fmt.Errorf("reading config: %w", err)
	}

	// Parse config file to extract merge_queue section
	var rawConfig struct {
		MergeQueue json.RawMessage `json:"merge_queue"`
	}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	if rawConfig.MergeQueue == nil {
		// No merge_queue section, use defaults
		return nil
	}

	// Parse merge_queue section into our config struct
	// We need special handling for poll_interval (string -> Duration)
	var mqRaw struct {
		Enabled              *bool   `json:"enabled"`
		TargetBranch         *string `json:"target_branch"`
		IntegrationBranches  *bool   `json:"integration_branches"`
		OnConflict           *string `json:"on_conflict"`
		RunTests             *bool   `json:"run_tests"`
		TestCommand          *string `json:"test_command"`
		DeleteMergedBranches *bool   `json:"delete_merged_branches"`
		RetryFlakyTests      *int    `json:"retry_flaky_tests"`
		PollInterval         *string `json:"poll_interval"`
		MaxConcurrent        *int    `json:"max_concurrent"`
	}

	if err := json.Unmarshal(rawConfig.MergeQueue, &mqRaw); err != nil {
		return fmt.Errorf("parsing merge_queue config: %w", err)
	}

	// Apply non-nil values to config (preserving defaults for missing fields)
	if mqRaw.Enabled != nil {
		e.config.Enabled = *mqRaw.Enabled
	}
	if mqRaw.TargetBranch != nil {
		e.config.TargetBranch = *mqRaw.TargetBranch
	}
	if mqRaw.IntegrationBranches != nil {
		e.config.IntegrationBranches = *mqRaw.IntegrationBranches
	}
	if mqRaw.OnConflict != nil {
		e.config.OnConflict = *mqRaw.OnConflict
	}
	if mqRaw.RunTests != nil {
		e.config.RunTests = *mqRaw.RunTests
	}
	if mqRaw.TestCommand != nil {
		e.config.TestCommand = *mqRaw.TestCommand
	}
	if mqRaw.DeleteMergedBranches != nil {
		e.config.DeleteMergedBranches = *mqRaw.DeleteMergedBranches
	}
	if mqRaw.RetryFlakyTests != nil {
		e.config.RetryFlakyTests = *mqRaw.RetryFlakyTests
	}
	if mqRaw.MaxConcurrent != nil {
		e.config.MaxConcurrent = *mqRaw.MaxConcurrent
	}
	if mqRaw.PollInterval != nil {
		dur, err := time.ParseDuration(*mqRaw.PollInterval)
		if err != nil {
			return fmt.Errorf("invalid poll_interval %q: %w", *mqRaw.PollInterval, err)
		}
		e.config.PollInterval = dur
	}

	return nil
}

// Config returns the current merge queue configuration.
func (e *Engineer) Config() *MergeQueueConfig {
	return e.config
}

// Run starts the Engineer main loop. It blocks until the context is cancelled
// or Stop() is called. Returns nil on graceful shutdown.
func (e *Engineer) Run(ctx context.Context) error {
	if err := e.LoadConfig(); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if !e.config.Enabled {
		return fmt.Errorf("merge queue is disabled in configuration")
	}

	fmt.Printf("[Engineer] Starting for rig %s (poll_interval=%s)\n",
		e.rig.Name, e.config.PollInterval)

	ticker := time.NewTicker(e.config.PollInterval)
	defer ticker.Stop()

	// Run one iteration immediately, then on ticker
	if err := e.processOnce(ctx); err != nil {
		fmt.Printf("[Engineer] Error: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[Engineer] Shutting down (context cancelled)")
			return nil
		case <-e.stopCh:
			fmt.Println("[Engineer] Shutting down (stop signal)")
			return nil
		case <-ticker.C:
			if err := e.processOnce(ctx); err != nil {
				fmt.Printf("[Engineer] Error: %v\n", err)
			}
		}
	}
}

// Stop signals the Engineer to stop processing. This is a non-blocking call.
func (e *Engineer) Stop() {
	close(e.stopCh)
}

// processOnce performs one iteration of the Engineer loop:
// 1. Query for ready merge-requests
// 2. If none, return (will try again on next tick)
// 3. Process the highest priority, oldest MR
func (e *Engineer) processOnce(ctx context.Context) error {
	// Check context before starting
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// 1. Query: bd ready --type=merge-request (filtered client-side)
	readyMRs, err := e.beads.ReadyWithType("merge-request")
	if err != nil {
		return fmt.Errorf("querying ready merge-requests: %w", err)
	}

	// 2. If empty, return
	if len(readyMRs) == 0 {
		return nil
	}

	// 3. Select highest priority, oldest MR
	// bd ready already returns sorted by priority then age, so first is best
	mr := readyMRs[0]

	fmt.Printf("[Engineer] Processing: %s (%s)\n", mr.ID, mr.Title)

	// 4. Claim: bd update <id> --status=in_progress
	inProgress := "in_progress"
	if err := e.beads.Update(mr.ID, beads.UpdateOptions{Status: &inProgress}); err != nil {
		return fmt.Errorf("claiming MR %s: %w", mr.ID, err)
	}

	// 5. Process (delegate to ProcessMR - implementation in separate issue gt-3x1.2)
	result := e.ProcessMR(ctx, mr)

	// 6. Handle result
	if result.Success {
		// Close with merged reason
		reason := fmt.Sprintf("merged: %s", result.MergeCommit)
		if err := e.beads.CloseWithReason(reason, mr.ID); err != nil {
			fmt.Printf("[Engineer] Warning: failed to close MR %s: %v\n", mr.ID, err)
		}
		fmt.Printf("[Engineer] ✓ Merged: %s\n", mr.ID)
	} else {
		// Failure handling (detailed implementation in gt-3x1.4)
		e.handleFailure(mr, result)
	}

	return nil
}

// ProcessResult contains the result of processing a merge request.
type ProcessResult struct {
	Success     bool
	MergeCommit string
	Error       string
	Conflict    bool
	TestsFailed bool
}

// ProcessMR processes a single merge request.
// It fetches the branch, checks for conflicts, and executes the merge.
func (e *Engineer) ProcessMR(ctx context.Context, mr *beads.Issue) ProcessResult {
	// Parse MR fields from description
	mrFields := beads.ParseMRFields(mr)
	if mrFields == nil {
		return ProcessResult{
			Success: false,
			Error:   "no MR fields found in description",
		}
	}

	if mrFields.Branch == "" {
		return ProcessResult{
			Success: false,
			Error:   "branch field is required in merge request",
		}
	}

	fmt.Printf("[Engineer] Processing MR:\n")
	fmt.Printf("  Branch: %s\n", mrFields.Branch)
	fmt.Printf("  Target: %s\n", mrFields.Target)
	fmt.Printf("  Worker: %s\n", mrFields.Worker)

	// Step 1: Fetch the source branch
	fmt.Printf("[Engineer] Fetching branch origin/%s\n", mrFields.Branch)
	if err := e.gitRun("fetch", "origin", mrFields.Branch); err != nil {
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("fetch failed: %v", err),
		}
	}

	// Step 2: Check for conflicts before attempting merge (optional pre-check)
	// This is done implicitly during the merge step in ExecuteMerge

	// Step 3: Execute the merge, test, and push
	return e.ExecuteMerge(ctx, mr, mrFields)
}

// handleFailure handles a failed merge request.
// This is a placeholder that will be fully implemented in gt-3x1.4.
func (e *Engineer) handleFailure(mr *beads.Issue, result ProcessResult) {
	// Reopen the MR (back to open status for rework)
	open := "open"
	if err := e.beads.Update(mr.ID, beads.UpdateOptions{Status: &open}); err != nil {
		fmt.Printf("[Engineer] Warning: failed to reopen MR %s: %v\n", mr.ID, err)
	}

	// Log the failure
	fmt.Printf("[Engineer] ✗ Failed: %s - %s\n", mr.ID, result.Error)

	// Full failure handling (assign back to worker, labels) in gt-3x1.4
}

// ExecuteMerge performs the actual git merge, test, and push operations.
// Steps:
// 1. git checkout <target>
// 2. git merge <branch> --no-ff -m 'Merge <branch>: <title>'
// 3. If config.run_tests: run test_command, if failed: reset and return failure
// 4. git push origin <target> (with retry logic)
// 5. Return Success with merge_commit SHA
func (e *Engineer) ExecuteMerge(ctx context.Context, mr *beads.Issue, mrFields *beads.MRFields) ProcessResult {
	target := mrFields.Target
	if target == "" {
		target = e.config.TargetBranch
	}
	branch := mrFields.Branch

	fmt.Printf("[Engineer] Merging %s → %s\n", branch, target)

	// 1. Checkout target branch
	if err := e.gitRun("checkout", target); err != nil {
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("checkout target failed: %v", err),
		}
	}

	// Pull latest from target to ensure we're up to date
	if err := e.gitRun("pull", "origin", target); err != nil {
		// Non-fatal warning - target might not exist on remote yet
		fmt.Printf("[Engineer] Warning: pull failed (may be expected): %v\n", err)
	}

	// 2. Merge the branch
	mergeMsg := fmt.Sprintf("Merge %s: %s", branch, mr.Title)
	err := e.gitRun("merge", "origin/"+branch, "--no-ff", "-m", mergeMsg)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "CONFLICT") || strings.Contains(errStr, "conflict") {
			// Abort the merge to clean up
			_ = e.gitRun("merge", "--abort")
			return ProcessResult{
				Success:  false,
				Error:    "merge conflict",
				Conflict: true,
			}
		}
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("merge failed: %v", err),
		}
	}

	// 3. Run tests if configured
	if e.config.RunTests {
		testCmd := e.config.TestCommand
		if testCmd == "" {
			testCmd = "go test ./..."
		}
		fmt.Printf("[Engineer] Running tests: %s\n", testCmd)
		if err := e.runTests(testCmd); err != nil {
			// Reset to before merge
			fmt.Printf("[Engineer] Tests failed, resetting merge\n")
			_ = e.gitRun("reset", "--hard", "HEAD~1")
			return ProcessResult{
				Success:     false,
				Error:       fmt.Sprintf("tests failed: %v", err),
				TestsFailed: true,
			}
		}
		fmt.Printf("[Engineer] Tests passed\n")
	}

	// 4. Push with retry logic
	if err := e.pushWithRetry(target); err != nil {
		// Reset to before merge on push failure
		fmt.Printf("[Engineer] Push failed, resetting merge\n")
		_ = e.gitRun("reset", "--hard", "HEAD~1")
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("push failed: %v", err),
		}
	}

	// 5. Get merge commit SHA
	mergeCommit, err := e.gitOutput("rev-parse", "HEAD")
	if err != nil {
		mergeCommit = "unknown"
	}

	fmt.Printf("[Engineer] Merged successfully: %s\n", mergeCommit)
	return ProcessResult{
		Success:     true,
		MergeCommit: mergeCommit,
	}
}

// pushWithRetry pushes to the target branch with exponential backoff retry.
// Uses 3 retries with 1s base delay by default.
func (e *Engineer) pushWithRetry(targetBranch string) error {
	const maxRetries = 3
	baseDelay := time.Second

	var lastErr error
	delay := baseDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("[Engineer] Push retry %d/%d after %v\n", attempt, maxRetries, delay)
			time.Sleep(delay)
			delay *= 2 // Exponential backoff
		}

		err := e.gitRun("push", "origin", targetBranch)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("push failed after %d retries: %v", maxRetries, lastErr)
}

// runTests executes the test command.
func (e *Engineer) runTests(testCmd string) error {
	parts := strings.Fields(testCmd)
	if len(parts) == 0 {
		return nil
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = e.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(stderr.String())
		if output == "" {
			output = strings.TrimSpace(stdout.String())
		}
		if output != "" {
			return fmt.Errorf("%v: %s", err, output)
		}
		return err
	}

	return nil
}

// gitRun executes a git command in the work directory.
func (e *Engineer) gitRun(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = e.workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return fmt.Errorf("%s", errMsg)
		}
		return err
	}

	return nil
}

// gitOutput executes a git command and returns stdout.
func (e *Engineer) gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = e.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("%s", errMsg)
		}
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}
