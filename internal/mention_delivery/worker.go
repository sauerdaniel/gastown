// Mention Delivery Worker for Phase-3
// Polls beads events for mentions and subscription matches, delivers via OpenClaw message CLI.

package mention_delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Worker struct {
	db           *sql.DB
	lastEventID  int64
	openclawCLI  string // e.g. "openclaw"
	pollInterval time.Duration
}

type Event struct {
	ID        int64
	IssueID   string
	EventType string
	Actor     string
	Comment   string
	CreatedAt time.Time
}

type Subscription struct {
	ID         int
	Subscriber string
	Channel    string
	Target     string
	SubType    string
	SubID      string
}

func NewWorker(beadsDBPath, openclawCLI string) (*Worker, error) {
	db, err := sql.Open("sqlite3", beadsDBPath+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}

	w := &Worker{
		db:           db,
		openclawCLI:  openclawCLI,
		pollInterval: 10 * time.Second,
	}

	// Load last processed event ID
	var lastID int64
	err = db.QueryRow("SELECT COALESCE(MAX(id), 0) FROM events").Scan(&lastID)
	if err != nil {
		return nil, err
	}
	w.lastEventID = lastID

	return w, nil
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	log.Printf("Worker started, polling every %v, lastEventID=%d", w.pollInterval, w.lastEventID)

	for {
		select {
		case <-ctx.Done():
			log.Println("Received shutdown signal, stopping...")
			return
		case <-ticker.C:
			if err := w.processEvents(); err != nil {
				log.Printf("processEvents error: %v", err)
			}
		}
	}
}

func (w *Worker) processEvents() error {
	// Fetch new events
	rows, err := w.db.Query(`
		SELECT id, issue_id, event_type, actor, comment, created_at 
		FROM events 
		WHERE id > ? 
		ORDER BY id ASC
	`, w.lastEventID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var created string
		if err := rows.Scan(&e.ID, &e.IssueID, &e.EventType, &e.Actor, &e.Comment, &created); err != nil {
			log.Printf("Scan err: %v", err)
			continue
		}
		if created != "" {
			e.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
		}
		events = append(events, e)
	}

	if len(events) == 0 {
		return nil
	}

	log.Printf("Processing %d new events", len(events))

	// Process each event
	for _, e := range events {
		w.processEvent(e)
	}

	// Update last ID
	w.lastEventID = events[len(events)-1].ID
	log.Printf("Updated lastEventID to %d", w.lastEventID)

	return nil
}

var mentionRE = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

func (w *Worker) processEvent(e Event) {
	log.Printf("Processing event %d on %s by %s: %s", e.ID, e.IssueID, e.Actor, e.Comment)

	// Mention detection in comment
	mentions := mentionRE.FindAllStringSubmatch(e.Comment, -1)
	for _, m := range mentions {
		if len(m) > 1 {
			mentioned := m[1]
			msg := fmt.Sprintf("🔔 Mentioned by @%s in bead %s:\n%s", e.Actor, e.IssueID, e.Comment)
			ch, tgt, ok := w.lookupChannelTarget(mentioned)
			if ok {
				if err := w.sendNotification(ch, tgt, mentioned, msg); err != nil {
					log.Printf("Mention notify failed for %s: %v", mentioned, err)
				}
			} else {
				log.Printf("No delivery prefs for mentioned %s", mentioned)
			}
		}
	}

	// Subscription matches: bead level
	rows, err := w.db.Query(`
		SELECT subscriber, channel, target 
		FROM subscriptions 
		WHERE sub_type = 'bead' AND sub_id = ?
		LIMIT 20
	`, e.IssueID)
	if err != nil {
		log.Printf("Sub query err for %s: %v", e.IssueID, err)
		return
	}
	defer rows.Close()

	subCount := 0
	for rows.Next() {
		var subscriber, channel, target string
		if err := rows.Scan(&subscriber, &channel, &target); err != nil {
			log.Printf("Sub scan err: %v", err)
			continue
		}
		msg := fmt.Sprintf("🔔 Update on your subscribed bead %s by @%s:\n%s", e.IssueID, e.Actor, e.Comment)
		if err := w.sendNotification(channel, target, subscriber, msg); err != nil {
			log.Printf("Sub notify failed for %s: %v", subscriber, err)
		}
		subCount++
	}
	if subCount > 0 {
		log.Printf("Notified %d subscribers for bead %s", subCount, e.IssueID)
	}
}

func (w *Worker) lookupChannelTarget(subscriber string) (channel, target string, ok bool) {
	var ch, tgt string
	err := w.db.QueryRow("SELECT channel, target FROM subscriptions WHERE subscriber = ? LIMIT 1", subscriber).Scan(&ch, &tgt)
	if err != nil || err == sql.ErrNoRows {
		return "", "", false
	}
	return ch, tgt, true
}

func (w *Worker) sendNotification(channel, target, subscriber, msg string) error {
	// Escape quotes in msg for shell
	safeMsg := strings.ReplaceAll(msg, `"`, `\"`)
	cmdStr := fmt.Sprintf("%s message send --channel %s --target %s --silent \"%s\"", w.openclawCLI, channel, target, safeMsg)

	log.Printf("Sending to %s (%s/%s): %s", subscriber, channel, target[:8]+"...", safeMsg[:50]+"...")

	cmd := exec.Command("sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed send to %s: %v\nOutput: %s", subscriber, err, out)
		return err
	}
	log.Printf("Sent to %s successfully: %s", subscriber, string(out))
	return nil
}

func main() {
	beadsDB := "/home/dsauer/.openclaw/beads/.beads/beads.db"
	openclaw := "openclaw"

	w, err := NewWorker(beadsDB, openclaw)
	if err != nil {
		log.Fatal(err)
	}
	defer w.db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Println("Mention delivery worker started. Press Ctrl+C to stop.")
	w.Run(ctx)
}