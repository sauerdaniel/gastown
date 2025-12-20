package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/refinery"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// MQ command flags
var (
	// Submit flags
	mqSubmitBranch   string
	mqSubmitIssue    string
	mqSubmitEpic     string
	mqSubmitPriority int

	// Retry flags
	mqRetryNow bool

	// Reject flags
	mqRejectReason string
	mqRejectNotify bool

	// List command flags
	mqListReady  bool
	mqListStatus string
	mqListWorker string
	mqListEpic   string
	mqListJSON   bool

	// Status command flags
	mqStatusJSON bool

	// Integration land flags
	mqIntegrationLandForce     bool
	mqIntegrationLandSkipTests bool
	mqIntegrationLandDryRun    bool

	// Integration status flags
	mqIntegrationStatusJSON bool
)

var mqCmd = &cobra.Command{
	Use:   "mq",
	Short: "Merge queue operations",
	Long: `Manage the merge queue for a rig.

The merge queue tracks work branches from polecats waiting to be merged.
Use these commands to view, submit, retry, and manage merge requests.`,
}

var mqSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit current branch to the merge queue",
	Long: `Submit the current branch to the merge queue.

Creates a merge-request bead that will be processed by the Engineer.

Auto-detection:
  - Branch: current git branch
  - Issue: parsed from branch name (e.g., polecat/Nux/gt-xyz → gt-xyz)
  - Worker: parsed from branch name
  - Rig: detected from current directory
  - Target: automatically determined (see below)
  - Priority: inherited from source issue

Target branch auto-detection:
  1. If --epic is specified: target integration/<epic>
  2. If source issue has a parent epic with integration/<epic> branch: target it
  3. Otherwise: target main

This ensures batch work on epics automatically flows to integration branches.

Examples:
  gt mq submit                           # Auto-detect everything
  gt mq submit --issue gt-abc            # Explicit issue
  gt mq submit --epic gt-xyz             # Target integration branch explicitly
  gt mq submit --priority 0              # Override priority (P0)`,
	RunE: runMqSubmit,
}

var mqRetryCmd = &cobra.Command{
	Use:   "retry <rig> <mr-id>",
	Short: "Retry a failed merge request",
	Long: `Retry a failed merge request.

Resets a failed MR so it can be processed again by the refinery.
The MR must be in a failed state (open with an error).

Examples:
  gt mq retry gastown gt-mr-abc123
  gt mq retry gastown gt-mr-abc123 --now`,
	Args: cobra.ExactArgs(2),
	RunE: runMQRetry,
}

var mqListCmd = &cobra.Command{
	Use:   "list <rig>",
	Short: "Show the merge queue",
	Long: `Show the merge queue for a rig.

Lists all pending merge requests waiting to be processed.

Output format:
  ID          STATUS       PRIORITY  BRANCH                    WORKER  AGE
  gt-mr-001   ready        P0        polecat/Nux/gt-xyz        Nux     5m
  gt-mr-002   in_progress  P1        polecat/Toast/gt-abc      Toast   12m
  gt-mr-003   blocked      P1        polecat/Capable/gt-def    Capable 8m
              (waiting on gt-mr-001)

Examples:
  gt mq list gastown
  gt mq list gastown --ready
  gt mq list gastown --status=open
  gt mq list gastown --worker=Nux`,
	Args: cobra.ExactArgs(1),
	RunE: runMQList,
}

var mqRejectCmd = &cobra.Command{
	Use:   "reject <rig> <mr-id-or-branch>",
	Short: "Reject a merge request",
	Long: `Manually reject a merge request.

This closes the MR with a 'rejected' status without merging.
The source issue is NOT closed (work is not done).

Examples:
  gt mq reject gastown polecat/Nux/gt-xyz --reason "Does not meet requirements"
  gt mq reject gastown mr-Nux-12345 --reason "Superseded by other work" --notify`,
	Args: cobra.ExactArgs(2),
	RunE: runMQReject,
}

var mqStatusCmd = &cobra.Command{
	Use:   "status <id>",
	Short: "Show detailed merge request status",
	Long: `Display detailed information about a merge request.

Shows all MR fields, current status with timestamps, dependencies,
blockers, and processing history.

Example:
  gt mq status gt-mr-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runMqStatus,
}

var mqIntegrationCmd = &cobra.Command{
	Use:   "integration",
	Short: "Manage integration branches for epics",
	Long: `Manage integration branches for batch work on epics.

Integration branches allow multiple MRs for an epic to target a shared
branch instead of main. After all epic work is complete, the integration
branch is landed to main as a single atomic unit.

Commands:
  create  Create an integration branch for an epic
  land    Merge integration branch to main
  status  Show integration branch status`,
}

var mqIntegrationCreateCmd = &cobra.Command{
	Use:   "create <epic-id>",
	Short: "Create an integration branch for an epic",
	Long: `Create an integration branch for batch work on an epic.

Creates a branch named integration/<epic-id> from main and pushes it
to origin. Future MRs for this epic's children can target this branch.

Actions:
  1. Verify epic exists
  2. Create branch integration/<epic-id> from main
  3. Push to origin
  4. Store integration branch info in epic metadata

Example:
  gt mq integration create gt-auth-epic
  # Creates integration/gt-auth-epic from main`,
	Args: cobra.ExactArgs(1),
	RunE: runMqIntegrationCreate,
}

var mqIntegrationLandCmd = &cobra.Command{
	Use:   "land <epic-id>",
	Short: "Merge integration branch to main",
	Long: `Merge an epic's integration branch to main.

Lands all work for an epic by merging its integration branch to main
as a single atomic merge commit.

Actions:
  1. Verify all MRs targeting integration/<epic> are merged
  2. Verify integration branch exists
  3. Merge integration/<epic> to main (--no-ff)
  4. Run tests on main
  5. Push to origin
  6. Delete integration branch
  7. Update epic status

Options:
  --force       Land even if some MRs still open
  --skip-tests  Skip test run
  --dry-run     Preview only, make no changes

Examples:
  gt mq integration land gt-auth-epic
  gt mq integration land gt-auth-epic --dry-run
  gt mq integration land gt-auth-epic --force --skip-tests`,
	Args: cobra.ExactArgs(1),
	RunE: runMqIntegrationLand,
}

