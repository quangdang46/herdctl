#!/usr/bin/env bash
# E2E smoke for NTM_BACKEND=herdr.
#
# FEATURES.md §1 P0 coverage (bd-gl28u.1.6 / bd-gl28u.1.8 / bd-gl28u.1.10):
#   spawn → list → status → status --watch → attach → send → kill
#   multi-agent spawn (cc=2,cod=1) — agent_name_taken regression
#   spawn --profile-set quick-impl/backend-team — persona JSON fields (p1-profile-set-e2e)
#   mid-session add (spawn --cc=1; add --cc=1 / --cod=1) — NTMIndex
#   spawn --label — full session name in registry + herdr workspace label
#   create --panes → session list → session stop/delete (mux lifecycle)
#   spawn --assign (agent_status wait; no beads → 0 assignments)
#
# Prerequisites:
#   - herdr binary on PATH (or HERDR_BIN_PATH)
#   - herdr server running (herdr status → server.status=running)
#   - go (if no prebuilt binary) or NTM_BIN / /tmp/herdctl / ./ntm
#
# Usage (from repo root):
#   ./scripts/test-herdr-backend.sh
#   NTM_BIN=/tmp/herdctl ./scripts/test-herdr-backend.sh
#   go build -o /tmp/herdctl ./cmd/herdctl && NTM_BIN=/tmp/herdctl ./scripts/test-herdr-backend.sh
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
CREATE_SESSION="${CREATE_SESSION:-test-create}"
WT_SESSION="${WT_SESSION:-wt-e2e}"
WT_NEG_SESSION="${WT_NEG_SESSION:-wt-neg}"
SAVE_SESSION="${SAVE_SESSION:-test-sessions-save}"
SAVE_NAME="${SAVE_NAME:-test-sessions-save-snap}"
RESTORED_SESSION="${RESTORED_SESSION:-test-sessions-restored}"
ASSIGN_SESSION="${ASSIGN_SESSION:-test-assign}"
PROFILE_SESSION="${PROFILE_SESSION:-ps-e2e}"
PROFILE_BT_SESSION="${PROFILE_BT_SESSION:-ps-bt}"
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

  # Prefer a prebuilt binary (go build -o /tmp/herdctl ./cmd/herdctl) over go run.
  for candidate in /tmp/herdctl "${ROOT_DIR}/ntm" "${ROOT_DIR}/cmd/herdctl/ntm"; do
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
    NTM_CMD=(go run ./cmd/herdctl)
    info "using: go run ./cmd/herdctl"
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
  kill_session_best_effort "$CREATE_SESSION"
  kill_session_best_effort "$SAVE_SESSION"
  kill_session_best_effort "$RESTORED_SESSION"
  kill_session_best_effort "$WT_SESSION"
  kill_session_best_effort "$WT_NEG_SESSION"
  # Swarm sessions use *_agents_* names (default cc_agents_1 in test_swarm_lifecycle).
  run_ntm swarm stop -y --force >/dev/null 2>&1 || true
  kill_session_best_effort "cc_agents_1"

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
  kill_session_best_effort "$CREATE_SESSION"
  kill_session_best_effort "$SAVE_SESSION"
  kill_session_best_effort "$RESTORED_SESSION"
  kill_session_best_effort "$WT_SESSION"
  kill_session_best_effort "$WT_NEG_SESSION"
  ok "pre-cleaned leftover demo/multi/add/label/create/worktree workspaces (if any)"
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
# on SIGINT / SIGTERM (bd-gl28u.1.8).
test_status_watch() {
  section "Test: status --watch ${DEMO_SESSION} (mux each tick)"
  log_case_context "$DEMO_SESSION"

  local out_file
  out_file="$(mktemp -t ntm-status-watch-XXXXXX)"

  # Background the ntm process directly (no subshell wrapper) so SIGINT/SIGTERM
  # hit the watch loop, not an intermediate bash that may swallow signals.
  set +e
  run_ntm status "$DEMO_SESSION" --watch --interval "$WATCH_INTERVAL_MS" >"$out_file" 2>&1 &
  local watch_pid=$!
  set -e
  info "status --watch pid=${watch_pid} interval_ms=${WATCH_INTERVAL_MS}"

  # Wait for at least WATCH_TICKS * interval (+ startup headroom).
  local wait_s
  wait_s="$(awk -v ms="$WATCH_INTERVAL_MS" -v ticks="$WATCH_TICKS" 'BEGIN { printf "%.2f", (ms * (ticks + 1)) / 1000.0 + 0.5 }')"
  sleep "$wait_s"

  local sent_sig="none"
  if ! kill -0 "$watch_pid" 2>/dev/null; then
    # Process already exited — may be error or immediate completion.
    wait "$watch_pid" 2>/dev/null || true
    local early_out
    early_out="$(cat "$out_file" 2>/dev/null || true)"
    if [[ "$early_out" == *"Error"* || "$early_out" == *"not found"* ]]; then
      bad "status --watch exited early with error"
      info "output: ${early_out}"
      rm -f "$out_file"
      return 1
    fi
    warn "status --watch exited before SIGINT (output may still be valid)"
  else
    # Clean stop path: SIGINT first (both SIGINT and SIGTERM print stop line).
    # runStatusOnce can take several seconds (herdr RPCs); allow up to ~8s.
    sent_sig="SIGINT"
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
      sent_sig="SIGTERM"
      kill -TERM "$watch_pid" 2>/dev/null || true
      for i in $(seq 1 10); do
        if ! kill -0 "$watch_pid" 2>/dev/null; then
          break
        fi
        sleep 0.2
      done
      if kill -0 "$watch_pid" 2>/dev/null; then
        warn "status --watch still running after SIGTERM; sending SIGKILL"
        sent_sig="SIGKILL"
        kill -KILL "$watch_pid" 2>/dev/null || true
      fi
    fi
    wait "$watch_pid" 2>/dev/null || true
  fi

  local out
  out="$(cat "$out_file" 2>/dev/null || true)"
  rm -f "$out_file"

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

  # SIGINT and SIGTERM both print "Watch mode stopped."; only SIGKILL may miss it.
  if [[ "$out" == *"Watch mode stopped"* ]]; then
    ok "status --watch stopped cleanly (Watch mode stopped)"
  elif [[ "$sent_sig" == "SIGKILL" ]]; then
    warn "status --watch did not print 'Watch mode stopped' (SIGKILL after hang)"
  else
    # Soft fail for residual races (redirect buffering / go run wrapper).
    warn "status --watch did not print 'Watch mode stopped' after ${sent_sig}"
    info "output tail: ${out: -200}"
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

test_send_dry_run() {
  section "Test: send ${DEMO_SESSION} --cc --dry-run"
  log_case_context "$DEMO_SESSION"
  local out rc=0
  out="$(run_ntm send "$DEMO_SESSION" --cc "$TEST_MSG" --dry-run 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "send ${DEMO_SESSION} --cc --dry-run (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  # Human dry-run output lists targets; must not require/mention tmux install.
  if [[ "$out" == *"tmux is not installed"* || "$out" == *"install tmux"* ]]; then
    bad "send --dry-run appears to require tmux under herdr"
    info "output: ${out}"
    return 1
  fi
  if [[ "$out" == *"Dry Run"* || "$out" == *"Would send"* || "$out" == *"pane"* || "$out" == *"dry_run"* ]]; then
    ok "send ${DEMO_SESSION} --cc --dry-run previewed panes (no crash)"
  else
    # Exit 0 with empty matching panes is still a successful dry-run path.
    if [[ "$out" == *"No matching panes"* ]]; then
      ok "send --dry-run exited 0 with no matching panes"
    else
      bad "send --dry-run output missing dry-run/pane markers"
      info "output: ${out}"
      return 1
    fi
  fi
  info "send --dry-run output: ${out}"
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

test_create_and_session_mgmt() {
  section "Test: create + session list/stop/delete (${CREATE_SESSION}) (bd-gl28u.1.10)"
  track_session "$CREATE_SESSION"
  kill_session_best_effort "$CREATE_SESSION"

  # create is interactive about attach/dir; answer y for mkdir, n for attach prompts.
  local out rc=0
  out="$(printf 'y\nn\n' | run_ntm create "$CREATE_SESSION" --panes=2 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "create ${CREATE_SESSION} --panes=2 (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "create ${CREATE_SESSION} --panes=2 succeeded"
  info "create output: ${out}"
  log_case_context "$CREATE_SESSION"

  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* || "$out" == *"attaching to tmux"* ]]; then
    bad "create required/invoked tmux under herdr"
    info "output: ${out}"
  else
    ok "create did not require tmux under herdr"
  fi

  local labels
  labels="$(workspace_labels)"
  assert_contains "herdr workspace list includes ${CREATE_SESSION}" "$labels" "$CREATE_SESSION"

  local ws_id panes
  ws_id="$(workspace_ids_for_label "$CREATE_SESSION" | head -n1)"
  if [[ -n "$ws_id" ]]; then
    ok "workspace id for ${CREATE_SESSION}: ${ws_id}"
    panes="$(pane_count_for_workspace "$ws_id")"
    if [[ "${panes:-0}" -ge 2 ]]; then
      ok "create workspace ${ws_id} has ${panes} panes (>=2)"
    else
      bad "create workspace ${ws_id} pane count=${panes}, expected >=2"
    fi
  else
    bad "could not resolve workspace id for ${CREATE_SESSION}"
  fi

  # session list (live) should show the created session (same path as ntm list).
  rc=0
  out="$(run_ntm session list 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm session list (exit=${rc})"
    info "output: ${out}"
  else
    ok "ntm session list succeeded"
    assert_contains "session list includes ${CREATE_SESSION}" "$out" "$CREATE_SESSION"
  fi

  # Also confirm top-level list still sees it.
  rc=0
  out="$(run_ntm list 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "ntm list after create (exit=${rc})"
    info "output: ${out}"
  else
    assert_contains "ntm list includes created ${CREATE_SESSION}" "$out" "$CREATE_SESSION"
  fi

  # session stop closes workspace + drops registry binding (muxKillSession).
  rc=0
  out="$(run_ntm session stop "$CREATE_SESSION" --force 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "session stop ${CREATE_SESSION} --force (exit=${rc})"
    info "output: ${out}"
    kill_session_best_effort "$CREATE_SESSION"
    return 1
  fi
  ok "session stop ${CREATE_SESSION} --force succeeded"
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "session stop required tmux under herdr"
  else
    ok "session stop did not require tmux"
  fi

  labels="$(workspace_labels)"
  if [[ "$labels" == *"$CREATE_SESSION"* ]]; then
    sleep 0.5
    labels="$(workspace_labels)"
  fi
  if [[ "$labels" == *"$CREATE_SESSION"* ]]; then
    bad "workspace label ${CREATE_SESSION} still present after session stop"
    info "labels: ${labels}"
  else
    ok "workspace label ${CREATE_SESSION} gone after session stop"
  fi

  # Re-create and exercise session delete (same muxKillSession path).
  kill_session_best_effort "$CREATE_SESSION"
  rc=0
  out="$(printf 'y\nn\n' | run_ntm create "$CREATE_SESSION" --panes=1 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "re-create ${CREATE_SESSION} for session delete (exit=${rc})"
    info "output: ${out}"
    return 1
  fi
  ok "re-create ${CREATE_SESSION} for session delete"

  rc=0
  out="$(run_ntm session delete "$CREATE_SESSION" --force 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "session delete ${CREATE_SESSION} --force (exit=${rc})"
    info "output: ${out}"
    kill_session_best_effort "$CREATE_SESSION"
    return 1
  fi
  ok "session delete ${CREATE_SESSION} --force succeeded"
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "session delete required tmux under herdr"
  else
    ok "session delete did not require tmux"
  fi

  labels="$(workspace_labels)"
  if [[ "$labels" == *"$CREATE_SESSION"* ]]; then
    sleep 0.5
    labels="$(workspace_labels)"
  fi
  if [[ "$labels" == *"$CREATE_SESSION"* ]]; then
    bad "workspace label ${CREATE_SESSION} still present after session delete"
    info "labels: ${labels}"
  else
    ok "workspace label ${CREATE_SESSION} gone after session delete"
  fi

  local list_out
  list_out="$(run_ntm session list 2>&1 || true)"
  if [[ "$list_out" == *"$CREATE_SESSION"* ]]; then
    bad "session list still shows ${CREATE_SESSION} after delete"
    info "list: ${list_out}"
  else
    ok "session list no longer shows ${CREATE_SESSION}"
  fi
}

# ---------------------------------------------------------------------------
# Summary / FEATURES checklist
# ---------------------------------------------------------------------------

test_robot_tilde_batch() {
  # P1 robot ~ flags batch (p1-robot-tilde-batch): events/attention/monitor/interrupt/
  # probe/agent-health/bulk-assign/mail-check under NTM_BACKEND=herdr.
  section "Test: robot ~ flags batch (p1-robot-tilde-batch)"
  local ROBOT_SESSION="${ROBOT_SESSION:-test-robot-tilde}"
  track_session "$ROBOT_SESSION"
  kill_session_best_effort "$ROBOT_SESSION"

  local out rc=0
  out="$(printf 'y\n' | run_ntm spawn "$ROBOT_SESSION" --cc=1 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${ROBOT_SESSION} for robot ~ batch (exit=${rc})"
    info "output: ${out:0:500}"
    return 1
  fi
  ok "spawn ${ROBOT_SESSION} for robot ~ batch"
  log_case_context "$ROBOT_SESSION"
  sleep 1

  # --robot-events (feed; backend-agnostic)
  rc=0
  out="$(run_ntm --robot-events --since-cursor=0 --events-limit=5 2>&1)" || rc=$?
  if printf '%s' "$out" | jq -e 'type=="object"' >/dev/null 2>&1; then
    ok "robot-events returns JSON object (exit=${rc})"
  else
    bad "robot-events invalid JSON (exit=${rc})"
    info "output: ${out:0:400}"
  fi
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "robot-events required tmux under herdr"
  else
    ok "robot-events did not require tmux"
  fi

  # --robot-attention (short timeout → wake_reason timeout)
  rc=0
  out="$(run_ntm --robot-attention --attention-timeout=2s --attention-poll=100ms 2>&1)" || rc=$?
  if printf '%s' "$out" | jq -e 'type=="object"' >/dev/null 2>&1; then
    ok "robot-attention returns JSON object (exit=${rc})"
  else
    bad "robot-attention invalid JSON (exit=${rc})"
    info "output: ${out:0:400}"
  fi
  if [[ "$out" == *"tmux: command not found"* ]]; then
    bad "robot-attention required tmux under herdr"
  else
    ok "robot-attention did not require tmux"
  fi

  # --robot-monitor: start banner emits only AFTER backendGetPanes (can be
  # 10s+ under herdr). Wait generously for first JSON object before failing.
  local mon_file mon_pid
  mon_file="$(mktemp -t ntm-robot-monitor-XXXXXX)"
  set +e
  run_ntm --robot-monitor="$ROBOT_SESSION" --interval=5s >"$mon_file" 2>&1 &
  mon_pid=$!
  set -e
  local i mon_out=""
  for i in $(seq 1 60); do
    if [[ -s "$mon_file" ]]; then
      mon_out="$(cat "$mon_file" 2>/dev/null || true)"
      if [[ "$mon_out" == *'"success"'* || "$mon_out" == *'"session"'* || "$mon_out" == *'Monitor started'* ]]; then
        break
      fi
    fi
    sleep 0.5
  done
  kill -INT "$mon_pid" 2>/dev/null || true
  sleep 0.5
  kill -TERM "$mon_pid" 2>/dev/null || true
  sleep 0.2
  kill -KILL "$mon_pid" 2>/dev/null || true
  wait "$mon_pid" 2>/dev/null || true
  mon_out="$(cat "$mon_file" 2>/dev/null || true)"
  rm -f "$mon_file"
  # Monitor start banner is pretty-printed multi-line JSON; take first object.
  if printf '%s' "$mon_out" | jq -e 'if type=="array" then .[0] else . end | .session != null or .success == true' >/dev/null 2>&1 \
     || printf '%s' "$mon_out" | jq -s -e '.[0].session != null or .[0].success == true' >/dev/null 2>&1; then
    ok "robot-monitor emitted start JSON for ${ROBOT_SESSION}"
  else
    # Fallback: look for success/session keys in stream
    if [[ "$mon_out" == *'"success"'* && ("$mon_out" == *'"session"'* || "$mon_out" == *'Monitor started'*) ]]; then
      ok "robot-monitor emitted start JSON keys for ${ROBOT_SESSION}"
    else
      bad "robot-monitor missing start JSON"
      info "output preview: ${mon_out:0:300}"
    fi
  fi
  if [[ "$mon_out" == *"tmux: command not found"* || "$mon_out" == *"GetFirstWindow"* ]]; then
    bad "robot-monitor used tmux under herdr"
  else
    ok "robot-monitor did not require tmux"
  fi

  # --robot-interrupt dry-run then real (no-wait)
  rc=0
  out="$(run_ntm --robot-interrupt="$ROBOT_SESSION" --dry-run 2>&1)" || rc=$?
  if printf '%s' "$out" | jq -e '.dry_run == true or .success == true or .would_affect != null' >/dev/null 2>&1; then
    ok "robot-interrupt --dry-run JSON OK (exit=${rc})"
  else
    # still accept valid object with success
    if printf '%s' "$out" | jq -e 'type=="object"' >/dev/null 2>&1; then
      ok "robot-interrupt --dry-run returned JSON object (exit=${rc})"
    else
      bad "robot-interrupt --dry-run invalid JSON (exit=${rc})"
      info "output: ${out:0:400}"
    fi
  fi

  rc=0
  # Prefer --no-wait (canonical); --interrupt-no-wait still works but may print deprecation on stderr.
  out="$(run_ntm --robot-interrupt="$ROBOT_SESSION" --no-wait 2>&1)" || rc=$?
  # Strip any leading non-JSON lines (deprecation warnings) before jq.
  json_out="$(printf '%s\n' "$out" | sed -n '/^{/,$p')"
  if printf '%s' "$json_out" | jq -e 'type=="object"' >/dev/null 2>&1; then
    ok "robot-interrupt real (no-wait) JSON OK (exit=${rc})"
  elif [[ "$out" == *'"success"'* && "$out" == *'"interrupted"'* ]]; then
    ok "robot-interrupt real (no-wait) JSON keys present (exit=${rc})"
  else
    bad "robot-interrupt real invalid JSON (exit=${rc})"
    info "output: ${out:0:400}"
  fi
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"GetPaneActivity"* ]]; then
    bad "robot-interrupt required tmux under herdr"
  else
    ok "robot-interrupt did not require tmux"
  fi

  # --robot-probe (after pane.ID target fix)
  rc=0
  out="$(run_ntm --robot-probe="$ROBOT_SESSION" --probe-timeout=1000 2>&1)" || rc=$?
  if printf '%s' "$out" | jq -e 'type=="object"' >/dev/null 2>&1; then
    local probe_err
    probe_err="$(printf '%s' "$out" | jq -r '.error // .probes[0].error // empty' 2>/dev/null || true)"
    if [[ "$probe_err" == *"session:"*":"*"."* ]]; then
      bad "robot-probe still used session:win.pane target form"
      info "output: ${out:0:400}"
    else
      ok "robot-probe returned JSON (exit=${rc})"
    fi
  else
    bad "robot-probe invalid JSON (exit=${rc})"
    info "output: ${out:0:400}"
  fi
  if [[ "$out" == *"tmux: command not found"* ]]; then
    bad "robot-probe required tmux under herdr"
  else
    ok "robot-probe did not require tmux"
  fi

  # --robot-agent-health --no-caut
  rc=0
  out="$(run_ntm --robot-agent-health="$ROBOT_SESSION" --no-caut 2>&1)" || rc=$?
  if printf '%s' "$out" | jq -e 'type=="object"' >/dev/null 2>&1; then
    ok "robot-agent-health --no-caut JSON OK (exit=${rc})"
  else
    bad "robot-agent-health invalid JSON (exit=${rc})"
    info "output: ${out:0:400}"
  fi
  if [[ "$out" == *"tmux: command not found"* ]]; then
    bad "robot-agent-health required tmux under herdr"
  else
    ok "robot-agent-health did not require tmux"
  fi

  # --robot-bulk-assign --dry-run
  rc=0
  out="$(run_ntm --robot-bulk-assign="$ROBOT_SESSION" --dry-run 2>&1)" || rc=$?
  if printf '%s' "$out" | jq -e 'type=="object"' >/dev/null 2>&1; then
    ok "robot-bulk-assign --dry-run JSON OK (exit=${rc})"
  else
    bad "robot-bulk-assign invalid JSON (exit=${rc})"
    info "output: ${out:0:400}"
  fi
  if [[ "$out" == *"tmux: command not found"* ]]; then
    bad "robot-bulk-assign required tmux under herdr"
  else
    ok "robot-bulk-assign did not require tmux"
  fi

  # --robot-mail-check without --mail-project → invalid flag JSON
  rc=0
  out="$(run_ntm --robot-mail-check 2>&1)" || rc=$?
  if printf '%s' "$out" | jq -e 'type=="object"' >/dev/null 2>&1; then
    local mail_code
    mail_code="$(printf '%s' "$out" | jq -r '.error_code // .error // empty' 2>/dev/null || true)"
    if [[ "$rc" -ne 0 || -n "$mail_code" ]]; then
      ok "robot-mail-check without --mail-project fails loudly (exit=${rc} code=${mail_code})"
    else
      bad "robot-mail-check without --mail-project should be invalid"
      info "output: ${out:0:300}"
    fi
  else
    # may print non-json usage; still accept nonzero exit
    if [[ $rc -ne 0 ]]; then
      ok "robot-mail-check without --mail-project nonzero exit=${rc}"
    else
      bad "robot-mail-check without --mail-project unexpectedly succeeded"
      info "output: ${out:0:300}"
    fi
  fi

  kill_session_best_effort "$ROBOT_SESSION"
}

