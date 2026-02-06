package witness

import (
	"testing"
	"time"
)

// TestClassifyMessage tests protocol message classification.
func TestClassifyMessage(t *testing.T) {
	tests := []struct {
		subject string
		want    ProtocolType
	}{
		{"POLECAT_DONE alice", ProtoPolecatDone},
		{"LIFECYCLE:Shutdown bob", ProtoLifecycleShutdown},
		{"HELP: Git conflict on polecat/alice-test", ProtoHelp},
		{"MERGED alice", ProtoMerged},
		{"MERGE_FAILED bob", ProtoMergeFailed},
		{"🤝 HANDOFF from alice", ProtoHandoff},
		{"SWARM_START batch-123", ProtoSwarmStart},
		{"💓 HEARTBEAT alice", ProtoHeartbeat},
		{"Unknown message format", ProtoUnknown},
		{"", ProtoUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			got := ClassifyMessage(tt.subject)
			if got != tt.want {
				t.Errorf("ClassifyMessage(%q) = %v, want %v", tt.subject, got, tt.want)
			}
		})
	}
}

// TestParsePolecatDone tests POLECAT_DONE message parsing.
func TestParsePolecatDone(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		want    *PolecatDonePayload
		wantErr bool
	}{
		{
			name:    "completed with MR",
			subject: "POLECAT_DONE alice",
			body:    "Exit: COMPLETED\nIssue: gt-123\nMR: mr-456\nBranch: polecat/alice-test",
			want: &PolecatDonePayload{
				PolecatName: "alice",
				Exit:        "COMPLETED",
				IssueID:     "gt-123",
				MRID:        "mr-456",
				Branch:      "polecat/alice-test",
			},
		},
		{
			name:    "phase complete with gate",
			subject: "POLECAT_DONE bob",
			body:    "Exit: PHASE_COMPLETE\nIssue: gt-789\nGate: gate-001",
			want: &PolecatDonePayload{
				PolecatName: "bob",
				Exit:        "PHASE_COMPLETE",
				IssueID:     "gt-789",
				Gate:        "gate-001",
			},
		},
		{
			name:    "escalated",
			subject: "POLECAT_DONE charlie",
			body:    "Exit: ESCALATED\nIssue: gt-999",
			want: &PolecatDonePayload{
				PolecatName: "charlie",
				Exit:        "ESCALATED",
				IssueID:     "gt-999",
			},
		},
		{
			name:    "invalid subject",
			subject: "NOT_POLECAT_DONE",
			body:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePolecatDone(tt.subject, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Error("ParsePolecatDone() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePolecatDone() unexpected error: %v", err)
			}
			if got.PolecatName != tt.want.PolecatName {
				t.Errorf("PolecatName = %q, want %q", got.PolecatName, tt.want.PolecatName)
			}
			if got.Exit != tt.want.Exit {
				t.Errorf("Exit = %q, want %q", got.Exit, tt.want.Exit)
			}
			if got.IssueID != tt.want.IssueID {
				t.Errorf("IssueID = %q, want %q", got.IssueID, tt.want.IssueID)
			}
			if got.Gate != tt.want.Gate {
				t.Errorf("Gate = %q, want %q", got.Gate, tt.want.Gate)
			}
		})
	}
}

// TestParseHelp tests HELP message parsing.
func TestParseHelp(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		want    *HelpPayload
		wantErr bool
	}{
		{
			name:    "git conflict help",
			subject: "HELP: Git conflict",
			body:    "Agent: alice\nIssue: gt-123\nProblem: Merge conflict on main\nTried: git merge --abort",
			want: &HelpPayload{
				Topic:   "Git conflict",
				Agent:   "alice",
				IssueID: "gt-123",
				Problem: "Merge conflict on main",
				Tried:   "git merge --abort",
			},
		},
		{
			name:    "test failure help",
			subject: "HELP: Test failures",
			body:    "Agent: bob\nIssue: gt-456\nProblem: Unit tests failing\nTried: Fixed imports but still failing",
			want: &HelpPayload{
				Topic:   "Test failures",
				Agent:   "bob",
				IssueID: "gt-456",
				Problem: "Unit tests failing",
				Tried:   "Fixed imports but still failing",
			},
		},
		{
			name:    "invalid subject",
			subject: "NOT_HELP",
			body:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHelp(tt.subject, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Error("ParseHelp() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHelp() unexpected error: %v", err)
			}
			if got.Topic != tt.want.Topic {
				t.Errorf("Topic = %q, want %q", got.Topic, tt.want.Topic)
			}
			if got.Agent != tt.want.Agent {
				t.Errorf("Agent = %q, want %q", got.Agent, tt.want.Agent)
			}
			if got.Problem != tt.want.Problem {
				t.Errorf("Problem = %q, want %q", got.Problem, tt.want.Problem)
			}
		})
	}
}

