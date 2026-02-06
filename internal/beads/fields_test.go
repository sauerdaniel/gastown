package beads

import (
	"strings"
	"testing"
)

// TestParseAttachmentFieldsP1 tests attachment field parsing (P1).
func TestParseAttachmentFieldsP1(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want *AttachmentFields
	}{
		{
			name: "complete fields",
			desc: "attached_molecule: bd-abc123\nattached_at: 2026-02-06T10:00:00Z\nattached_args: --no-tmux foo bar\ndispatched_by: mayor",
			want: &AttachmentFields{
				AttachedMolecule: "bd-abc123",
				AttachedAt:       "2026-02-06T10:00:00Z",
				AttachedArgs:     "--no-tmux foo bar",
				DispatchedBy:     "mayor",
			},
		},
		{
			name: "partial fields",
			desc: "attached_molecule: bd-xyz\nattached_at: 2026-02-06T12:00:00Z",
			want: &AttachmentFields{
				AttachedMolecule: "bd-xyz",
				AttachedAt:       "2026-02-06T12:00:00Z",
			},
		},
		{
			name: "no fields",
			desc: "Some other content",
			want: nil,
		},
		{
			name: "empty description",
			desc: "",
			want: nil,
		},
		{
			name: "case insensitive keys",
			desc: "Attached-Molecule: bd-test\nAttached-At: 2026-01-01T00:00:00Z",
			want: &AttachmentFields{
				AttachedMolecule: "bd-test",
				AttachedAt:       "2026-01-01T00:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{Description: tt.desc}
			got := ParseAttachmentFields(issue)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseAttachmentFields() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseAttachmentFields() = nil, want non-nil")
			}
			if got.AttachedMolecule != tt.want.AttachedMolecule {
				t.Errorf("AttachedMolecule = %q, want %q", got.AttachedMolecule, tt.want.AttachedMolecule)
			}
			if got.AttachedAt != tt.want.AttachedAt {
				t.Errorf("AttachedAt = %q, want %q", got.AttachedAt, tt.want.AttachedAt)
			}
			if got.AttachedArgs != tt.want.AttachedArgs {
				t.Errorf("AttachedArgs = %q, want %q", got.AttachedArgs, tt.want.AttachedArgs)
			}
			if got.DispatchedBy != tt.want.DispatchedBy {
				t.Errorf("DispatchedBy = %q, want %q", got.DispatchedBy, tt.want.DispatchedBy)
			}
		})
	}
}