# Swarm lifecycle under herdr (bd-gl28u.6.1 / p1-swarm-e2e):
# dry-run JSON → live create → status → stop -y --force → --remote rejection.
# Requires swarm.enabled=true (written to an isolated NTM_CONFIG for this case).
# --remote is intentionally N/A on herdr (SSH tmux relay); tiled layout best-effort only.
test_swarm_lifecycle() {
  section "Test: swarm create/status/stop under herdr (bd-gl28u.6.1)"

  local SWARM_SESSION="${SWARM_SESSION:-cc_agents_1}"
  local scan_dir proj_dir swarm_cfg prev_ntm_config
  scan_dir="$(mktemp -d -t ntm-herdr-swarm-scan-XXXXXX)"
  proj_dir="${scan_dir}/swarmproj"
  mkdir -p "${proj_dir}/.beads" "${proj_dir}/.git"
  # BeadScanner uses `br list`; empty open count is fine — tier3 still allocates.
  : >"${proj_dir}/.beads/issues.jsonl"

  swarm_cfg="${TEST_HOME}/swarm-config.toml"
  cat >"$swarm_cfg" <<'EOF'
[swarm]
enabled = true
default_scan_dir = "/tmp"
sessions_per_type = 1
stagger_delay_ms = 50
tier1_threshold = 400
tier2_threshold = 100

[swarm.tier1_allocation]
cc = 1
cod = 0
gmi = 0

[swarm.tier2_allocation]
cc = 1
cod = 0
gmi = 0

[swarm.tier3_allocation]
cc = 1
cod = 0
gmi = 0
EOF

  prev_ntm_config="${NTM_CONFIG:-}"
  export NTM_CONFIG="$swarm_cfg"

  # Pre-clean any leftover swarm sessions from prior runs.
  run_ntm swarm stop -y --force >/dev/null 2>&1 || true
  kill_session_best_effort "$SWARM_SESSION"

  local out rc=0

  # --- dry-run JSON ---
  rc=0
  out="$(run_ntm swarm --scan-dir="$scan_dir" --sessions-per-type=1 --dry-run --json 2>/dev/null)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "swarm dry-run --json (exit=${rc})"
    info "output: ${out:0:400}"
  elif ! printf '%s' "$out" | jq -e '.dry_run == true and (.sessions|length) >= 1' >/dev/null 2>&1; then
    bad "swarm dry-run JSON missing dry_run/sessions"
    info "output: ${out:0:400}"
  else
    ok "swarm dry-run --json plan has sessions"
    local sname
    sname="$(printf '%s' "$out" | jq -r '.sessions[0].name // empty' 2>/dev/null || true)"
    if [[ -n "$sname" ]]; then
      SWARM_SESSION="$sname"
      ok "swarm dry-run session name: ${SWARM_SESSION}"
    fi
  fi
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "swarm dry-run required tmux under herdr"
  else
    ok "swarm dry-run did not require tmux"
  fi

  # --- negative: --remote rejected under herdr ---
  rc=0
  out="$(run_ntm swarm --scan-dir="$scan_dir" --sessions-per-type=1 --remote=user@host 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]] && [[ "$out" == *"not supported"* || "$out" == *"no SSH"* ]]; then
    ok "swarm --remote rejected under herdr (exit=${rc})"
  else
    bad "swarm --remote should error under herdr (exit=${rc})"
    info "output: ${out:0:400}"
  fi

  # --- live create ---
  track_session "$SWARM_SESSION"
  rc=0
  out="$(run_ntm swarm --scan-dir="$scan_dir" --sessions-per-type=1 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "swarm live create (exit=${rc})"
    info "output: ${out:0:600}"
    # still try cleanup
    run_ntm swarm stop -y --force >/dev/null 2>&1 || true
    kill_session_best_effort "$SWARM_SESSION"
    if [[ -n "$prev_ntm_config" ]]; then export NTM_CONFIG="$prev_ntm_config"; else unset NTM_CONFIG; fi
    rm -rf "$scan_dir"
    return 1
  fi
  ok "swarm live create succeeded"
  info "create output: ${out:0:400}"
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* || "$out" == *"attaching to tmux"* ]]; then
    bad "swarm live create required/invoked tmux under herdr"
  else
    ok "swarm live create did not require tmux"
  fi

  log_case_context "$SWARM_SESSION"

  # list / workspace must show *_agents_*
  rc=0
  out="$(run_ntm list 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "list after swarm create (exit=${rc})"
    info "output: ${out}"
  else
    assert_contains "list includes swarm session ${SWARM_SESSION}" "$out" "$SWARM_SESSION"
  fi

  local labels
  labels="$(workspace_labels)"
  assert_contains "herdr workspace list includes ${SWARM_SESSION}" "$labels" "$SWARM_SESSION"

  # --- status ---
  rc=0
  out="$(run_ntm swarm status 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "swarm status (exit=${rc})"
    info "output: ${out}"
  else
    ok "swarm status succeeded"
    assert_contains "swarm status mentions ${SWARM_SESSION}" "$out" "$SWARM_SESSION"
  fi
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "swarm status required tmux under herdr"
  else
    ok "swarm status did not require tmux"
  fi

  # --- stop -y --force ---
  rc=0
  out="$(run_ntm swarm stop -y --force 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "swarm stop -y --force (exit=${rc})"
    info "output: ${out}"
    kill_session_best_effort "$SWARM_SESSION"
  else
    ok "swarm stop -y --force succeeded"
    info "stop output: ${out:0:300}"
  fi
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "swarm stop required tmux under herdr"
  else
    ok "swarm stop did not require tmux"
  fi

  # session / workspace should disappear
  sleep 0.5
  out="$(run_ntm list 2>&1 || true)"
  if [[ "$out" == *"$SWARM_SESSION"* ]]; then
    sleep 0.5
    out="$(run_ntm list 2>&1 || true)"
  fi
  if [[ "$out" == *"$SWARM_SESSION"* ]]; then
    bad "list still shows ${SWARM_SESSION} after swarm stop"
    info "list: ${out}"
    kill_session_best_effort "$SWARM_SESSION"
  else
    ok "list no longer shows ${SWARM_SESSION}"
  fi

  labels="$(workspace_labels)"
  if [[ "$labels" == *"$SWARM_SESSION"* ]]; then
    sleep 0.5
    labels="$(workspace_labels)"
  fi
  if [[ "$labels" == *"$SWARM_SESSION"* ]]; then
    bad "workspace label ${SWARM_SESSION} still present after swarm stop"
    info "labels: ${labels}"
    kill_session_best_effort "$SWARM_SESSION"
  else
    ok "workspace label ${SWARM_SESSION} gone after swarm stop"
  fi

  if [[ -n "$prev_ntm_config" ]]; then
    export NTM_CONFIG="$prev_ntm_config"
  else
    unset NTM_CONFIG
  fi
  rm -rf "$scan_dir"
}


