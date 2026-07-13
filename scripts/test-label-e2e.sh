#!/usr/bin/env bash
# E2E smoke for labeled session workflows.
# Focuses on deterministic behavior: naming, shared project dirs, list visibility,
# robot dry-run labeling, and validation errors.

set -uo pipefail

PASS=0
FAIL=0
TOTAL=0
NTM_CMD=()

BASE="e2e-label-$$"
FRONT_SESSION="${BASE}--frontend"
BACK_SESSION="${BASE}--backend"
ROBOT_SESSION="${BASE}--api"
QUICK_BASE="${BASE}-quick"

LOGFILE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/test-label-e2e-$(date +%Y%m%d-%H%M%S).log"
CREATED_SESSIONS=()

ts() {
  date '+%Y-%m-%dT%H:%M:%S'
}

log() {
  local msg="[$(ts)] $*"
  echo "$msg" >> "$LOGFILE"
  echo "$msg" >&2
}

run_ntm() {
  "${NTM_CMD[@]}" "$@"
}

assert_eq() {
  local name="$1"
  local got="$2"
  local want="$3"
  TOTAL=$((TOTAL + 1))
  if [[ "$got" == "$want" ]]; then
    PASS=$((PASS + 1))
    log "[PASS] ${name}"
  else
    FAIL=$((FAIL + 1))
    log "[FAIL] ${name} (got='${got}' want='${want}')"
  fi
}

assert_nonempty() {
  local name="$1"
  local value="$2"
  TOTAL=$((TOTAL + 1))
  if [[ -n "$value" ]]; then
    PASS=$((PASS + 1))
    log "[PASS] ${name}"
  else
    FAIL=$((FAIL + 1))
    log "[FAIL] ${name} (value empty)"
  fi
}

assert_fails() {
  local name="$1"
  shift
  TOTAL=$((TOTAL + 1))
  if "$@" >/dev/null 2>&1; then
    FAIL=$((FAIL + 1))
    log "[FAIL] ${name} (expected non-zero exit)"
  else
    PASS=$((PASS + 1))
    log "[PASS] ${name}"
  fi
}

cleanup() {
  log "[INFO] cleanup starting"
  for session in "${CREATED_SESSIONS[@]}"; do
    tmux kill-session -t "$session" >/dev/null 2>&1 || true
  done
  log "[INFO] cleanup complete"
}

trap cleanup EXIT

preflight() {
  local missing=0
  for cmd in go tmux jq; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      log "[ERROR] missing dependency: ${cmd}"
      missing=1
    fi
  done
  NTM_CMD=("go" "run" "./cmd/herdctl")
  if [[ $missing -ne 0 ]]; then
    exit 1
  fi
  log "[INFO] preflight ok"
  log "[INFO] using command: ${NTM_CMD[*]}"
  log "[INFO] log file: ${LOGFILE}"
  log "[INFO] base project: ${BASE}"
}

test_create_labeled_sessions() {
  log "[INFO] test: create labeled frontend session"
  local out1
  out1=$(run_ntm --json create "$BASE" --label frontend --panes=1 2>&1)
  local rc1=$?
  if [[ $rc1 -ne 0 ]]; then
    assert_eq "create frontend command exits 0" "$rc1" "0"
    return
  fi
  CREATED_SESSIONS+=("$FRONT_SESSION")

  local session1
  session1=$(echo "$out1" | jq -r '.session // ""')
  local dir1
  dir1=$(echo "$out1" | jq -r '.working_directory // ""')
  assert_eq "create frontend session name" "$session1" "$FRONT_SESSION"
  assert_nonempty "create frontend working_directory present" "$dir1"

  log "[INFO] test: create labeled backend session"
  local out2
  out2=$(run_ntm --json create "$BASE" --label backend --panes=1 2>&1)
  local rc2=$?
  if [[ $rc2 -ne 0 ]]; then
    assert_eq "create backend command exits 0" "$rc2" "0"
    return
  fi
  CREATED_SESSIONS+=("$BACK_SESSION")

  local session2
  session2=$(echo "$out2" | jq -r '.session // ""')
  local dir2
  dir2=$(echo "$out2" | jq -r '.working_directory // ""')
  assert_eq "create backend session name" "$session2" "$BACK_SESSION"
  assert_nonempty "create backend working_directory present" "$dir2"
  assert_eq "frontend/backend share working_directory" "$dir1" "$dir2"

  if tmux has-session -t "$FRONT_SESSION" 2>/dev/null; then
    assert_eq "frontend tmux session exists" "yes" "yes"
  else
    assert_eq "frontend tmux session exists" "no" "yes"
  fi
  if tmux has-session -t "$BACK_SESSION" 2>/dev/null; then
    assert_eq "backend tmux session exists" "yes" "yes"
  else
    assert_eq "backend tmux session exists" "no" "yes"
  fi
}

