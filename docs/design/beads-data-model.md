# Beads Data Model for Gas Town P1

This document defines the Beads data model for P1 orchestration (oc-q7rq Gas Town Orchestration) and worker lifecycle (oc-hyor Worker Lifecycle). These schemas extend the base Beads architecture to support multi-agent coordination.

## Overview

Gas Town uses Beads as the **single source of truth** for work state. This document defines:

1. **Convoy Representation** - How batch work is grouped and tracked
2. **Worker Lifecycle Events** - How Mayor tracks worker spawn/death/health
3. **Hook/Workspace Mapping** - How git-backed workspaces map to work items

## 1. Convoy Representation

A **convoy** is a coordinated batch of work across multiple rigs, managed by the Mayor.

### Convoy Bead Schema

Convoys are represented as Beads issues with `type: convoy` in town-level beads (`~/gt/.beads/`):

```yaml
id: hq-convoy-1
type: convoy
title: "February Security Audit"
description: "Coordinate security audit across all rigs"
status: active
priority: 1
owner: mayor
created: 2026-02-06T14:00:00Z
updated: 2026-02-06T15:00:00Z

# Convoy-specific fields
convoy:
  # Target rigs for this convoy
  rigs:
    - gastown
    - beads
    - openclaw
  
  # Work items spawned from this convoy (rig-level issue IDs)
  spawned_work:
    - gt-456   # gastown rig issue
    - bd-789   # beads rig issue
    - oc-123   # openclaw rig issue
  
  # Convoy workflow stage
  stage: execution  # planning | execution | review | complete
  
  # Coordination metadata
  coordinator: mayor
  started: 2026-02-06T14:30:00Z
  deadline: 2026-02-13T23:59:59Z
```

### Convoy Lifecycle

```
planning → execution → review → complete
```

- **planning**: Mayor is gathering requirements, assigning work
- **execution**: Workers are actively working on spawned issues
- **review**: All work complete, under final review
- **complete**: Convoy closed, results delivered

### Convoy Dependencies

Convoys can block other convoys:

```bash
# Convoy 2 depends on convoy 1
bd dep add hq-convoy-2 --depends-on hq-convoy-1
```

### Querying Convoys

```bash
# List active convoys
bd list --type convoy --status active

# Show convoy details
bd show hq-convoy-1

# List work spawned from a convoy
bd list --convoy hq-convoy-1

# Convoy activity
bd activity --convoy hq-convoy-1
```

## 2. Worker Lifecycle Events

Workers (Polecats, Dogs) are **ephemeral** and stateless. All lifecycle state lives in Beads.

### Worker Bead Schema

Each worker has a bead in the appropriate beads database (town or rig):

```yaml
id: gt-polecat-alice
type: agent
subtype: polecat
title: "Polecat: alice"
status: active
owner: witness
created: 2026-02-06T14:00:00Z
updated: 2026-02-06T15:00:00Z

# Agent-specific fields
agent:
  # Worker identity
  name: alice
  role_bead: hq-polecat-role  # Reference to role definition
  
  # Current assignment
  assigned_work: gt-456
  assigned_at: 2026-02-06T14:30:00Z
  
  # Lifecycle state
  lifecycle:
    state: working  # spawning | idle | working | blocked | crashed | terminated
    spawned_at: 2026-02-06T14:00:00Z
    last_heartbeat: 2026-02-06T15:00:00Z
    heartbeat_timeout: 300  # seconds
    health: healthy  # healthy | stale | dead
    
  # Resource tracking
  resources:
    pid: 12345
    session_id: "agent:alice:main"
    workspace: "/home/dsauer/gt/gastown/polecats/alice"
    worktree_base: "/home/dsauer/gt/gastown/mayor/rig"
  
  # Performance metrics
  metrics:
    tasks_completed: 12
    average_duration: 1800  # seconds
    last_completion: 2026-02-06T13:00:00Z
```

### Worker Lifecycle States

```
spawning → idle → working → {blocked | crashed | terminated}
                      ↑           |
                      └───────────┘
```