# spawn --worktrees: git worktree dirs + StartAgent Cwd isolation (p1-worktrees-e2e).
# Requires a real git project under NTM_PROJECTS_BASE so CreateForAgent can
# `git worktree add`. Asserts:
#   - .ntm/worktrees/<sess>/cc_{1,2} exist and are valid git worktrees
#   - herdr agent get / registry Cwd equals worktree paths (not project root)
#   - broken pre-existing non-worktree dir fails closed (no silent shared cwd)

ensure_git_project() {
  local proj="$1"
  mkdir -p "$proj"
  if [[ ! -d "$proj/.git" ]]; then
    git -C "$proj" init -b main >/dev/null 2>&1 || git -C "$proj" init >/dev/null
    git -C "$proj" config user.email "herdr-e2e@example.com"
    git -C "$proj" config user.name "herdr-e2e"
    if [[ ! -f "$proj/README.md" ]]; then
      printf '%s\n' "herdr worktrees e2e" >"$proj/README.md"
    fi
    git -C "$proj" add README.md >/dev/null 2>&1 || true
    # Commit only if there is something to commit (fresh or dirty).
    if ! git -C "$proj" rev-parse HEAD >/dev/null 2>&1; then
      git -C "$proj" commit -m "init" >/dev/null
    elif ! git -C "$proj" diff --cached --quiet 2>/dev/null || ! git -C "$proj" diff --quiet 2>/dev/null; then
      git -C "$proj" add -A >/dev/null 2>&1 || true
      git -C "$proj" commit -m "init" >/dev/null 2>&1 || true
    fi
  fi
  # Ensure at least one commit exists for worktree add -b.
  if ! git -C "$proj" rev-parse HEAD >/dev/null 2>&1; then
    printf '%s\n' "herdr worktrees e2e" >"$proj/README.md"
    git -C "$proj" add README.md
    git -C "$proj" -c user.email=herdr-e2e@example.com -c user.name=herdr-e2e commit -m "init" >/dev/null
  fi
}