// TestFormatAttachmentFieldsP1 tests attachment field formatting (P1).
func TestFormatAttachmentFieldsP1(t *testing.T) {
	tests := []struct {
		name   string
		fields *AttachmentFields
		want   string
	}{
		{
			name: "all fields",
			fields: &AttachmentFields{
				AttachedMolecule: "bd-abc",
				AttachedAt:       "2026-02-06T10:00:00Z",
				AttachedArgs:     "--no-tmux",
				DispatchedBy:     "mayor",
			},
			want: "attached_molecule: bd-abc\nattached_at: 2026-02-06T10:00:00Z\nattached_args: --no-tmux\ndispatched_by: mayor",
		},
		{
			name: "partial fields",
			fields: &AttachmentFields{
				AttachedMolecule: "bd-xyz",
			},
			want: "attached_molecule: bd-xyz",
		},
		{
			name:   "nil fields",
			fields: nil,
			want:   "",
		},
		{
			name:   "empty fields",
			fields: &AttachmentFields{},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAttachmentFields(tt.fields)
			if got != tt.want {
				t.Errorf("FormatAttachmentFields() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSetAttachmentFieldsP1 tests updating issue descriptions with attachment fields (P1).
func TestSetAttachmentFieldsP1(t *testing.T) {
	tests := []struct {
		name     string
		issue    *Issue
		fields   *AttachmentFields
		wantDesc string
	}{
		{
			name: "add fields to empty description",
			issue: &Issue{
				Description: "",
			},
			fields: &AttachmentFields{
				AttachedMolecule: "bd-abc",
				AttachedAt:       "2026-02-06T10:00:00Z",
			},
			wantDesc: "attached_molecule: bd-abc\nattached_at: 2026-02-06T10:00:00Z",
		},
		{
			name: "replace existing fields",
			issue: &Issue{
				Description: "attached_molecule: bd-old\nattached_at: 2026-01-01T00:00:00Z\n\nSome other content",
			},
			fields: &AttachmentFields{
				AttachedMolecule: "bd-new",
				AttachedAt:       "2026-02-06T10:00:00Z",
			},
			wantDesc: "attached_molecule: bd-new\nattached_at: 2026-02-06T10:00:00Z\n\nSome other content",
		},
		{
			name: "preserve non-attachment content",
			issue: &Issue{
				Description: "Some important notes\n\nattached_molecule: bd-old",
			},
			fields: &AttachmentFields{
				AttachedMolecule: "bd-new",
			},
			wantDesc: "attached_molecule: bd-new\n\nSome important notes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SetAttachmentFields(tt.issue, tt.fields)
			if got != tt.wantDesc {
				t.Errorf("SetAttachmentFields() = %q, want %q", got, tt.wantDesc)
			}
		})
	}
}

// TestParseMRFieldsP1 tests merge request field parsing (P1).
func TestParseMRFieldsP1(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want *MRFields
	}{
		{
			name: "complete fields",
			desc: "branch: polecat/alice-test\ntarget: main\nsource_issue: gt-123\nworker: alice\nrig: greenplace\nconvoy: hq-convoy1",
			want: &MRFields{
				Branch:      "polecat/alice-test",
				Target:      "main",
				SourceIssue: "gt-123",
				Worker:      "alice",
				Rig:         "greenplace",
				ConvoyID:    "hq-convoy1",
			},
		},
		{
			name: "with retry count",
			desc: "branch: polecat/alice-test\nretry_count: 3\nlast_conflict_sha: abc123",
			want: &MRFields{
				Branch:          "polecat/alice-test",
				RetryCount:      3,
				LastConflictSHA: "abc123",
			},
		},
		{
			name: "no fields",
			desc: "Some other content",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{Description: tt.desc}
			got := ParseMRFields(issue)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseMRFields() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseMRFields() = nil, want non-nil")
			}
			if got.Branch != tt.want.Branch {
				t.Errorf("Branch = %q, want %q", got.Branch, tt.want.Branch)
			}
			if got.Target != tt.want.Target {
				t.Errorf("Target = %q, want %q", got.Target, tt.want.Target)
			}
			if got.RetryCount != tt.want.RetryCount {
				t.Errorf("RetryCount = %d, want %d", got.RetryCount, tt.want.RetryCount)
			}
			if got.ConvoyID != tt.want.ConvoyID {
				t.Errorf("ConvoyID = %q, want %q", got.ConvoyID, tt.want.ConvoyID)
			}
		})
	}
}

// TestParseConvoyFields tests convoy field parsing.
func TestParseConvoyFields(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want *ConvoyFields
	}{
		{
			name: "complete fields",
			desc: "rigs: greenplace,sandport\nspawned_work: gt-123,gt-456\nstage: execution\ncoordinator: mayor\nstarted: 2026-02-06T10:00:00Z",
			want: &ConvoyFields{
				Rigs:        "greenplace,sandport",
				SpawnedWork: "gt-123,gt-456",
				Stage:       "execution",
				Coordinator: "mayor",
				Started:     "2026-02-06T10:00:00Z",
			},
		},
		{
			name: "with formula",
			desc: "rigs: greenplace\nstage: planning\nformula: mol-test-convoy",
			want: &ConvoyFields{
				Rigs:    "greenplace",
				Stage:   "planning",
				Formula: "mol-test-convoy",
			},
		},
		{
			name: "no fields",
			desc: "Regular issue content",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{Description: tt.desc}
			got := ParseConvoyFields(issue)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseConvoyFields() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseConvoyFields() = nil, want non-nil")
			}
			if got.Rigs != tt.want.Rigs {
				t.Errorf("Rigs = %q, want %q", got.Rigs, tt.want.Rigs)
			}
			if got.Stage != tt.want.Stage {
				t.Errorf("Stage = %q, want %q", got.Stage, tt.want.Stage)
			}
			if got.Coordinator != tt.want.Coordinator {
				t.Errorf("Coordinator = %q, want %q", got.Coordinator, tt.want.Coordinator)
			}
		})
	}
}