var mqIntegrationStatusCmd = &cobra.Command{
	Use:   "status <epic-id>",
	Short: "Show integration branch status for an epic",
	Long: `Display the status of an integration branch.

Shows:
  - Integration branch name and creation date
  - Number of commits ahead of main
  - Merged MRs (closed, targeting integration branch)
  - Pending MRs (open, targeting integration branch)

Example:
  gt mq integration status gt-auth-epic`,
	Args: cobra.ExactArgs(1),
	RunE: runMqIntegrationStatus,
}

func init() {
	// Submit flags
	mqSubmitCmd.Flags().StringVar(&mqSubmitBranch, "branch", "", "Source branch (default: current branch)")
	mqSubmitCmd.Flags().StringVar(&mqSubmitIssue, "issue", "", "Source issue ID (default: parse from branch name)")
	mqSubmitCmd.Flags().StringVar(&mqSubmitEpic, "epic", "", "Target epic's integration branch instead of main")
	mqSubmitCmd.Flags().IntVarP(&mqSubmitPriority, "priority", "p", -1, "Override priority (0-4, default: inherit from issue)")

	// Retry flags
	mqRetryCmd.Flags().BoolVar(&mqRetryNow, "now", false, "Immediately process instead of waiting for refinery loop")

	// List flags
	mqListCmd.Flags().BoolVar(&mqListReady, "ready", false, "Show only ready-to-merge (no blockers)")
	mqListCmd.Flags().StringVar(&mqListStatus, "status", "", "Filter by status (open, in_progress, closed)")
	mqListCmd.Flags().StringVar(&mqListWorker, "worker", "", "Filter by worker name")
	mqListCmd.Flags().StringVar(&mqListEpic, "epic", "", "Show MRs targeting integration/<epic>")
	mqListCmd.Flags().BoolVar(&mqListJSON, "json", false, "Output as JSON")

	// Reject flags
	mqRejectCmd.Flags().StringVarP(&mqRejectReason, "reason", "r", "", "Reason for rejection (required)")
	mqRejectCmd.Flags().BoolVar(&mqRejectNotify, "notify", false, "Send mail notification to worker")
	_ = mqRejectCmd.MarkFlagRequired("reason")

	// Status flags
	mqStatusCmd.Flags().BoolVar(&mqStatusJSON, "json", false, "Output as JSON")

	// Add subcommands
	mqCmd.AddCommand(mqSubmitCmd)
	mqCmd.AddCommand(mqRetryCmd)
	mqCmd.AddCommand(mqListCmd)
	mqCmd.AddCommand(mqRejectCmd)
	mqCmd.AddCommand(mqStatusCmd)

	// Integration branch subcommands
	mqIntegrationCmd.AddCommand(mqIntegrationCreateCmd)

// Integration land flags
	mqIntegrationLandCmd.Flags().BoolVar(&mqIntegrationLandForce, "force", false, "Land even if some MRs still open")
	mqIntegrationLandCmd.Flags().BoolVar(&mqIntegrationLandSkipTests, "skip-tests", false, "Skip test run")
	mqIntegrationLandCmd.Flags().BoolVar(&mqIntegrationLandDryRun, "dry-run", false, "Preview only, make no changes")
	mqIntegrationCmd.AddCommand(mqIntegrationLandCmd)

	// Integration status flags
	mqIntegrationStatusCmd.Flags().BoolVar(&mqIntegrationStatusJSON, "json", false, "Output as JSON")
	mqIntegrationCmd.AddCommand(mqIntegrationStatusCmd)

	mqCmd.AddCommand(mqIntegrationCmd)

	rootCmd.AddCommand(mqCmd)
}

// branchInfo holds parsed branch information.
type branchInfo struct {
	Branch string // Full branch name
	Issue  string // Issue ID extracted from branch
	Worker string // Worker name (polecat name)
}

// parseBranchName extracts issue ID and worker from a branch name.
// Supports formats:
//   - polecat/<worker>/<issue>  → issue=<issue>, worker=<worker>
//   - <issue>                   → issue=<issue>, worker=""
func parseBranchName(branch string) branchInfo {
	info := branchInfo{Branch: branch}

	// Try polecat/<worker>/<issue> format
	if strings.HasPrefix(branch, "polecat/") {
		parts := strings.SplitN(branch, "/", 3)
		if len(parts) == 3 {
			info.Worker = parts[1]
			info.Issue = parts[2]
			return info
		}
	}

	// Try to find an issue ID pattern in the branch name
	// Common patterns: prefix-xxx, prefix-xxx.n (subtask)
	issuePattern := regexp.MustCompile(`([a-z]+-[a-z0-9]+(?:\.[0-9]+)?)`)
	if matches := issuePattern.FindStringSubmatch(branch); len(matches) > 1 {
		info.Issue = matches[1]
	}

	return info
}

// findCurrentRig determines the current rig from the working directory.
// Returns the rig name and rig object, or an error if not in a rig.
func findCurrentRig(townRoot string) (string, *rig.Rig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, fmt.Errorf("getting current directory: %w", err)
	}

	// Get relative path from town root to cwd
	relPath, err := filepath.Rel(townRoot, cwd)
	if err != nil {
		return "", nil, fmt.Errorf("computing relative path: %w", err)
	}

	// The first component of the relative path should be the rig name
	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) == 0 || parts[0] == "" || parts[0] == "." {
		return "", nil, fmt.Errorf("not inside a rig directory")
	}

	rigName := parts[0]

	// Load rig manager and get the rig
	rigsConfigPath := filepath.Join(townRoot, "mayor", "rigs.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Rigs: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := rig.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(rigName)
	if err != nil {
		return "", nil, fmt.Errorf("rig '%s' not found: %w", rigName, err)
	}

	return rigName, r, nil
}

