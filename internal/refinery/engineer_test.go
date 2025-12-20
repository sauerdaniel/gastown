package refinery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/rig"
)

func TestDefaultMergeQueueConfig(t *testing.T) {
	cfg := DefaultMergeQueueConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled to be true by default")
	}
	if cfg.TargetBranch != "main" {
		t.Errorf("expected TargetBranch to be 'main', got %q", cfg.TargetBranch)
	}
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("expected PollInterval to be 30s, got %v", cfg.PollInterval)
	}
	if cfg.MaxConcurrent != 1 {
		t.Errorf("expected MaxConcurrent to be 1, got %d", cfg.MaxConcurrent)
	}
	if cfg.OnConflict != "assign_back" {
		t.Errorf("expected OnConflict to be 'assign_back', got %q", cfg.OnConflict)
	}
}

func TestEngineer_LoadConfig_NoFile(t *testing.T) {
	// Create a temp directory without config.json
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}

	e := NewEngineer(r)

	// Should not error with missing config file
	if err := e.LoadConfig(); err != nil {
		t.Errorf("unexpected error with missing config: %v", err)
	}

	// Should use defaults
	if e.config.PollInterval != 30*time.Second {
		t.Errorf("expected default PollInterval, got %v", e.config.PollInterval)
	}
}

func TestEngineer_LoadConfig_WithMergeQueue(t *testing.T) {
	// Create a temp directory with config.json
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write config file
	config := map[string]interface{}{
		"type":    "rig",
		"version": 1,
		"name":    "test-rig",
		"merge_queue": map[string]interface{}{
			"enabled":        true,
			"target_branch":  "develop",
			"poll_interval":  "10s",
			"max_concurrent": 2,
			"run_tests":      false,
			"test_command":   "make test",
		},
	}

	data, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}

	e := NewEngineer(r)

	if err := e.LoadConfig(); err != nil {
		t.Errorf("unexpected error loading config: %v", err)
	}

	// Check that config values were loaded
	if e.config.TargetBranch != "develop" {
		t.Errorf("expected TargetBranch 'develop', got %q", e.config.TargetBranch)
	}
	if e.config.PollInterval != 10*time.Second {
		t.Errorf("expected PollInterval 10s, got %v", e.config.PollInterval)
	}
	if e.config.MaxConcurrent != 2 {
		t.Errorf("expected MaxConcurrent 2, got %d", e.config.MaxConcurrent)
	}
	if e.config.RunTests != false {
		t.Errorf("expected RunTests false, got %v", e.config.RunTests)
	}
	if e.config.TestCommand != "make test" {
		t.Errorf("expected TestCommand 'make test', got %q", e.config.TestCommand)
	}

	// Check that defaults are preserved for unspecified fields
	if e.config.OnConflict != "assign_back" {
		t.Errorf("expected OnConflict default 'assign_back', got %q", e.config.OnConflict)
	}
}

func TestEngineer_LoadConfig_NoMergeQueueSection(t *testing.T) {
	// Create a temp directory with config.json without merge_queue
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write config file without merge_queue
	config := map[string]interface{}{
		"type":    "rig",
		"version": 1,
		"name":    "test-rig",
	}

	data, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}

	e := NewEngineer(r)

	if err := e.LoadConfig(); err != nil {
		t.Errorf("unexpected error loading config: %v", err)
	}

	// Should use all defaults
	if e.config.PollInterval != 30*time.Second {
		t.Errorf("expected default PollInterval, got %v", e.config.PollInterval)
	}
}

func TestEngineer_LoadConfig_InvalidPollInterval(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	config := map[string]interface{}{
		"merge_queue": map[string]interface{}{
			"poll_interval": "not-a-duration",
		},
	}

	data, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}

	e := NewEngineer(r)

	err = e.LoadConfig()
	if err == nil {
		t.Error("expected error for invalid poll_interval")
	}
}

func TestNewEngineer(t *testing.T) {
	r := &rig.Rig{
		Name: "test-rig",
		Path: "/tmp/test-rig",
	}

	e := NewEngineer(r)

	if e.rig != r {
		t.Error("expected rig to be set")
	}
	if e.beads == nil {
		t.Error("expected beads client to be initialized")
	}
	if e.config == nil {
		t.Error("expected config to be initialized with defaults")
	}
	if e.stopCh == nil {
		t.Error("expected stopCh to be initialized")
	}
}

func TestProcessMR_NoMRFields(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}
	e := NewEngineer(r)

	// Create an issue without MR fields
	issue := &beads.Issue{
		ID:          "gt-mr-test",
		Title:       "Test MR",
		Type:        "merge-request",
		Description: "This issue has no MR fields",
	}

	result := e.ProcessMR(nil, issue)

	if result.Success {
		t.Error("expected failure when MR fields are missing")
	}
	if result.Error != "no MR fields found in description" {
		t.Errorf("expected 'no MR fields found in description', got %q", result.Error)
	}
}

func TestProcessMR_MissingBranch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}
	e := NewEngineer(r)

	// Create an issue with MR fields but no branch
	issue := &beads.Issue{
		ID:          "gt-mr-test",
		Title:       "Test MR",
		Type:        "merge-request",
		Description: "target: main\nworker: TestWorker",
	}

	result := e.ProcessMR(nil, issue)

	if result.Success {
		t.Error("expected failure when branch field is missing")
	}
	if result.Error != "branch field is required in merge request" {
		t.Errorf("expected 'branch field is required in merge request', got %q", result.Error)
	}
}

func TestProcessResult_Fields(t *testing.T) {
	// Test that ProcessResult can represent various failure states
	tests := []struct {
		name   string
		result ProcessResult
	}{
		{
			name: "success",
			result: ProcessResult{
				Success:     true,
				MergeCommit: "abc123",
			},
		},
		{
			name: "conflict",
			result: ProcessResult{
				Success:  false,
				Error:    "merge conflict",
				Conflict: true,
			},
		},
		{
			name: "tests_failed",
			result: ProcessResult{
				Success:     false,
				Error:       "tests failed",
				TestsFailed: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the struct fields work as expected
			if tt.result.Success && tt.result.MergeCommit == "" && !tt.result.Conflict && !tt.result.TestsFailed {
				// This is fine for a non-failing success case
			}
		})
	}
}

func TestEngineer_RunTestsEmptyCommand(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}
	e := NewEngineer(r)

	// Empty test command should not error
	if err := e.runTests(""); err != nil {
		t.Errorf("empty test command should not error, got: %v", err)
	}
}

func TestEngineer_RunTestsSuccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}
	e := NewEngineer(r)

	// Run a simple command that should succeed
	if err := e.runTests("true"); err != nil {
		t.Errorf("expected 'true' command to succeed, got: %v", err)
	}
}

func TestEngineer_RunTestsFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engineer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	r := &rig.Rig{
		Name: "test-rig",
		Path: tmpDir,
	}
	e := NewEngineer(r)

	// Run a command that should fail
	err = e.runTests("false")
	if err == nil {
		t.Error("expected 'false' command to fail")
	}
}