// TestParseMerged tests MERGED message parsing.
func TestParseMerged(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		want    *MergedPayload
		wantErr bool
	}{
		{
			name:    "successful merge",
			subject: "MERGED alice",
			body:    "Branch: polecat/alice-test\nIssue: gt-123\nMerged-At: 2026-02-06T10:00:00Z",
			want: &MergedPayload{
				PolecatName: "alice",
				Branch:      "polecat/alice-test",
				IssueID:     "gt-123",
			},
		},
		{
			name:    "merge without timestamp",
			subject: "MERGED bob",
			body:    "Branch: polecat/bob-feature\nIssue: gt-456",
			want: &MergedPayload{
				PolecatName: "bob",
				Branch:      "polecat/bob-feature",
				IssueID:     "gt-456",
			},
		},
		{
			name:    "invalid subject",
			subject: "NOT_MERGED",
			body:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMerged(tt.subject, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Error("ParseMerged() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMerged() unexpected error: %v", err)
			}
			if got.PolecatName != tt.want.PolecatName {
				t.Errorf("PolecatName = %q, want %q", got.PolecatName, tt.want.PolecatName)
			}
			if got.Branch != tt.want.Branch {
				t.Errorf("Branch = %q, want %q", got.Branch, tt.want.Branch)
			}
		})
	}
}

// TestParseMergeFailed tests MERGE_FAILED message parsing.
func TestParseMergeFailed(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		want    *MergeFailedPayload
		wantErr bool
	}{
		{
			name:    "build failure",
			subject: "MERGE_FAILED alice",
			body:    "Branch: polecat/alice-test\nIssue: gt-123\nFailureType: build\nError: compilation error in main.go",
			want: &MergeFailedPayload{
				PolecatName: "alice",
				Branch:      "polecat/alice-test",
				IssueID:     "gt-123",
				FailureType: "build",
				Error:       "compilation error in main.go",
			},
		},
		{
			name:    "test failure",
			subject: "MERGE_FAILED bob",
			body:    "Branch: polecat/bob-feature\nIssue: gt-456\nFailureType: test\nError: TestFoo failed",
			want: &MergeFailedPayload{
				PolecatName: "bob",
				Branch:      "polecat/bob-feature",
				IssueID:     "gt-456",
				FailureType: "test",
				Error:       "TestFoo failed",
			},
		},
		{
			name:    "invalid subject",
			subject: "NOT_MERGE_FAILED",
			body:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMergeFailed(tt.subject, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Error("ParseMergeFailed() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMergeFailed() unexpected error: %v", err)
			}
			if got.PolecatName != tt.want.PolecatName {
				t.Errorf("PolecatName = %q, want %q", got.PolecatName, tt.want.PolecatName)
			}
			if got.FailureType != tt.want.FailureType {
				t.Errorf("FailureType = %q, want %q", got.FailureType, tt.want.FailureType)
			}
			if got.Error != tt.want.Error {
				t.Errorf("Error = %q, want %q", got.Error, tt.want.Error)
			}
		})
	}
}

