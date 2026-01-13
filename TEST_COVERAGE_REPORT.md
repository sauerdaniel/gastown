# Gastown Core Framework - Test Coverage Report & Gap Analysis

**Generated:** 2026-01-13
**Issue:** hq-2k3we - Comprehensive test suite review and enhancement
**Rig:** gastown (core framework quality)

---

## Executive Summary

The gastown core framework consists of **53 packages** with **440 Go source files** and **136 test files** (30% test-to-source ratio). Overall test coverage shows strong foundation with critical gaps in core infrastructure components.

### Key Metrics
| Metric | Value |
|--------|-------|
| Total Packages | 53 |
| Packages with Tests | 43 (81.1%) |
| Packages without Tests | 10 (18.9%) |
| High Coverage Packages (>80%) | 13 |
| Low Coverage Packages (<30%) | 4 |
| Existing Benchmarks | 0 |
| Existing Fuzz Tests | 0 |

### Critical Findings
- **Strengths:** Core agent, lock, protocol, runtime, and session packages have good coverage (80%+)
- **Critical Gaps:** Main CLI entry point, boot system, mayor functionality, and Claude integration have 0% coverage
- **Missing Test Types:** No benchmarks, no fuzz tests, limited E2E tests
- **Test Infrastructure:** **BLOCKING BUG** - Infinite recursion in beads dependency (`daemon_autostart.go:228`) causes stack overflow in `TestQuerySessionEvents_FindsEventsFromAllLocations`

### Critical Test Infrastructure Issue

**Bug:** Stack Overflow in `beads@v0.47.1` Dependency
- **Location:** `github.com/steveyegge/beads@v0.47.1/cmd/bd/daemon_autostart.go:228` in `acquireStartLock()`
- **Error:** `goroutine stack exceeds 1000000000-byte limit` (3.3M+ recursive frames)
- **Impact:**
  - `internal/cmd` tests FAIL (9.6% coverage)
  - `internal/doctor` tests timeout (35-43s vs expected <30s)
  - Overall test suite marked as FAIL
- **Root Cause:** Infinite recursion in `acquireStartLock()` - function calls itself without proper termination
- **Action Required:** Upgrade `beads` library to version with fix, or apply patch

---

## 1. Unit Test Coverage Analysis

### Packages with Excellent Coverage (80-100%)

| Package | Coverage | Notes |
|---------|----------|-------|
| internal/activity | 100.0% | Fully tested |
| internal/checkpoint | 90.5% | Solid coverage |
| internal/mq | 100.0% | Merge queue well tested |
| internal/util | 96.9% | Utility functions covered |
| internal/suggest | 97.1% | Auto-suggestion complete |
| internal/lock | 84.4% | File locking well tested |
| internal/protocol | 81.4% | Core protocol covered |
| internal/runtime | 82.9% | Runtime logic covered |
| internal/session | 79.4% | Session management covered |
| internal/opencode | 76.9% | OpenCode integration covered |
| internal/agent | 86.7% | Agent core logic covered |

### Packages with Good Coverage (70-80%)

| Package | Coverage | Notes |
|---------|----------|-------|
| internal/feed | 67.3% | Feed functionality covered |
| internal/formula | 68.2% | Formula parsing covered |
| internal/state | 67.6% | State management covered |

### Packages with Moderate Coverage (30-70%)

| Package | Coverage | Priority |
|---------|----------|----------|
| internal/config | 59.7% | Medium |
| internal/deps | 52.8% | Medium |
| internal/workspace | 46.6% | Medium |
| internal/townlog | 46.9% | Medium |
| internal/mail | 35.6% | High (messaging is critical) |
| internal/crew | 34.6% | Medium |
| internal/polecat | 33.9% | High (core agent type) |
| internal/tmux | 33.4% | Medium |
| internal/deacon | 32.2% | Medium |
| internal/connection | 21.5% | Low |
| internal/plugin | 37.9% | Low |
| internal/shell | 61.8% | Medium |
| internal/web | 26.4% | Low |
| internal/templates | 28.0% | Low |
| internal/swarm | 13.8% | High (distributed execution) |

### Packages with Critical Coverage (<30%)

| Package | Coverage | Priority | Reason |
|---------|----------|----------|--------|
| internal/style | 2.0% | Low | UI styling |
| internal/dog | 0.8% | High | Deacon helper - critical for monitoring |
| internal/refinery | 17.6% | **Critical** | Merge queue processor - core workflow |
| internal/witness | 17.3% | **Critical** | Polecat monitoring - production critical |

### Packages Without Tests (0%) - **HIGH PRIORITY**

