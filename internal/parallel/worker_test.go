package parallel

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_Execute(t *testing.T) {
	tests := []struct {
		name        string
		maxWorkers  int
		tasks       []func() int
		wantResults []int
	}{
		{
			name:        "no tasks",
			maxWorkers:  4,
			tasks:       nil,
			wantResults: nil,
		},
		{
			name:       "single task",
			maxWorkers: 4,
			tasks:      []func() int{func() int { return 1 }},
			wantResults: []int{1},
		},
		{
			name:       "multiple tasks",
			maxWorkers: 2,
			tasks: []func() int{
				func() int { return 1 },
				func() int { return 2 },
				func() int { return 3 },
			},
			wantResults: []int{1, 2, 3},
		},
		{
			name:       "more tasks than workers",
			maxWorkers: 2,
			tasks: []func() int{
				func() int { return 1 },
				func() int { return 2 },
				func() int { return 3 },
				func() int { return 4 },
			},
			// Results may be in different order, just check count
			wantResults: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool[int](tt.maxWorkers)
			results := pool.Execute(tt.tasks...)

			if tt.wantResults != nil {
				if len(results) != len(tt.wantResults) {
					t.Errorf("Execute() returned %d results, want %d", len(results), len(tt.wantResults))
				}
			} else if len(results) != len(tt.tasks) {
				t.Errorf("Execute() returned %d results, want %d", len(results), len(tt.tasks))
			}
		})
	}
}

func TestWorkerPool_ExecuteNamed(t *testing.T) {
	tasks := map[string]func() int{
		"task1": func() int { return 1 },
		"task2": func() int { return 2 },
		"task3": func() int { return 3 },
	}

	pool := NewWorkerPool[int](2)
	results := pool.ExecuteNamed(tasks)

	if len(results) != 3 {
		t.Errorf("ExecuteNamed() returned %d results, want 3", len(results))
	}

	if results["task1"] != 1 {
		t.Errorf("ExecuteNamed()[task1] = %d, want 1", results["task1"])
	}
	if results["task2"] != 2 {
		t.Errorf("ExecuteNamed()[task2] = %d, want 2", results["task2"])
	}
	if results["task3"] != 3 {
		t.Errorf("ExecuteNamed()[task3] = %d, want 3", results["task3"])
	}
}

func TestWorkerPool_Concurrency(t *testing.T) {
	// Test that the worker pool limits concurrency
	maxConcurrent := int32(0)
	maxConcurrentPtr := &maxConcurrent

	// Create tasks that increment and decrement a counter
	numTasks := 10
	maxWorkers := 3
	tasks := make([]func() int, numTasks)
	for i := 0; i < numTasks; i++ {
		tasks[i] = func() int {
			current := atomic.AddInt32(maxConcurrentPtr, 1)
			defer atomic.AddInt32(maxConcurrentPtr, -1)

			// Give other goroutines a chance to start
			time.Sleep(10 * time.Millisecond)

			// Check that we didn't exceed max workers
			if current > int32(maxWorkers) {
				t.Errorf("concurrent tasks exceeded max workers: %d > %d", current, maxWorkers)
			}
			return int(current)
		}
	}

	pool := NewWorkerPool[int](maxWorkers)
	results := pool.Execute(tasks...)

	if len(results) != numTasks {
		t.Errorf("Execute() returned %d results, want %d", len(results), numTasks)
	}
}

func TestExecuteWithError(t *testing.T) {
	tests := []struct {
		name    string
		tasks   []func() (int, error)
		wantLen int
	}{
		{
			name:    "all successful",
			tasks: []func() (int, error){
				func() (int, error) { return 1, nil },
				func() (int, error) { return 2, nil },
			},
			wantLen: 2,
		},
		{
			name:    "mixed success and error",
			tasks: []func() (int, error){
				func() (int, error) { return 1, nil },
				func() (int, error) { return 0, fmt.Errorf("error") },
				func() (int, error) { return 3, nil },
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, errs := ExecuteWithError(2, tt.tasks...)

			if len(results) != tt.wantLen {
				t.Errorf("ExecuteWithError() returned %d results, want %d", len(results), tt.wantLen)
			}
			if len(errs) != tt.wantLen {
				t.Errorf("ExecuteWithError() returned %d errors, want %d", len(errs), tt.wantLen)
			}
		})
	}
}

func TestPrefetch(t *testing.T) {
	// Test successful prefetches
	fetchFuncs := []func() (string, int, error){
		func() (string, int, error) { return "item1", 1, nil },
		func() (string, int, error) { return "item2", 2, nil },
		func() (string, int, error) { return "item3", 3, nil },
	}

	values, errors := Prefetch(fetchFuncs...)

	if len(values) != 3 {
		t.Errorf("Prefetch() returned %d values, want 3", len(values))
	}
	if len(errors) != 0 {
		t.Errorf("Prefetch() returned %d errors, want 0", len(errors))
	}
	if values["item1"] != 1 {
		t.Errorf("Prefetch()[item1] = %d, want 1", values["item1"])
	}
}

func TestPrefetch_WithErrors(t *testing.T) {
	// Test prefetches with errors
	fetchFuncs := []func() (string, int, error){
		func() (string, int, error) { return "item1", 1, nil },
		func() (string, int, error) { return "item2", 0, fmt.Errorf("fetch error") },
		func() (string, int, error) { return "item3", 3, nil },
	}

	values, errors := Prefetch(fetchFuncs...)

	if len(values) != 2 {
		t.Errorf("Prefetch() returned %d values, want 2", len(values))
	}
	if len(errors) != 1 {
		t.Errorf("Prefetch() returned %d errors, want 1", len(errors))
	}
	if errors["item2"] == nil {
		t.Errorf("Prefetch()[item2] error = nil, want error")
	}
}

func TestBatch(t *testing.T) {
	tasks := make([]func() int, 10)
	for i := 0; i < 10; i++ {
		i := i
		tasks[i] = func() int { return i }
	}

	results := Batch(tasks, 3)

	if len(results) != 10 {
		t.Errorf("Batch() returned %d results, want 10", len(results))
	}
}

func BenchmarkWorkerPool_Execute(b *testing.B) {
	pool := NewWorkerPool[int](4)
	tasks := make([]func() int, 100)
	for i := 0; i < 100; i++ {
		tasks[i] = func() int { return 1 }
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Execute(tasks...)
	}
}

func BenchmarkWorkerPool_ExecuteManyWorkers(b *testing.B) {
	pool := NewWorkerPool[int](100)
	tasks := make([]func() int, 100)
	for i := 0; i < 100; i++ {
		tasks[i] = func() int { return 1 }
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Execute(tasks...)
	}
}
