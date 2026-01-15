package shutdown

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	townRoot := t.TempDir()
	c := New(townRoot)

	if c.townRoot != townRoot {
		t.Errorf("expected townRoot %q, got %q", townRoot, c.townRoot)
	}

	if c.GracePeriod() != DefaultGracePeriod {
		t.Errorf("expected grace period %v, got %v", DefaultGracePeriod, c.GracePeriod())
	}
}

func TestSetGracePeriod(t *testing.T) {
	c := New("")

	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"too short", 1 * time.Second, MinGracePeriod},
		{"too long", 10 * time.Minute, MaxGracePeriod},
		{"valid", 45 * time.Second, 45 * time.Second},
		{"exactly min", MinGracePeriod, MinGracePeriod},
		{"exactly max", MaxGracePeriod, MaxGracePeriod},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.SetGracePeriod(tt.input)
			if c.GracePeriod() != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, c.GracePeriod())
			}
		})
	}
}

func TestBeginGracefulShutdown(t *testing.T) {
	townRoot := t.TempDir()
	c := New(townRoot)

	// First call should succeed
	if err := c.BeginGracefulShutdown(); err != nil {
		t.Fatalf("first BeginGracefulShutdown failed: %v", err)
	}

	// Check that signal file was created
	signalPath := c.signalPath()
	if _, err := os.Stat(signalPath); err != nil {
		t.Errorf("signal file not created: %v", err)
	}

	// Second call should fail (already in progress)
	if err := c.BeginGracefulShutdown(); err == nil {
		t.Error("second BeginGracefulShutdown should fail but succeeded")
	}

	// Check InProgress
	if !InProgress(townRoot) {
		t.Error("InProgress should return true after BeginGracefulShutdown")
	}
}

func TestEndGracefulShutdown(t *testing.T) {
	townRoot := t.TempDir()
	c := New(townRoot)

	// Begin shutdown
	if err := c.BeginGracefulShutdown(); err != nil {
		t.Fatalf("BeginGracefulShutdown failed: %v", err)
	}

	// End shutdown
	if err := c.EndGracefulShutdown(); err != nil {
		t.Fatalf("EndGracefulShutdown failed: %v", err)
	}

	// Check that signal file was removed
	signalPath := c.signalPath()
	if _, err := os.Stat(signalPath); !os.IsNotExist(err) {
		t.Error("signal file not removed after EndGracefulShutdown")
	}

	// Check InProgress
	if InProgress(townRoot) {
		t.Error("InProgress should return false after EndGracefulShutdown")
	}
}

func TestGetStartTime(t *testing.T) {
	townRoot := t.TempDir()
	c := New(townRoot)

	// Before begin
	startTime := c.GetStartTime()
	if !startTime.IsZero() {
		t.Error("GetStartTime should return zero before BeginGracefulShutdown")
	}

	// After begin - allow a 1 second tolerance for time precision
	beforeBegin := time.Now().Add(-time.Second)
	if err := c.BeginGracefulShutdown(); err != nil {
		t.Fatalf("BeginGracefulShutdown failed: %v", err)
	}
	afterBegin := time.Now().Add(time.Second)

	startTime = c.GetStartTime()
	if startTime.IsZero() {
		t.Error("GetStartTime should return non-zero after BeginGracefulShutdown")
	}
	if startTime.Before(beforeBegin) || startTime.After(afterBegin) {
		t.Errorf("GetStartTime %v is outside expected range [%v, %v]", startTime, beforeBegin, afterBegin)
	}
}

func TestElapsed(t *testing.T) {
	townRoot := t.TempDir()
	c := New(townRoot)

	// Before begin
	if elapsed := c.Elapsed(); elapsed != 0 {
		t.Errorf("Elapsed should return 0 before BeginGracefulShutdown, got %v", elapsed)
	}

	// After begin
	if err := c.BeginGracefulShutdown(); err != nil {
		t.Fatalf("BeginGracefulShutdown failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	elapsed := c.Elapsed()
	if elapsed == 0 {
		t.Error("Elapsed should return non-zero after BeginGracefulShutdown")
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("Elapsed %v is less than expected 10ms", elapsed)
	}
}

func TestRemaining(t *testing.T) {
	townRoot := t.TempDir()
	c := New(townRoot)
	// Set grace period to something larger than MinGracePeriod (5s)
	c.SetGracePeriod(6 * time.Second)

	// Before begin
	if remaining := c.Remaining(); remaining != 0 {
		t.Errorf("Remaining should return 0 before BeginGracefulShutdown, got %v", remaining)
	}

	// After begin
	if err := c.BeginGracefulShutdown(); err != nil {
		t.Fatalf("BeginGracefulShutdown failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	remaining := c.Remaining()
	if remaining == 0 {
		t.Error("Remaining should return non-zero immediately after BeginGracefulShutdown")
	}
	// Allow for timing variance - remaining should be close to 6s minus elapsed
	// We use a wide range (4.5s to 6s) to account for system scheduling variance
	if remaining > 6*time.Second || remaining < 4*time.Second {
		t.Errorf("Remaining %v is outside expected range [4s, 6s]", remaining)
	}

	// Note: We don't test full expiration here since it would take 6 seconds
	// The GracePeriodElapsed test covers that case
}

func TestSignalPath(t *testing.T) {
	townRoot := "/test/root"
	c := New(townRoot)

	expected := filepath.Join(townRoot, "daemon", signalFile)
	if got := c.signalPath(); got != expected {
		t.Errorf("signalPath() = %q, want %q", got, expected)
	}
}

func TestCoordinatorPath(t *testing.T) {
	townRoot := "/test/root"
	c := New(townRoot)

	expected := filepath.Join(townRoot, "daemon", coordinatorFile)
	if got := c.coordinatorPath(); got != expected {
		t.Errorf("coordinatorPath() = %q, want %q", got, expected)
	}
}

func TestInProgress(t *testing.T) {
	townRoot := t.TempDir()

	// Initially not in progress
	if InProgress(townRoot) {
		t.Error("InProgress should return false initially")
	}

	// Create signal file manually
	signalPath := filepath.Join(townRoot, "daemon", signalFile)
	if err := os.MkdirAll(filepath.Dir(signalPath), 0755); err != nil {
		t.Fatalf("failed to create daemon dir: %v", err)
	}
	if err := os.WriteFile(signalPath, []byte("123"), 0644); err != nil {
		t.Fatalf("failed to create signal file: %v", err)
	}

	// Now should be in progress
	if !InProgress(townRoot) {
		t.Error("InProgress should return true when signal file exists")
	}
}
