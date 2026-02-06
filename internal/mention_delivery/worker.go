// Mention Delivery Worker for Phase-3
// Polls beads events for mentions and subscription matches, delivers via OpenClaw message CLI.

package mention_delivery

import (
	\"database/sql\"
	\"encoding/json\"
	\"fmt\"
	\"log\"
	\"regexp\"
	\"strings\"
	\"time\"

	_ \"github.com/mattn/go-sqlite3\"
)

type Worker struct {
	db           *sql.DB
	lastEventID  int64
	openclawCLI  string // e.g. \"openclaw\"
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
	db, err := sql.Open(\"sqlite3\", beadsDBPath+\"?_foreign_keys=on\")
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
	err = db.QueryRow(\"SELECT COALESCE(MAX(id), 0) FROM events\").Scan(&lastID)
	if err != nil {
		return nil, err
	}
	w.lastEventID = lastID

	return w, nil
}

func (w *Worker) Run() {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.processEvents(); err != nil {
				log.Printf(\"Error processing events: %v\", err)
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
			return err
		}
		e.CreatedAt, _ = time.Parse(\"2006-01-02 15:04:05\", created) // beads format
		events = append(events, e)
	}

	if len(events) == 0 {
		return nil
	}

	// Process each event
	for _, e := range events {
		w.processEvent(e)
	}

	// Update last ID
	w.lastEventID = events[len(events)-1].ID

	return nil
}

var mentionRE = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

func (w *Worker) processEvent(e Event) {
	// Mention detection in comment
	mentions := mentionRE.FindAllStringSubmatch(e.Comment, -1)
	for _, m := range mentions {
		if len(m) > 1 {
			mentioned := m[1]
			msg := fmt.Sprintf(\"Mentioned by %s in bead %s: %s\", e.Actor, e.IssueID, e.Comment)
			w.sendNotification(mentioned, msg)
		}
	}

	// TODO: Subscription matches
	// Query subs for sub_type=\\'bead\\', sub_id=e.IssueID
	// For each, send update msg
}

func (w *Worker) sendNotification(subscriber, msg string) {
	// TODO: Lookup channel/target for subscriber
	// Hardcode for demo
	channel := \"webchat\"
	target := \"5ti98kdhtogi1eozd66dswd+n6ijkfxipgm0yuxisx0=\" // from context

	cmd := fmt.Sprintf(\"%s message send --channel %s --target %s \\\"%s\\\"\", 
		w.openclawCLI, channel, target, msg)
	
	// Exec cmd
	// out, err := exec.Command(\"sh\", \"-c\", cmd).CombinedOutput()
	// log.Printf(\"Sent: %s\", out)
	log.Printf(\"WOULD SEND: %s\", cmd)
}

func main() {
	beadsDB := \"/home/dsauer/.openclaw/beads/.beads/beads.db\"
	openclaw := \"openclaw\"

	w, err := NewWorker(beadsDB, openclaw)
	if err != nil {
		log.Fatal(err)
	}
	defer w.db.Close()

	log.Println(\"Mention delivery worker started\")
	w.Run()
}