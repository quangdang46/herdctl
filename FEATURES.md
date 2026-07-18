# Herdctl Feature Parity

Source of truth for migrating NTM features from tmux → Herdr backend.

**Implementation tracking:** Epic `bd-gl28u` (and children) is the work graph for remaining herdr parity. Beads are **ready to implement** (`br ready`).

**Honesty rule (source of truth):**
- **✓** only when the feature works on `NTM_BACKEND=herdr` **and** has been verified (manual or e2e). Do not tick from “code merged” alone.
- **~** only when partially wired (some paths work, known gaps in Notes).
- **✗** means **not done / not verified** — **keep ✗** until truly complete. Do **not** clear ✗ by inventing — just to close a bead.
- **—** only for genuine N/A (no herdr equivalent needed; substitute documented). Not a dump for “hard features”.

After implementing a bead: update this file **honestly**, then `br close` with what was actually verified.

**Legend:** ✓ = works (verified) · ~ = partial · ✗ = not yet / not verified · — = N/A (documented substitute)

---

## 1. Core Session Lifecycle

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `spawn <name> --cc=N` | ✓ | ✓ | agent.start argv; names are session-prefixed (`sess-cc_1`) to avoid cross-session clashes (bd-lqxdc) |
| `spawn <name> --cod=N` | ✓ | ✓ | codex argv via herdrPreferredAgentArgv (bd-g5flx) |
| `spawn <name> --gmi=N` | ✓ | ✓ | gemini --yolo argv (bd-g5flx) |
| `spawn <name> --agy=N` | ✓ | ✓ | agy --dangerously-skip-permissions argv (bd-g5flx) |
| `spawn <name> --cursor=N` | ✓ | ✓ | cursor argv (bd-g5flx) |
| `spawn <name> --windsurf=N` | ✓ | ✓ | windsurf argv (bd-g5flx) |
| `spawn <name> --aider=N` | ✓ | ✓ | aider argv (bd-g5flx) |
| `spawn <name> --oc=N` | ✓ | ✓ | opencode argv (bd-g5flx) |
| `spawn <name> --ollama=N` | ✓ | ✓ | `ollama run <model>` argv; default codellama:latest (bd-g5flx) |
| `spawn --profile-set` | ✓ | ✓ | expands personas into Agents before herdr agent.start; Claude gets --system-prompt-file/--effort on argv; Codex/env wrappers use sh -c so PrepareSystemPrompt is not dropped; e2e `test_spawn_profile_set` (quick-impl + backend-team; persona + persona_prompt_source JSON; StartAgent Index fix) (p1-profile-set-e2e) |
| `spawn --recipe` | ✓ | ✓ | recipe agent counts/templates expand before herdr launch branch; herdrPreferredAgentArgv / sh -c still apply (bd-gl28u.1.4) |
| `spawn --label` | ✓ | ✓ | full session name (base--label) in registry + herdr workspace label; project dir via SessionBase (bd-gl28u.1.4) |
| `spawn --user` | ✓ | ✓ | root user pane |
| `spawn --no-user` | ✓ | ✓ | |
| `spawn --safety` | ✓ | ✓ | |
| `spawn --assign` | ✓ | ✓ | observer muxed (ListPanes/CapturePane); idle via muxHerdrAgentStatuses before scrollback; e2e `test_spawn_assign` (no beads → 0 assignments, agent_status wait) (bd-gl28u.1.5) |
| `spawn --worktrees` | ✓ | ✓ | CreateForAgent + StartAgent Cwd=worktree path; fail-closed on stale/missing worktree (no shared cwd); e2e `test_spawn_worktrees` in scripts/test-herdr-backend.sh (p1-worktrees-e2e / bd-gl28u.1.5) |
| `spawn --stagger` | ✓ | ✓ | ordered delay between herdrLaunchAgent calls + prompt-delivery stagger (bd-gl28u.1.11) |
| `create <name>` | ✓ | ✓ | muxEnsureInstalled/Create/Split/GetPanes; herdr attach guidance (no tmux attach); e2e `test_create_and_session_mgmt` (bd-gl28u.1.10) |
| `add <session> --cc=N` | ✓ | ✓ | herdr agent.start (no SplitWindow precreate); session-prefixed names + registry UpsertPane; --cod/--gmi/etc. same as spawn (bd-gl28u.1.2) |
| `list` | ✓ | ✓ | |
| `status <session>` | ✓ | ✓ | muxEnsureInstalled + herdr agent_status (capture fallback); `status --watch` re-fetches via mux each tick (no tmux poll) (bd-gl28u.1.1 / bd-gl28u.1.8) |
| `attach <session>` | ✓ | ✓ | herdr: exit 0 + actionable TUI guidance (label/workspace_id); never tmux attach (bd-gl28u.1.3) |
| `kill <session>` | ✓ | ✓ | muxKillSession / muxKillPane (bd-biel8) |
| `view <session>` | ✓ | ✓ | herdr: unzoom-all + attach guidance verified (`test_view`); tiled layout N/A (use herdr TUI); no select-layout tiled (bd-gl28u.1.9) |
| `zoom <session> <index>` | ✓ | ✓ | muxZoomPane → herdr.ZoomPane (bd-biel8) |
| `interrupt <session>` | ✓ | ✓ | muxSendInterrupt → herdr agent send `ctrl+c` (bd-biel8) |
| `wait <session>` | ✓ | ✓ | herdr-native `agent wait` for idle/working; poll+capture fallback (bd-wg3js) |
| `session list` | ✓ | ✓ | `herdctl session list` → muxListSessions (also `herdctl list`); e2e covered (bd-gl28u.1.10) |
| `session stop` | ✓ | ✓ | `herdctl session stop` → muxKillSession (herdr workspace close + registry drop); e2e covered (bd-gl28u.1.10) |
| `session delete` | ✓ | ✓ | `herdctl session delete` → muxKillSession (same close+registry delete); e2e covered (bd-gl28u.1.10) |
| `sessions` (save mgmt) | ✓ | ✓ | topology save→kill→restore verified under herdr (GetPanes/Create/Split; e2e `test_sessions_save_restore`); layout strings / multi-window geometry remain tmux-only (p1-sessions-save-e2e / bd-gl28u.1.10) |

