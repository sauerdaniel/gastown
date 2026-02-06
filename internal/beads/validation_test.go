package beads

import (
	"strings"
	"testing"
)

// TestValidateHeartbeatTimeout tests heartbeat timeout validation.
func TestValidateHeartbeatTimeout(t *testing.T) {
	tests := []struct {
		name       string
		timeoutSec int
		wantErr    bool
	}{
		{"below minimum", 30, true},
		{"minimum valid", 60, false},
		{"normal timeout", 180, false},
		{"maximum valid", 3600, false},
		{"above maximum", 7200, true},
		{"zero timeout", 0, true},
		{"negative timeout", -100, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHeartbeatTimeout(tt.timeoutSec)
			if tt.wantErr && err == nil {
				t.Error("ValidateHeartbeatTimeout() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateHeartbeatTimeout() unexpected error: %v", err)
			}
		})
	}
}

// TestValidateConvoyStage tests convoy stage validation.
func TestValidateConvoyStage(t *testing.T) {
	tests := []struct {
		name    string
		stage   string
		wantErr bool
	}{
		{"planning stage", ConvoyStagePlanning, false},
		{"execution stage", ConvoyStageExecution, false},
		{"review stage", ConvoyStageReview, false},
		{"complete stage", ConvoyStageComplete, false},
		{"invalid stage", "invalid", true},
		{"empty stage", "", true},
		{"mixed case", "Planning", true},
		{"typo", "planing", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConvoyStage(tt.stage)
			if tt.wantErr && err == nil {
				t.Error("ValidateConvoyStage() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateConvoyStage() unexpected error: %v", err)
			}
		})
	}
}

// TestValidateConvoyStageTransition tests stage transition validation.
func TestValidateConvoyStageTransition(t *testing.T) {
	tests := []struct {
		name         string
		currentStage string
		newStage     string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "planning to execution",
			currentStage: ConvoyStagePlanning,
			newStage:     ConvoyStageExecution,
			wantErr:      false,
		},
		{
			name:         "execution to review",
			currentStage: ConvoyStageExecution,
			newStage:     ConvoyStageReview,
			wantErr:      false,
		},
		{
			name:         "review to complete",
			currentStage: ConvoyStageReview,
			newStage:     ConvoyStageComplete,
			wantErr:      false,
		},
		{
			name:         "planning to complete (skip ahead)",
			currentStage: ConvoyStagePlanning,
			newStage:     ConvoyStageComplete,
			wantErr:      false, // Forward transitions are allowed (can skip)
		},
		{
			name:         "execution to planning (backward)",
			currentStage: ConvoyStageExecution,
			newStage:     ConvoyStagePlanning,
			wantErr:      true,
			errContains:  "must move forward",
		},
		{
			name:         "complete to review (backward)",
			currentStage: ConvoyStageComplete,
			newStage:     ConvoyStageReview,
			wantErr:      true,
			errContains:  "must move forward",
		},
		{
			name:         "same stage",
			currentStage: ConvoyStageExecution,
			newStage:     ConvoyStageExecution,
			wantErr:      true,
			errContains:  "must move forward",
		},
		{
			name:         "invalid current stage",
			currentStage: "invalid",
			newStage:     ConvoyStageExecution,
			wantErr:      true,
			errContains:  "current stage",
		},
		{
			name:         "invalid new stage",
			currentStage: ConvoyStagePlanning,
			newStage:     "invalid",
			wantErr:      true,
			errContains:  "new stage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConvoyStageTransition(tt.currentStage, tt.newStage)
			if tt.wantErr {
				if err == nil {
					t.Error("ValidateConvoyStageTransition() expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateConvoyStageTransition() error = %v, want to contain %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateConvoyStageTransition() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestValidateConvoyStageTransitionWithReopening tests stage transition validation with reopening.
func TestValidateConvoyStageTransitionWithReopening(t *testing.T) {
	tests := []struct {
		name         string
		currentStage string
		newStage     string
		wantErr      bool
	}{
		{
			name:         "normal forward transition",
			currentStage: ConvoyStagePlanning,
			newStage:     ConvoyStageExecution,
			wantErr:      false,
		},
		{
			name:         "reopen complete to planning",
			currentStage: ConvoyStageComplete,
			newStage:     ConvoyStagePlanning,
			wantErr:      false, // Allowed for reopening
		},
		{
			name:         "backward transition (not reopening)",
			currentStage: ConvoyStageReview,
			newStage:     ConvoyStageExecution,
			wantErr:      true,
		},
		{
			name:         "complete to review (not planning)",
			currentStage: ConvoyStageComplete,
			newStage:     ConvoyStageReview,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConvoyStageTransitionWithReopening(tt.currentStage, tt.newStage)
			if tt.wantErr && err == nil {
				t.Error("ValidateConvoyStageTransitionWithReopening() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateConvoyStageTransitionWithReopening() unexpected error: %v", err)
			}
		})
	}
}

// TestStageTransitionPaths tests valid full stage transition paths.
func TestStageTransitionPaths(t *testing.T) {
	// Test a full valid path: planning → execution → review → complete
	stages := []string{ConvoyStagePlanning, ConvoyStageExecution, ConvoyStageReview, ConvoyStageComplete}
	
	for i := 0; i < len(stages)-1; i++ {
		err := ValidateConvoyStageTransition(stages[i], stages[i+1])
		if err != nil {
			t.Errorf("Valid path stage %s → %s failed: %v", stages[i], stages[i+1], err)
		}
	}
}

// TestStageTransitionSkipping tests that skipping stages is allowed.
func TestStageTransitionSkipping(t *testing.T) {
	// Test that you can skip stages (e.g., planning → review)
	err := ValidateConvoyStageTransition(ConvoyStagePlanning, ConvoyStageReview)
	if err != nil {
		t.Errorf("Skipping stages should be allowed: %v", err)
	}
	
	// Test planning directly to complete
	err = ValidateConvoyStageTransition(ConvoyStagePlanning, ConvoyStageComplete)
	if err != nil {
		t.Errorf("Skipping to complete should be allowed: %v", err)
	}
}

// TestHeartbeatTimeoutEdgeCases tests edge cases for heartbeat timeout validation.
func TestHeartbeatTimeoutEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		timeoutSec int
		wantErr    bool
	}{
		{"exactly minimum", 60, false},
		{"one below minimum", 59, true},
		{"exactly maximum", 3600, false},
		{"one above maximum", 3601, true},
		{"very large", 1000000, true},
		{"very negative", -1000000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHeartbeatTimeout(tt.timeoutSec)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateHeartbeatTimeout(%d) expected error, got nil", tt.timeoutSec)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateHeartbeatTimeout(%d) unexpected error: %v", tt.timeoutSec, err)
			}
		})
	}
}
