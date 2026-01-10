# Agent Instructions

See **CLAUDE.md** for complete agent context and instructions.

This file exists for compatibility with tools that look for AGENTS.md.

## Continuous Autonomous Operation

> **CRITICAL**: After completing ANY task, DO NOT wait. IMMEDIATELY check for more work.

### The Problem

Agents previously had a reactive workflow - they would complete a task and stop, waiting for user input. This caused issues like:
- Convoys stuck at partial completion
- No monitoring of polecats/refinery progress
- Users returning hours later to find no actual progress

### The Solution: Continuous Work Loop

Agents MUST follow a continuous loop pattern:

```bash
while true; do
  # 1. Check for hooked work (highest priority)
  if gt hook | grep -q "Hooked:"; then
    exec_hooked_work
    continue  # Loop back for more work
  fi

  # 2. Check mail for new assignments
  if gt mail inbox | grep -q "unread"; then
    process_mail
    continue
  fi

  # 3. Check for ready work and dispatch
  READY=$(bd ready --status=open --assignee=none 2>/dev/null | head -5)
  if [ -n "$READY" ]; then
    echo "Dispatching ready work: $READY"
    for issue in $READY; do
      # Determine appropriate rig/polecat
      gt sling $issue <appropriate-rig>
    done
    continue
  fi

  # 4. Monitor active convoys (CRITICAL for convoy progression)
  CONVOYS=$(gt convoy list --status=in_progress 2>/dev/null)
  if [ -n "$CONVOYS" ]; then
    echo "Active convoys - checking progress..."
    for convoy_id in $(gt convoy list --json 2>/dev/null | jq -r '.[].id'); do
      STATUS=$(gt convoy status $convoy_id 2>/dev/null)

      # Check for completed items that need next steps
      if echo "$STATUS" | grep -q "completed"; then
        echo "Convoy $convoy_id has completions - checking for next work"
        # Find and dispatch next ready issues in convoy
        READY_IN_CONV=$(bd ready --status=open --assignee=none 2>/dev/null | head -3)
        for issue in $READY_IN_CONV; do
          gt sling $issue <appropriate-rig>
        done
      fi

      # Check for blocked work that needs attention
      if echo "$STATUS" | grep -q "blocked"; then
        echo "Convoy $convoy_id has blocked items - investigating"
        # Attempt to resolve blockages
      fi
    done
  fi

  # 5. Check refinery queues for stuck work
  for rig in $(gt rig list 2>/dev/null | awk '{print $1}'); do
    MQ_COUNT=$(bd list --assignee=$rig/refinery --status=open 2>/dev/null | wc -l)
    if [ "$MQ_COUNT" -gt 0 ]; then
      echo "Refinery $rig: $MQ_COUNT items - nudging..."
      gt nudge $rig/refinery "Process merge queue"
    fi
  done

  # 6. Session getting full? Use handoff to cycle
  TOKENS=$(prompt_tokens_current 2>/dev/null || echo "0")
  if [ "$TOKENS" -gt 100000 ]; then
    echo "Session at $TOKENS tokens - cycling for fresh context"
    gt handoff -s "Cycle session" -m "Continuing work loop, tokens at $TOKENS"
    # Handoff triggers new session that resumes loop
  fi

  # 7. Brief pause before next cycle
  sleep 10
done
```

### When to Actually Stop

Only stop when ALL of these are true:
- [ ] No work on hook (`gt hook` shows empty)
- [ ] No mail to process (`gt mail inbox` shows no unread)
- [ ] No ready work (`bd ready --assignee=none` returns empty)
- [ ] No active convoys (`gt convoy list` shows nothing in progress)
- [ ] All refineries empty (`bd list --assignee=*/refinery --status=open` returns empty)
- [ ] All agents healthy and making progress

**If ANY of the above is false, there is work to do. DO NOT STOP.**

### Key Commands for Autonomous Monitoring

| Command | Purpose |
|---------|---------|
| `gt hook` | Check for assigned work |
| `gt mail inbox` | Check for new assignments |
| `bd ready` | Find unblocked, unassigned work |
| `gt convoy list` | See all convoys and their status |
| `gt convoy status <id>` | Detailed convoy progress |
| `gt sling <issue> <rig>` | Dispatch work to polecat |
| `gt nudge <target> "msg"` | Send message to agent |
| `gt handoff` | Cycle session for fresh context |

### Fixes Issue #289

This continuous operation pattern fixes the issue where Mayor would:
1. Create convoy and sling first issue
2. Say "Monitoring convoy progress"
3. Then stop and wait indefinitely

Now Mayor will:
1. Create convoy and sling first issue
2. Enter continuous monitoring loop
3. Periodically check convoy status
4. Auto-sling next ready issues when previous complete
5. Report completion when convoy finishes

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Dependency Management

Periodically check for outdated dependencies:

```bash
go list -m -u all | grep '\['
```

Update direct dependencies:

```bash
go get <package>@latest
go mod tidy
go build ./...
go test ./...
```

Check release notes for breaking changes before major version bumps.
