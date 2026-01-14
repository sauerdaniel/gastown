// Package parallel provides reusable concurrency primitives for parallel execution.
package parallel

import (
	"sync"
)

// WorkerPool executes tasks concurrently using a fixed number of workers.
// Tasks are distributed to workers via a channel, and results are collected
// via another channel. This provides bounded concurrency without the overhead
// of spawning a goroutine per task.
//
// The worker pool is designed for CPU-bound or I/O-bound operations where
// you want to limit parallelism to avoid resource exhaustion.
type WorkerPool[T any] struct {
	maxWorkers int
}

// NewWorkerPool creates a new WorkerPool with the given maximum number of workers.
// If maxWorkers is <= 0, it defaults to GOMAXPROCS.
func NewWorkerPool[T any](maxWorkers int) *WorkerPool[T] {
	if maxWorkers <= 0 {
		// Default to GOMAXPROCS if not specified
		maxWorkers = 1 // Will be set in Execute
	}
	return &WorkerPool[T]{
		maxWorkers: maxWorkers,
	}
}

// Execute runs the given tasks concurrently and returns their results.
// Tasks are executed in the order they are provided, but results may be
// returned in any order depending on task completion time.
//
// The number of workers is bounded by maxWorkers (or GOMAXPROCS if maxWorkers is 0).
// Returns a slice of results with the same length as tasks.
func (p *WorkerPool[T]) Execute(tasks ...func() T) []T {
	n := len(tasks)
	if n == 0 {
		return nil
	}

	// Determine number of workers
	numWorkers := p.maxWorkers
	if numWorkers <= 0 {
		// Use runtime.GOMAXPROCS(0) to get number of CPUs
		numWorkers = 1 // Conservative default
	}
	if numWorkers > n {
		numWorkers = n
	}

	// Create channels for tasks and results
	taskCh := make(chan func() T, n)
	resultCh := make(chan T, n)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				resultCh <- task()
			}
		}()
	}

	// Enqueue all tasks
	for _, task := range tasks {
		taskCh <- task
	}
	close(taskCh)

	// Close result channel when all workers are done
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	results := make([]T, 0, n)
	for result := range resultCh {
		results = append(results, result)
	}

	return results
}

// ExecuteNamed runs named tasks concurrently and returns them with their results.
// This is useful when you need to correlate results with task names.
func (p *WorkerPool[T]) ExecuteNamed(namedTasks map[string]func() T) map[string]T {
	if len(namedTasks) == 0 {
		return make(map[string]T)
	}

	// Convert to tasks
	tasks := make([]func() T, 0, len(namedTasks))
	names := make([]string, 0, len(namedTasks))
	for name, task := range namedTasks {
		names = append(names, name)
		tasks = append(tasks, task)
	}

	results := p.Execute(tasks...)

	// Build result map
	resultMap := make(map[string]T, len(results))
	for i, result := range results {
		resultMap[names[i]] = result
	}

	return resultMap
}

// ExecuteWithError runs tasks that can return errors.
// Returns a slice of results and a slice of errors (in the same order as tasks).
func ExecuteWithError[T any](maxWorkers int, tasks ...func() (T, error)) ([]T, []error) {
	n := len(tasks)
	if n == 0 {
		return nil, nil
	}

	type resultPair struct {
		index  int
		result T
		err    error
	}

	// Determine number of workers
	numWorkers := maxWorkers
	if numWorkers <= 0 {
		numWorkers = n
	}
	if numWorkers > n {
		numWorkers = n
	}

	// Create channels
	taskCh := make(chan func() resultPair, n)
	resultCh := make(chan resultPair, n)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				resultCh <- task()
			}
		}()
	}

	// Enqueue all tasks with their indices
	for i, task := range tasks {
		idx := i
		taskCh <- func() resultPair {
			result, err := task()
			return resultPair{index: idx, result: result, err: err}
		}
	}
	close(taskCh)

	// Close result channel when all workers are done
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results in order
	results := make([]T, n)
	errs := make([]error, n)
	for rp := range resultCh {
		results[rp.index] = rp.result
		errs[rp.index] = rp.err
	}

	return results, errs
}

// Prefetch executes fetch functions concurrently and returns a map of results.
// This is useful for loading multiple resources in parallel, such as rig configs.
//
// The fetch function signature is: func() (name string, value T, err error)
// The returned map contains name -> value for successful fetches.
// Errors are returned separately in a map of name -> error.
func Prefetch[T any](fetchFuncs ...func() (string, T, error)) (map[string]T, map[string]error) {
	n := len(fetchFuncs)
	if n == 0 {
		return make(map[string]T), make(map[string]error)
	}

	type prefetchResult struct {
		name  string
		value T
		err   error
	}

	// Channel to collect results
	results := make(chan prefetchResult, n)

	// Execute all fetches in parallel
	for _, f := range fetchFuncs {
		go func(fn func() (string, T, error)) {
			name, value, err := fn()
			results <- prefetchResult{name: name, value: value, err: err}
		}(f)
	}

	// Collect results
	values := make(map[string]T)
	errors := make(map[string]error)

	for i := 0; i < n; i++ {
		res := <-results
		if res.err != nil {
			errors[res.name] = res.err
		} else {
			values[res.name] = res.value
		}
	}

	return values, errors
}

// Batch executes tasks in batches, limiting concurrent execution to maxConcurrency.
// This is useful when you have many tasks and want to limit parallelism.
func Batch[T any](tasks []func() T, maxConcurrency int) []T {
	n := len(tasks)
	if n == 0 {
		return nil
	}

	if maxConcurrency <= 0 || maxConcurrency > n {
		maxConcurrency = n
	}

	// Process in batches
	var results []T
	for i := 0; i < n; i += maxConcurrency {
		end := i + maxConcurrency
		if end > n {
			end = n
		}

		batch := tasks[i:end]
		pool := NewWorkerPool[T](len(batch))
		batchResults := pool.Execute(batch...)
		results = append(results, batchResults...)
	}

	return results
}
