#!/usr/bin/env bash
# E2E smoke for NTM_BACKEND=herdr.
#
# Exercises the core session lifecycle against a live Herdr server:
#   spawn → list → send → kill
# plus multi-agent spawn (cc=2,cod=1) to verify unique agent names
# (agent_name_taken regression).
#
# Prerequisites:
#   - herdr binary on PATH (or HERDR_BIN_PATH)
#   - herdr server running (herdr status → server.status=running)
#   - go (if no local ./ntm binary) or a prebuilt ./ntm
#
# Usage (from repo root):
#   ./scripts/test-herdr-backend.sh
#   NTM_BIN=./ntm ./scripts/test-herdr-backend.sh
#
# Exit codes: 0 on full pass, non-zero on any failure.

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
TEST_MSG="${TEST_MSG:-echo herdr-test}"

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

  # Prefer a local binary built for this host.
  for candidate in "${ROOT_DIR}/ntm" "${ROOT_DIR}/cmd/ntm/ntm"; do
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
  ok "pre-cleaned leftover demo/multi workspaces (if any)"
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

test_send() {
  section "Test: send ${DEMO_SESSION} --cc '${TEST_MSG}'"
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

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

print_summary() {
  section "Summary"
  local total=$((PASS + FAIL + SKIP))
  info "passed=${PASS} failed=${FAIL} skipped=${SKIP} total=${total}"
  info "registry=${HERDCTL_HERDR_REGISTRY}"
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
  check_prereqs
  seed_registry

  test_spawn_demo
  test_list
  test_send
  test_kill_demo
  test_multi_agent

  print_summary
}

main "$@"