// TestFormatConvoyFields tests convoy field formatting.
func TestFormatConvoyFields(t *testing.T) {
	fields := &ConvoyFields{
		Rigs:        "greenplace,sandport",
		SpawnedWork: "gt-123,gt-456",
		Stage:       "execution",
		Coordinator: "mayor",
		Started:     "2026-02-06T10:00:00Z",
	}

	got := FormatConvoyFields(fields)
	
	// Check that all fields are present
	if !strings.Contains(got, "rigs: greenplace,sandport") {
		t.Errorf("FormatConvoyFields() missing rigs field")
	}
	if !strings.Contains(got, "spawned_work: gt-123,gt-456") {
		t.Errorf("FormatConvoyFields() missing spawned_work field")
	}
	if !strings.Contains(got, "stage: execution") {
		t.Errorf("FormatConvoyFields() missing stage field")
	}
}

// TestSetConvoyFields tests updating issue descriptions with convoy fields.
func TestSetConvoyFields(t *testing.T) {
	tests := []struct {
		name     string
		issue    *Issue
		fields   *ConvoyFields
		wantDesc string
	}{
		{
			name: "add fields to empty description",
			issue: &Issue{
				Description: "",
			},
			fields: &ConvoyFields{
				Rigs:  "greenplace",
				Stage: "planning",
			},
			wantDesc: "rigs: greenplace\nstage: planning",
		},
		{
			name: "replace existing fields",
			issue: &Issue{
				Description: "rigs: oldrig\nstage: planning\n\nSome notes",
			},
			fields: &ConvoyFields{
				Rigs:  "newrig",
				Stage: "execution",
			},
			wantDesc: "rigs: newrig\nstage: execution\n\nSome notes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SetConvoyFields(tt.issue, tt.fields)
			if got != tt.wantDesc {
				t.Errorf("SetConvoyFields() = %q, want %q", got, tt.wantDesc)
			}
		})
	}
}

// TestParseHookFields tests hook field parsing.
func TestParseHookFields(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want *HookFields
	}{
		{
			name: "complete fields",
			desc: "hook_workspace: polecats/alice\nhook_worktree_base: mayor/greenplace\nhook_branch: polecat/alice-20260206-143000\nhook_artifacts: output.txt,result.json\nhook_commits: abc123,def456",
			want: &HookFields{
				Workspace:    "polecats/alice",
				WorktreeBase: "mayor/greenplace",
				Branch:       "polecat/alice-20260206-143000",
				Artifacts:    "output.txt,result.json",
				Commits:      "abc123,def456",
			},
		},
		{
			name: "partial fields",
			desc: "hook_workspace: polecats/bob\nhook_branch: polecat/bob-test",
			want: &HookFields{
				Workspace: "polecats/bob",
				Branch:    "polecat/bob-test",
			},
		},
		{
			name: "no fields",
			desc: "Regular content",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{Description: tt.desc}
			got := ParseHookFields(issue)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseHookFields() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseHookFields() = nil, want non-nil")
			}
			if got.Workspace != tt.want.Workspace {
				t.Errorf("Workspace = %q, want %q", got.Workspace, tt.want.Workspace)
			}
			if got.Branch != tt.want.Branch {
				t.Errorf("Branch = %q, want %q", got.Branch, tt.want.Branch)
			}
		})
	}
}