- **spawning**: Process starting, workspace initializing
- **idle**: Ready for work, no current assignment
- **working**: Actively working on assigned task
- **blocked**: Waiting on dependency or external resource
- **crashed**: Process died unexpectedly (Witness will clean up)
- **terminated**: Gracefully shut down

### Worker Health States

```
healthy → stale → dead
```

- **healthy**: Heartbeat received within timeout window
- **stale**: Heartbeat overdue (Witness will nudge)
- **dead**: No heartbeat for 2x timeout (Witness will terminate)

### Worker Lifecycle Events

All lifecycle events are recorded as **Beads comments** on the worker's bead:

```bash
# Worker spawned
bd comments add gt-polecat-alice "🎬 Spawned: pid=12345, session=agent:alice:main"

# Worker started work
bd comments add gt-polecat-alice "🔨 Started work on gt-456"

# Worker completed work
bd comments add gt-polecat-alice "✅ Completed gt-456 (duration: 1800s)"

# Worker heartbeat
bd comments add gt-polecat-alice "💓 Heartbeat: health=healthy, status=working"

# Worker crashed
bd comments add gt-polecat-alice "💀 Crashed: exit_code=1, stderr=..."

# Worker terminated
bd comments add gt-polecat-alice "🛑 Terminated: reason=cleanup"
```

### Worker Assignment Protocol

**Atomic assignment** prevents race conditions when multiple workers compete for work:

```bash
# 1. Worker queries for ready work
bd list --ready --limit 1

# 2. Worker attempts to claim task (atomic update)
bd update gt-456 --assignee alice --status in_progress

# 3. If successful, record assignment
bd comments add gt-polecat-alice "🔨 Claimed gt-456"
bd comments add gt-456 "🤖 Assigned to polecat alice"
```

The `bd update` command is atomic at the file level (via filesystem locks on JSONL).

### Mayor's Worker Tracking

Mayor tracks all workers via Beads queries:

```bash
# List all active workers
bd list --type agent --status active

# Check worker health (stale heartbeats)
bd list --type agent --stale-since 5m

# Check crashed workers
bd activity --type comment --since 1h | grep "💀 Crashed"

# Worker capacity (idle workers)
bd list --type agent --status active --lifecycle idle
```

## 3. Hook/Workspace Mapping

**Hooks** are git-backed workspaces where agents produce durable artifacts. Hooks map to Beads work items via **reference fields**.

### Hook Architecture

From Gas Town architecture:
- Each worker has a **worktree** (not full clone)
- Worktrees are based on `mayor/rig` (canonical clone with `.beads/`)
- Artifacts are committed to the worktree, then merged to main

### Hook Reference in Beads

Work items reference their hook workspace:

```yaml
id: gt-456
type: task
title: "Implement feature X"
assignee: alice
status: in_progress

# Hook reference
hook:
  workspace: "polecats/alice"
  worktree_base: "mayor/rig"
  branch: "polecat/alice-20260206-143000"
  
  # Artifact locations (relative to workspace)
  artifacts:
    - path: "src/feature_x.go"
      type: code
    - path: "docs/feature_x.md"
      type: documentation
    - path: "tests/feature_x_test.go"
      type: test
  
  # Git metadata
  commits:
    - sha: "abc123def456"
      message: "Implement feature X"
      timestamp: 2026-02-06T14:45:00Z
```

### Hook Lifecycle

```
1. Worker spawned → worktree created
2. Work assigned → branch created
3. Work progresses → commits made, artifacts added
4. Work complete → MR created, worktree merged
5. Worker terminated → worktree cleaned up
```

### Hook Artifact References

Large artifacts (code, docs, binaries) live in git. Beads stores **references**, not content:

```bash
# Add artifact reference to task
bd comments add gt-456 "📦 Artifact: src/feature_x.go (commit: abc123)"

# Link to external resource
bd comments add gt-456 "🔗 MR: https://gitlab.com/project/merge_requests/42"

# Document location
bd update gt-456 --hook-workspace "polecats/alice" --hook-branch "polecat/alice-20260206"
```