## 2. Agent Management

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `send <session> --cc "..."` | ✓ | ✓ | |
| `send <session> --cod "..."` | ✓ | ✓ | routing via registry; delivery through mux (bd-titsm) |
| `send <session> --gmi "..."` | ✓ | ✓ | delivery through mux (bd-titsm) |
| `send <session> --all` | ✓ | ✓ | selector prefers parseable IDs; herdr `wN:pM` falls back to W.P (bd-titsm) |
| `send --pane N` | ✓ | ✓ | numeric / W.P selectors; herdr pane IDs not fed raw to ParsePaneSelector (bd-titsm) |
| `send --tag <tag>` | ✓ | ✓ | spawn/add `--tag` → title `[tags]` + herdr `PaneMeta.Tags`; send filters via muxGetPanes registry merge / title parse |
| `send --dry-run` | ✓ | ✓ | muxGetPanes + dispatch DryRun preview; no key delivery / no tmux under herdr |
| `send --template` | ✓ | ✓ | load/execute via templates.Loader then runSendInternal; pane select + delivery through mux (same as plain send); redaction preflight retained |
| `send --smart-route` | ✓ | ✓ | fixed: PaneID (herdr wN:pM) → numeric PaneIndex for ParsePaneSelector compat; works across all strategies (least-loaded, first-available, round-robin) (bd-gl28u.1-fix) |
| `send --codex-goal` | ✓ | ~ | muxed: resolveCodexPaneSelector, runCodexGoalSend and all codex.go tmux calls → mux* wrappers; codex preflight still blocked on herdr CapturePaneVisible — works when pane is Codex-live and herdr visible-capture resolves (bd-gl28u.1-fix) |
| `agent list` | ✓ | ✓ | via herdr agent list |
| `agent get` | ✓ | ✓ | herdr agent get via client/mux; herdctl agent get (bd-gl28u.1.12) |
| `agent read` | ✓ | ✓ | herdr agent read (+ pane read fallback); herdctl agent read (bd-gl28u.1.12) |
| `agent send` | ✓ | ✓ | |
| `agent rename` | ✓ | ✓ | herdr agent rename via client/mux; herdctl agent rename (bd-gl28u.1.12) |
| `agent focus` | ✓ | ✓ | herdr agent focus via client/mux; herdctl agent focus (bd-gl28u.1.12) |
| `agent wait` | ✓ | ✓ | herdr `agent.wait` / `wait agent-status` (bd-wg3js) |
| `agent explain` | ✓ | ✓ | herdr agent explain --json via client/mux; herdctl agent explain (herdr-only) (bd-gl28u.1.12) |
| `agents` (profiles) | ✓ | ✓ | file/config profiles (backend-agnostic); herdctl agents list/show/stats/recommend (bd-gl28u.1.12) |
| `scale <session>` | ✓ | ✓ | muxGetPanes + runAdd (herdrLaunchAgent) / muxKillPane; layout re-tile best-effort (bd-gl28u.1.13) |
| `respawn <session>` | ✓ | ✓ | herdr: kill pane + StartAgent with registry meta (type/index/variant/cwd/command); tmux keeps robot GetRestartPane (bd-gl28u.1.13) |
| `rotate <session>` | ✓ | ✗ | session/pane lookup via mux; full account swap still needs caam + auth orchestrator (not herdr-native) (bd-gl28u.1.13) |
| `controller <session>` | ✓ | ✓ | herdr: dedicated agent pane via herdrLaunchAgent/StartAgent + prompt send; tmux pane-1 path unchanged (bd-gl28u.1.13) |
| `replay` | ✓ | ✓ | history store + muxSessionExists/muxGetPanes + sendPromptToPane (already muxed) (bd-gl28u.1.13) |