func runMqSubmit(cmd *cobra.Command, args []string) error {
	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Find current rig
	rigName, _, err := findCurrentRig(townRoot)
	if err != nil {
		return err
	}

	// Initialize git for the current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}
	g := git.NewGit(cwd)

	// Get current branch
	branch := mqSubmitBranch
	if branch == "" {
		branch, err = g.CurrentBranch()
		if err != nil {
			return fmt.Errorf("getting current branch: %w", err)
		}
	}

	if branch == "main" || branch == "master" {
		return fmt.Errorf("cannot submit main/master branch to merge queue")
	}

	// Parse branch info
	info := parseBranchName(branch)

	// Override with explicit flags
	issueID := mqSubmitIssue
	if issueID == "" {
		issueID = info.Issue
	}
	worker := info.Worker

	if issueID == "" {
		return fmt.Errorf("cannot determine source issue from branch '%s'; use --issue to specify", branch)
	}

	// Initialize beads for looking up source issue
	bd := beads.New(cwd)

	// Determine target branch
	target := "main"
	if mqSubmitEpic != "" {
		// Explicit --epic flag takes precedence
		target = "integration/" + mqSubmitEpic
	} else {
		// Auto-detect: check if source issue has a parent epic with an integration branch
		autoTarget, err := detectIntegrationBranch(bd, g, issueID)
		if err != nil {
			// Non-fatal: log and continue with main as target
			fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("(note: %v)", err)))
		} else if autoTarget != "" {
			target = autoTarget
		}
	}

	// Get source issue for priority inheritance
	var priority int
	if mqSubmitPriority >= 0 {
		priority = mqSubmitPriority
	} else {
		// Try to inherit from source issue
		sourceIssue, err := bd.Show(issueID)
		if err != nil {
			// Issue not found, use default priority
			priority = 2
		} else {
			priority = sourceIssue.Priority
		}
	}

	// Build title
	title := fmt.Sprintf("Merge: %s", issueID)

	// Build description with MR fields
	mrFields := &beads.MRFields{
		Branch:      branch,
		Target:      target,
		SourceIssue: issueID,
		Worker:      worker,
		Rig:         rigName,
	}
	description := beads.FormatMRFields(mrFields)

	// Create the merge-request issue
	createOpts := beads.CreateOptions{
		Title:       title,
		Type:        "merge-request",
		Priority:    priority,
		Description: description,
	}

	issue, err := bd.Create(createOpts)
	if err != nil {
		return fmt.Errorf("creating merge request: %w", err)
	}

	// Success output
	fmt.Printf("%s Created merge request\n", style.Bold.Render("✓"))
	fmt.Printf("  MR ID: %s\n", style.Bold.Render(issue.ID))
	fmt.Printf("  Source: %s\n", branch)
	fmt.Printf("  Target: %s\n", target)
	fmt.Printf("  Issue: %s\n", issueID)
	if worker != "" {
		fmt.Printf("  Worker: %s\n", worker)
	}
	fmt.Printf("  Priority: P%d\n", priority)

	return nil
}

func runMQRetry(cmd *cobra.Command, args []string) error {
	rigName := args[0]
	mrID := args[1]

	mgr, _, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	// Get the MR first to show info
	mr, err := mgr.GetMR(mrID)
	if err != nil {
		if err == refinery.ErrMRNotFound {
			return fmt.Errorf("merge request '%s' not found in rig '%s'", mrID, rigName)
		}
		return fmt.Errorf("getting merge request: %w", err)
	}

	// Show what we're retrying
	fmt.Printf("Retrying merge request: %s\n", mrID)
	fmt.Printf("  Branch: %s\n", mr.Branch)
	fmt.Printf("  Worker: %s\n", mr.Worker)
	if mr.Error != "" {
		fmt.Printf("  Previous error: %s\n", style.Dim.Render(mr.Error))
	}

	// Perform the retry
	if err := mgr.Retry(mrID, mqRetryNow); err != nil {
		if err == refinery.ErrMRNotFailed {
			return fmt.Errorf("merge request '%s' has not failed (status: %s)", mrID, mr.Status)
		}
		return fmt.Errorf("retrying merge request: %w", err)
	}

	if mqRetryNow {
		fmt.Printf("%s Merge request processed\n", style.Bold.Render("✓"))
	} else {
		fmt.Printf("%s Merge request queued for retry\n", style.Bold.Render("✓"))
		fmt.Printf("  %s\n", style.Dim.Render("Will be processed on next refinery cycle"))
	}

	return nil
}

func runMQList(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	_, r, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	// Create beads wrapper for the rig
	b := beads.New(r.Path)

	// Build list options - query for merge-request type
	// Priority -1 means no priority filter (otherwise 0 would filter to P0 only)
	opts := beads.ListOptions{
		Type:     "merge-request",
		Priority: -1,
	}

	// Apply status filter if specified
	if mqListStatus != "" {
		opts.Status = mqListStatus
	} else if !mqListReady {
		// Default to open if not showing ready
		opts.Status = "open"
	}

	var issues []*beads.Issue

	if mqListReady {
		// Use ready query which filters by no blockers
		allReady, err := b.Ready()
		if err != nil {
			return fmt.Errorf("querying ready MRs: %w", err)
		}
		// Filter to only merge-request type
		for _, issue := range allReady {
			if issue.Type == "merge-request" {
				issues = append(issues, issue)
			}
		}
	} else {
		issues, err = b.List(opts)
		if err != nil {
			return fmt.Errorf("querying merge queue: %w", err)
		}
	}

	// Apply additional filters
	var filtered []*beads.Issue
	for _, issue := range issues {
		// Parse MR fields
		fields := beads.ParseMRFields(issue)

		// Filter by worker
		if mqListWorker != "" {
			worker := ""
			if fields != nil {
				worker = fields.Worker
			}
			if !strings.EqualFold(worker, mqListWorker) {
				continue
			}
		}

		// Filter by epic (target branch)
		if mqListEpic != "" {
			target := ""
			if fields != nil {
				target = fields.Target
			}
			expectedTarget := "integration/" + mqListEpic
			if target != expectedTarget {
				continue
			}
		}

		filtered = append(filtered, issue)
	}

	// JSON output
	if mqListJSON {
		return outputJSON(filtered)
	}

	// Human-readable output
	fmt.Printf("%s Merge queue for '%s':\n\n", style.Bold.Render("📋"), rigName)

	if len(filtered) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(empty)"))
		return nil
	}

	// Print header
	fmt.Printf("  %-12s %-12s %-8s %-30s %-10s %s\n",
		"ID", "STATUS", "PRIORITY", "BRANCH", "WORKER", "AGE")
	fmt.Printf("  %s\n", strings.Repeat("-", 90))

	// Print each MR
	for _, issue := range filtered {
		fields := beads.ParseMRFields(issue)

		// Determine display status
		displayStatus := issue.Status
		if issue.Status == "open" {
			if len(issue.BlockedBy) > 0 || issue.BlockedByCount > 0 {
				displayStatus = "blocked"
			} else {
				displayStatus = "ready"
			}
		}

		// Format status with styling
		styledStatus := displayStatus
		switch displayStatus {
		case "ready":
			styledStatus = style.Bold.Render("ready")
		case "in_progress":
			styledStatus = style.Bold.Render("in_progress")
		case "blocked":
			styledStatus = style.Dim.Render("blocked")
		case "closed":
			styledStatus = style.Dim.Render("closed")
		}

		// Get MR fields
		branch := ""
		worker := ""
		if fields != nil {
			branch = fields.Branch
			worker = fields.Worker
		}

		// Truncate branch if too long
		if len(branch) > 30 {
			branch = branch[:27] + "..."
		}

		// Format priority
		priority := fmt.Sprintf("P%d", issue.Priority)

		// Calculate age
		age := formatMRAge(issue.CreatedAt)

		// Truncate ID if needed
		displayID := issue.ID
		if len(displayID) > 12 {
			displayID = displayID[:12]
		}

		fmt.Printf("  %-12s %-12s %-8s %-30s %-10s %s\n",
			displayID, styledStatus, priority, branch, worker, style.Dim.Render(age))

		// Show blocking info if blocked
		if displayStatus == "blocked" && len(issue.BlockedBy) > 0 {
			fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("             (waiting on %s)", issue.BlockedBy[0])))
		}
	}

	return nil
}