// TestParseHeartbeat tests HEARTBEAT message parsing.
func TestParseHeartbeat(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		want    *HeartbeatPayload
		wantErr bool
	}{
		{
			name:    "healthy worker",
			subject: "💓 HEARTBEAT alice",
			body:    "type: polecat\nrig: greenplace\nhealth: healthy\nstate: working\nassigned_work: gt-123",
			want: &HeartbeatPayload{
				WorkerName:   "alice",
				WorkerType:   "polecat",
				Rig:          "greenplace",
				Health:       "healthy",
				State:        "working",
				AssignedWork: "gt-123",
			},
		},
		{
			name:    "idle worker",
			subject: "💓 HEARTBEAT bob",
			body:    "type: dog\nrig: sandport\nhealth: healthy\nstate: idle",
			want: &HeartbeatPayload{
				WorkerName: "bob",
				WorkerType: "dog",
				Rig:        "sandport",
				Health:     "healthy",
				State:      "idle",
			},
		},
		{
			name:    "stale worker",
			subject: "💓 HEARTBEAT charlie",
			body:    "type: polecat\nrig: greenplace\nhealth: stale\nstate: working\nwork: gt-456",
			want: &HeartbeatPayload{
				WorkerName:   "charlie",
				WorkerType:   "polecat",
				Rig:          "greenplace",
				Health:       "stale",
				State:        "working",
				AssignedWork: "gt-456",
			},
		},
		{
			name:    "invalid subject",
			subject: "NOT_HEARTBEAT",
			body:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHeartbeat(tt.subject, tt.body)
			if tt.wantErr {
				if err == nil {
					t.Error("ParseHeartbeat() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHeartbeat() unexpected error: %v", err)
			}
			if got.WorkerName != tt.want.WorkerName {
				t.Errorf("WorkerName = %q, want %q", got.WorkerName, tt.want.WorkerName)
			}
			if got.WorkerType != tt.want.WorkerType {
				t.Errorf("WorkerType = %q, want %q", got.WorkerType, tt.want.WorkerType)
			}
			if got.Health != tt.want.Health {
				t.Errorf("Health = %q, want %q", got.Health, tt.want.Health)
			}
			if got.State != tt.want.State {
				t.Errorf("State = %q, want %q", got.State, tt.want.State)
			}
		})
	}
}

// TestParseSwarmStart tests SWARM_START message parsing.
func TestParseSwarmStart(t *testing.T) {
	tests := []struct {
		name string
		body string
		want *SwarmStartPayload
	}{
		{
			name: "basic swarm",
			body: "SwarmID: batch-123\nTotal: 5",
			want: &SwarmStartPayload{
				SwarmID: "batch-123",
				Total:   5,
			},
		},
		{
			name: "empty body",
			body: "",
			want: &SwarmStartPayload{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSwarmStart(tt.body)
			if err != nil {
				t.Fatalf("ParseSwarmStart() unexpected error: %v", err)
			}
			if got.SwarmID != tt.want.SwarmID {
				t.Errorf("SwarmID = %q, want %q", got.SwarmID, tt.want.SwarmID)
			}
			if got.Total != tt.want.Total {
				t.Errorf("Total = %d, want %d", got.Total, tt.want.Total)
			}
		})
	}
}

// TestCleanupWispLabels tests cleanup wisp label generation.
func TestCleanupWispLabels(t *testing.T) {
	labels := CleanupWispLabels("alice", "pending")
	
	wantLabels := []string{"cleanup", "polecat:alice", "state:pending"}
	if len(labels) != len(wantLabels) {
		t.Errorf("CleanupWispLabels() returned %d labels, want %d", len(labels), len(wantLabels))
	}
	
	for i, want := range wantLabels {
		if i >= len(labels) || labels[i] != want {
			t.Errorf("Label[%d] = %q, want %q", i, labels[i], want)
		}
	}
}

// TestSwarmWispLabels tests swarm wisp label generation.
func TestSwarmWispLabels(t *testing.T) {
	startTime := time.Date(2026, 2, 6, 10, 0, 0, 0, time.UTC)
	labels := SwarmWispLabels("batch-123", 10, 3, startTime)
	
	// Check that labels contain expected values
	expectedContains := []string{
		"swarm",
		"swarm_id:batch-123",
		"total:10",
		"completed:3",
		"start:2026-02-06T10:00:00Z",
	}
	
	for _, expected := range expectedContains {
		found := false
		for _, label := range labels {
			if label == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SwarmWispLabels() missing label: %q", expected)
		}
	}
}

