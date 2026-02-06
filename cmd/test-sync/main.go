package main

import (
    "log"
    "github.com/steveyegge/gastown/internal/projection"
)

func main() {
    config := projection.Config{
        BeadsDBPath:  "/home/dsauer/.openclaw/beads/.beads/beads.db",
        ProjDBPath:   "/home/dsauer/.openclaw/workspace/mission/cache/projections.db",
        CacheDir:     "/home/dsauer/.openclaw/workspace/mission/cache",
        PollInterval: 0,
        Logger:       log.New(log.Writer(), "[projection-sync] ", log.LstdFlags),
    }
    
    d := projection.New(config)
    if err := d.Sync(); err != nil {
        log.Fatalf("Sync failed: %v", err)
    }
    
    log.Println("Sync completed successfully")
}