agent_cwd_from_herdr() {
  local name="$1"
  # herdr agent get envelope: .result.agent.cwd (preferred) or flat .result.cwd
  herdr_json agent get "$name" \
    | jq -r '.result.agent.cwd // .result.cwd // empty' 2>/dev/null || true
}

registry_pane_cwds_for_session() {
  local session="$1"
  if [[ ! -f "$HERDCTL_HERDR_REGISTRY" ]]; then
    return 0
  fi
  jq -r --arg s "$session" '
    .sessions[$s].panes // []
    | .[]?
    | select((.type // .agent_type // "") != "user")
    | .cwd // empty
  ' "$HERDCTL_HERDR_REGISTRY" 2>/dev/null || true
}

canonical_path() {
  # Resolve macOS /var → /private/var and other symlinks for path compares.
  local p="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$p" 2>/dev/null && return 0
  fi
  # Fallback: pwd -P via subshell if path exists.
  if [[ -e "$p" ]]; then
    (cd "$p" 2>/dev/null && pwd -P) || printf '%s' "$p"
  else
    printf '%s' "$p"
  fi
}

test_spawn_worktrees() {
  section "Test: spawn --worktrees ${WT_SESSION} --cc=2 --no-user (p1-worktrees-e2e)"
  track_session "$WT_SESSION"
  track_session "$WT_NEG_SESSION"
  kill_session_best_effort "$WT_SESSION"
  kill_session_best_effort "$WT_NEG_SESSION"

  local proj="${NTM_PROJECTS_BASE}/${WT_SESSION}"
  ensure_git_project "$proj"
  if [[ ! -d "$proj/.git" ]]; then
    bad "git project missing at ${proj}"
    return 1
  fi
  ok "git project ready at ${proj}"

  local out rc=0
  out="$(printf 'y\n' | run_ntm spawn "$WT_SESSION" --cc=2 --worktrees --no-user 2>&1)" || rc=$?
  LAST_OUTPUT="$out"
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${WT_SESSION} --cc=2 --worktrees --no-user (exit=${rc})"
    info "output: ${out}"
    kill_session_best_effort "$WT_SESSION"
    return 1
  fi
  ok "spawn ${WT_SESSION} --cc=2 --worktrees --no-user succeeded"
  info "spawn output: ${out}"
  log_case_context "$WT_SESSION"

  # Worktree dirs exist and are valid git worktrees.
  local wt_root="${proj}/.ntm/worktrees/${WT_SESSION}"
  local a1="${wt_root}/cc_1"
  local a2="${wt_root}/cc_2"
  if [[ -d "$a1" && -d "$a2" ]]; then
    ok "worktree dirs exist: ${a1} and ${a2}"
  else
    bad "worktree dirs missing under ${wt_root}"
    info "listing: $(ls -la "${proj}/.ntm/worktrees" 2>&1 || true)"
    kill_session_best_effort "$WT_SESSION"
    return 1
  fi

  local agent
  for agent in cc_1 cc_2; do
    local wtp="${wt_root}/${agent}"
    if git -C "$wtp" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      ok "git recognizes worktree ${agent}"
    else
      bad "git does not recognize worktree ${agent} at ${wtp}"
    fi
    local branch
    branch="$(git -C "$wtp" branch --show-current 2>/dev/null || true)"
    assert_eq "worktree ${agent} branch" "$branch" "ntm/${WT_SESSION}/${agent}"
  done

  # herdr worktree list from primary repo should include both paths.
  local wt_list
  wt_list="$(git -C "$proj" worktree list 2>/dev/null || true)"
  assert_contains "primary repo worktree list has cc_1" "$wt_list" "cc_1"
  assert_contains "primary repo worktree list has cc_2" "$wt_list" "cc_2"

  # Agent names present.
  local agents
  agents="$(agent_names)"
  assert_contains "agent list has ${WT_SESSION}-cc_1" "$agents" "${WT_SESSION}-cc_1"
  assert_contains "agent list has ${WT_SESSION}-cc_2" "$agents" "${WT_SESSION}-cc_2"

  # herdr agent get Cwd equals worktree path (not project root).
  local proj_canon a1_canon a2_canon cwd1 cwd2
  proj_canon="$(canonical_path "$proj")"
  a1_canon="$(canonical_path "$a1")"
  a2_canon="$(canonical_path "$a2")"

  cwd1="$(agent_cwd_from_herdr "${WT_SESSION}-cc_1")"
  cwd2="$(agent_cwd_from_herdr "${WT_SESSION}-cc_2")"
  local cwd1_canon cwd2_canon
  cwd1_canon="$(canonical_path "${cwd1:-}")"
  cwd2_canon="$(canonical_path "${cwd2:-}")"

  if [[ -n "$cwd1" && "$cwd1_canon" == "$a1_canon" ]]; then
    ok "herdr agent get ${WT_SESSION}-cc_1 cwd is worktree path"
  else
    bad "herdr agent get ${WT_SESSION}-cc_1 cwd='${cwd1}' want='${a1}'"
  fi
  if [[ -n "$cwd2" && "$cwd2_canon" == "$a2_canon" ]]; then
    ok "herdr agent get ${WT_SESSION}-cc_2 cwd is worktree path"
  else
    bad "herdr agent get ${WT_SESSION}-cc_2 cwd='${cwd2}' want='${a2}'"
  fi
  if [[ -n "$cwd1" && "$cwd1_canon" == "$proj_canon" ]]; then
    bad "agent cc_1 cwd is project root (shared cwd leak)"
  fi
  if [[ -n "$cwd2" && "$cwd2_canon" == "$proj_canon" ]]; then
    bad "agent cc_2 cwd is project root (shared cwd leak)"
  fi

  # Registry pane Cwd should also point at worktrees for agent panes.
  local reg_cwds
  reg_cwds="$(registry_pane_cwds_for_session "$WT_SESSION" | tr '\n' ' ')"
  info "registry agent pane cwds: ${reg_cwds}"
  local reg_ok=0 reg_line
  while IFS= read -r reg_line; do
    [[ -z "$reg_line" ]] && continue
    local reg_canon
    reg_canon="$(canonical_path "$reg_line")"
    if [[ "$reg_canon" == "$a1_canon" || "$reg_canon" == "$a2_canon" ]]; then
      reg_ok=$((reg_ok + 1))
    elif [[ "$reg_canon" == "$proj_canon" ]]; then
      bad "registry pane cwd is project root (shared cwd): ${reg_line}"
    fi
  done < <(registry_pane_cwds_for_session "$WT_SESSION")
  if [[ "$reg_ok" -ge 2 ]]; then
    ok "registry has ${reg_ok} agent pane cwd(s) under worktrees"
  else
    # Soft if registry shape differs, but still report.
    bad "registry agent pane worktree cwd count=${reg_ok}, expected >=2"
  fi

  # Cleanup positive session before negative case.
  local kill_out kill_rc=0
  kill_out="$(run_ntm kill "$WT_SESSION" --force 2>&1)" || kill_rc=$?
  if [[ $kill_rc -ne 0 ]]; then
    bad "kill ${WT_SESSION} --force (exit=${kill_rc})"
    info "output: ${kill_out}"
    kill_session_best_effort "$WT_SESSION"
  else
    ok "kill ${WT_SESSION} --force succeeded"
  fi

  # ---- Negative: broken pre-existing non-worktree dir ----
  section "Test: spawn --worktrees fail-closed on stale non-worktree dir (${WT_NEG_SESSION})"
  local neg_proj="${NTM_PROJECTS_BASE}/${WT_NEG_SESSION}"
  ensure_git_project "$neg_proj"
  local stale="${neg_proj}/.ntm/worktrees/${WT_NEG_SESSION}/cc_1"
  mkdir -p "$stale"
  printf '%s\n' "not-a-worktree" >"${stale}/junk.txt"

  rc=0
  out="$(printf 'y\n' | run_ntm spawn "$WT_NEG_SESSION" --cc=2 --worktrees --no-user 2>&1)" || rc=$?
  LAST_OUTPUT="$out"
  if [[ $rc -ne 0 ]]; then
    ok "spawn ${WT_NEG_SESSION} --worktrees failed closed (exit=${rc})"
    if [[ "$out" == *"not a valid git worktree"* || "$out" == *"worktree"* || "$out" == *"refusing shared cwd"* ]]; then
      ok "error mentions invalid/missing worktree"
    else
      warn "failure message did not clearly mention worktree; still non-zero"
      info "output: ${out}"
    fi
  else
    bad "spawn ${WT_NEG_SESSION} --worktrees should fail on stale non-worktree dir"
    info "output: ${out}"
  fi

  # No agents should have been launched for the negative session with shared cwd.
  local neg_agents
  neg_agents="$(agent_names_for_session "$WT_NEG_SESSION" | tr '\n' ' ')"
  if [[ -z "${neg_agents// /}" ]]; then
    ok "no herdr agents launched for failed ${WT_NEG_SESSION} spawn"
  else
    # If any agents leaked, ensure none use project root as cwd.
    bad "agents present after failed worktree spawn: ${neg_agents}"
    local n
    for n in $(agent_names_for_session "$WT_NEG_SESSION"); do
      local nc
      nc="$(agent_cwd_from_herdr "$n")"
      info "leaked agent ${n} cwd=${nc}"
      if [[ "$(canonical_path "${nc:-}")" == "$(canonical_path "$neg_proj")" ]]; then
        bad "leaked agent ${n} uses shared project root cwd"
      fi
    done
  fi

  kill_session_best_effort "$WT_NEG_SESSION"
  # Best-effort prune leftover worktrees from positive case.
  if [[ -d "$proj/.git" ]]; then
    git -C "$proj" worktree prune >/dev/null 2>&1 || true
  fi
}


# ---------------------------------------------------------------------------
# spawn --assign: wait via agent_status; 0 beads → honest empty assign (bd-gl28u.1.5)
# ---------------------------------------------------------------------------

test_spawn_assign() {
  section "Test: spawn ${ASSIGN_SESSION} --cc=1 --no-user --assign (no beads)"
  track_session "$ASSIGN_SESSION"
  kill_session_best_effort "$ASSIGN_SESSION"

  # Isolated project dir without beads so assign reports 0 planned/dispatched.
  local proj="${NTM_PROJECTS_BASE}/${ASSIGN_SESSION}"
  mkdir -p "$proj"
  local out rc=0
  # --json so we can assert assign.summary; ready-timeout covers agent_status wait.
  out="$(cd "$proj" && printf 'y\n' | run_ntm spawn "$ASSIGN_SESSION" --cc=1 --no-user --assign --ready-timeout=60s --limit=1 --json 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${ASSIGN_SESSION} --assign (exit=${rc})"
    info "output: ${out}"
    kill_session_best_effort "$ASSIGN_SESSION"
    return
  fi
  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* || "$out" == *"failed to connect to tmux"* ]]; then
    bad "spawn --assign required tmux under herdr"
    info "output: ${out}"
    kill_session_best_effort "$ASSIGN_SESSION"
    return
  fi
  ok "spawn ${ASSIGN_SESSION} --assign succeeded (exit=0, no tmux errors)"
  log_case_context "$ASSIGN_SESSION"

  # Prefer python for JSON asserts when available; fallback to string checks.
  local idle_count assigned_count bead_count
  if command -v python3 >/dev/null 2>&1; then
    idle_count="$(python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); a=d.get("assign") or {}; s=a.get("summary") or {}; print(s.get("idle_agent_count", -1))' <<<"$out" 2>/dev/null || echo -1)"
    assigned_count="$(python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); a=d.get("assign") or {}; s=a.get("summary") or {}; print(s.get("assigned_count", -1))' <<<"$out" 2>/dev/null || echo -1)"
    bead_count="$(python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); a=d.get("assign") or {}; s=a.get("summary") or {}; print(s.get("actionable_count", -1))' <<<"$out" 2>/dev/null || echo -1)"
    if [[ "$assigned_count" == "0" ]]; then
      ok "assign.summary.assigned_count=0 (no beads / none dispatched)"
    else
      bad "assign.summary.assigned_count expected 0, got ${assigned_count}"
      info "output: ${out}"
    fi
    if [[ "$bead_count" == "0" ]]; then
      ok "assign.summary.actionable_count=0 (no beads in isolated project)"
    else
      # Soft: isolated dir may still see parent beads via br discovery; still require 0 assigned.
      info "actionable_count=${bead_count} (non-zero OK if beads discovered; assigned must stay 0 without crash)"
    fi
    if [[ "$idle_count" != "-1" && "$idle_count" -ge 1 ]]; then
      ok "assign.summary.idle_agent_count=${idle_count} (agent_status/wait path saw idle agent)"
    else
      # Soft under slow agent boot — spawn still succeeded with empty assign.
      info "idle_agent_count=${idle_count} (soft: agent may still be booting; no crash)"
    fi
  else
    if [[ "$out" == *'"assigned_count": 0'* || "$out" == *'"assigned_count":0'* ]]; then
      ok "assign JSON has assigned_count=0"
    else
      bad "assign JSON missing assigned_count=0"
      info "output: ${out}"
    fi
  fi

  kill_session_best_effort "$ASSIGN_SESSION"
  ok "spawn --assign smoke complete"
}