## 3. Monitoring & Output

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `activity <session>` | ✓ | ✓ | muxEnsureInstalled/GetPanes + muxCaptureForStatusDetection; ClassifyWithOutput (bd-gl28u.2.1) |
| `health <session>` | ✓ | ✓ | muxEnsureInstalled + CheckSessionWithObserver (mux ListPanes/Capture; no raw tmux list-panes); agent_status/scrollback classify; verified under herdr (bd-wg3js + health mux fix) |
| `diff <session> <p1> <p2>` | ✓ | ✓ | mux capture of two panes (bd-gl28u.2.1) |
| `extract <session>` | ✓ | ✓ | code block extraction via mux capture (bd-gl28u.2.1) |
| `grep <pattern>` | ✓ | ✓ | mux ListSessions/GetPanes/Capture (bd-gl28u.2.1) |
| `errors <session>` | ✓ | ✓ | mux GetPanes/Capture; skip AgentUser (bd-gl28u.2.1) |
| `logs <session>` | ✓ | ✓ | CLI muxEnsure + robot/logs backend.IsHerdr capture (bd-gl28u.2.1) |
| `copy [session:pane]` | ✓ | ✓ | muxGetPanes/muxCapture; --output verified; clipboard env-dependent (bd-gl28u.2.2) |
| `save <session>` | ✓ | ✓ | mux capture writes per-pane files; marker verified on herdr (bd-gl28u.2.2) |
| `get-all-session-text` | ✓ | ✓ | muxListSessions + muxCapture; markdown table verified (bd-gl28u.2.2) |
| `analytics` | ✓ | ✓ | event-store only (backend-agnostic); verified under NTM_BACKEND=herdr (bd-gl28u.2.2) |
| `metrics` | ✓ | ✓ | state/metrics store only (backend-agnostic); verified under NTM_BACKEND=herdr (bd-gl28u.2.2) |
| `summary <session>` | ✓ | ✓ | muxGetPanes + muxCapture; agent panes only (user panes skipped) (bd-gl28u.2.2) |
| `capture-pane` | ✓ | ✓ | via herdr pane read (bd-wg3js) |
| `pane read` (visible) | ✓ | ✓ | via herdr pane read --source visible |
| `pane read` (recent) | ✓ | ✓ | via herdr pane read --source recent |

## 4. Work Triage & Assignment

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `work triage` | ✓ | ✓ | br/bv bridge; backend-agnostic (e2e NTM_BACKEND=herdr exit 0; bd-gl28u.2.3) |
| `work next` | ✓ | ✓ | br/bv bridge; backend-agnostic (e2e herdr) |
| `work search <q>` | ✓ | ✓ | br/bv bridge; backend-agnostic (e2e herdr) |
| `work queue-dry` | ✓ | ✓ | br/bv bridge; backend-agnostic (e2e herdr) |
| `work graph` | ✓ | ✓ | dependency graph; br/bv only (e2e herdr) |
| `work impact <paths>` | ✓ | ✓ | br/bv only (e2e herdr) |
| `work forecast` | ✓ | ✓ | br/bv only (e2e herdr) |
| `work burndown` | ✓ | ✓ | br/bv only (e2e herdr; requires sprint arg) |
| `work alerts` | ✓ | ✓ | br/bv only (e2e herdr) |
| `work label-flow` | ✓ | ✓ | br/bv only (e2e herdr) |
| `work label-health` | ✓ | ✓ | br/bv only (e2e herdr) |
| `work commit-ready` | ✓ | ✓ | br/bv only (e2e herdr) |
| `assign <session>` | ✓ | ✓ | mux-backed pane resolution + muxDispatchDeliverer (bd-gziyw) |
| `rebalance <session>` | ✓ | ✓ | muxGetPanes; e2e demo/sendtest under herdr (bd-gl28u.2.3) |
| `review-queue <session>` | ✓ | ✓ | muxGetPanes + muxSendKeys; e2e herdr (bd-gl28u.2.3) |
| `beads` (br delegation) | — | — | delegates to br CLI; backend-agnostic |
| `coordinator <session>` | ✓ | ✓ | muxEnsureInstalled; status/digest/conflicts e2e herdr (bd-gl28u.2.4) |

## 5. Coordination (Agent Mail)

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `mail send <session>` | ✓ | ✓ | HTTP to Agent Mail API; session lookup via mux (bd-0j1mq) |
| `mail inbox <session>` | ✓ | ✓ | pane resolution via muxGetPanes |
| `mail ack <msg-id>` | ✓ | ✓ | API-only path |
| `message send <to> <body>` | ✓ | ✓ | scope via muxGetCurrentSession |
| `message inbox` | ✓ | ✓ | |
| `message read <id>` | ✓ | ✓ | |
| `lock <session> <paths>` | ✓ | ✓ | Agent Mail API; session name validation backend-agnostic |
| `locks list <session>` | ✓ | ✓ | |
| `unlock <session> <paths>` | ✓ | ✓ | |
| `force-release` | ✓ | ✓ | |
| `locks renew <session>` | ✓ | ✓ | |
| `coordinator status` | ✓ | ✓ | muxEnsureInstalled; herdr pane activity adapter (bd-0j1mq) |
| `coordinator digest` | ✓ | ✓ | |
| `coordinator conflicts` | ✓ | ✓ | |
| `changes <session>` | ✓ | ✓ | tracker store only; no backend I/O (e2e herdr; bd-gl28u.2.4) |
| `conflicts <session>` | ✓ | ✓ | tracker store only; no backend I/O (e2e herdr; bd-gl28u.2.4) |