// TestParseRoleConfigP1 tests role configuration parsing (P1).
func TestParseRoleConfigP1(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want *RoleConfig
	}{
		{
			name: "complete config",
			desc: "session_pattern: gt-{rig}-{role}\nwork_dir_pattern: {town}/{rig}\nneeds_pre_sync: true\nstart_command: exec claude\nping_timeout: 30s\nconsecutive_failures: 3",
			want: &RoleConfig{
				SessionPattern:      "gt-{rig}-{role}",
				WorkDirPattern:      "{town}/{rig}",
				NeedsPreSync:        true,
				StartCommand:        "exec claude",
				PingTimeout:         "30s",
				ConsecutiveFailures: 3,
				EnvVars:             map[string]string{},
			},
		},
		{
			name: "with env vars",
			desc: "session_pattern: test\nenv_var: KEY1=value1\nenv_var: KEY2=value2",
			want: &RoleConfig{
				SessionPattern: "test",
				EnvVars: map[string]string{
					"KEY1": "value1",
					"KEY2": "value2",
				},
			},
		},
		{
			name: "no config",
			desc: "Some other content",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRoleConfig(tt.desc)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseRoleConfig() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseRoleConfig() = nil, want non-nil")
			}
			if got.SessionPattern != tt.want.SessionPattern {
				t.Errorf("SessionPattern = %q, want %q", got.SessionPattern, tt.want.SessionPattern)
			}
			if got.NeedsPreSync != tt.want.NeedsPreSync {
				t.Errorf("NeedsPreSync = %v, want %v", got.NeedsPreSync, tt.want.NeedsPreSync)
			}
			if got.ConsecutiveFailures != tt.want.ConsecutiveFailures {
				t.Errorf("ConsecutiveFailures = %d, want %d", got.ConsecutiveFailures, tt.want.ConsecutiveFailures)
			}
		})
	}
}

// TestExpandRolePatternP1 tests role pattern placeholder expansion (P1).
func TestExpandRolePatternP1(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		townRoot string
		rig      string
		agentName string
		role     string
		want     string
	}{
		{
			name:      "all placeholders",
			pattern:   "{town}/{rig}/polecats/{name}",
			townRoot:  "/home/user/town",
			rig:       "greenplace",
			agentName: "alice",
			role:      "polecat",
			want:      "/home/user/town/greenplace/polecats/alice",
		},
		{
			name:      "session pattern",
			pattern:   "gt-{rig}-{role}",
			townRoot:  "",
			rig:       "sandport",
			agentName: "",
			role:      "witness",
			want:      "gt-sandport-witness",
		},
		{
			name:      "no placeholders",
			pattern:   "static-path",
			townRoot:  "",
			rig:       "",
			agentName: "",
			role:      "",
			want:      "static-path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandRolePattern(tt.pattern, tt.townRoot, tt.rig, tt.agentName, tt.role)
			if got != tt.want {
				t.Errorf("ExpandRolePattern() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConvoyStageConstants verifies stage constants are defined.
func TestConvoyStageConstants(t *testing.T) {
	if ConvoyStagePlanning != "planning" {
		t.Errorf("ConvoyStagePlanning = %q, want planning", ConvoyStagePlanning)
	}
	if ConvoyStageExecution != "execution" {
		t.Errorf("ConvoyStageExecution = %q, want execution", ConvoyStageExecution)
	}
	if ConvoyStageReview != "review" {
		t.Errorf("ConvoyStageReview = %q, want review", ConvoyStageReview)
	}
	if ConvoyStageComplete != "complete" {
		t.Errorf("ConvoyStageComplete = %q, want complete", ConvoyStageComplete)
	}
}

// TestParseSynthesisFields tests synthesis field parsing.
func TestParseSynthesisFields(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want *SynthesisFields
	}{
		{
			name: "complete fields",
			desc: "convoy: hq-convoy1\nreview_id: review-123\noutput_path: /tmp/synthesis.md\nformula: mol-test",
			want: &SynthesisFields{
				ConvoyID:   "hq-convoy1",
				ReviewID:   "review-123",
				OutputPath: "/tmp/synthesis.md",
				Formula:    "mol-test",
			},
		},
		{
			name: "partial fields",
			desc: "convoy: hq-test\noutput_path: /tmp/out.txt",
			want: &SynthesisFields{
				ConvoyID:   "hq-test",
				OutputPath: "/tmp/out.txt",
			},
		},
		{
			name: "no fields",
			desc: "Regular content",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{Description: tt.desc}
			got := ParseSynthesisFields(issue)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseSynthesisFields() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseSynthesisFields() = nil, want non-nil")
			}
			if got.ConvoyID != tt.want.ConvoyID {
				t.Errorf("ConvoyID = %q, want %q", got.ConvoyID, tt.want.ConvoyID)
			}
			if got.OutputPath != tt.want.OutputPath {
				t.Errorf("OutputPath = %q, want %q", got.OutputPath, tt.want.OutputPath)
			}
		})
	}
}