test_sessions_save_restore() {
  # p1-sessions-save-e2e: topology save → kill → restore under herdr.
  # Layout strings / multi-window geometry remain tmux-only; this case verifies
  # pane-count topology via CreateSession + SplitWindow.
  section "Test: sessions save → kill → restore topology (${SAVE_SESSION}) (p1-sessions-save-e2e)"
  track_session "$SAVE_SESSION"
  track_session "$RESTORED_SESSION"
  kill_session_best_effort "$SAVE_SESSION"
  kill_session_best_effort "$RESTORED_SESSION"
  run_ntm sessions delete "$SAVE_NAME" --force >/dev/null 2>&1 || true

  local out rc=0
  out="$(printf 'y\n' | run_ntm spawn "$SAVE_SESSION" --cc=1 --cod=1 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${SAVE_SESSION} --cc=1 --cod=1 for sessions save (exit=${rc})"
    info "output: ${out:0:500}"
    return 1
  fi
  ok "spawn ${SAVE_SESSION} --cc=1 --cod=1 for sessions save"
  log_case_context "$SAVE_SESSION"
  sleep 1

  local saved_panes=0 work_dir="" file_path=""
  rc=0
  out="$(run_ntm --json sessions save "$SAVE_SESSION" --name "$SAVE_NAME" --overwrite 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "sessions save ${SAVE_SESSION} (exit=${rc})"
    info "output: ${out:0:500}"
    kill_session_best_effort "$SAVE_SESSION"
    return 1
  fi
  ok "sessions save ${SAVE_SESSION} --name ${SAVE_NAME} --overwrite"

  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* || "$out" == *"EnsureInstalled"* ]]; then
    bad "sessions save required tmux under herdr"
  else
    ok "sessions save did not require tmux binary"
  fi

  if command -v jq >/dev/null 2>&1; then
    local success
    success="$(printf '%s\n' "$out" | jq -r 'if type=="object" then .success // empty else empty end' 2>/dev/null || true)"
    if [[ "$success" == "true" ]]; then
      ok "sessions save JSON success=true"
    else
      bad "sessions save JSON success!='true' (got='${success}')"
      info "output: ${out:0:400}"
    fi
    saved_panes="$(printf '%s\n' "$out" | jq -r 'if type=="object" then (.state.panes|length) else 0 end' 2>/dev/null || echo 0)"
    work_dir="$(printf '%s\n' "$out" | jq -r 'if type=="object" then (.state.cwd // .state.work_dir // empty) else empty end' 2>/dev/null || true)"
    file_path="$(printf '%s\n' "$out" | jq -r 'if type=="object" then (.file_path // empty) else empty end' 2>/dev/null || true)"
  fi

  if [[ "${saved_panes:-0}" -ge 2 ]]; then
    ok "sessions save captured panes=${saved_panes} (>=2 multi-pane)"
  else
    bad "sessions save pane count=${saved_panes}, expected >=2"
    info "output: ${out:0:400}"
  fi

  if [[ -n "$work_dir" && "$work_dir" != "null" ]]; then
    ok "sessions save work_dir set (${work_dir})"
  else
    bad "sessions save missing work_dir/cwd"
  fi

  if [[ -n "$file_path" && -f "$file_path" ]]; then
    ok "sessions save wrote file under sessions store (${file_path})"
  else
    local fallback="${HOME}/.ntm/sessions/${SAVE_NAME}.json"
    if [[ -f "$fallback" ]]; then
      file_path="$fallback"
      ok "sessions save file present at ${file_path}"
    else
      bad "sessions save file missing (file_path='${file_path}')"
    fi
  fi

  rc=0
  out="$(run_ntm --json sessions list 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "sessions list after save (exit=${rc})"
    info "output: ${out:0:300}"
  else
    assert_contains "sessions list includes ${SAVE_NAME}" "$out" "$SAVE_NAME"
  fi

  rc=0
  out="$(run_ntm --json sessions show "$SAVE_NAME" 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "sessions show ${SAVE_NAME} (exit=${rc})"
    info "output: ${out:0:300}"
  else
    ok "sessions show ${SAVE_NAME}"
    assert_contains "sessions show mentions saved source session" "$out" "$SAVE_SESSION"
  fi

  rc=0
  out="$(run_ntm kill "$SAVE_SESSION" --force 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "kill ${SAVE_SESSION} before restore (exit=${rc})"
    info "output: ${out:0:300}"
    kill_session_best_effort "$SAVE_SESSION"
  else
    ok "kill ${SAVE_SESSION} before restore"
  fi

  rc=0
  out="$(run_ntm --json sessions restore "$SAVE_NAME" --name "$RESTORED_SESSION" --force 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "sessions restore ${SAVE_NAME} → ${RESTORED_SESSION} (exit=${rc})"
    info "output: ${out:0:500}"
    return 1
  fi
  ok "sessions restore ${SAVE_NAME} --name ${RESTORED_SESSION} --force"

  if [[ "$out" == *"tmux: command not found"* || "$out" == *"tmux is not installed"* ]]; then
    bad "sessions restore required tmux under herdr"
  else
    ok "sessions restore did not require tmux binary"
  fi

  if command -v jq >/dev/null 2>&1; then
    local rsuccess
    rsuccess="$(printf '%s\n' "$out" | jq -r 'if type=="object" then .success // empty else empty end' 2>/dev/null || true)"
    if [[ "$rsuccess" == "true" ]]; then
      ok "sessions restore JSON success=true"
    else
      bad "sessions restore JSON success!='true' (got='${rsuccess}')"
    fi
  fi

  local list_out
  list_out="$(run_ntm list 2>&1 || true)"
  if [[ "$list_out" == *"$RESTORED_SESSION"* ]]; then
    ok "ntm list shows restored session ${RESTORED_SESSION}"
  else
    bad "ntm list missing restored session ${RESTORED_SESSION}"
    info "list: ${list_out}"
  fi

  local st_out st_rc=0
  st_out="$(run_ntm status "$RESTORED_SESSION" 2>&1)" || st_rc=$?
  if [[ $st_rc -ne 0 ]]; then
    bad "status ${RESTORED_SESSION} after restore (exit=${st_rc})"
    info "output: ${st_out:0:400}"
  else
    ok "status ${RESTORED_SESSION} after restore"
    local ws_id panes
    ws_id="$(workspace_ids_for_label "$RESTORED_SESSION" | head -n1)"
    panes="$(pane_count_for_workspace "$ws_id")"
    if [[ "${panes:-0}" -ge "${saved_panes:-2}" ]]; then
      ok "restored workspace pane count=${panes} (>= saved ${saved_panes})"
    else
      local status_panes
      status_panes="$(printf '%s\n' "$st_out" | sed -n 's/.*Panes:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1)"
      if [[ "${status_panes:-0}" -ge "${saved_panes:-2}" ]]; then
        ok "status Panes=${status_panes} (>= saved ${saved_panes})"
      elif [[ "${panes:-0}" -ge 2 || "${status_panes:-0}" -ge 2 ]]; then
        ok "restored topology pane count herdr=${panes} status=${status_panes:-?} (>=2)"
      else
        bad "restored pane count too low (herdr=${panes} status_panes=${status_panes:-?} saved=${saved_panes})"
        info "status: ${st_out:0:400}"
      fi
    fi
  fi

  local notmux_out notmux_rc=0
  notmux_out="$(PATH=/usr/bin:/bin run_ntm --json sessions list 2>&1)" || notmux_rc=$?
  if [[ $notmux_rc -ne 0 ]]; then
    bad "sessions list without tmux on PATH (exit=${notmux_rc})"
    info "output: ${notmux_out:0:300}"
  elif [[ "$notmux_out" == *"tmux: command not found"* || "$notmux_out" == *"tmux is not installed"* ]]; then
    bad "sessions list required tmux binary"
  else
    ok "sessions list works without tmux on PATH"
  fi

  kill_session_best_effort "$RESTORED_SESSION"
  kill_session_best_effort "$SAVE_SESSION"
  run_ntm sessions delete "$SAVE_NAME" --force >/dev/null 2>&1 || true
}