| Package | Priority | Reason |
|---------|----------|--------|
| cmd/gt | **Critical** | Main CLI entry point - user-facing |
| internal/boot | **Critical** | Bootstrap logic - startup critical |
| internal/mayor | **Critical** | Core mayor functionality - coordination |
| internal/claude | **Critical** | Claude integration - AI agent layer |
| internal/constants | Medium | Application constants |
| internal/events | High | Event system infrastructure |
| internal/tui/convoy | Medium | UI convoy functionality |
| internal/tui/feed | Medium | UI feed functionality |
| internal/version | Low | Version management |
| internal/wrappers | Low | Wrapper utilities |

---

## 2. Integration Test Gap Analysis

### Currently Covered Integration Scenarios

1. **Beads Routing** - Multi-rig routing, redirect handling, cross-rig sling
2. **Hook Slots** - Basic bead hooking, persistence, status transitions
3. **Installation** - Command setup, database initialization
4. **Sling Functionality** - Formula-on-bead dispatch, variable passing
5. **Mail System** - Worker matching, message validation
6. **Merge Queue** - Branch parsing, status formatting, MR filtering
7. **Agent Configuration** - Rig-level config, tmux integration
8. **Formula System** - Real formula parsing, topological sort
9. **Shell Integration** - Shell detection, RC file manipulation

### Critical Integration Test Gaps

| Gap | Priority | Components |
|-----|----------|------------|
| **Polecat Lifecycle Management** | High | Spawning, execution, hooks, convoy, cleanup |
| **Formula and Molecule Integration** | High | Parsing, creation, execution, tracking |
| **Convoy Auto-Management** | High | Creation, tracking, closure, reopening |
| **Cross-Rig Work Distribution** | High | Multi-rig sling, routing, messaging, convoy |
| **Agent Spawning with Sessions** | High | Startup, hooks, work assignment, cleanup |
| **Merge Queue Integration** | Medium | Polecat branch, MR submission, processing |
| **Message Routing** | Medium | Prefix routing, queue management, delivery |
| **Formula Composition** | Medium | Extends, compose, aspects |
| **Non-Polecat Agent Work** | Medium | Mayor, crew, witness, deacon |
| **Error Recovery** | Medium | Status changes, recovery, retry, cleanup |

---

## 3. End-to-End Test Needs

### High Priority E2E Tests

| Scenario | User Journey | Expected Outcome |
|----------|--------------|------------------|
| **Complete Onboarding** | gt install → gt rig add → gt crew add | Complete workspace structure with proper configuration |
| **Mayor Convoy Orchestration** | gt convoy create → assign work → monitor → auto-close | Proper tracking across agents with notifications |
| **Cross-Rig Work Distribution** | Multi-rig setup → sling work → merge queue | Correct routing with convoy tracking across rigs |
| **Formula Execution** | Create formula → execute → track | All steps execute in order with proper tracking |

### Medium Priority E2E Tests

| Scenario | User Journey | Expected Outcome |
|----------|--------------|------------------|
| **Agent-to-Agent Messaging** | Start agents → send nudges → verify mail | Message delivery with persistence |
| **Convoy Management** | Create convoy → add issues → refresh → recover | Dynamic management with recovery |
| **Merge Queue Integration** | Create MRs → refinery process → auto-merge | Sequential merging with conflict handling |
| **System Health** | gt doctor → dashboard → patrol → agents check | All health checks pass, real-time status |

---

## 4. Edge Case Testing Needs

### High Priority Edge Cases

| Category | Scenarios | Priority |
|----------|-----------|----------|
| **Concurrent Access** | Multiple agents on same bead, bead creation/deletion races, status update races | High |
| **Network Failures** | Connection timeouts, agent unresponsiveness, message delivery failures | High |
| **Database Corruption** | JSONL corruption, partial writes, invalid JSON, missing entries | High |
| **Agent Crash/Recovery** | Process termination, state recovery, duplicate work prevention | High |
| **Convoy Race Conditions** | State corruption, leadership races, heartbeat races | High |

### Medium Priority Edge Cases

| Category | Scenarios | Priority |
|----------|-----------|----------|
| **Git Operation Failures** | Merge conflicts, push failures, auth failures, repo corruption | Medium |
| **Tmux Failures** | Server unavailable, session creation failures, permission issues | Medium |
| **Invalid Input** | Malformed formulas, invalid bead IDs, corrupted configs | Medium |
| **Resource Exhaustion** | Disk space, memory limits, file descriptors, CPU limits | Medium |

---

## 5. Performance Testing Opportunities

### Critical Performance Tests (None Currently Exist)

| Area | Metric | Priority |
|------|--------|----------|
| **Beads Database Operations** | Query latency with 1000+ issues | High |
| **Concurrent Bead Operations** | Throughput with multiple writers | High |
| **Agent Spawning** | Time from command to ready agent | High |
| **Message Throughput** | Messages per second | Medium |
| **Git Operations** | Clone, checkout, commit performance | Medium |
| **Merge Queue Processing** | Time to process 10+ MRs | Medium |

### Memory Profiling Needs