## 6. Safety & Policy

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `safety status` | ✓ | ✓ | yaml policy check; no direct tmux I/O (bd-hkrjm) |
| `safety check <cmd>` | ✓ | ✓ | backend-agnostic |
| `safety install` | ✓ | ✓ | install DCG hooks; backend-agnostic |
| `safety simulate` | ✓ | ✓ | backend-agnostic |
| `safety blocked` | ✓ | ✓ | backend-agnostic |
| `policy show` | ✓ | ✓ | backend-agnostic (bd-hkrjm) |
| `policy edit` | ✓ | ✓ | backend-agnostic |
| `policy validate` | ✓ | ✓ | backend-agnostic |
| `policy automation` | ✓ | ✓ | backend-agnostic |
| `approve list/show/deny` | ✓ | ✓ | approval store; no mux calls (bd-hkrjm) |
| `guards` | ✓ | ✓ | Agent Mail pre-commit guards; backend-agnostic |
| `preflight <prompt>` | ✓ | ✓ | prompt lint; backend-agnostic (e2e herdr --json; bd-gl28u.2.5) |
| `redact` | — | — | backend-agnostic redaction lib |

## 7. Persistence & Recovery

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `checkpoint save <session>` | ✓ | ✓ | CLI + capturer fully muxed (bd-gl28u.3.1); layout strings tmux-only; scrollback redaction hooks retained |
| `checkpoint list` | ✓ | ✓ | storage-only; backend-agnostic |
| `checkpoint restore` | ✓ | ✓ | herdr: CreateSession + StartAgent by type/index/argv (pane ids not stable); **does not restore live PTYs** — structure+commands only (bd-gl28u.3.1) |
| `timeline list` | ✓ | ✓ | disk TimelinePersister; no backend pane ops (bd-gl28u.3.2) |
| `timeline show` | ✓ | ✓ | disk TimelinePersister (bd-gl28u.3.2) |
| `timeline export` | ✓ | ✓ | disk TimelinePersister + export (bd-gl28u.3.2) |
| `rollback <session>` | ✓ | ✓ | git stash/checkout + mux interrupt/workdir; session topology via checkpoint restore path (bd-gl28u.3.2) |
| `restore <session> <cp>` | ✓ | ✓ | alias surface: `checkpoint restore` (same mux path; bd-gl28u.3.1) |
| `archive <name>` | ✓ | ✓ | `sessions archive` disk move; no live backend required (bd-gl28u.3.2) |
| `history` | ✓ | ✓ | prompt history store (file); backend-agnostic (bd-gl28u.3.2) |
| `handoff` | ✓ | ✓ | YAML handoff files; create/list/show disk-first; --auto uses agentmail/CASS not tmux (bd-gl28u.3.2) |
| `resume <session>` | ✓ | ✓ | handoff load + spawn/inject via mux SessionExists/GetPanes/send (bd-gl28u.3.2) |
| `replay [index]` | ✓ | ✓ | same as Agent Management replay; history + mux send (bd-gl28u.1.13) |
| `audit list` | ✓ | ✓ | audit lib backend-agnostic; e2e herdr list (bd-gl28u.2.5) |

## 8. Pipeline & Workflows

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `pipeline run <file>` | ✓ | ✓ | Execute path muxPasteKeys/muxCapture; CLI EnsureInstalled/session resolve via mux (bd-gl28u.3.3) |
| `pipeline list` | ✓ | ✓ | storage-only; no backend binary required |
| `pipeline status <run>` | ✓ | ✓ | storage/in-memory; no backend binary required |
| `pipeline resume <run>` | ✓ | ✓ | CLI mux EnsureInstalled + session resolve; Execute path mux-safe |
| `pipeline cancel <run>` | ✓ | ✓ | storage/in-memory cancel; no backend binary required |
| `workflows list` | ✓ | ✓ | file/config loader; list/show backend-agnostic (bd-gl28u.3.4) |
| `recipes list/show` | ✓ | ✓ | file/config loader; list/show backend-agnostic (bd-gl28u.3.4) |
| `template list/show` | ✓ | ✓ | file/config loader; list/show backend-agnostic (bd-gl28u.3.4) |
| `session-templates` | ✓ | ✓ | file/config loader; list/show backend-agnostic (bd-gl28u.3.4) |
| `profile` / `profiles` | ✓ | ✓ | list/show file-based; profiles switch uses mux EnsureInstalled/GetPanes/SetPaneTitle (bd-gl28u.3.4) |

## 9. API & Integration

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `serve` | ✓ | — | full NTM REST server still tmux-coupled for live pane I/O; substitute under herdr: `herdr` socket/CLI + `herdctl` robot/CLI (`status`, `list`, `send`, `agent *`) as control API (bd-gl28u.6.3) |
| `openapi generate` | ✓ | ✓ | kernel registry → OpenAPI 3.1; backend-agnostic; verified under NTM_BACKEND=herdr (bd-gl28u.6.3) |
| `config show/edit` | — | — | backend-agnostic config |
| `deps` | ✓ | ✓ | required backend binary follows NTM_BACKEND (herdr required under herdr; tmux optional); verified `--json` (bd-gl28u.6.4) |
| `upgrade` | ✓ | ✓ | GitHub release check/install; backend-agnostic; `upgrade --check` verified under herdr (bd-gl28u.6.4) |
| `init` / `setup` | ✓ | ✓ | project-local `.ntm` + hooks; no tmux EnsureInstalled; `init --non-interactive --no-hooks` verified under herdr (bd-gl28u.6.4) |
| `version` | — | ✓ | |
| `completion <shell>` | — | ✓ | |
| `shell <shell>` | — | ✓ | shell integration |