# ---------------------------------------------------------------------------
# spawn --profile-set: persona expansion + JSON persona fields (p1-profile-set-e2e)
# ---------------------------------------------------------------------------

test_spawn_profile_set() {
  section "Test: spawn --profile-set quick-impl + backend-team (p1-profile-set-e2e)"
  track_session "$PROFILE_SESSION"
  track_session "$PROFILE_BT_SESSION"
  kill_session_best_effort "$PROFILE_SESSION"
  kill_session_best_effort "$PROFILE_BT_SESSION"

  if ! command -v jq >/dev/null 2>&1; then
    bad "jq required for profile-set JSON assertions"
    return 1
  fi

  local out rc=0 json
  # quick-impl: 2 implementer claude agents. Herdr always has a root/user pane;
  # agent_counts.claude==2 is the profile-set contract.
  out="$(printf 'y\n' | run_ntm spawn "$PROFILE_SESSION" --profile-set quick-impl --no-user --json 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${PROFILE_SESSION} --profile-set quick-impl (exit=${rc})"
    info "output: ${out:0:600}"
    kill_session_best_effort "$PROFILE_SESSION"
    return 1
  fi
  ok "spawn ${PROFILE_SESSION} --profile-set quick-impl succeeded (exit=0)"
  log_case_context "$PROFILE_SESSION"

  json="$(printf '%s\n' "$out" | sed -n '/^{/,$p')"
  local profile_set claude_count agent_pane_count
  profile_set="$(printf '%s' "$json" | jq -r '.profile_set // empty' 2>/dev/null || true)"
  assert_eq "profile_set=quick-impl" "$profile_set" "quick-impl"

  claude_count="$(printf '%s' "$json" | jq -r '.agent_counts.claude // 0' 2>/dev/null || echo 0)"
  assert_eq "agent_counts.claude=2" "$claude_count" "2"

  agent_pane_count="$(printf '%s' "$json" | jq -r '[.panes[]? | select(.type=="claude")] | length' 2>/dev/null || echo 0)"
  assert_eq "claude panes length=2" "$agent_pane_count" "2"

  # Persona fields on agent panes (persona + persona_prompt_source).
  local missing_persona missing_prompt
  missing_persona="$(printf '%s' "$json" | jq -r '[.panes[]? | select(.type=="claude" and ((.persona//"")==""))] | length' 2>/dev/null || echo 99)"
  missing_prompt="$(printf '%s' "$json" | jq -r '[.panes[]? | select(.type=="claude" and ((.persona_prompt_source//"")==""))] | length' 2>/dev/null || echo 99)"
  if [[ "$missing_persona" == "0" ]]; then
    ok "all claude panes have persona field"
  else
    bad "claude panes missing persona (count=${missing_persona})"
    info "panes: $(printf '%s' "$json" | jq -c '[.panes[]? | {type,persona,title}]' 2>/dev/null || true)"
  fi
  if [[ "$missing_prompt" == "0" ]]; then
    ok "all claude panes have persona_prompt_source"
  else
    bad "claude panes missing persona_prompt_source (count=${missing_prompt})"
  fi

  # Persona should be implementer for quick-impl.
  local personas
  personas="$(printf '%s' "$json" | jq -r '[.panes[]? | select(.type=="claude") | .persona] | unique | join(",")' 2>/dev/null || true)"
  assert_eq "quick-impl personas=implementer" "$personas" "implementer"

  # Session-prefixed agent names.
  local agents
  agents="$(agent_names)"
  assert_contains "agent list has ${PROFILE_SESSION}-cc_1" "$agents" "${PROFILE_SESSION}-cc_1"
  assert_contains "agent list has ${PROFILE_SESSION}-cc_2" "$agents" "${PROFILE_SESSION}-cc_2"

  # Prompt files written for personas with system_prompt.
  local prompt_dir="${NTM_PROJECTS_BASE}/${PROFILE_SESSION}/.ntm/prompts"
  if [[ -f "${prompt_dir}/implementer.md" ]]; then
    ok "prompt file written: ${prompt_dir}/implementer.md"
  else
    bad "missing prompt file ${prompt_dir}/implementer.md"
  fi

  kill_session_best_effort "$PROFILE_SESSION"
  ok "quick-impl profile-set cleanup done"

  # Optional larger set: backend-team = architect, implementer×2, tester (4 claude).
  rc=0
  out="$(printf 'y\n' | run_ntm spawn "$PROFILE_BT_SESSION" --profile-set backend-team --no-user --json 2>&1)" || rc=$?
  if [[ $rc -ne 0 ]]; then
    bad "spawn ${PROFILE_BT_SESSION} --profile-set backend-team (exit=${rc})"
    info "output: ${out:0:600}"
    kill_session_best_effort "$PROFILE_BT_SESSION"
    return 1
  fi
  ok "spawn ${PROFILE_BT_SESSION} --profile-set backend-team succeeded (exit=0)"

  json="$(printf '%s\n' "$out" | sed -n '/^{/,$p')"
  profile_set="$(printf '%s' "$json" | jq -r '.profile_set // empty' 2>/dev/null || true)"
  assert_eq "profile_set=backend-team" "$profile_set" "backend-team"

  claude_count="$(printf '%s' "$json" | jq -r '.agent_counts.claude // 0' 2>/dev/null || echo 0)"
  assert_eq "backend-team agent_counts.claude=4" "$claude_count" "4"

  # Ordered personas: architect, implementer, implementer, tester
  local ordered
  ordered="$(printf '%s' "$json" | jq -r '[.panes[]? | select(.type=="claude") | .persona] | join(",")' 2>/dev/null || true)"
  assert_eq "backend-team ordered personas" "$ordered" "architect,implementer,implementer,tester"

  local titles
  titles="$(printf '%s' "$json" | jq -r '[.panes[]? | select(.type=="claude") | .title] | join(" ")' 2>/dev/null || true)"
  assert_contains "title has architect" "$titles" "architect"
  assert_contains "title has tester" "$titles" "tester"

  agents="$(agent_names)"
  assert_contains "agent list has ${PROFILE_BT_SESSION}-cc_1" "$agents" "${PROFILE_BT_SESSION}-cc_1"
  assert_contains "agent list has ${PROFILE_BT_SESSION}-cc_4" "$agents" "${PROFILE_BT_SESSION}-cc_4"

  prompt_dir="${NTM_PROJECTS_BASE}/${PROFILE_BT_SESSION}/.ntm/prompts"
  for p in architect implementer tester; do
    if [[ -f "${prompt_dir}/${p}.md" ]]; then
      ok "backend-team prompt file: ${p}.md"
    else
      bad "missing backend-team prompt ${prompt_dir}/${p}.md"
    fi
  done

  kill_session_best_effort "$PROFILE_BT_SESSION"
  ok "backend-team profile-set cleanup done"
}

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
  info "checklist: send --dry-run        (test_send_dry_run)"
  info "checklist: kill --force          (test_kill_demo)"
  info "checklist: multi-agent spawn     (test_multi_agent)"
  info "checklist: add --cc/--cod        (test_add_agents / bd-gl28u.1.2)"
  info "checklist: spawn --label         (test_spawn_label / bd-gl28u.1.4)"
  info "checklist: create + session list/stop/delete (test_create_and_session_mgmt / bd-gl28u.1.10)"
  info "checklist: checkpoint save/restore (test_checkpoint_save_restore / bd-gl28u.3.1)"
  info "checklist: timeline/history/handoff (test_persistence_disk_cmds / bd-gl28u.3.2)"
  info "checklist: sessions save/restore topology (test_sessions_save_restore / p1-sessions-save-e2e)"
  info "checklist: robot ~ flags batch (test_robot_tilde_batch / p1-robot-tilde-batch)"
  info "checklist: swarm create/status/stop (test_swarm_lifecycle / bd-gl28u.6.1)"
  info "checklist: spawn --worktrees      (test_spawn_worktrees / p1-worktrees-e2e)"
  info "checklist: spawn --assign        (test_spawn_assign / bd-gl28u.1.5)"
  info "checklist: spawn --profile-set  (test_spawn_profile_set / p1-profile-set-e2e)"
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
  info "sessions: demo=${DEMO_SESSION} multi=${MULTI_SESSION} add=${ADD_SESSION} label=${LABEL_SESSION} create=${CREATE_SESSION} cp=${CP_SESSION} wt=${WT_SESSION} assign=${ASSIGN_SESSION} profile=${PROFILE_SESSION} save=${SAVE_SESSION} restored=${RESTORED_SESSION}"
  check_prereqs
  seed_registry

  test_spawn_demo
  test_list
  test_status
  test_agent_ops
  test_status_watch
  test_attach
  test_send_dry_run
  test_send
  test_kill_demo
  test_multi_agent
  test_add_agents
  test_spawn_label
  test_create_and_session_mgmt
  test_checkpoint_save_restore
  test_persistence_disk_cmds
  test_sessions_save_restore
  test_robot_tilde_batch
  test_swarm_lifecycle
  test_spawn_worktrees
  test_spawn_assign
  test_spawn_profile_set

  print_features_checklist
  print_summary
}

main "$@"