// TestAssessHelpRequest tests help request assessment.
func TestAssessHelpRequest(t *testing.T) {
	tests := []struct {
		name              string
		payload           *HelpPayload
		wantCanHelp       bool
		wantNeedsEscalation bool
	}{
		{
			name: "git conflict - needs escalation",
			payload: &HelpPayload{
				Topic:   "Git conflict",
				Problem: "merge conflict on main branch",
			},
			wantCanHelp:       false,
			wantNeedsEscalation: true,
		},
		{
			name: "git push issue - can help",
			payload: &HelpPayload{
				Topic:   "Git push failed",
				Problem: "push rejected",
			},
			wantCanHelp:       true,
			wantNeedsEscalation: false,
		},
		{
			name: "test failure - needs escalation",
			payload: &HelpPayload{
				Topic:   "Test failures",
				Problem: "unit tests failing",
			},
			wantCanHelp:       false,
			wantNeedsEscalation: true,
		},
		{
			name: "build issue - can help",
			payload: &HelpPayload{
				Topic:   "Build failed",
				Problem: "compile error",
			},
			wantCanHelp:       true,
			wantNeedsEscalation: false,
		},
		{
			name: "unclear requirements - needs escalation",
			payload: &HelpPayload{
				Topic:   "Requirements unclear",
				Problem: "don't understand what to implement",
			},
			wantCanHelp:       false,
			wantNeedsEscalation: true,
		},
		{
			name: "unknown issue - needs escalation",
			payload: &HelpPayload{
				Topic:   "Unknown problem",
				Problem: "something weird happened",
			},
			wantCanHelp:       false,
			wantNeedsEscalation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AssessHelpRequest(tt.payload)
			if got.CanHelp != tt.wantCanHelp {
				t.Errorf("CanHelp = %v, want %v", got.CanHelp, tt.wantCanHelp)
			}
			if got.NeedsEscalation != tt.wantNeedsEscalation {
				t.Errorf("NeedsEscalation = %v, want %v", got.NeedsEscalation, tt.wantNeedsEscalation)
			}
			// Verify that an action or escalation reason is provided
			if got.CanHelp && got.HelpAction == "" {
				t.Error("CanHelp is true but HelpAction is empty")
			}
			if got.NeedsEscalation && got.EscalationReason == "" {
				t.Error("NeedsEscalation is true but EscalationReason is empty")
			}
		})
	}
}

// TestProtocolTypeConstants verifies protocol type constants.
func TestProtocolTypeConstants(t *testing.T) {
	tests := []struct {
		name  string
		value ProtocolType
		want  string
	}{
		{"polecat_done", ProtoPolecatDone, "polecat_done"},
		{"lifecycle_shutdown", ProtoLifecycleShutdown, "lifecycle_shutdown"},
		{"help", ProtoHelp, "help"},
		{"merged", ProtoMerged, "merged"},
		{"merge_failed", ProtoMergeFailed, "merge_failed"},
		{"handoff", ProtoHandoff, "handoff"},
		{"swarm_start", ProtoSwarmStart, "swarm_start"},
		{"heartbeat", ProtoHeartbeat, "heartbeat"},
		{"unknown", ProtoUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("Protocol constant %s = %q, want %q", tt.name, tt.value, tt.want)
			}
		})
	}
}

// TestPatternMatching tests all protocol patterns.
func TestPatternMatching(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		pattern string
		matches bool
	}{
		{"polecat done matches", "POLECAT_DONE alice", "polecat_done", true},
		{"polecat done no match", "DONE alice", "polecat_done", false},
		{"heartbeat matches", "💓 HEARTBEAT bob", "heartbeat", true},
		{"heartbeat no match", "HEARTBEAT bob", "heartbeat", false},
		{"handoff matches", "🤝 HANDOFF from alice", "handoff", true},
		{"swarm matches", "SWARM_START batch-123", "swarm_start", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protoType := ClassifyMessage(tt.subject)
			matches := (protoType != ProtoUnknown)
			if matches != tt.matches {
				t.Errorf("Pattern matching for %q: got %v, want %v (classified as %v)",
					tt.subject, matches, tt.matches, protoType)
			}
		})
	}
}