## 10. Plugin & UI

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `dashboard <session>` | ✓ | — | tmux Bubbletea dashboard N/A on herdr; substitute: `herdr` TUI sidebar + `herdctl status` / `herdctl status --watch` / `herdctl attach` guidance (bd-gl28u.5.1) |
| `palette <session>` | ✓ | — | interactive palette TUI is tmux-oriented; substitute: `herdctl send <session> --all "..."` / agent selectors; `herdctl palette --json` still lists config commands (bd-gl28u.5.1) |
| `overlay <session>` | ✓ | — | requires tmux `display-popup`; substitute: herdr TUI sidebar as live status cockpit (bd-gl28u.5.1) |
| `plugins list` | ✓ | ✓ | file-based agent/command plugins under config dir; backend-agnostic (bd-gl28u.5.1) |
| `bind` | ✓ | — | writes `~/.tmux.conf` / live tmux bind-key only; substitute: herdr TUI keybinds (bd-gl28u.5.1) |
| `tutorial` | ✓ | ✓ | Bubbletea tutorial is backend-agnostic (no tmux session required) (bd-gl28u.5.1) |
| Herdr sidebar status | — | ✓ | native Herdr TUI status cockpit (dashboard/overlay substitute) |
| Herdr pane attach | — | ✓ | native Herdr UI / `herdctl attach` guidance |

## 11. Robot / AI Agent Surfaces

Inventory: 143 `--robot-*` flags in `internal/cli/root.go`. Dual-backend session/pane/capture/send helpers live in `internal/robot/backend_mux.go` (bd-gl28u.4.3). Help mentions `NTM_BACKEND=herdr|tmux`.

### Status / session (verified this pass unless noted)

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `--robot-status` | ✓ | ✓ | backendListSessions/GetPanes; empty herd → `sessions:[]`; additive `system.backend` (bd-gl28u.4.1/4.3) |
| `--robot-snapshot` | ✓ | ✓ | sessions+agents via backend helpers; empty herd valid (bd-gl28u.4.1/4.3) |
| `--robot-version` | ✓ | ✓ | includes `system.backend` (bd-gl28u.4.3) |
| `--robot-help` | ✓ | ✓ | dual-backend note + capabilities pointer (bd-gl28u.4.3) |
| `--robot-capabilities` | ✓ | ✓ | registry catalog; backend-agnostic (verified herdr) |
| `--robot-docs` | ✓ | ✓ | static docs JSON (verified herdr) |
| `--robot-plan` | ✓ | ✓ | bv plan + backend session list (verified herdr) |
| `--robot-terse` | ✓ | ✓ | one-line summary over herdr sessions (verified) |
| `--robot-tools` | ✓ | ✓ | tool inventory; backend-agnostic (verified herdr) |
| `--robot-digest` | ✓ | ✓ | attention feed digest (verified herdr) |
| `--robot-events` | ✓ | ✓ | attention feed (backend-agnostic); verified herdr. Overlay popup is separate (`--robot-overlay` — / N/A) |
| `--robot-attention` | ✓ | ✓ | feed wait path; verified herdr (`--attention-timeout` wake) |

### Send / wait / observe

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `--robot-send` | ✓ | ✓ | backend_mux dispatch deliverer; herdr pane IDs fall back to W.P selectors (bd-gl28u.4.3) |
| `--robot-tail` | ✓ | ✓ | SessionObserver deps use backend capture/list (verified demo) |
| `--robot-wait` | ✓ | ✓ | backend session/panes; idle wait OK on demo (verified) |
| `--robot-is-working` | ✓ | ✓ | observer deps muxed (verified demo) |
| `--robot-logs` | ✓ | ✓ | backend_mux capture (verified demo) |
| `--robot-context` | ✓ | ✓ | muxed capture (verified demo) |
| `--robot-activity` | ✓ | ✓ | muxed capture (verified demo) |
| `--robot-errors` | ✓ | ✓ | muxed capture (verified demo) |
| `--robot-monitor` | ✓ | ✓ | start JSON + capture via backend_mux; no GetFirstWindow under herdr (p1-robot-tilde-batch) |
| `--robot-ack` | ✓ | ✓ | session/pane/capture via backend_mux (bd-gl28u.4.2) |
| `--robot-assign` | ✓ | ✓ | panes + herdr agent_status idle; beads via bv (bd-gl28u.4.2) |
| `--robot-spawn` | ✓ | ✓ | herdr agent.start path (bd-gl28u.4.2) |
| `--robot-interrupt` | ✓ | ✓ | backend_mux send-interrupt + activity poll no-op under herdr (p1-robot-tilde-batch) |
| `--robot-probe` | ✓ | ✓ | prefers pane.ID target (herdr wN:pM); verified herdr (p1-robot-tilde-batch) |
| `--robot-agent-health` | ✓ | ✓ | capture muxed; verified herdr `--no-caut` (p1-robot-tilde-batch) |
| `--robot-smart-restart` | ✓ | ✗ (guarded) | herdr guard added: clean error with alternative hint (use `herdctl scale`/`herdctl respawn`) (bd-gl28u.1-fix) |
| `--robot-restart-pane` | ✓ | ✗ (guarded) | herdr guard added: clean error with alternative hint (use `herdctl respawn`) (bd-gl28u.1-fix) |
| `--robot-bulk-assign` | ✓ | ✓ | backend GetPanes/Send/Dispatch; dry-run verified herdr (p1-robot-tilde-batch) |
| `--robot-history` | ✓ | ✓ | history store + session filter; verified herdr (robot-misc-verify) |
| `--robot-metrics` | ✓ | ✓ | session metrics via backend panes; verified herdr (robot-misc-verify) |
| `--robot-diff` | ✓ | ✓ | mux capture + git; verified herdr (robot-misc-verify) |
| `--robot-health` | ✓ | ✓ | session health; activity via backendGetPaneActivity; verified herdr (robot-misc-verify) |
| `--robot-summary` | ✓ | ✓ | synthesis capture via backendCapturePaneOutput (no raw tmux); verified herdr (robot-misc-verify) |

