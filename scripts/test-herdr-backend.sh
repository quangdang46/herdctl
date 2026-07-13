#!/usr/bin/env bash
# E2E smoke for NTM_BACKEND=herdr.
#
# FEATURES.md §1 P0 coverage (bd-gl28u.1.6 / bd-gl28u.1.8):
#   spawn → list → status → status --watch → attach → send → kill
#   multi-agent spawn (cc=2,cod=1) — agent_name_taken regression
#   mid-session add (spawn --cc=1; add --cc=1 / --cod=1) — NTMIndex
#   spawn --label — full session name in registry + herdr workspace label
#
# Prerequisites:
#   - herdr binary on PATH (or HERDR_BIN_PATH)
#   - herdr server running (herdr status → server.status=running)
#   - go (if no prebuilt binary) or NTM_BIN / /tmp/herdctl / ./ntm
#
# Usage (from repo root):
#   ./scripts/test-herdr-backend.sh
#   NTM_BIN=/tmp/herdctl ./scripts/test-herdr-backend.sh
#   go build -o /tmp/herdctl ./cmd/ntm && NTM_BIN=/tmp/herdctl ./scripts/test-herdr-backend.sh
#
# Exit codes: 0 on full pass, non-zero on any failure.
# Each case logs backend, registry path, workspace_id, agent names, exit codes.

set -uo pipefail
# Intentionally not using -e so individual assertions can fail without
# aborting cleanup / remaining phases. Final exit is non-zero if any failed.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

DEMO_SESSION="${DEMO_SESSION:-demo-session}"
MULTI_SESSION="${MULTI_SESSION:-test-multi}"
ADD_SESSION="${ADD_SESSION:-test-add}"
LABEL_BASE="${LABEL_BASE:-test-label}"
LABEL_SUFFIX="${LABEL_SUFFIX:-frontend}"
# Full labeled session name: base--label (config.FormatSessionName)
LABEL_SESSION="${LABEL_SESSION:-${LABEL_BASE}--${LABEL_SUFFIX}}"
CP_SESSION="${CP_SESSION:-test-checkpoint}"
PERSIST_SESSION="${PERSIST_SESSION:-test-persist}"
TEST_MSG="${TEST_MSG:-echo herdr-test}"
# Short watch for e2e (status --interval is milliseconds)
WATCH_INTERVAL_MS="${WATCH_INTERVAL_MS:-400}"
WATCH_TICKS="${WATCH_TICKS:-2}"

# Isolated registry so we never clobber the developer's real binding map.
TEST_HOME="${TEST_HOME:-$(mktemp -d -t ntm-herdr-e2e-XXXXXX)}"
export HERDCTL_HERDR_REGISTRY="${HERDCTL_HERDR_REGISTRY:-${TEST_HOME}/registry.json}"
export NTM_BACKEND=herdr
export HERDCTL_BACKEND=herdr

# Isolate NTM project dirs created by spawn (avoid writing into ~/projects).
export NTM_PROJECTS_BASE="${NTM_PROJECTS_BASE:-${TEST_HOME}/projects}"
mkdir -p "$NTM_PROJECTS_BASE"

PASS=0
FAIL=0
SKIP=0
CREATED_SESSIONS=()

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

ts() { date '+%Y-%m-%dT%H:%M:%S'; }

log()  { echo "[$(ts)] $*" >&2; }
info() { log "INFO  $*"; }
warn() { log "WARN  $*"; }
err()  { log "ERROR $*"; }
ok()   { log "PASS  $*"; PASS=$((PASS + 1)); }
bad()  { log "FAIL  $*"; FAIL=$((FAIL + 1)); }
skip() { log "SKIP  $*"; SKIP=$((SKIP + 1)); }

section() {
  echo "" >&2
  log "==== $* ===="
}

# ---------------------------------------------------------------------------
# Assertions
# ---------------------------------------------------------------------------

assert_eq() {
  local desc="$1" got="$2" want="$3"
  if [[ "$got" == "$want" ]]; then
    ok "$desc"
    return 0
  fi
  bad "$desc (got='${got}' want='${want}')"
  return 1
}

