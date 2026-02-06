package beads

import (
	"fmt"
)

// ValidateHeartbeatTimeout validates that a heartbeat timeout is within allowed range.
// Timeout must be between 60 and 3600 seconds (1 minute to 1 hour).
func ValidateHeartbeatTimeout(timeoutSec int) error {
	const (
		minTimeout = 60   // 1 minute
		maxTimeout = 3600 // 1 hour
	)

	if timeoutSec < minTimeout {
		return fmt.Errorf("heartbeat timeout %d seconds is too short (minimum: %d seconds)", timeoutSec, minTimeout)
	}
	if timeoutSec > maxTimeout {
		return fmt.Errorf("heartbeat timeout %d seconds is too long (maximum: %d seconds)", timeoutSec, maxTimeout)
	}
	return nil
}

// ValidateConvoyStage validates that a convoy stage is valid.
func ValidateConvoyStage(stage string) error {
	validStages := map[string]bool{
		ConvoyStagePlanning:  true,
		ConvoyStageExecution: true,
		ConvoyStageReview:    true,
		ConvoyStageComplete:  true,
	}

	if !validStages[stage] {
		return fmt.Errorf("invalid convoy stage %q (must be: planning, execution, review, or complete)", stage)
	}
	return nil
}

// ValidateConvoyStageTransition validates that a convoy stage transition is allowed.
// Valid transitions:
//   planning → execution → review → complete
// Backward transitions are not allowed (except for reopening, which goes to planning).
func ValidateConvoyStageTransition(currentStage, newStage string) error {
	// Validate both stages are valid
	if err := ValidateConvoyStage(currentStage); err != nil {
		return fmt.Errorf("current stage: %w", err)
	}
	if err := ValidateConvoyStage(newStage); err != nil {
		return fmt.Errorf("new stage: %w", err)
	}

	// Define stage order
	stageOrder := map[string]int{
		ConvoyStagePlanning:  1,
		ConvoyStageExecution: 2,
		ConvoyStageReview:    3,
		ConvoyStageComplete:  4,
	}

	currentOrder := stageOrder[currentStage]
	newOrder := stageOrder[newStage]

	// Allow forward transitions only
	if newOrder <= currentOrder {
		return fmt.Errorf("invalid stage transition from %q to %q (must move forward: planning → execution → review → complete)", currentStage, newStage)
	}

	return nil
}

// ValidateConvoyStageTransitionWithReopening is like ValidateConvoyStageTransition
// but allows reopening a completed convoy (complete → planning).
func ValidateConvoyStageTransitionWithReopening(currentStage, newStage string) error {
	// Allow reopening: complete → planning
	if currentStage == ConvoyStageComplete && newStage == ConvoyStagePlanning {
		return nil
	}

	return ValidateConvoyStageTransition(currentStage, newStage)
}