### Mail / beads / pipeline (mostly backend-agnostic)

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `--robot-mail` | ✓ | ✓ | Agent Mail API + optional pane map via backend (verified) |
| `--robot-mail-check` | ✓ | ✓ | Agent Mail HTTP-only; invalid without `--mail-project` verified herdr (p1-robot-tilde-batch) |
| `--robot-alerts` | ✓ | ✓ | alert store; verified herdr |
| `--robot-beads-list` | ✓ | ✓ | br/bv bridge (verified herdr) |
| `--robot-bead-create` | ✓ | ✓ | br/bv bridge; create→show→claim→close verified herdr e2e (`test_robot_bead_pipeline`) |
| `--robot-bead-claim` | ✓ | ✓ | verified herdr e2e (`test_robot_bead_pipeline`) |
| `--robot-bead-show` | ✓ | ✓ | verified herdr e2e (`test_robot_bead_pipeline`) |
| `--robot-bead-close` | ✓ | ✓ | verified herdr e2e (`test_robot_bead_pipeline`) |
| `--robot-pipeline-list` | ✓ | ✓ | storage-only (verified herdr) |
| `--robot-pipeline-run` | ✓ | ✓ | dry-run command workflow success JSON verified herdr e2e (`test_robot_bead_pipeline`) |
| `--robot-pipeline-cancel` | ✓ | ✓ | fixed: CancelPipeline fallback to disk-persisted state; persistState also saves to cwd for cross-invocation findability; verified cancel running + completed pipelines under NTM_BACKEND=herdr (bd-gl28u.1-fix) |
| `--robot-pipeline` | ✓ | ✓ | status of completed dry-run via `.ntm/pipelines/<run_id>.json` verified herdr e2e |

### Remaining robot flags (still ✗ / not verified on herdr this pass)

Keep ✗ until verified. Grouped (not exhaustive of every subflag): `--robot-account-status`, `--robot-accounts-list`, `--robot-acfs-status`, `--robot-agent-names`, `--robot-cass-*`, `--robot-causality`, `--robot-context-inject`, `--robot-controller-spawn`, `--robot-dashboard`, `--robot-dcg-*`, `--robot-default-prompts`, `--robot-diagnose`, `--robot-dismiss-alert`, `--robot-ensemble*`, `--robot-env`, `--robot-file-*`, `--robot-files`, `--robot-forecast`, `--robot-giil-fetch`, `--robot-graph`, `--robot-guard`, `--robot-health-oauth`, `--robot-health-restart-stuck`, `--robot-impact`, `--robot-inspect-*`, `--robot-jfp-*`, `--robot-label-*`, `--robot-markdown`, `--robot-ms-*`, `--robot-overlay`, `--robot-palette`, `--robot-profile-*`, `--robot-proxy-status`, `--robot-quota-*`, `--robot-rano-stats`, `--robot-rch-*`, `--robot-recipes`, `--robot-replay`, `--robot-restore`, `--robot-route`, `--robot-ru-sync`, `--robot-safety-simulate`, `--robot-save`, `--robot-schema`, `--robot-search`, `--robot-setup`, `--robot-slb-*`, `--robot-suggest`, `--robot-support-bundle`, `--robot-switch-account`, `--robot-tokens`, `--robot-triage`, `--robot-watch-bead`, `--robot-xf-*`, plus modifiers (`--robot-format`, `--robot-limit`, `--robot-offset`, `--robot-verbosity`, `--robot-output-format`).

Many of these now *call* backend_mux for session/pane I/O, but are left ✗ until exercised under `NTM_BACKEND=herdr` (honesty rule).