### Hook Cleanup

When a worker terminates, Witness handles cleanup:

```bash
# 1. Check if work is merged
git branch --merged | grep "polecat/alice-20260206"

# 2. If merged, remove worktree
git worktree remove polecats/alice

# 3. Update worker bead
bd update gt-polecat-alice --status terminated
bd comments add gt-polecat-alice "🧹 Cleanup: worktree removed, branch merged"
```

## 4. Integration with OpenClaw Agents

OpenClaw agents (james, mort, etc.) operate in a **hybrid model**:

- **Work state** → Beads (tasks, status, dependencies)
- **Narrative context** → Memory files (daily notes, MEMORY.md)
- **Tools/config** → Workspace files (AGENTS.md, TOOLS.md, SOUL.md)

### OpenClaw Agent Bead Schema

OpenClaw agents have simpler beads (no worktrees, persistent sessions):

```yaml
id: hq-agent-mort
type: agent
subtype: openclaw
title: "Agent: mort"
status: active
owner: james

agent:
  name: mort
  role: "Research & documentation"
  
  lifecycle:
    state: active  # active | idle | offline
    last_heartbeat: 2026-02-06T15:00:00Z
    session_id: "agent:mort:main"
    
  workspace:
    path: "/home/dsauer/.openclaw/workspace-mort"
    memory_files: true
    persistent: true
```

### Heartbeat Integration

OpenClaw agents use Beads for heartbeat coordination:

```bash
# On heartbeat, query Beads
bd list --assignee mort --status open --pretty
bd activity --since 30m --limit 50

# Report via Beads comment (if significant)
bd comments add oc-61li "Progress: Applied AGENTS.md migration to all workspaces"
```

## 5. Data Model Summary

| Concept | Beads Type | Location | Lifetime | Key Fields |
|---------|-----------|----------|----------|------------|
| **Convoy** | issue (type: convoy) | Town beads | Project lifetime | rigs, spawned_work, stage |
| **Worker** | issue (type: agent) | Town/rig beads | Ephemeral | lifecycle, health, assigned_work |
| **Work Item** | issue (type: task/bug/feature) | Rig beads | Until closed | assignee, status, hook |
| **Hook** | Reference in work item | Git worktree | Work lifetime | workspace, branch, artifacts |
| **Lifecycle Event** | Comment on agent bead | Beads activity | Permanent | Event type (🎬🔨✅💀🛑) |

## 6. P1 Implementation Checklist for Jennings

For **oc-q7rq (Gas Town Orchestration)**:
- [ ] Implement convoy CRUD (create, list, show, update)
- [ ] Add convoy field to Beads schema
- [ ] Implement convoy → work spawning
- [ ] Add `--convoy` filter to `bd list`
- [ ] Update Mayor to coordinate convoys

For **oc-hyor (Worker Lifecycle)**:
- [ ] Implement agent bead schema
- [ ] Add lifecycle state tracking
- [ ] Implement heartbeat timeout detection
- [ ] Add worker health checks to Witness
- [ ] Implement atomic work assignment
- [ ] Add lifecycle event logging (comments)

For **Hook Integration**:
- [ ] Add hook reference fields to Beads schema
- [ ] Implement artifact tracking in `bd comments`
- [ ] Add hook metadata to `bd show`
- [ ] Implement worktree cleanup in Witness

## 7. References

- [architecture.md](./architecture.md) - Gas Town system architecture
- [dog-pool-architecture.md](./dog-pool-architecture.md) - Dog pool design
- [/home/dsauer/.openclaw/workspace-mort/BEADS.md](../../workspace-mort/BEADS.md) - OpenClaw Beads integration
- Architecture Vision (James memory) - Strategic overview

---

*Document created: 2026-02-06*  
*For: oc-61li (Beads as SSOT) → oc-q7rq (Gas Town Orchestration) + oc-hyor (Worker Lifecycle)*  
*Author: mort (subagent)*