// formatMRAge formats the age of an MR from its created_at timestamp.
func formatMRAge(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		// Try other formats
		t, err = time.Parse("2006-01-02T15:04:05Z", createdAt)
		if err != nil {
			return "?"
		}
	}

	d := time.Since(t)

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// outputJSON outputs data as JSON.
func outputJSON(data interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func runMQReject(cmd *cobra.Command, args []string) error {
	rigName := args[0]
	mrIDOrBranch := args[1]

	mgr, _, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	result, err := mgr.RejectMR(mrIDOrBranch, mqRejectReason, mqRejectNotify)
	if err != nil {
		return fmt.Errorf("rejecting MR: %w", err)
	}

	fmt.Printf("%s Rejected: %s\n", style.Bold.Render("✗"), result.Branch)
	fmt.Printf("  Worker: %s\n", result.Worker)
	fmt.Printf("  Reason: %s\n", mqRejectReason)

	if result.IssueID != "" {
		fmt.Printf("  Issue:  %s %s\n", result.IssueID, style.Dim.Render("(not closed - work not done)"))
	}

	if mqRejectNotify {
		fmt.Printf("  %s\n", style.Dim.Render("Worker notified via mail"))
	}

	return nil
}

// MRStatusOutput is the JSON output structure for gt mq status.
type MRStatusOutput struct {
	// Core issue fields
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Priority  int    `json:"priority"`
	Type      string `json:"type"`
	Assignee  string `json:"assignee,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	ClosedAt  string `json:"closed_at,omitempty"`

	// MR-specific fields
	Branch      string `json:"branch,omitempty"`
	Target      string `json:"target,omitempty"`
	SourceIssue string `json:"source_issue,omitempty"`
	Worker      string `json:"worker,omitempty"`
	Rig         string `json:"rig,omitempty"`
	MergeCommit string `json:"merge_commit,omitempty"`
	CloseReason string `json:"close_reason,omitempty"`

	// Dependencies
	DependsOn []DependencyInfo `json:"depends_on,omitempty"`
	Blocks    []DependencyInfo `json:"blocks,omitempty"`
}

// DependencyInfo represents a dependency or blocker.
type DependencyInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Type     string `json:"type"`
}

func runMqStatus(cmd *cobra.Command, args []string) error {
	mrID := args[0]

	// Use current working directory for beads operations
	// (beads repos are per-rig, not per-workspace)
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Initialize beads client
	bd := beads.New(workDir)

	// Fetch the issue
	issue, err := bd.Show(mrID)
	if err != nil {
		if err == beads.ErrNotFound {
			return fmt.Errorf("merge request '%s' not found", mrID)
		}
		return fmt.Errorf("fetching merge request: %w", err)
	}

	// Parse MR-specific fields from description
	mrFields := beads.ParseMRFields(issue)

	// Build output structure
	output := MRStatusOutput{
		ID:        issue.ID,
		Title:     issue.Title,
		Status:    issue.Status,
		Priority:  issue.Priority,
		Type:      issue.Type,
		Assignee:  issue.Assignee,
		CreatedAt: issue.CreatedAt,
		UpdatedAt: issue.UpdatedAt,
		ClosedAt:  issue.ClosedAt,
	}

	// Add MR fields if present
	if mrFields != nil {
		output.Branch = mrFields.Branch
		output.Target = mrFields.Target
		output.SourceIssue = mrFields.SourceIssue
		output.Worker = mrFields.Worker
		output.Rig = mrFields.Rig
		output.MergeCommit = mrFields.MergeCommit
		output.CloseReason = mrFields.CloseReason
	}

	// Add dependency info from the issue's Dependencies field
	for _, dep := range issue.Dependencies {
		output.DependsOn = append(output.DependsOn, DependencyInfo{
			ID:       dep.ID,
			Title:    dep.Title,
			Status:   dep.Status,
			Priority: dep.Priority,
			Type:     dep.Type,
		})
	}

	// Add blocker info from the issue's Dependents field
	for _, dep := range issue.Dependents {
		output.Blocks = append(output.Blocks, DependencyInfo{
			ID:       dep.ID,
			Title:    dep.Title,
			Status:   dep.Status,
			Priority: dep.Priority,
			Type:     dep.Type,
		})
	}

	// JSON output
	if mqStatusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	// Human-readable output
	return printMqStatus(issue, mrFields)
}

// printMqStatus prints detailed MR status in human-readable format.
func printMqStatus(issue *beads.Issue, mrFields *beads.MRFields) error {
	// Header
	fmt.Printf("%s %s\n", style.Bold.Render("📋 Merge Request:"), issue.ID)
	fmt.Printf("   %s\n\n", issue.Title)

	// Status section
	fmt.Printf("%s\n", style.Bold.Render("Status"))
	statusDisplay := formatStatus(issue.Status)
	fmt.Printf("   State:    %s\n", statusDisplay)
	fmt.Printf("   Priority: P%d\n", issue.Priority)
	if issue.Type != "" {
		fmt.Printf("   Type:     %s\n", issue.Type)
	}
	if issue.Assignee != "" {
		fmt.Printf("   Assignee: %s\n", issue.Assignee)
	}

	// Timestamps
	fmt.Printf("\n%s\n", style.Bold.Render("Timeline"))
	if issue.CreatedAt != "" {
		fmt.Printf("   Created: %s %s\n", issue.CreatedAt, formatTimeAgo(issue.CreatedAt))
	}
	if issue.UpdatedAt != "" && issue.UpdatedAt != issue.CreatedAt {
		fmt.Printf("   Updated: %s %s\n", issue.UpdatedAt, formatTimeAgo(issue.UpdatedAt))
	}
	if issue.ClosedAt != "" {
		fmt.Printf("   Closed:  %s %s\n", issue.ClosedAt, formatTimeAgo(issue.ClosedAt))
	}

	// MR-specific fields
	if mrFields != nil {
		fmt.Printf("\n%s\n", style.Bold.Render("Merge Details"))
		if mrFields.Branch != "" {
			fmt.Printf("   Branch:       %s\n", mrFields.Branch)
		}
		if mrFields.Target != "" {
			fmt.Printf("   Target:       %s\n", mrFields.Target)
		}
		if mrFields.SourceIssue != "" {
			fmt.Printf("   Source Issue: %s\n", mrFields.SourceIssue)
		}
		if mrFields.Worker != "" {
			fmt.Printf("   Worker:       %s\n", mrFields.Worker)
		}
		if mrFields.Rig != "" {
			fmt.Printf("   Rig:          %s\n", mrFields.Rig)
		}
		if mrFields.MergeCommit != "" {
			fmt.Printf("   Merge Commit: %s\n", mrFields.MergeCommit)
		}
		if mrFields.CloseReason != "" {
			fmt.Printf("   Close Reason: %s\n", mrFields.CloseReason)
		}
	}

	// Dependencies (what this MR is waiting on)
	if len(issue.Dependencies) > 0 {
		fmt.Printf("\n%s\n", style.Bold.Render("Waiting On"))
		for _, dep := range issue.Dependencies {
			statusIcon := getStatusIcon(dep.Status)
			fmt.Printf("   %s %s: %s %s\n",
				statusIcon,
				dep.ID,
				truncateString(dep.Title, 50),
				style.Dim.Render(fmt.Sprintf("[%s]", dep.Status)))
		}
	}

	// Blockers (what's waiting on this MR)
	if len(issue.Dependents) > 0 {
		fmt.Printf("\n%s\n", style.Bold.Render("Blocking"))
		for _, dep := range issue.Dependents {
			statusIcon := getStatusIcon(dep.Status)
			fmt.Printf("   %s %s: %s %s\n",
				statusIcon,
				dep.ID,
				truncateString(dep.Title, 50),
				style.Dim.Render(fmt.Sprintf("[%s]", dep.Status)))
		}
	}

	// Description (if present and not just MR fields)
	desc := getDescriptionWithoutMRFields(issue.Description)
	if desc != "" {
		fmt.Printf("\n%s\n", style.Bold.Render("Notes"))
		// Indent each line
		for _, line := range strings.Split(desc, "\n") {
			fmt.Printf("   %s\n", line)
		}
	}

	return nil
}

// formatStatus formats the status with appropriate styling.
func formatStatus(status string) string {
	switch status {
	case "open":
		return style.Info.Render("● open")
	case "in_progress":
		return style.Bold.Render("▶ in_progress")
	case "closed":
		return style.Dim.Render("✓ closed")
	default:
		return status
	}
}

// getStatusIcon returns an icon for the given status.
func getStatusIcon(status string) string {
	switch status {
	case "open":
		return "○"
	case "in_progress":
		return "▶"
	case "closed":
		return "✓"
	default:
		return "•"
	}
}

// formatTimeAgo formats a timestamp as a relative time string.
func formatTimeAgo(timestamp string) string {
	// Try parsing common formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	var t time.Time
	var err error
	for _, format := range formats {
		t, err = time.Parse(format, timestamp)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "" // Can't parse, return empty
	}

	d := time.Since(t)
	if d < 0 {
		return style.Dim.Render("(in the future)")
	}

	var ago string
	if d < time.Minute {
		ago = fmt.Sprintf("%ds ago", int(d.Seconds()))
	} else if d < time.Hour {
		ago = fmt.Sprintf("%dm ago", int(d.Minutes()))
	} else if d < 24*time.Hour {
		ago = fmt.Sprintf("%dh ago", int(d.Hours()))
	} else {
		ago = fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}

	return style.Dim.Render("(" + ago + ")")
}

// truncateString truncates a string to maxLen, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// getDescriptionWithoutMRFields returns the description with MR field lines removed.
func getDescriptionWithoutMRFields(description string) string {
	if description == "" {
		return ""
	}

	// Known MR field keys (lowercase)
	mrKeys := map[string]bool{
		"branch":       true,
		"target":       true,
		"source_issue": true,
		"source-issue": true,
		"sourceissue":  true,
		"worker":       true,
		"rig":          true,
		"merge_commit": true,
		"merge-commit": true,
		"mergecommit":  true,
		"close_reason": true,
		"close-reason": true,
		"closereason":  true,
		"type":         true,
	}

	var lines []string
	for _, line := range strings.Split(description, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			lines = append(lines, line)
			continue
		}

		// Check if this is an MR field line
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx != -1 {
			key := strings.ToLower(strings.TrimSpace(trimmed[:colonIdx]))
			if mrKeys[key] {
				continue // Skip MR field lines
			}
		}

		lines = append(lines, line)
	}

	// Trim leading/trailing blank lines
	result := strings.Join(lines, "\n")
	result = strings.TrimSpace(result)
	return result
}

// runMqIntegrationCreate creates an integration branch for an epic.
func runMqIntegrationCreate(cmd *cobra.Command, args []string) error {
	epicID := args[0]

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Find current rig
	_, r, err := findCurrentRig(townRoot)
	if err != nil {
		return err
	}

	// Initialize beads for the rig
	bd := beads.New(r.Path)

	// 1. Verify epic exists
	epic, err := bd.Show(epicID)
	if err != nil {
		if err == beads.ErrNotFound {
			return fmt.Errorf("epic '%s' not found", epicID)
		}
		return fmt.Errorf("fetching epic: %w", err)
	}

	// Verify it's actually an epic
	if epic.Type != "epic" {
		return fmt.Errorf("'%s' is a %s, not an epic", epicID, epic.Type)
	}

	// Build integration branch name
	branchName := "integration/" + epicID

	// Initialize git for the rig
	g := git.NewGit(r.Path)

	// Check if integration branch already exists locally
	exists, err := g.BranchExists(branchName)
	if err != nil {
		return fmt.Errorf("checking branch existence: %w", err)
	}
	if exists {
		return fmt.Errorf("integration branch '%s' already exists locally", branchName)
	}

	// Check if branch exists on remote
	remoteExists, err := g.RemoteBranchExists("origin", branchName)
	if err != nil {
		// Log warning but continue - remote check isn't critical
		fmt.Printf("  %s\n", style.Dim.Render("(could not check remote, continuing)"))
	}
	if remoteExists {
		return fmt.Errorf("integration branch '%s' already exists on origin", branchName)
	}

	// Ensure we have latest main
	fmt.Printf("Fetching latest from origin...\n")
	if err := g.Fetch("origin"); err != nil {
		return fmt.Errorf("fetching from origin: %w", err)
	}

	// 2. Create branch from origin/main
	fmt.Printf("Creating branch '%s' from main...\n", branchName)
	if err := g.CreateBranchFrom(branchName, "origin/main"); err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}

	// 3. Push to origin
	fmt.Printf("Pushing to origin...\n")
	if err := g.Push("origin", branchName, false); err != nil {
		// Clean up local branch on push failure
		_ = g.DeleteBranch(branchName, true)
		return fmt.Errorf("pushing to origin: %w", err)
	}

	// 4. Store integration branch info in epic metadata
	// Update the epic's description to include the integration branch info
	newDesc := addIntegrationBranchField(epic.Description, branchName)
	if newDesc != epic.Description {
		if err := bd.Update(epicID, beads.UpdateOptions{Description: &newDesc}); err != nil {
			// Non-fatal - branch was created, just metadata update failed
			fmt.Printf("  %s\n", style.Dim.Render("(warning: could not update epic metadata)"))
		}
	}

	// Success output
	fmt.Printf("\n%s Created integration branch\n", style.Bold.Render("✓"))
	fmt.Printf("  Epic:   %s\n", epicID)
	fmt.Printf("  Branch: %s\n", branchName)
	fmt.Printf("  From:   main\n")
	fmt.Printf("\n  Future MRs for this epic's children can target:\n")
	fmt.Printf("    gt mq submit --epic %s\n", epicID)

	return nil
}

// addIntegrationBranchField adds or updates the integration_branch field in a description.
func addIntegrationBranchField(description, branchName string) string {
	fieldLine := "integration_branch: " + branchName

	// If description is empty, just return the field
	if description == "" {
		return fieldLine
	}

	// Check if integration_branch field already exists
	lines := strings.Split(description, "\n")
	var newLines []string
	found := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "integration_branch:") {
			// Replace existing field
			newLines = append(newLines, fieldLine)
			found = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !found {
		// Add field at the beginning
		newLines = append([]string{fieldLine}, newLines...)
	}

	return strings.Join(newLines, "\n")
}

// runMqIntegrationLand merges an integration branch to main.
func runMqIntegrationLand(cmd *cobra.Command, args []string) error {
	epicID := args[0]

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Find current rig
	_, r, err := findCurrentRig(townRoot)
	if err != nil {
		return err
	}

	// Initialize beads and git for the rig
	bd := beads.New(r.Path)
	g := git.NewGit(r.Path)

	// Build integration branch name
	branchName := "integration/" + epicID

	// Show what we're about to do
	if mqIntegrationLandDryRun {
		fmt.Printf("%s Dry run - no changes will be made\n\n", style.Bold.Render("🔍"))
	}

	// 1. Verify epic exists
	epic, err := bd.Show(epicID)
	if err != nil {
		if err == beads.ErrNotFound {
			return fmt.Errorf("epic '%s' not found", epicID)
		}
		return fmt.Errorf("fetching epic: %w", err)
	}

	if epic.Type != "epic" {
		return fmt.Errorf("'%s' is a %s, not an epic", epicID, epic.Type)
	}

	fmt.Printf("Landing integration branch for epic: %s\n", epicID)
	fmt.Printf("  Title: %s\n\n", epic.Title)

	// 2. Verify integration branch exists
	fmt.Printf("Checking integration branch...\n")
	exists, err := g.BranchExists(branchName)
	if err != nil {
		return fmt.Errorf("checking branch existence: %w", err)
	}

	// Also check remote if local doesn't exist
	if !exists {
		remoteExists, err := g.RemoteBranchExists("origin", branchName)
		if err != nil {
			return fmt.Errorf("checking remote branch: %w", err)
		}
		if !remoteExists {
			return fmt.Errorf("integration branch '%s' does not exist (locally or on origin)", branchName)
		}
		// Fetch and create local tracking branch
		fmt.Printf("Fetching integration branch from origin...\n")
		if err := g.FetchBranch("origin", branchName); err != nil {
			return fmt.Errorf("fetching branch: %w", err)
		}
	}
	fmt.Printf("  %s Branch exists\n", style.Bold.Render("✓"))

	// 3. Verify all MRs targeting this integration branch are merged
	fmt.Printf("Checking open merge requests...\n")
	openMRs, err := findOpenMRsForIntegration(bd, branchName)
	if err != nil {
		return fmt.Errorf("checking open MRs: %w", err)
	}

	if len(openMRs) > 0 {
		fmt.Printf("\n  %s Open merge requests targeting %s:\n", style.Bold.Render("⚠"), branchName)
		for _, mr := range openMRs {
			fmt.Printf("    - %s: %s\n", mr.ID, mr.Title)
		}
		fmt.Println()

		if !mqIntegrationLandForce {
			return fmt.Errorf("cannot land: %d open MRs (use --force to override)", len(openMRs))
		}
		fmt.Printf("  %s Proceeding anyway (--force)\n", style.Dim.Render("⚠"))
	} else {
		fmt.Printf("  %s No open MRs targeting integration branch\n", style.Bold.Render("✓"))
	}

	// Dry run stops here
	if mqIntegrationLandDryRun {
		fmt.Printf("\n%s Dry run complete. Would perform:\n", style.Bold.Render("🔍"))
		fmt.Printf("  1. Merge %s to main (--no-ff)\n", branchName)
		if !mqIntegrationLandSkipTests {
			fmt.Printf("  2. Run tests on main\n")
		}
		fmt.Printf("  3. Push main to origin\n")
		fmt.Printf("  4. Delete integration branch (local and remote)\n")
		fmt.Printf("  5. Update epic status to closed\n")
		return nil
	}

	// Ensure working directory is clean
	status, err := g.Status()
	if err != nil {
		return fmt.Errorf("checking git status: %w", err)
	}
	if !status.Clean {
		return fmt.Errorf("working directory is not clean; please commit or stash changes")
	}

	// Fetch latest
	fmt.Printf("Fetching latest from origin...\n")
	if err := g.Fetch("origin"); err != nil {
		return fmt.Errorf("fetching from origin: %w", err)
	}

	// 4. Checkout main and merge integration branch
	fmt.Printf("Checking out main...\n")
	if err := g.Checkout("main"); err != nil {
		return fmt.Errorf("checking out main: %w", err)
	}

	// Pull latest main
	if err := g.Pull("origin", "main"); err != nil {
		// Non-fatal if pull fails (e.g., first time)
		fmt.Printf("  %s\n", style.Dim.Render("(pull from origin/main skipped)"))
	}

	// Merge with --no-ff
	fmt.Printf("Merging %s to main...\n", branchName)
	mergeMsg := fmt.Sprintf("Merge %s: %s\n\nEpic: %s", branchName, epic.Title, epicID)
	if err := g.MergeNoFF("origin/"+branchName, mergeMsg); err != nil {
		// Abort merge on failure
		_ = g.AbortMerge()
		return fmt.Errorf("merge failed: %w", err)
	}
	fmt.Printf("  %s Merged successfully\n", style.Bold.Render("✓"))

	// 5. Run tests (if configured and not skipped)
	if !mqIntegrationLandSkipTests {
		testCmd := getTestCommand(r.Path)
		if testCmd != "" {
			fmt.Printf("Running tests: %s\n", testCmd)
			if err := runTestCommand(r.Path, testCmd); err != nil {
				// Tests failed - reset main
				fmt.Printf("  %s Tests failed, resetting main...\n", style.Bold.Render("✗"))
				_ = g.Checkout("main")
				resetErr := resetHard(g, "HEAD~1")
				if resetErr != nil {
					return fmt.Errorf("tests failed and could not reset: %w (test error: %v)", resetErr, err)
				}
				return fmt.Errorf("tests failed: %w", err)
			}
			fmt.Printf("  %s Tests passed\n", style.Bold.Render("✓"))
		} else {
			fmt.Printf("  %s\n", style.Dim.Render("(no test command configured)"))
		}
	} else {
		fmt.Printf("  %s\n", style.Dim.Render("(tests skipped)"))
	}

	// 6. Push to origin
	fmt.Printf("Pushing main to origin...\n")
	if err := g.Push("origin", "main", false); err != nil {
		// Reset on push failure
		resetErr := resetHard(g, "HEAD~1")
		if resetErr != nil {
			return fmt.Errorf("push failed and could not reset: %w (push error: %v)", resetErr, err)
		}
		return fmt.Errorf("push failed: %w", err)
	}
	fmt.Printf("  %s Pushed to origin\n", style.Bold.Render("✓"))

	// 7. Delete integration branch
	fmt.Printf("Deleting integration branch...\n")
	// Delete remote first
	if err := g.DeleteRemoteBranch("origin", branchName); err != nil {
		fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("(could not delete remote branch: %v)", err)))
	} else {
		fmt.Printf("  %s Deleted from origin\n", style.Bold.Render("✓"))
	}
	// Delete local
	if err := g.DeleteBranch(branchName, true); err != nil {
		fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("(could not delete local branch: %v)", err)))
	} else {
		fmt.Printf("  %s Deleted locally\n", style.Bold.Render("✓"))
	}

	// 8. Update epic status
	fmt.Printf("Updating epic status...\n")
	if err := bd.Close(epicID); err != nil {
		fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("(could not close epic: %v)", err)))
	} else {
		fmt.Printf("  %s Epic closed\n", style.Bold.Render("✓"))
	}

	// Success output
	fmt.Printf("\n%s Successfully landed integration branch\n", style.Bold.Render("✓"))
	fmt.Printf("  Epic:   %s\n", epicID)
	fmt.Printf("  Branch: %s → main\n", branchName)

	return nil
}

// findOpenMRsForIntegration finds all open merge requests targeting an integration branch.
func findOpenMRsForIntegration(bd *beads.Beads, targetBranch string) ([]*beads.Issue, error) {
	// List all open merge requests
	opts := beads.ListOptions{
		Type:   "merge-request",
		Status: "open",
	}
	allMRs, err := bd.List(opts)
	if err != nil {
		return nil, err
	}

	// Filter to those targeting this integration branch
	var openMRs []*beads.Issue
	for _, mr := range allMRs {
		fields := beads.ParseMRFields(mr)
		if fields != nil && fields.Target == targetBranch {
			openMRs = append(openMRs, mr)
		}
	}

	return openMRs, nil
}

// getTestCommand returns the test command from rig config.
func getTestCommand(rigPath string) string {
	configPath := filepath.Join(rigPath, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		// Try .gastown/config.json as fallback
		configPath = filepath.Join(rigPath, ".gastown", "config.json")
		data, err = os.ReadFile(configPath)
		if err != nil {
			return ""
		}
	}

	var rawConfig struct {
		MergeQueue struct {
			TestCommand string `json:"test_command"`
		} `json:"merge_queue"`
		TestCommand string `json:"test_command"` // Legacy fallback
	}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return ""
	}

	if rawConfig.MergeQueue.TestCommand != "" {
		return rawConfig.MergeQueue.TestCommand
	}
	return rawConfig.TestCommand
}

// runTestCommand executes a test command in the given directory.
func runTestCommand(workDir, testCmd string) error {
	parts := strings.Fields(testCmd)
	if len(parts) == 0 {
		return nil
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// resetHard performs a git reset --hard to the given ref.
func resetHard(g *git.Git, ref string) error {
	// We need to use the git package, but it doesn't have a Reset method
	// For now, use the internal run method via Checkout workaround
	// This is a bit of a hack but works for now
	cmd := exec.Command("git", "reset", "--hard", ref)
	cmd.Dir = g.WorkDir()
	return cmd.Run()
}

// detectIntegrationBranch checks if an issue is a child of an epic that has an integration branch.
// Returns the integration branch target (e.g., "integration/gt-epic") if found, or "" if not.
func detectIntegrationBranch(bd *beads.Beads, g *git.Git, issueID string) (string, error) {
	// Get the source issue
	issue, err := bd.Show(issueID)
	if err != nil {
		return "", fmt.Errorf("looking up issue %s: %w", issueID, err)
	}

	// Check if issue has a parent
	if issue.Parent == "" {
		return "", nil // No parent, no integration branch
	}

	// Get the parent issue
	parent, err := bd.Show(issue.Parent)
	if err != nil {
		return "", fmt.Errorf("looking up parent %s: %w", issue.Parent, err)
	}

	// Check if parent is an epic
	if parent.Type != "epic" {
		return "", nil // Parent is not an epic
	}

	// Check if integration branch exists
	integrationBranch := "integration/" + parent.ID

	// Check local first (faster)
	exists, err := g.BranchExists(integrationBranch)
	if err != nil {
		return "", fmt.Errorf("checking local branch: %w", err)
	}
	if exists {
		return integrationBranch, nil
	}

	// Check remote
	exists, err = g.RemoteBranchExists("origin", integrationBranch)
	if err != nil {
		// Remote check failure is non-fatal
		return "", nil
	}
	if exists {
		return integrationBranch, nil
	}

	return "", nil // No integration branch found
}

// IntegrationStatusOutput is the JSON output structure for integration status.
type IntegrationStatusOutput struct {
	Epic         string                        `json:"epic"`
	Branch       string                        `json:"branch"`
	Created      string                        `json:"created,omitempty"`
	AheadOfMain  int                           `json:"ahead_of_main"`
	MergedMRs    []IntegrationStatusMRSummary  `json:"merged_mrs"`
	PendingMRs   []IntegrationStatusMRSummary  `json:"pending_mrs"`
}

// IntegrationStatusMRSummary represents a merge request in the integration status output.
type IntegrationStatusMRSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
}

// runMqIntegrationStatus shows the status of an integration branch for an epic.
func runMqIntegrationStatus(cmd *cobra.Command, args []string) error {
	epicID := args[0]

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Find current rig
	_, r, err := findCurrentRig(townRoot)
	if err != nil {
		return err
	}

	// Initialize beads for the rig
	bd := beads.New(r.Path)

	// Build integration branch name
	branchName := "integration/" + epicID

	// Initialize git for the rig
	g := git.NewGit(r.Path)

	// Fetch from origin to ensure we have latest refs
	if err := g.Fetch("origin"); err != nil {
		// Non-fatal, continue with local data
	}

	// Check if integration branch exists (locally or remotely)
	localExists, _ := g.BranchExists(branchName)
	remoteExists, _ := g.RemoteBranchExists("origin", branchName)

	if !localExists && !remoteExists {
		return fmt.Errorf("integration branch '%s' does not exist", branchName)
	}

	// Determine which ref to use for comparison
	ref := branchName
	if !localExists && remoteExists {
		ref = "origin/" + branchName
	}

	// Get branch creation date
	createdDate, err := g.BranchCreatedDate(ref)
	if err != nil {
		createdDate = "" // Non-fatal
	}

	// Get commits ahead of main
	aheadCount, err := g.CommitsAhead("main", ref)
	if err != nil {
		aheadCount = 0 // Non-fatal
	}

	// Query for MRs targeting this integration branch
	targetBranch := "integration/" + epicID

	// Get all merge-request issues
	allMRs, err := bd.List(beads.ListOptions{
		Type:   "merge-request",
		Status: "",  // all statuses
	})
	if err != nil {
		return fmt.Errorf("querying merge requests: %w", err)
	}

	// Filter by target branch and separate into merged/pending
	var mergedMRs, pendingMRs []*beads.Issue
	for _, mr := range allMRs {
		fields := beads.ParseMRFields(mr)
		if fields == nil || fields.Target != targetBranch {
			continue
		}

		if mr.Status == "closed" {
			mergedMRs = append(mergedMRs, mr)
		} else {
			pendingMRs = append(pendingMRs, mr)
		}
	}

	// Build output structure
	output := IntegrationStatusOutput{
		Epic:        epicID,
		Branch:      branchName,
		Created:     createdDate,
		AheadOfMain: aheadCount,
		MergedMRs:   make([]IntegrationStatusMRSummary, 0, len(mergedMRs)),
		PendingMRs:  make([]IntegrationStatusMRSummary, 0, len(pendingMRs)),
	}

	for _, mr := range mergedMRs {
		// Extract the title without "Merge: " prefix for cleaner display
		title := strings.TrimPrefix(mr.Title, "Merge: ")
		output.MergedMRs = append(output.MergedMRs, IntegrationStatusMRSummary{
			ID:    mr.ID,
			Title: title,
		})
	}

	for _, mr := range pendingMRs {
		title := strings.TrimPrefix(mr.Title, "Merge: ")
		output.PendingMRs = append(output.PendingMRs, IntegrationStatusMRSummary{
			ID:     mr.ID,
			Title:  title,
			Status: mr.Status,
		})
	}

	// JSON output
	if mqIntegrationStatusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	// Human-readable output
	return printIntegrationStatus(&output)
}

// printIntegrationStatus prints the integration status in human-readable format.
func printIntegrationStatus(output *IntegrationStatusOutput) error {
	fmt.Printf("Integration: %s\n", style.Bold.Render(output.Branch))
	if output.Created != "" {
		fmt.Printf("Created: %s\n", output.Created)
	}
	fmt.Printf("Ahead of main: %d commits\n", output.AheadOfMain)

	// Merged MRs
	fmt.Printf("\nMerged MRs (%d):\n", len(output.MergedMRs))
	if len(output.MergedMRs) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(none)"))
	} else {
		for _, mr := range output.MergedMRs {
			fmt.Printf("  %-12s  %s\n", mr.ID, mr.Title)
		}
	}

	// Pending MRs
	fmt.Printf("\nPending MRs (%d):\n", len(output.PendingMRs))
	if len(output.PendingMRs) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(none)"))
	} else {
		for _, mr := range output.PendingMRs {
			statusInfo := ""
			if mr.Status != "" && mr.Status != "open" {
				statusInfo = fmt.Sprintf(" (%s)", mr.Status)
			}
			fmt.Printf("  %-12s  %s%s\n", mr.ID, mr.Title, style.Dim.Render(statusInfo))
		}
	}

	return nil
}
