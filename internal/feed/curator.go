// Package feed provides the feed daemon that curates raw events into a user-facing feed.
//
// The curator:
// 1. Tails ~/gt/.events.jsonl (raw events)
// 2. Filters by visibility tag (drops audit-only events)
// 3. Deduplicates repeated updates (5 molecule updates → "agent active")
// 4. Aggregates related events (3 issues closed → "batch complete")
// 5. Writes curated events to ~/gt/.feed.jsonl
package feed

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/events"
)

// FeedFile is the name of the curated feed file.
const FeedFile = ".feed.jsonl"

// FeedEvent is the structure of events written to the feed.
type FeedEvent struct {
	Timestamp string                 `json:"ts"`
	Source    string                 `json:"source"`
	Type      string                 `json:"type"`
	Actor     string                 `json:"actor"`
	Summary   string                 `json:"summary"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	Count     int                    `json:"count,omitempty"` // For aggregated events
}

// ringBuffer is a circular buffer for caching recent feed events in memory.
// This eliminates repeated full file reads for dedupe/aggregation checks.
type ringBuffer struct {
	events []FeedEvent
	size   int
	head   int
	count  int
	mu     sync.RWMutex
}

// newRingBuffer creates a new ring buffer with the given capacity.
func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{
		events: make([]FeedEvent, capacity),
		size:   capacity,
	}
}

// add adds a new event to the ring buffer, overwriting the oldest if full.
func (rb *ringBuffer) add(event FeedEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.events[rb.head] = event
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// recent returns events within the given time window, most recent first.
func (rb *ringBuffer) recent(window time.Duration) []FeedEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-window)
	var result []FeedEvent

	// Iterate backwards from most recent to oldest
	for i := 0; i < rb.count; i++ {
		// Calculate index: (head - 1 - i + size) % size
		idx := (rb.head - 1 - i + rb.size) % rb.size
		event := rb.events[idx]

		// Parse timestamp
		ts, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			continue
		}

		// Stop if we've gone past the window
		if ts.Before(cutoff) {
			break
		}

		result = append(result, event)
	}

	return result
}

// eventsRingBuffer is a circular buffer for caching recent raw events in memory.
type eventsRingBuffer struct {
	events []events.Event
	size   int
	head   int
	count  int
	mu     sync.RWMutex
}

// newEventsRingBuffer creates a new events ring buffer with the given capacity.
func newEventsRingBuffer(capacity int) *eventsRingBuffer {
	return &eventsRingBuffer{
		events: make([]events.Event, capacity),
		size:   capacity,
	}
}

// add adds a new event to the ring buffer, overwriting the oldest if full.
func (rb *eventsRingBuffer) add(event events.Event) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.events[rb.head] = event
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// recent returns events within the given time window, most recent first.
func (rb *eventsRingBuffer) recent(window time.Duration) []events.Event {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-window)
	var result []events.Event

	// Iterate backwards from most recent to oldest
	for i := 0; i < rb.count; i++ {
		// Calculate index: (head - 1 - i + size) % size
		idx := (rb.head - 1 - i + rb.size) % rb.size
		event := rb.events[idx]

		// Parse timestamp
		ts, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			continue
		}

		// Stop if we've gone past the window
		if ts.Before(cutoff) {
			break
		}

		result = append(result, event)
	}

	return result
}

// Curator manages the feed curation process.
// Uses in-memory ring buffer caches for efficient dedupe/aggregation lookups.
type Curator struct {
	townRoot   string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	feedCache  *ringBuffer        // Cache of recent feed events for dedupe
	eventsCache *eventsRingBuffer  // Cache of recent raw events for aggregation
}

// Deduplication/aggregation settings
const (
	// Dedupe window for repeated done events from same actor
	doneDedupeWindow = 10 * time.Second

	// Aggregation window for sling events
	slingAggregateWindow = 30 * time.Second

	// Mail aggregation window
	mailAggregateWindow = 30 * time.Second

	// Minimum events to trigger aggregation
	minAggregateCount = 3
)

// NewCurator creates a new feed curator.
func NewCurator(townRoot string) *Curator {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Curator{
		townRoot:    townRoot,
		ctx:         ctx,
		cancel:      cancel,
		feedCache:   newRingBuffer(1000), // Cache last 1000 feed events (~10-30s of activity)
		eventsCache: newEventsRingBuffer(5000), // Cache last 5000 raw events for sling counting
	}
	// Initialize caches from existing files on startup
	c.initFeedCache()
	c.initEventsCache()
	return c
}

// initFeedCache loads recent feed events from disk into the ring buffer cache.
// This primes the cache so we don't start with an empty state after restart.
func (c *Curator) initFeedCache() {
	feedPath := filepath.Join(c.townRoot, FeedFile)
	data, err := os.ReadFile(feedPath)
	if err != nil {
		return // No existing feed file, start with empty cache
	}

	// Parse lines and add to cache (most recent last, so they're most recent in buffer)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event FeedEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		c.feedCache.add(event)
	}
}

// initEventsCache loads recent raw events from disk into the ring buffer cache.
func (c *Curator) initEventsCache() {
	eventsPath := filepath.Join(c.townRoot, events.EventsFile)
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		return // No existing events file, start with empty cache
	}

	// Parse lines and add to cache
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event events.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		c.eventsCache.add(event)
	}
}

// Start begins the curator goroutine.
func (c *Curator) Start() error {
	eventsPath := filepath.Join(c.townRoot, events.EventsFile)

	// Open events file, creating if needed
	file, err := os.OpenFile(eventsPath, os.O_RDONLY|os.O_CREATE, 0644) //nolint:gosec // G302: events file is non-sensitive operational data
	if err != nil {
		return fmt.Errorf("opening events file: %w", err)
	}

	// Seek to end to only process new events
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		_ = file.Close() //nolint:gosec // G104: best effort cleanup on error
		return fmt.Errorf("seeking to end: %w", err)
	}

	c.wg.Add(1)
	go c.run(file)

	return nil
}

// Stop gracefully stops the curator.
func (c *Curator) Stop() {
	c.cancel()
	c.wg.Wait()
}

// run is the main curator loop.
// ZFC: No in-memory state to clean up - state is derived from the events file.
func (c *Curator) run(file *os.File) {
	defer c.wg.Done()
	defer file.Close()

	reader := bufio.NewReader(file)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return

		case <-ticker.C:
			// Read available lines
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					break // No more data available
				}
				c.processLine(line)
			}
		}
	}
}

// processLine processes a single line from the events file.
func (c *Curator) processLine(line string) {
	if line == "" || line == "\n" {
		return
	}

	var rawEvent events.Event
	if err := json.Unmarshal([]byte(line), &rawEvent); err != nil {
		return // Skip malformed lines
	}

	// Cache all raw events for aggregation queries (sling counting)
	c.eventsCache.add(rawEvent)

	// Filter by visibility - only process feed-visible events
	if rawEvent.Visibility != events.VisibilityFeed && rawEvent.Visibility != events.VisibilityBoth {
		return
	}

	// Apply deduplication and aggregation
	if c.shouldDedupe(&rawEvent) {
		return
	}

	// Write to feed
	c.writeFeedEvent(&rawEvent)
}

// shouldDedupe checks if an event should be deduplicated.
// ZFC: Derives state from the FEED file (what we've already output), not in-memory cache.
// Returns true if the event should be dropped.
func (c *Curator) shouldDedupe(event *events.Event) bool {
	switch event.Type {
	case events.TypeDone:
		// Dedupe repeated done events from same actor within window
		// Check if we've already written a done event for this actor to the feed
		recentFeedEvents := c.readRecentFeedEvents(doneDedupeWindow)
		for _, e := range recentFeedEvents {
			if e.Type == events.TypeDone && e.Actor == event.Actor {
				return true // Skip duplicate (already in feed)
			}
		}
		return false
	}

	// Sling and mail events are not deduplicated, only aggregated in writeFeedEvent
	return false
}

// readRecentFeedEvents reads feed events from the in-memory cache within the given time window.
// Uses ring buffer cache for O(1) lookups instead of reading the entire file.
func (c *Curator) readRecentFeedEvents(window time.Duration) []FeedEvent {
	return c.feedCache.recent(window)
}

// readRecentEvents reads events from the in-memory cache within the given time window.
// Uses ring buffer cache for O(1) lookups instead of reading the entire file.
func (c *Curator) readRecentEvents(window time.Duration) []events.Event {
	return c.eventsCache.recent(window)
}

// countRecentSlings counts sling events from an actor within the given window.
// ZFC: Derives count from the events file, not in-memory cache.
func (c *Curator) countRecentSlings(actor string, window time.Duration) int {
	recentEvents := c.readRecentEvents(window)
	count := 0
	for _, e := range recentEvents {
		if e.Type == events.TypeSling && e.Actor == actor {
			count++
		}
	}
	return count
}

// writeFeedEvent writes a curated event to the feed file and updates the cache.
func (c *Curator) writeFeedEvent(event *events.Event) {
	feedEvent := FeedEvent{
		Timestamp: event.Timestamp,
		Source:    event.Source,
		Type:      event.Type,
		Actor:     event.Actor,
		Summary:   c.generateSummary(event),
		Payload:   event.Payload,
	}

	// Check for aggregation opportunity (derive from events file)
	if event.Type == events.TypeSling {
		slingCount := c.countRecentSlings(event.Actor, slingAggregateWindow)
		if slingCount >= minAggregateCount {
			feedEvent.Count = slingCount
			feedEvent.Summary = fmt.Sprintf("%s dispatching work to %d agents", event.Actor, slingCount)
		}
	}

	data, err := json.Marshal(feedEvent)
	if err != nil {
		return
	}
	data = append(data, '\n')

	feedPath := filepath.Join(c.townRoot, FeedFile)
	f, err := os.OpenFile(feedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gosec // G302: feed file is non-sensitive operational data
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return
	}

	// Update cache with the newly written event
	c.feedCache.add(feedEvent)
}

// generateSummary creates a human-readable summary of an event.
func (c *Curator) generateSummary(event *events.Event) string {
	switch event.Type {
	case events.TypeSling:
		if target, ok := event.Payload["target"].(string); ok {
			if bead, ok := event.Payload["bead"].(string); ok {
				return fmt.Sprintf("%s assigned %s to %s", event.Actor, bead, target)
			}
		}
		return fmt.Sprintf("%s dispatched work", event.Actor)

	case events.TypeDone:
		if bead, ok := event.Payload["bead"].(string); ok {
			return fmt.Sprintf("%s completed work on %s", event.Actor, bead)
		}
		return fmt.Sprintf("%s signaled done", event.Actor)

	case events.TypeHandoff:
		return fmt.Sprintf("%s handed off to fresh session", event.Actor)

	case events.TypeMail:
		if to, ok := event.Payload["to"].(string); ok {
			if subj, ok := event.Payload["subject"].(string); ok {
				return fmt.Sprintf("%s → %s: %s", event.Actor, to, subj)
			}
		}
		return fmt.Sprintf("%s sent mail", event.Actor)

	case events.TypePatrolStarted:
		if rig, ok := event.Payload["rig"].(string); ok {
			return fmt.Sprintf("%s patrol started for %s", event.Actor, rig)
		}
		return fmt.Sprintf("%s started patrol", event.Actor)

	case events.TypePatrolComplete:
		if msg, ok := event.Payload["message"].(string); ok {
			return msg
		}
		return fmt.Sprintf("%s completed patrol", event.Actor)

	case events.TypeMerged:
		if worker, ok := event.Payload["worker"].(string); ok {
			return fmt.Sprintf("Merged work from %s", worker)
		}
		return "Work merged"

	case events.TypeMergeFailed:
		if reason, ok := event.Payload["reason"].(string); ok {
			return fmt.Sprintf("Merge failed: %s", reason)
		}
		return "Merge failed"

	case events.TypeSessionDeath:
		session, _ := event.Payload["session"].(string)
		reason, _ := event.Payload["reason"].(string)
		if session != "" && reason != "" {
			return fmt.Sprintf("Session %s terminated: %s", session, reason)
		}
		if session != "" {
			return fmt.Sprintf("Session %s terminated", session)
		}
		return "Session terminated"

	case events.TypeMassDeath:
		count, _ := event.Payload["count"].(float64) // JSON numbers are float64
		possibleCause, _ := event.Payload["possible_cause"].(string)
		if count > 0 && possibleCause != "" {
			return fmt.Sprintf("MASS DEATH: %d sessions died - %s", int(count), possibleCause)
		}
		if count > 0 {
			return fmt.Sprintf("MASS DEATH: %d sessions died simultaneously", int(count))
		}
		return "Multiple sessions died simultaneously"

	default:
		return fmt.Sprintf("%s: %s", event.Actor, event.Type)
	}
}