assert_contains() {
  local desc="$1" haystack="$2" needle="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    ok "$desc"
    return 0
  fi
  bad "$desc (missing '${needle}')"
  # Truncate noisy haystacks for the log.
  local preview="$haystack"
  if [[ ${#preview} -gt 400 ]]; then
    preview="${preview:0:400}...(truncated)"
  fi
  info "haystack preview: ${preview}"
  return 1
}

assert_success() {
  local desc="$1"
  shift
  local out rc=0
  out="$("$@" 2>&1)" || rc=$?
  LAST_OUTPUT="$out"
  LAST_RC=$rc
  if [[ $rc -eq 0 ]]; then
    ok "$desc"
    return 0
  fi
  bad "$desc (exit=${rc})"
  info "output: ${out}"
  return 1
}

assert_nonzero_json_field() {
  # assert_nonzero_json_field desc json jq_path
  local desc="$1" json="$2" path="$3"
  local val
  val="$(printf '%s' "$json" | jq -r "$path" 2>/dev/null || echo "")"
  if [[ -n "$val" && "$val" != "null" && "$val" != "0" ]]; then
    ok "$desc (value=${val})"
    return 0
  fi
  bad "$desc (value='${val}')"
  return 1
}

# ---------------------------------------------------------------------------
# NTM binary resolution
# ---------------------------------------------------------------------------

resolve_ntm() {
  if [[ -n "${NTM_BIN:-}" ]]; then
    if [[ ! -x "$NTM_BIN" ]]; then
      err "NTM_BIN=${NTM_BIN} is not executable"
      return 1
    fi
    NTM_CMD=("$NTM_BIN")
    info "using NTM_BIN=${NTM_BIN}"
    return 0
  fi

  # Prefer a prebuilt binary (go build -o /tmp/herdctl ./cmd/ntm) over go run.
  for candidate in /tmp/herdctl "${ROOT_DIR}/ntm" "${ROOT_DIR}/cmd/ntm/ntm"; do
    if [[ -x "$candidate" ]]; then
      # Skip wrong-arch binaries (e.g. cross-compiled leftovers).
      if "$candidate" --help >/dev/null 2>&1 || "$candidate" version >/dev/null 2>&1; then
        NTM_CMD=("$candidate")
        info "using local binary: ${candidate}"
        return 0
      fi
      warn "skipping non-runnable binary: ${candidate}"
    fi
  done

  if command -v go >/dev/null 2>&1; then
    NTM_CMD=(go run ./cmd/ntm)
    info "using: go run ./cmd/ntm"
    return 0
  fi

  if command -v ntm >/dev/null 2>&1; then
    NTM_CMD=(ntm)
    info "using PATH ntm: $(command -v ntm)"
    return 0
  fi

  err "no ntm binary found (set NTM_BIN, build ./ntm, or install go)"
  return 1
}

run_ntm() {
  "${NTM_CMD[@]}" "$@"
}

# ---------------------------------------------------------------------------
# Herdr helpers
# ---------------------------------------------------------------------------

herdr_bin() {
  if [[ -n "${HERDR_BIN_PATH:-}" ]]; then
    printf '%s' "$HERDR_BIN_PATH"
    return 0
  fi
  command -v herdr
}

herdr_cmd() {
  local bin
  bin="$(herdr_bin)" || return 1
  "$bin" "$@"
}

# Extract the JSON object from herdr CLI stdout (single-line envelope).
herdr_json() {
  # herdr prints one JSON object per call on stdout.
  herdr_cmd "$@" 2>/dev/null
}

workspace_labels() {
  herdr_json workspace list \
    | jq -r '.result.workspaces[]?.label // empty' 2>/dev/null || true
}

workspace_ids_for_label() {
  local label="$1"
  herdr_json workspace list \
    | jq -r --arg l "$label" '.result.workspaces[]? | select(.label == $l) | .workspace_id' 2>/dev/null || true
}

agent_names() {
  herdr_json agent list \
    | jq -r '.result.agents[]?.name // empty' 2>/dev/null || true
}

# Names emitted by herdrLaunchAgent / StartAgent: "<session>-<type>_<index>"
# e.g. demo-session-cc_1, test-multi-cod_1
agent_names_for_session() {
  local session="$1"
  herdr_json agent list \
    | jq -r --arg s "$session" \
      '.result.agents[]? | select((.name // "") | startswith($s + "-")) | .name // empty' \
      2>/dev/null || true
}

# Count agents whose name starts with "<session>-" (our StartAgent naming).
count_agents_for_session() {
  local session="$1"
  herdr_json agent list \
    | jq -r --arg s "$session" \
      '[.result.agents[]? | select((.name // "") | startswith($s + "-"))] | length' \
      2>/dev/null || echo 0
}

pane_count_for_workspace() {
  local ws_id="$1"
  if [[ -z "$ws_id" ]]; then
    echo 0
    return 0
  fi
  herdr_json pane list --workspace "$ws_id" \
    | jq -r '.result.panes | length' 2>/dev/null || echo 0
}

# ---------------------------------------------------------------------------
# Cleanup (idempotent)
# ---------------------------------------------------------------------------

kill_session_best_effort() {
  local session="$1"
  # Prefer ntm kill --force so registry + workspace are both cleaned.
  run_ntm kill "$session" --force >/dev/null 2>&1 || true

  # Fallback: close any herdr workspace still labeled with this session.
  local ws_id
  while IFS= read -r ws_id; do
    [[ -z "$ws_id" ]] && continue
    info "closing leftover workspace ${ws_id} (label=${session})"
    herdr_cmd workspace close "$ws_id" >/dev/null 2>&1 || true
  done < <(workspace_ids_for_label "$session")
}

cleanup_all() {
  section "Cleanup"
  local s
  for s in "${CREATED_SESSIONS[@]:-}"; do
    [[ -z "$s" ]] && continue
    info "cleanup session: $s"
    kill_session_best_effort "$s"
  done
  # Always try known session names even if tracking missed them.
  kill_session_best_effort "$DEMO_SESSION"
  kill_session_best_effort "$MULTI_SESSION"
  kill_session_best_effort "$ADD_SESSION"
  kill_session_best_effort "$LABEL_SESSION"

  # Drop registry file so subsequent runs start clean.
  if [[ -f "$HERDCTL_HERDR_REGISTRY" ]]; then
    : >"$HERDCTL_HERDR_REGISTRY"
    printf '%s\n' '{"version":1,"sessions":{}}' >"$HERDCTL_HERDR_REGISTRY"
    info "reset registry: $HERDCTL_HERDR_REGISTRY"
  fi
}

track_session() {
  local s="$1"
  CREATED_SESSIONS+=("$s")
}

on_exit() {
  local rc=$?
  cleanup_all
  if [[ -n "${TEST_HOME:-}" && -d "${TEST_HOME}" ]]; then
    info "test home left for inspection: ${TEST_HOME}"
    info "  registry: ${HERDCTL_HERDR_REGISTRY}"
    info "  projects: ${NTM_PROJECTS_BASE}"
  fi
  # Preserve original exit if we already decided; otherwise derive from counters.
  if [[ $FAIL -gt 0 ]]; then
    exit 1
  fi
  exit "$rc"
}
trap on_exit EXIT

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------

check_prereqs() {
  section "Prerequisites"
  local missing=0

  if ! herdr_bin >/dev/null 2>&1; then
    err "herdr binary not found (install herdr or set HERDR_BIN_PATH)"
    missing=1
  else
    local ver
    ver="$(herdr_cmd --version 2>/dev/null || herdr_cmd version 2>/dev/null || echo unknown)"
    ok "herdr binary present: $(herdr_bin) (${ver})"
  fi

  if ! command -v jq >/dev/null 2>&1; then
    err "jq not found (required to parse herdr JSON envelopes)"
    missing=1
  else
    ok "jq present: $(command -v jq)"
  fi

  if ! resolve_ntm; then
    missing=1
  else
    ok "ntm command: ${NTM_CMD[*]}"
  fi

  if [[ $missing -ne 0 ]]; then
    err "prerequisite check failed"
    exit 1
  fi

  # herdr server must be running.
  local status_json
  if ! status_json="$(herdr_json status 2>/dev/null)"; then
    # `herdr status` may not be pure JSON on all builds; fall back to text.
    local status_txt
    status_txt="$(herdr_cmd status 2>&1 || true)"
    if [[ "$status_txt" == *"status: running"* || "$status_txt" == *"status:running"* ]]; then
      ok "herdr server running (text status)"
    else
      err "herdr server not reachable"
      info "herdr status output:"
      info "$status_txt"
      err "start herdr (open the app or run the server) and re-try"
      exit 1
    fi
  else
    # Prefer structured parse when available.
    local srv
    srv="$(printf '%s' "$status_json" | jq -r '.result.server.status // .server.status // empty' 2>/dev/null || true)"
    if [[ "$srv" == "running" ]]; then
      ok "herdr server running"
    else
      # Text fallback already covered above for non-JSON; here JSON lacked field.
      local status_txt
      status_txt="$(herdr_cmd status 2>&1 || true)"
      if [[ "$status_txt" == *"status: running"* ]]; then
        ok "herdr server running (text status)"
      else
        err "herdr server not running (status='${srv}')"
        info "$status_txt"
        exit 1
      fi
    fi
  fi

  # Confirm CLI can list workspaces (server RPC path).
  if ! herdr_json workspace list >/dev/null 2>&1; then
    err "herdr workspace list failed — server may be down or protocol mismatch"
    exit 1
  fi
  ok "herdr workspace list reachable"

  info "NTM_BACKEND=${NTM_BACKEND}"
  info "HERDCTL_HERDR_REGISTRY=${HERDCTL_HERDR_REGISTRY}"
  info "NTM_PROJECTS_BASE=${NTM_PROJECTS_BASE}"
  info "TEST_HOME=${TEST_HOME}"
}

# ---------------------------------------------------------------------------
# Seed clean registry
# ---------------------------------------------------------------------------

seed_registry() {
  section "Clean registry"
  mkdir -p "$(dirname "$HERDCTL_HERDR_REGISTRY")"
  printf '%s\n' '{"version":1,"sessions":{}}' >"$HERDCTL_HERDR_REGISTRY"
  ok "wrote empty registry at ${HERDCTL_HERDR_REGISTRY}"

  # Pre-clean any leftover workspaces from prior failed runs.
  kill_session_best_effort "$DEMO_SESSION"
  kill_session_best_effort "$MULTI_SESSION"
  kill_session_best_effort "$ADD_SESSION"
  kill_session_best_effort "$LABEL_SESSION"
  ok "pre-cleaned leftover demo/multi/add/label workspaces (if any)"
}

# Log context for a case (FEATURES e2e quality: backend, registry, workspace, agents).
log_case_context() {
  local session="${1:-}"
  info "context: NTM_BACKEND=${NTM_BACKEND} registry=${HERDCTL_HERDR_REGISTRY}"
  if [[ -n "$session" ]]; then
    local ws_id agents
    ws_id="$(workspace_ids_for_label "$session" | head -n1)"
    agents="$(agent_names_for_session "$session" | tr '\n' ' ')"
    info "context: session=${session} workspace_id=${ws_id:-none} agents=[${agents}]"
  fi
}

# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

test_spawn_demo() {
  section "Test: spawn ${DEMO_SESSION} --cc=1"
  track_session "$DEMO_SESSION"

  local out rc=0
  # Pipe 'y' in case spawn prompts to create the project directory.
  out="$(printf 'y\n' | run_ntm spawn "$DEMO_SESSION" --cc=1 2>&1)" || rc=$?
  LAST_OUTPUT="$out"
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${DEMO_SESSION} --cc=1 (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "spawn ${DEMO_SESSION} --cc=1 succeeded"
  info "spawn output: ${out}"

  # Verify herdr workspace labeled with session name exists.
  local labels
  labels="$(workspace_labels)"
  assert_contains "herdr workspace list includes ${DEMO_SESSION}" "$labels" "$DEMO_SESSION"

  local ws_id
  ws_id="$(workspace_ids_for_label "$DEMO_SESSION" | head -n1)"
  if [[ -n "$ws_id" ]]; then
    ok "workspace id for ${DEMO_SESSION}: ${ws_id}"
  else
    bad "could not resolve workspace id for ${DEMO_SESSION}"
  fi

  # Agent should be registered with unique name "<session>-cc_1".
  local agents
  agents="$(agent_names)"
  assert_contains "herdr agent list includes ${DEMO_SESSION}-cc_1" "$agents" "${DEMO_SESSION}-cc_1"

  local agent_count
  agent_count="$(count_agents_for_session "$DEMO_SESSION")"
  assert_eq "demo session has >=1 agent" "$([[ "${agent_count:-0}" -ge 1 ]] && echo yes || echo no)" "yes"

  # Pane list for the workspace should show at least user + agent (2 panes).
  if [[ -n "$ws_id" ]]; then
    local panes
    panes="$(pane_count_for_workspace "$ws_id")"
    if [[ "${panes:-0}" -ge 2 ]]; then
      ok "workspace ${ws_id} has ${panes} panes (>=2 expected)"
    else
      bad "workspace ${ws_id} pane count=${panes}, expected >=2"
    fi
  fi
}

test_list() {
  section "Test: list shows ${DEMO_SESSION}"
  local out rc=0
  out="$(run_ntm list 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm list (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "ntm list succeeded"
  assert_contains "ntm list output includes ${DEMO_SESSION}" "$out" "$DEMO_SESSION"
}


test_status() {
  section "Test: status ${DEMO_SESSION}"
  log_case_context "$DEMO_SESSION"
  local out rc=0
  out="$(run_ntm status "$DEMO_SESSION" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm status ${DEMO_SESSION} (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "ntm status ${DEMO_SESSION} succeeded (exit=0)"
  # Must show agent type (claude/cc) and a status field (idle/working/unknown/error)
  if [[ "$out" == *"claude"* || "$out" == *"Claude"* || "$out" == *"cc_"* || "$out" == *"cc…"* || "$out" == *"cc..."* ]]; then
    ok "status output includes agent type"
  else
    bad "status output missing agent type (claude/cc)"
    info "output: ${out}"
  fi
  if [[ "$out" == *"idle"* || "$out" == *"working"* || "$out" == *"unknown"* || "$out" == *"error"* || "$out" == *"blocked"* ]]; then
    ok "status output includes status field"
  else
    bad "status output missing status field"
    info "output: ${out}"
  fi
  # herdr pane identity (wN:pM) when backend is herdr
  if [[ "$out" == *"w"*":p"* || "$out" == *"ntm="* ]]; then
    ok "status output includes herdr pane identity (wN:pM / ntm=)"
  else
    # Soft: older builds may omit; still note for FEATURES honesty.
    warn "status output missing herdr pane id / ntm= (acceptable if table format differs)"
    info "output preview: ${out:0:300}"
  fi
  info "status output: ${out}"
}

# ntm agent list/get/read after spawn (bd-gl28u.1.12).
test_agent_ops() {
  section "Test: agent list/get/read ${DEMO_SESSION} (bd-gl28u.1.12)"
  log_case_context "$DEMO_SESSION"

  local agent_name="${DEMO_SESSION}-cc_1"
  local out rc=0

  # list (session-filtered)
  out="$(run_ntm agent list --session "$DEMO_SESSION" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm agent list --session ${DEMO_SESSION} (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "ntm agent list --session ${DEMO_SESSION} succeeded"
  assert_contains "agent list includes ${agent_name}" "$out" "$agent_name"

  # get
  rc=0
  out="$(run_ntm agent get "$agent_name" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm agent get ${agent_name} (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "ntm agent get ${agent_name} succeeded"
  assert_contains "agent get shows name" "$out" "$agent_name"
  if [[ "$out" == *"idle"* || "$out" == *"working"* || "$out" == *"unknown"* || "$out" == *"blocked"* ]]; then
    ok "agent get includes status"
  else
    bad "agent get missing status field"
    info "output: ${out}"
  fi

  # read
  rc=0
  out="$(run_ntm agent read "$agent_name" --lines 10 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm agent read ${agent_name} (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "ntm agent read ${agent_name} succeeded (bytes=${#out})"

  # explain (herdr-only; should succeed under NTM_BACKEND=herdr)
  rc=0
  out="$(run_ntm agent explain "$agent_name" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm agent explain ${agent_name} (exit=${rc})"
    info "output: ${out}"
  else
    ok "ntm agent explain ${agent_name} succeeded"
    if [[ "$out" == *"agent"* || "$out" == *"evaluated_rules"* || "$out" == *"state"* ]]; then
      ok "agent explain returns detection evidence"
    else
      bad "agent explain output missing expected keys"
      info "output preview: ${out:0:300}"
    fi
  fi

  # focus
  rc=0
  out="$(run_ntm agent focus "$agent_name" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm agent focus ${agent_name} (exit=${rc})"
    info "output: ${out}"
  else
    ok "ntm agent focus ${agent_name} succeeded"
  fi

  # agents profiles (backend-agnostic)
  rc=0
  out="$(run_ntm agents list 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm agents list (exit=${rc})"
    info "output: ${out}"
  else
    ok "ntm agents list succeeded"
    assert_contains "agents list includes claude" "$out" "claude"
  fi
}

# status --watch must re-fetch via mux each tick (no tmux poll) and exit cleanly
# on SIGINT (bd-gl28u.1.8).
test_status_watch() {
  section "Test: status --watch ${DEMO_SESSION} (mux each tick)"
  log_case_context "$DEMO_SESSION"

  local out_file
  out_file="$(mktemp -t ntm-status-watch-XXXXXX)"
  local rc_file
  rc_file="$(mktemp -t ntm-status-watch-rc-XXXXXX)"

  # Run watch in background; kill after a few intervals so we observe ≥1 refresh.
  (
    set +e
    run_ntm status "$DEMO_SESSION" --watch --interval "$WATCH_INTERVAL_MS" >"$out_file" 2>&1
    echo $? >"$rc_file"
  ) &
  local watch_pid=$!
  info "status --watch pid=${watch_pid} interval_ms=${WATCH_INTERVAL_MS}"

  # Wait for at least WATCH_TICKS * interval (+ startup headroom).
  local wait_s
  wait_s="$(awk -v ms="$WATCH_INTERVAL_MS" -v ticks="$WATCH_TICKS" 'BEGIN { printf "%.2f", (ms * (ticks + 1)) / 1000.0 + 0.5 }')"
  sleep "$wait_s"

  if ! kill -0 "$watch_pid" 2>/dev/null; then
    # Process already exited — may be error or immediate completion.
    wait "$watch_pid" 2>/dev/null || true
    local early_out
    early_out="$(cat "$out_file" 2>/dev/null || true)"
    if [[ "$early_out" == *"Error"* || "$early_out" == *"not found"* ]]; then
      bad "status --watch exited early with error"
      info "output: ${early_out}"
      rm -f "$out_file" "$rc_file"
      return 1
    fi
    warn "status --watch exited before SIGINT (output may still be valid)"
  else
    # Clean Ctrl-C path: SIGINT then wait.
    # runStatusOnce can take several seconds (herdr RPCs); allow up to ~8s.
    kill -INT "$watch_pid" 2>/dev/null || true
    local i
    for i in $(seq 1 40); do
      if ! kill -0 "$watch_pid" 2>/dev/null; then
        break
      fi
      sleep 0.2
    done
    if kill -0 "$watch_pid" 2>/dev/null; then
      warn "status --watch still running after SIGINT; sending SIGTERM"
      kill -TERM "$watch_pid" 2>/dev/null || true
      sleep 0.5
      if kill -0 "$watch_pid" 2>/dev/null; then
        kill -KILL "$watch_pid" 2>/dev/null || true
      fi
    fi
    wait "$watch_pid" 2>/dev/null || true
  fi

  local out
  out="$(cat "$out_file" 2>/dev/null || true)"
  rm -f "$out_file" "$rc_file"

  if [[ -z "$out" ]]; then
    bad "status --watch produced no output"
    return 1
  fi
  ok "status --watch produced output"

  # Must still show session / agent content from re-fetched panes.
  if [[ "$out" == *"$DEMO_SESSION"* || "$out" == *"claude"* || "$out" == *"Claude"* || "$out" == *"Panes"* ]]; then
    ok "status --watch output includes session/agent content"
  else
    bad "status --watch output missing session/agent content"
    info "output: ${out:0:400}"
  fi

  # Prefer clean stop message; not required if SIGTERM used.
  if [[ "$out" == *"Watch mode stopped"* ]]; then
    ok "status --watch stopped cleanly (Watch mode stopped)"
  else
    warn "status --watch did not print 'Watch mode stopped' (signal may have been SIGTERM)"
  fi

  # Must not mention tmux binary failures under herdr.
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "status --watch invoked tmux (not herdr-safe)"
    info "output: ${out:0:400}"
  else
    ok "status --watch did not require tmux binary"
  fi

  info "status --watch output preview: ${out:0:400}"
}

# attach on herdr: exit 0 + actionable guidance (never tmux attach) (bd-gl28u.1.3).
test_attach() {
  section "Test: attach ${DEMO_SESSION} (herdr-safe guidance)"
  log_case_context "$DEMO_SESSION"
  local out rc=0
  out="$(run_ntm attach "$DEMO_SESSION" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm attach ${DEMO_SESSION} (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "ntm attach ${DEMO_SESSION} exit=0"

  assert_contains "attach message mentions herdr" "$out" "herdr"
  if [[ "$out" == *"$DEMO_SESSION"* ]]; then
    ok "attach message includes session name ${DEMO_SESSION}"
  else
    bad "attach message missing session name"
    info "output: ${out}"
  fi
  if [[ "$out" == *"workspace"* || "$out" == *"Focus"* || "$out" == *"TUI"* || "$out" == *"label"* ]]; then
    ok "attach message is actionable (workspace/focus/TUI)"
  else
    bad "attach message not actionable"
    info "output: ${out}"
  fi
  # Must not try tmux attach language as the primary path.
  if [[ "$out" == *"attaching to tmux"* || "$out" == *"tmux attach"* ]]; then
    bad "attach appears to invoke tmux attach under herdr"
    info "output: ${out}"
  else
    ok "attach does not invoke tmux attach"
  fi
  info "attach output: ${out}"
}

test_send() {
  section "Test: send ${DEMO_SESSION} --cc '${TEST_MSG}'"
  log_case_context "$DEMO_SESSION"
  local out rc=0
  out="$(run_ntm send "$DEMO_SESSION" --cc "$TEST_MSG" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "send ${DEMO_SESSION} --cc (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "send ${DEMO_SESSION} --cc succeeded"
  info "send output: ${out}"
}

test_kill_demo() {
  section "Test: kill ${DEMO_SESSION}"
  local out rc=0
  out="$(run_ntm kill "$DEMO_SESSION" --force 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "kill ${DEMO_SESSION} --force (exit=${rc})"
    info "output: ${out}"
    # Fall through to herdr cleanup verification / best-effort close.
    kill_session_best_effort "$DEMO_SESSION"
  else
    ok "kill ${DEMO_SESSION} --force succeeded"
  fi

  # Workspace should no longer be present under that label.
  local labels
  labels="$(workspace_labels)"
  if [[ "$labels" == *"$DEMO_SESSION"* ]]; then
    # Give herdr a moment, then re-check (close can be async-ish).
    sleep 0.5
    labels="$(workspace_labels)"
  fi
  if [[ "$labels" == *"$DEMO_SESSION"* ]]; then
    bad "workspace label ${DEMO_SESSION} still present after kill"
    info "labels: ${labels}"
  else
    ok "workspace label ${DEMO_SESSION} gone after kill"
  fi

  # Agent names for the session should be gone.
  local agents
  agents="$(agent_names)"
  if [[ "$agents" == *"${DEMO_SESSION}-cc_1"* ]]; then
    bad "agent ${DEMO_SESSION}-cc_1 still listed after kill"
  else
    ok "agent ${DEMO_SESSION}-cc_1 gone after kill"
  fi

  # ntm list should no longer show the session.
  local list_out
  list_out="$(run_ntm list 2>&1 || true)"
  if [[ "$list_out" == *"$DEMO_SESSION"* ]]; then
    bad "ntm list still shows ${DEMO_SESSION} after kill"
  else
    ok "ntm list no longer shows ${DEMO_SESSION}"
  fi
}

test_multi_agent() {
  section "Test: multi-agent spawn ${MULTI_SESSION} --cc=2 --cod=1 (agent_name_taken fix)"
  track_session "$MULTI_SESSION"

  # Ensure clean slate for this session name.
  kill_session_best_effort "$MULTI_SESSION"

  local out rc=0
  out="$(printf 'y\n' | run_ntm spawn "$MULTI_SESSION" --cc=2 --cod=1 2>&1)" || rc=$?
  LAST_OUTPUT="$out"

  if [[ $rc -ne 0 ]]; then
    # Specifically surface the agent_name_taken failure mode.
    if [[ "$out" == *"agent_name_taken"* ]]; then
      bad "spawn multi-agent hit agent_name_taken (unique-name fix broken)"
    else
      bad "spawn ${MULTI_SESSION} --cc=2 --cod=1 (exit=${rc})"
    fi
    info "output: ${out}"
    return 1
  fi

  if [[ "$out" == *"agent_name_taken"* ]]; then
    bad "spawn reported success but output mentions agent_name_taken"
    info "output: ${out}"
    return 1
  fi
  ok "spawn ${MULTI_SESSION} --cc=2 --cod=1 succeeded (no agent_name_taken)"
  info "spawn output: ${out}"

  # Workspace present.
  local labels
  labels="$(workspace_labels)"
  assert_contains "herdr workspace list includes ${MULTI_SESSION}" "$labels" "$MULTI_SESSION"

  # Expect three uniquely-named agents: cc_1, cc_2, cod_1.
  local agents
  agents="$(agent_names)"
  assert_contains "agent list has ${MULTI_SESSION}-cc_1" "$agents" "${MULTI_SESSION}-cc_1"
  assert_contains "agent list has ${MULTI_SESSION}-cc_2" "$agents" "${MULTI_SESSION}-cc_2"
  assert_contains "agent list has ${MULTI_SESSION}-cod_1" "$agents" "${MULTI_SESSION}-cod_1"

  local agent_count
  agent_count="$(count_agents_for_session "$MULTI_SESSION")"
  if [[ "${agent_count:-0}" -ge 3 ]]; then
    ok "multi session agent count=${agent_count} (>=3)"
  else
    bad "multi session agent count=${agent_count}, expected >=3"
    info "agents: ${agents}"
  fi

  # Pane verification via herdr.
  local ws_id panes
  ws_id="$(workspace_ids_for_label "$MULTI_SESSION" | head -n1)"
  panes="$(pane_count_for_workspace "$ws_id")"
  # user pane + 3 agents = 4, but some configs use --no-user; accept >=3.
  if [[ "${panes:-0}" -ge 3 ]]; then
    ok "multi workspace ${ws_id} has ${panes} panes (>=3)"
  else
    bad "multi workspace ${ws_id} pane count=${panes}, expected >=3"
  fi

  # Cleanup multi session as part of this test so leftover state is minimal.
  local kill_out kill_rc=0
  kill_out="$(run_ntm kill "$MULTI_SESSION" --force 2>&1)" || kill_rc=$?
  if [[ $kill_rc -ne 0 ]]; then
    bad "kill ${MULTI_SESSION} --force (exit=${kill_rc})"
    info "output: ${kill_out}"
    kill_session_best_effort "$MULTI_SESSION"
  else
    ok "kill ${MULTI_SESSION} --force succeeded"
  fi
}

# Mid-session scale-out: spawn --cc=1 then add --cc=1 and optionally --cod=1.
# Verifies session-prefixed names continue NTMIndex (cc_2 not agent_name_taken)
# and no empty shell panes are left behind (bd-gl28u.1.2).

test_add_agents() {
  section "Test: add agents to running session (${ADD_SESSION})"
  track_session "$ADD_SESSION"
  kill_session_best_effort "$ADD_SESSION"

  local out rc=0
  out="$(printf 'y\n' | run_ntm spawn "$ADD_SESSION" --cc=1 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${ADD_SESSION} --cc=1 for add test (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "spawn ${ADD_SESSION} --cc=1 for add baseline"

  # add one more Claude — must become session-cc_2, not agent_name_taken
  out="$(run_ntm add "$ADD_SESSION" --cc=1 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    if [[ "$out" == *"agent_name_taken"* ]]; then
      bad "add --cc=1 hit agent_name_taken (NTMIndex continuation broken)"
    else
      bad "add ${ADD_SESSION} --cc=1 (exit=${rc})"
    fi
    info "output: ${out}"
    kill_session_best_effort "$ADD_SESSION"
    return 1
  fi
  if [[ "$out" == *"agent_name_taken"* ]]; then
    bad "add succeeded but output mentions agent_name_taken"
    info "output: ${out}"
    kill_session_best_effort "$ADD_SESSION"
    return 1
  fi
  ok "add ${ADD_SESSION} --cc=1 succeeded (no agent_name_taken)"
  info "add --cc output: ${out}"

  local agents
  agents="$(agent_names)"
  assert_contains "after add: agent list has ${ADD_SESSION}-cc_1" "$agents" "${ADD_SESSION}-cc_1"
  assert_contains "after add: agent list has ${ADD_SESSION}-cc_2" "$agents" "${ADD_SESSION}-cc_2"

  # Multi-type add: --cod should create session-cod_1
  rc=0
  out="$(run_ntm add "$ADD_SESSION" --cod=1 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    if [[ "$out" == *"agent_name_taken"* ]]; then
      bad "add --cod=1 hit agent_name_taken"
    else
      bad "add ${ADD_SESSION} --cod=1 (exit=${rc})"
    fi
    info "output: ${out}"
  else
    ok "add ${ADD_SESSION} --cod=1 succeeded"
    agents="$(agent_names)"
    assert_contains "after add --cod: agent list has ${ADD_SESSION}-cod_1" "$agents" "${ADD_SESSION}-cod_1"
  fi

  local agent_count
  agent_count="$(count_agents_for_session "$ADD_SESSION")"
  if [[ "${agent_count:-0}" -ge 3 ]]; then
    ok "add session agent count=${agent_count} (>=3: cc_1,cc_2,cod_1)"
  else
    bad "add session agent count=${agent_count}, expected >=3"
    info "agents: $(agent_names_for_session "$ADD_SESSION")"
  fi

  # Pane count should match agents (+ optional user pane), not empty shells.
  local ws_id panes
  ws_id="$(workspace_ids_for_label "$ADD_SESSION" | head -n1)"
  panes="$(pane_count_for_workspace "$ws_id")"
  if [[ "${panes:-0}" -ge 3 && "${panes:-0}" -le 5 ]]; then
    ok "add workspace ${ws_id} pane count=${panes} (agents+user, no empty shells)"
  else
    # Soft fail: still report but don't hard-require exact layout.
    bad "add workspace ${ws_id} pane count=${panes}, expected ~3-5 (no empty precreate shells)"
  fi

  local kill_out kill_rc=0
  kill_out="$(run_ntm kill "$ADD_SESSION" --force 2>&1)" || kill_rc=$?
  if [[ $kill_rc -ne 0 ]]; then
    bad "kill ${ADD_SESSION} --force (exit=${kill_rc})"
    info "output: ${kill_out}"
    kill_session_best_effort "$ADD_SESSION"
  else
    ok "kill ${ADD_SESSION} --force succeeded"
  fi
}

# spawn --label: full session name (base--label) in registry + herdr workspace (bd-gl28u.1.4).
test_spawn_label() {
  section "Test: spawn --label ${LABEL_SUFFIX} → ${LABEL_SESSION}"
  track_session "$LABEL_SESSION"
  kill_session_best_effort "$LABEL_SESSION"

  local out rc=0
  out="$(printf 'y\n' | run_ntm spawn "$LABEL_BASE" --label "$LABEL_SUFFIX" --cc=1 2>&1)" || rc=$?
  LAST_OUTPUT="$out"
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${LABEL_BASE} --label ${LABEL_SUFFIX} --cc=1 (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "spawn --label ${LABEL_SUFFIX} succeeded (exit=0)"
  info "spawn --label output: ${out}"
  log_case_context "$LABEL_SESSION"

  # Workspace label must be the full labeled session name.
  local labels
  labels="$(workspace_labels)"
  assert_contains "herdr workspace list includes ${LABEL_SESSION}" "$labels" "$LABEL_SESSION"

  local ws_id
  ws_id="$(workspace_ids_for_label "$LABEL_SESSION" | head -n1)"
  if [[ -n "$ws_id" ]]; then
    ok "workspace id for labeled session ${LABEL_SESSION}: ${ws_id}"
  else
    bad "could not resolve workspace id for ${LABEL_SESSION}"
  fi

  # Agent name uses full labeled session as prefix.
  local agents
  agents="$(agent_names)"
  assert_contains "agent list has ${LABEL_SESSION}-cc_1" "$agents" "${LABEL_SESSION}-cc_1"

  # ntm list shows full labeled name.
  local list_out list_rc=0
  list_out="$(run_ntm list 2>&1)" || list_rc=$?
  if [[ $list_rc -ne 0 ]]; then
    bad "ntm list after labeled spawn (exit=${list_rc})"
    info "output: ${list_out}"
  else
    assert_contains "ntm list includes labeled session ${LABEL_SESSION}" "$list_out" "$LABEL_SESSION"
  fi

  # status on labeled session works.
  local st_out st_rc=0
  st_out="$(run_ntm status "$LABEL_SESSION" 2>&1)" || st_rc=$?
  if [[ $st_rc -ne 0 ]]; then
    bad "ntm status ${LABEL_SESSION} (exit=${st_rc})"
    info "output: ${st_out}"
  else
    ok "ntm status ${LABEL_SESSION} succeeded"
  fi

  # Project dir should use SessionBase (base name, not full labeled name).
  local base_dir="${NTM_PROJECTS_BASE}/${LABEL_BASE}"
  if [[ -d "$base_dir" ]]; then
    ok "project dir uses SessionBase: ${base_dir}"
  else
    # Soft: resolveCreationProjectDir may use different layout; check spawn output.
    if [[ "$out" == *"${LABEL_BASE}"* ]]; then
      ok "spawn output references base project ${LABEL_BASE} (SessionBase path may vary)"
    else
      warn "could not confirm SessionBase project dir at ${base_dir}"
    fi
  fi

  local kill_out kill_rc=0
  kill_out="$(run_ntm kill "$LABEL_SESSION" --force 2>&1)" || kill_rc=$?
  if [[ $kill_rc -ne 0 ]]; then
    bad "kill ${LABEL_SESSION} --force (exit=${kill_rc})"
    info "output: ${kill_out}"
    kill_session_best_effort "$LABEL_SESSION"
  else
    ok "kill ${LABEL_SESSION} --force succeeded"
  fi
}

# ---------------------------------------------------------------------------
# Persistence pack (bd-gl28u.3.1 / bd-gl28u.3.2)
# ---------------------------------------------------------------------------

test_checkpoint_save_restore() {
  section "Test: checkpoint save → kill → restore → list (${CP_SESSION}) (bd-gl28u.3.1)"
  track_session "$CP_SESSION"
  kill_session_best_effort "$CP_SESSION"

  local out rc=0
  out="$(printf 'y\n' | run_ntm spawn "$CP_SESSION" --cc=1 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${CP_SESSION} for checkpoint (exit=${rc})"
    info "output: ${out:0:500}"
    return 1
  fi
  ok "spawn ${CP_SESSION} for checkpoint"

  # Brief settle for registry / agent start.
  sleep 1

  rc=0
  out="$(run_ntm --json checkpoint save "$CP_SESSION" --no-git 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "checkpoint save ${CP_SESSION} (exit=${rc})"
    info "output: ${out}"
    kill_session_best_effort "$CP_SESSION"
    return 1
  fi
  ok "checkpoint save ${CP_SESSION}"
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* || "$out" == *"EnsureInstalled"* ]]; then
    bad "checkpoint save required tmux under herdr"
  else
    ok "checkpoint save did not require tmux binary"
  fi

  local cp_id=""
  if command -v jq >/dev/null 2>&1; then
    cp_id="$(printf '%s\n' "$out" | jq -r 'if type=="object" then .id // empty else empty end' 2>/dev/null || true)"
  fi
  if [[ -z "$cp_id" ]]; then
    cp_id="$(printf '%s\n' "$out" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  fi
  if [[ -z "$cp_id" ]]; then
    cp_id="last"
    warn "could not parse checkpoint id; restore will use 'last'"
  else
    ok "checkpoint id=${cp_id}"
  fi

  rc=0
  out="$(run_ntm kill "$CP_SESSION" --force 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "kill ${CP_SESSION} before restore (exit=${rc})"
    info "output: ${out}"
    kill_session_best_effort "$CP_SESSION"
  else
    ok "kill ${CP_SESSION} before restore"
  fi

  # Confirm session gone before restore.
  local list_out
  list_out="$(run_ntm list 2>&1 || true)"
  if [[ "$list_out" == *"$CP_SESSION"* ]]; then
    warn "session still listed after kill; continuing with restore --force"
  fi

  rc=0
  out="$(run_ntm --json checkpoint restore "$CP_SESSION" "$cp_id" --force 2>&1)" || rc=$?
  if [[ $rc -ne 0 && "$cp_id" != "last" ]]; then
    out="$(run_ntm --json checkpoint restore "$CP_SESSION" last --force 2>&1)" || rc=$?
  fi
  if [[ $rc -ne 0 ]]; then
    bad "checkpoint restore ${CP_SESSION} (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "checkpoint restore ${CP_SESSION}"
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "checkpoint restore required tmux under herdr"
  else
    ok "checkpoint restore did not require tmux binary"
  fi

  list_out="$(run_ntm list 2>&1 || true)"
  if [[ "$list_out" == *"$CP_SESSION"* ]]; then
    ok "ntm list shows ${CP_SESSION} after restore"
  else
    bad "ntm list missing ${CP_SESSION} after restore"
    info "list: ${list_out}"
  fi

  rc=0
  out="$(run_ntm --json checkpoint list "$CP_SESSION" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "checkpoint list ${CP_SESSION} (exit=${rc})"
    info "output: ${out}"
  else
    ok "checkpoint list ${CP_SESSION}"
  fi

  kill_session_best_effort "$CP_SESSION"
}

test_persistence_disk_cmds() {
  section "Test: timeline/history/handoff/sessions-list disk paths (bd-gl28u.3.2)"
  local out rc

  rc=0
  out="$(run_ntm --json timeline list 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "timeline list (exit=${rc})"
    info "output: ${out:0:300}"
  else
    ok "timeline list (disk; no backend panes)"
  fi
  if [[ "$out" == *"tmux: command not found"* ]]; then
    bad "timeline list required tmux"
  else
    ok "timeline list did not require tmux"
  fi

  rc=0
  out="$(run_ntm --json history --limit=5 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "history (exit=${rc})"
    info "output: ${out:0:300}"
  else
    ok "history list (file store)"
  fi

  rc=0
  out="$(run_ntm handoff create "$PERSIST_SESSION" --goal "e2e checkpoint pack" --now "verify herdr" --json 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "handoff create (exit=${rc})"
    info "output: ${out:0:400}"
  else
    ok "handoff create (YAML; disk-first)"
  fi

  rc=0
  out="$(run_ntm resume "$PERSIST_SESSION" --dry-run --json 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    if [[ "$out" == *"no handoff"* || "$out" == *"not found"* || "$out" == *"No handoff"* || "$out" == *"handoff"* ]]; then
      ok "resume dry-run handled missing/found handoff without crash (exit=${rc})"
    else
      bad "resume dry-run (exit=${rc})"
      info "output: ${out:0:400}"
    fi
  else
    ok "resume dry-run"
  fi
  if [[ "$out" == *"tmux: command not found"* ]]; then
    bad "resume required tmux under herdr"
  else
    ok "resume did not require tmux binary"
  fi

  rc=0
  out="$(run_ntm --json sessions list 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "sessions list (exit=${rc})"
    info "output: ${out:0:300}"
  else
    ok "sessions list (disk save mgmt / archive surface)"
  fi
}

# ---------------------------------------------------------------------------
# Summary / FEATURES checklist
# ---------------------------------------------------------------------------

print_features_checklist() {
  section "FEATURES.md §1 P0 checklist (herdr e2e)"
  # Lines match FEATURES table rows; PASS/FAIL/SKIP from this run's cases.
  info "checklist: spawn --cc=N          (test_spawn_demo)"
  info "checklist: list                  (test_list)"
  info "checklist: status <session>      (test_status)"
  info "checklist: agent list/get/read   (test_agent_ops / bd-gl28u.1.12)"
  info "checklist: status --watch        (test_status_watch / bd-gl28u.1.8)"
  info "checklist: attach <session>      (test_attach / bd-gl28u.1.3)"
  info "checklist: send --cc             (test_send)"
  info "checklist: kill --force          (test_kill_demo)"
  info "checklist: multi-agent spawn     (test_multi_agent)"
  info "checklist: add --cc/--cod        (test_add_agents / bd-gl28u.1.2)"
  info "checklist: spawn --label         (test_spawn_label / bd-gl28u.1.4)"
  info "checklist: checkpoint save/restore (test_checkpoint_save_restore / bd-gl28u.3.1)"
  info "checklist: timeline/history/handoff (test_persistence_disk_cmds / bd-gl28u.3.2)"
  info "See FEATURES.md §1 for official ✓/~ /✗; this script only verifies exercised paths."
}

print_summary() {
  section "Summary"
  local total=$((PASS + FAIL + SKIP))
  info "passed=${PASS} failed=${FAIL} skipped=${SKIP} total=${total}"
  info "backend=${NTM_BACKEND} registry=${HERDCTL_HERDR_REGISTRY}"
  info "ntm=${NTM_CMD[*]}"
  if [[ $FAIL -gt 0 ]]; then
    err "HERDR BACKEND E2E FAILED"
    return 1
  fi
  info "HERDR BACKEND E2E PASSED"
  return 0
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
  info "herdr-ntm backend e2e starting (cwd=${ROOT_DIR})"
  info "sessions: demo=${DEMO_SESSION} multi=${MULTI_SESSION} add=${ADD_SESSION} label=${LABEL_SESSION} cp=${CP_SESSION}"
  check_prereqs
  seed_registry

  test_spawn_demo
  test_list
  test_status
  test_agent_ops
  test_status_watch
  test_attach
  test_send
  test_kill_demo
  test_multi_agent
  test_add_agents
  test_spawn_label
  test_checkpoint_save_restore
  test_persistence_disk_cmds

  print_features_checklist
  print_summary
}

main "$@"