| Area | Focus | Priority |
|------|-------|----------|
| Beads database loading | Memory usage with large datasets | High |
| Agent session management | Memory leaks across session lifecycle | High |
| Message queue | Memory growth under load | Medium |
| Git operation buffers | Buffer management | Low |

---

## 6. Fuzz Testing Opportunities

### Critical Fuzz Targets (High Priority)

| Target | Input Format | Expected Bugs |
|--------|--------------|---------------|
| **Beads Database JSON Parsing** | Random JSON strings | Buffer overflows, integer overflow, panics, memory leaks |
| **Config File JSON Parsing** | Random JSON configs | Version overflow, type validation, map key collisions |
| **Git Command Input** | Random branch names, URLs, messages | Command injection, buffer overflows, Unicode issues |

### Medium Priority Fuzz Targets

| Target | Input Format | Expected Bugs |
|--------|--------------|---------------|
| **Formula TOML Parsing** | Random TOML | Integer overflow, circular ref failures, stack overflow |
| **Session Name Parsing** | Random session strings | Buffer overflows, Unicode issues, index errors |
| **Bead ID Prefix Validation** | Random prefixes | Path traversal, buffer overflows, null byte injection |
| **Agent Identity Validation** | Random agent IDs | Null byte injection, string overflow, special characters |

---

## 7. Prioritized Action Plan

### Phase 1: Critical Infrastructure (Immediate)

1. **Add tests for packages with 0% coverage**
   - cmd/gt (main CLI)
   - internal/boot (bootstrap)
   - internal/mayor (core coordination)
   - internal/claude (AI integration)

2. **Improve coverage for critical low-coverage packages**
   - internal/refinery (17.6% → 70%+)
   - internal/witness (17.3% → 70%+)
   - internal/swarm (13.8% → 60%+)

3. **Fix failing integration tests**
   - internal/cmd timeout issues
   - internal/doctor timeout issues

### Phase 2: Integration & Edge Cases (Short-term)

4. **Implement critical integration tests**
   - Polecat lifecycle management
   - Formula and molecule integration
   - Convoy auto-management
   - Cross-rig work distribution

5. **Add edge case tests**
   - Concurrent access scenarios
   - Database corruption recovery
   - Agent crash/recovery
   - Network failure handling

### Phase 3: Performance & Fuzz (Medium-term)

6. **Add benchmark tests**
   - Beads database operations
   - Concurrent operations
   - Agent spawning performance
   - Git operation performance

7. **Implement fuzz tests**
   - Beads JSON parsing
   - Config file parsing
   - Git command input validation

### Phase 4: E2E & Advanced (Long-term)

8. **Implement E2E tests**
   - Complete onboarding workflow
   - Mayor convoy orchestration
   - Cross-rig distribution
   - Formula execution

9. **Add comprehensive performance testing**
   - Memory profiling
   - Load testing
   - Stress testing

---

## 8. Test Infrastructure Improvements

### Immediate Needs

1. **Fix timeout issues** in existing integration tests
2. **Create test utilities** for common scenarios (agent mocking, git repo setup)
3. **Add test data management** framework for consistent test scenarios
4. **Implement cleanup automation** to prevent test pollution

### Long-term Needs

1. **Create dedicated E2E test environment** with isolated resources
2. **Implement chaos engineering patterns** for distributed system testing
3. **Add continuous fuzzing** integration with CI/CD
4. **Create performance regression detection** in CI pipeline

---

## 9. Summary Statistics

| Category | Count | Percentage |
|----------|-------|------------|
| Packages with >80% coverage | 13 | 24.5% |
| Packages with 50-80% coverage | 7 | 13.2% |
| Packages with 30-50% coverage | 9 | 17.0% |
| Packages with <30% coverage | 4 | 7.5% |
| Packages with 0% coverage | 10 | 18.9% |
| Integration test gaps identified | 10 | - |
| E2E tests needed | 10 | - |
| Critical edge cases | 5 | - |
| Performance benchmarks needed | 6+ | - |
| Fuzz targets identified | 8 | - |

---

## 10. Conclusion

The gastown framework has a solid testing foundation with 81% of packages having some test coverage. The core agent, protocol, and runtime systems are well-tested. However, critical gaps exist in:

1. **BLOCKING: Test infrastructure bug** - beads dependency stack overflow prevents full test suite from running
2. **Infrastructure components** (CLI entry, boot, mayor, Claude integration) - 0% coverage
3. **Integration testing** (polecat lifecycle, convoy management, cross-rig operations) - major gaps
4. **Edge case coverage** (concurrency, failures, corruption recovery) - needs comprehensive testing
5. **Performance testing** (benchmarks, memory profiling, load testing) - none exist
6. **Fuzz testing** (input validation, security hardening) - none exist

**Immediate Action Required:** Fix the beads dependency infinite recursion bug to unblock the test suite. Once resolved, proceed with the prioritized action plan to significantly improve the reliability, security, and maintainability of the gastown core framework.