test_list_visibility() {
  log "[INFO] test: labeled sessions visible in list --json"
  local list_json
  list_json=$(run_ntm list --json 2>/dev/null || true)
  local front_found
  front_found=$(echo "$list_json" | jq -r --arg s "$FRONT_SESSION" '[.sessions[]? | select(.name == $s)] | length')
  local back_found
  back_found=$(echo "$list_json" | jq -r --arg s "$BACK_SESSION" '[.sessions[]? | select(.name == $s)] | length')
  assert_eq "frontend appears in list --json" "$front_found" "1"
  assert_eq "backend appears in list --json" "$back_found" "1"
}

test_robot_spawn_label_dry_run() {
  log "[INFO] test: robot spawn dry-run labels session"
  local out
  out=$(run_ntm --robot-spawn="$BASE" --spawn-label=api --spawn-cc=1 --dry-run 2>&1)
  local rc=$?
  assert_eq "robot spawn dry-run exits 0" "$rc" "0"
  if [[ $rc -ne 0 ]]; then
    return
  fi
  local session
  session=$(echo "$out" | jq -r '.session // ""')
  local dry
  dry=$(echo "$out" | jq -r '.dry_run // false')
  assert_eq "robot dry-run session naming" "$session" "$ROBOT_SESSION"
  assert_eq "robot dry_run=true" "$dry" "true"
}

test_quick_label() {
  log "[INFO] test: quick --label returns labeled session"
  local out
  out=$(run_ntm --json quick "$QUICK_BASE" --label docs --no-git --no-vscode --no-claude 2>&1)
  local rc=$?
  assert_eq "quick --label exits 0" "$rc" "0"
  if [[ $rc -ne 0 ]]; then
    return
  fi
  local session
  session=$(echo "$out" | jq -r '.session // ""')
  local dir
  dir=$(echo "$out" | jq -r '.working_directory // ""')
  assert_eq "quick session naming" "$session" "${QUICK_BASE}--docs"
  assert_nonempty "quick working_directory present" "$dir"
}

test_validation_rejections() {
  log "[INFO] test: invalid labels and project names rejected"
  assert_fails "reject label with separator" run_ntm spawn "$BASE" --label "bad--label" --cc=1
  assert_fails "reject label with punctuation" run_ntm quick "$BASE" --label "bad!" --no-git --no-vscode --no-claude
  assert_fails "reject project with reserved separator (spawn)" run_ntm spawn "${BASE}--bad" --cc=1
  assert_fails "reject project with reserved separator (create)" run_ntm create "${BASE}--bad"
}

print_summary() {
  log "[INFO] ================================"
  log "[INFO] LABEL E2E SUMMARY"
  log "[INFO] TOTAL: ${TOTAL}"
  log "[INFO] PASS:  ${PASS}"
  log "[INFO] FAIL:  ${FAIL}"
  log "[INFO] LOG:   ${LOGFILE}"
  log "[INFO] ================================"
  if [[ $FAIL -gt 0 ]]; then
    log "[FAIL] label e2e checks failed"
    return 1
  fi
  log "[PASS] label e2e checks passed"
  return 0
}

main() {
  preflight
  test_create_labeled_sessions
  test_list_visibility
  test_robot_spawn_label_dry_run
  test_quick_label
  test_validation_rejections
  print_summary
}

main "$@"