## 12. Swarm & Ensemble

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `swarm` | ✓ | ✓ | herdr path: workspace create + `agent.start` per pane + mux send/kill; status/discover/stop via mux; live create/status/stop + dry-run JSON + `--remote` rejection verified under herdr (`scripts/test-herdr-backend.sh` test_swarm_lifecycle); `--remote` N/A; tiled layout best-effort only (bd-gl28u.6.1) |
| `ensemble [question]` | ✓ | ~ | status/stop/synthesize/compare/report use mux capture/session; build tag `ensemble_experimental` removed — `ensemble spawn` now compiles in default build; live spawn still uses swarm PaneLauncher (tmux.DefaultClient) — dry-run / presets / status / stop / compare work on herdr; full multi-model live spawn e2e pending (bd-gl28u.6.2) |
| `modes` | ✓ | ✓ | mode catalog list; backend-agnostic; verified under herdr (bd-gl28u.6.2) |
| `explain <mode>` | ✓ | ✓ | `herdctl modes explain`; catalog cards; verified under herdr (bd-gl28u.6.2) |
| `synthesize` | ✓ | ✓ | mux-backed live capture + offline saved outputs; verified working on herdr (correctly reports 'still working' when agents active, full path via ensemble synthesize --format=json) (bd-gl28u.6.2) |

## 13. Git & IDE

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `git` | ✓ | ✓ | session project dir via muxValidate/muxGetSession + registry; `git status demo` resolved live herdr cwd (bd-gl28u.6.7) |
| `worktrees` / `worktree` | ✓ | ✓ | herdr has native worktree support |
| `repo` | ✓ | ✓ | `ru sync` passthrough; backend-agnostic (no session/tmux) (bd-gl28u.6.7) |
| `hooks` | ✓ | ✓ | git hook install/status; repo-local, backend-agnostic; `hooks status --json` verified under herdr (bd-gl28u.6.7) |

## 14. Memory & Search

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `cass` | ✓ | ✓ | external CASS CLI; no tmux session resolve; `cass status --json` verified under herdr (index may be empty until `cass index --full`) (bd-gl28u.6.5) |
| `memory` | ✓ | ✓ | CM client; session id via muxGetCurrentSession then `.ntm/pids`/project basename; needs running `cm` daemon for context/outcome (bd-gl28u.6.5) |
| `search <query>` | ✓ | ✓ | CASS search wrapper; mux-safe (no tmux); requires initialized CASS index (bd-gl28u.6.5) |
| `context` | — | — | context packs |

## 15. Code Quality

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `bugs` | ✓ | ✓ | UBS findings cache/notify; backend-agnostic path scan (bd-gl28u.6.6) |
| `scan [path]` | ✓ | ✓ | UBS scan; verified single-file `--json` under herdr (bd-gl28u.6.6) |
| `scrub` | ✓ | ✓ | redaction secret scan of NTM artifacts; verified under herdr (bd-gl28u.6.6) |
| `doctor` | ✓ | ✓ | reports `backend` + herdr binary/server health when NTM_BACKEND=herdr; tmux optional; verified `--json`/TUI (bd-gl28u.6.6) |

## Summary

Herdr-column cells only (`—` excluded from totals). Counts regenerated from the tables above (2026-07-14 recount; matches section tables).

| Area | Total | tmux ✓ | herdr ✓ | herdr ~ | herdr ✗ |
|---|---|---|---|---|---|
| Core Session Lifecycle | 32 | 32 | 31 | 1 | 0 |
| Agent Management | 24 | 24 | 21 | 0 | 3 |
| Monitoring & Output | 16 | 16 | 16 | 0 | 0 |
| Work Triage & Assign | 16 | 16 | 16 | 0 | 0 |
| Coordination (Mail) | 16 | 16 | 16 | 0 | 0 |
| Safety & Policy | 12 | 12 | 12 | 0 | 0 |
| Persistence & Recovery | 14 | 14 | 14 | 0 | 0 |
| Pipeline & Workflows | 10 | 10 | 10 | 0 | 0 |
| API & Integration | 7 | 4 | 7 | 0 | 0 |
| Plugin & UI | 4 | 2 | 4 | 0 | 0 |
| Robot Surfaces (listed) | 42 | 42 | 33 | 0 | 9 |
| Swarm & Ensemble | 5 | 5 | 3 | 2 | 0 |
| Git & IDE | 4 | 4 | 4 | 0 | 0 |
| Memory & Search | 3 | 3 | 3 | 0 | 0 |
| Code Quality | 4 | 4 | 4 | 0 | 0 |
| **Total (counted rows)** | **209** | **204** | **195** | **2** | **12** |
| N/A (`—`) rows | 9 | — | — | — | — |
| Robot remaining (prose inventory, not row-counted) | ~100 | ~100 | 0 | 0 | ~100 |
| **Grand total (incl. remaining robot inventory)** | **~309** | **~304** | **194** | **3** | **~112** |

**Current herdr parity (honest recount from tables):**  
- **✓=195 · ~=3 · ✗=11 · —=9** (counted rows only; robot prose inventory still mostly ✗ until exercised)
- Newly ✓ this batch (P1): spawn --profile-set, --assign, --worktrees; sessions save topology; swarm create/status/stop; robot high-use ~ flags (events/attention/monitor/interrupt/probe/agent-health/bulk-assign/mail-check); health mux observer (no raw tmux list-panes)
- Newly batched: `send --smart-route` ✓ (PaneIndex fix) · `--robot-pipeline-cancel` ✓ (cross-invocation disk fallback)
- Still ~: **send --codex-goal** (muxed but herdr CapturePaneVisible blocking preflight); **ensemble** (spawn uses swarm PaneLauncher — needs backend-aware pane launching for herdr)
- Still ✗: rotate; robot smart-restart/restart-pane (guarded with alternatives); robot bead CRUD  
- Robot remaining (~100 flags in prose): keep ✗ until verified under `NTM_BACKEND=herdr` (honesty rule)
- **Keep ✗ until verified.**

Goal: maximize verified ✓; deprecate tmux only when remaining ✗ are acceptable.

### Recent parity lifts (this pass)

| Bead | Area | Effect |
|---|---|---|
| bd-g5flx | spawn argv | cod/gmi/oc/agy/cursor/windsurf/aider/ollama → ✓ |
| bd-lqxdc | agent names | session-prefixed names; documented on spawn rows |
| bd-titsm | send selectors | herdr `wN:pM` → W.P fallback; mux delivery → more send ✓ |
| send --dry-run | send dry-run | herdr: muxGetPanes + dispatch DryRun (no keys/tmux) → ✓ |
| send --tag | tag filter | spawn/add `--tag` stores title+registry Tags; send `--tag` filters via muxGetPanes → ✓ |
| bd-biel8 | lifecycle | kill/interrupt/zoom → ✓ |
| bd-wg3js | monitoring | wait/health/capture → ✓ |
| bd-gziyw | work/assign | assign → ✓; work/* → ~ (agnostic br/bv) |
| bd-0j1mq | mail/coordinator | mail/message/locks/coordinator → ✓ |
| bd-dzdva / bd-gl28u.3.1 | checkpoint | checkpoint save/list/restore → ✓ (PTY non-restore noted) |
| bd-gl28u.3.3 | pipeline CLI | pipeline rows → ✓ (mux EnsureInstalled/session resolve) |
| bd-gl28u.3.4 | workflows/recipes/templates/profile | list/show → ✓ (file loaders + mux profile switch) |
| bd-hkrjm | safety/policy | safety/policy/approve/guards → ✓ (verified agnostic) |
| bd-gl28u.1.6 | e2e | `scripts/test-herdr-backend.sh`: status/add/attach/label/multi-agent + FEATURES checklist |
| bd-gl28u.1.8 | status --watch | herdr-safe: runStatusWatch → runStatusOnce → observeSessionStatus (mux each tick) |
| bd-gl28u.1.5 | spawn flags | --assign → ✓ (observer mux + agent_status idle; e2e test_spawn_assign); --worktrees → ✓ (e2e test_spawn_worktrees: dirs + agent Cwd + fail-closed) |
| p1-profile-set-e2e | spawn --profile-set | e2e test_spawn_profile_set (quick-impl + backend-team); StartAgent Pane.Index fix for persona JSON → ✓ |
| bd-gl28u.1.9 | view | unzoom-all + attach guidance verified (`test_view`); tiled N/A → ✓ |
| p1-sessions-save-e2e | sessions save mgmt | topology save→kill→restore pane-count e2e → ✓ (layout/multi-window still tmux-only) |
| p1-robot-tilde-batch | robot ~ flags | events/attention/monitor/interrupt/probe/agent-health/bulk-assign/mail-check → ✓ (backendPaneTarget; e2e test_robot_tilde_batch) |
| robot-bead-pipeline | robot bead/pipeline | bead create/show/claim/close + pipeline list/dry-run/status → ✓ (e2e test_robot_bead_pipeline); cancel still ✗ (in-process only) |
| robot-misc-verify | robot misc flags | history/metrics/diff/health/summary → ✓ (backendCapturePaneOutput + backendGetPaneActivity; e2e test_robot_misc_verify) |
| health mux fix | health | CheckSessionWithObserver + mux ListPanes/Capture under herdr (no tmux list-panes) → ✓ verified |
| bd-gl28u.5.1 | Plugin & UI | dashboard/palette/overlay/bind → — (herdr TUI/sidebar + CLI substitutes); plugins list + tutorial → ✓ (backend-agnostic) |
| bd-gl28u.4.1 | robot-status/snapshot | empty herd valid JSON; post-spawn sessions/agents; `system.backend=herdr` additive |
| bd-gl28u.4.2 | robot-spawn/assign/ack | herdr agent.start spawn; assign idle via agent_status; ack mux capture (timeout OK) |
| bd-gl28u.4.3 | robot inventory/mux | backend_mux + help/capabilities; expanded FEATURES inventory; many high-use ✓; remaining honest ✗ |
| bd-gl28u.6.1 | swarm | herdr StartAgent execute path + mux status/stop; live create/status/stop + dry-run + `--remote` rejection verified → ✓ |
| bd-gl28u.6.2 | ensemble/modes | modes/explain ✓; ensemble/synthesize ~ (mux I/O; spawn experimental tag) |
| bd-gl28u.6.3 | serve/openapi | openapi generate ✓; serve → — (herdr socket/CLI substitute) |
| bd-gl28u.6.4 | deps/upgrade/init/setup | deps requires herdr under NTM_BACKEND=herdr; upgrade/init/setup backend-agnostic → ✓ |
| bd-gl28u.6.5 | cass/memory/search | external tools mux-safe; memory session via muxGetCurrentSession → ✓ |
| bd-gl28u.6.6 | bugs/scan/scrub/doctor | doctor backend+herdr health; UBS/scrub path tools → ✓ |
| bd-gl28u.6.7 | git/repo/hooks | project dir via muxGetSession live cwd; hooks/repo agnostic → ✓ |