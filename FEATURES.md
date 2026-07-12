# Herdctl Feature Parity

Source of truth for migrating NTM features from tmux → Herdr backend.

**Legend:** ✓ = works · ~ = partial · ✗ = not yet · — = N/A (Herdr-native or tmux-agnostic)

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
| `spawn --profile-set` | ✓ | ✗ | persona profiles |
| `spawn --recipe` | ✓ | ✗ | |
| `spawn --label` | ✓ | ✗ | multi-session labels |
| `spawn --user` | ✓ | ✓ | root user pane |
| `spawn --no-user` | ✓ | ✓ | |
| `spawn --safety` | ✓ | ✓ | |
| `spawn --assign` | ✓ | ✗ | spawn+assign workflow |
| `spawn --worktrees` | ✓ | ✗ | git worktree isolation |
| `spawn --stagger` | ✓ | ~ | stagger delay via agent.start |
| `create <name>` | ✓ | ~ | simple session create (no agents) |
| `add <session> --cc=N` | ✓ | ✗ | add agents to running session |
| `list` | ✓ | ✓ | |
| `status <session>` | ✓ | ✗ | detailed agent/pane status |
| `attach <session>` | ✓ | ✗ | uses tmux attach; Herdr uses `herdr session attach` |
| `kill <session>` | ✓ | ✓ | muxKillSession / muxKillPane (bd-biel8) |
| `view <session>` | ✓ | ✗ | tile all panes |
| `zoom <session> <index>` | ✓ | ✓ | muxZoomPane → herdr.ZoomPane (bd-biel8) |
| `interrupt <session>` | ✓ | ✓ | muxSendInterrupt → herdr agent send `ctrl+c` (bd-biel8) |
| `wait <session>` | ✓ | ✓ | herdr-native `agent wait` for idle/working; poll+capture fallback (bd-wg3js) |
| `session list` | ✓ | ~ | via sessions subcommand |
| `session stop` | ✓ | ✗ | |
| `session delete` | ✓ | ✗ | |
| `sessions` (save mgmt) | ✓ | ✗ | managed session save/load |

## 2. Agent Management

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `send <session> --cc "..."` | ✓ | ✓ | |
| `send <session> --cod "..."` | ✓ | ✓ | routing via registry; delivery through mux (bd-titsm) |
| `send <session> --gmi "..."` | ✓ | ✓ | delivery through mux (bd-titsm) |
| `send <session> --all` | ✓ | ✓ | selector prefers parseable IDs; herdr `wN:pM` falls back to W.P (bd-titsm) |
| `send --pane N` | ✓ | ✓ | numeric / W.P selectors; herdr pane IDs not fed raw to ParsePaneSelector (bd-titsm) |
| `send --tag <tag>` | ✓ | ✗ | |
| `send --dry-run` | ✓ | ~ | |
| `send --template` | ✓ | ✗ | |
| `send --smart-route` | ✓ | ✗ | |
| `send --codex-goal` | ✓ | ✗ | deep Codex integration |
| `agent list` | ✓ | ✓ | via herdr agent list |
| `agent get` | ✓ | ~ | |
| `agent read` | ✓ | ~ | via herdr pane read |
| `agent send` | ✓ | ✓ | |
| `agent rename` | ✓ | ~ | herdr agent rename |
| `agent focus` | ✓ | ~ | |
| `agent wait` | ✓ | ✓ | herdr `agent.wait` / `wait agent-status` (bd-wg3js) |
| `agent explain` | ✓ | ✗ | herdr detection explain |
| `agents` (profiles) | ✓ | ✗ | |
| `scale <session>` | ✓ | ✗ | |
| `respawn <session>` | ✓ | ✗ | |
| `rotate <session>` | ✓ | ✗ | account rotation |
| `controller <session>` | ✓ | ✗ | dedicated controller pane |
| `replay` | ✓ | ✗ | |

## 3. Monitoring & Output

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `activity <session>` | ✓ | ✗ | |
| `health <session>` | ✓ | ✓ | muxEnsureInstalled + herdr pane read / agent status; classification still uses scrollback patterns (bd-wg3js) |
| `diff <session> <p1> <p2>` | ✓ | ✗ | |
| `extract <session>` | ✓ | ✗ | code block extraction |
| `grep <pattern>` | ✓ | ✗ | |
| `errors <session>` | ✓ | ✗ | |
| `logs <session>` | ✓ | ✗ | |
| `copy [session:pane]` | ✓ | ✗ | copy to clipboard |
| `save <session>` | ✓ | ✗ | save output to file |
| `get-all-session-text` | ✓ | ✗ | |
| `analytics` | ✓ | ✗ | |
| `metrics` | ✓ | ✗ | |
| `summary <session>` | ✓ | ✗ | activity summary |
| `capture-pane` | ✓ | ✓ | via herdr pane read (bd-wg3js) |
| `pane read` (visible) | ✓ | ✓ | via herdr pane read --source visible |
| `pane read` (recent) | ✓ | ✓ | via herdr pane read --source recent |

## 4. Work Triage & Assignment

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `work triage` | ✓ | ~ | br/bv bridge; no direct tmux I/O — mostly backend-agnostic (bd-gziyw) |
| `work next` | ✓ | ~ | same as work triage |
| `work search <q>` | ✓ | ~ | same as work triage |
| `work queue-dry` | ✓ | ~ | same as work triage |
| `work graph` | ✓ | ~ | dependency graph; br/bv only |
| `work impact <paths>` | ✓ | ~ | br/bv only |
| `work forecast` | ✓ | ~ | br/bv only |
| `work burndown` | ✓ | ~ | br/bv only |
| `work alerts` | ✓ | ~ | br/bv only |
| `work label-flow` | ✓ | ~ | br/bv only |
| `work label-health` | ✓ | ~ | br/bv only |
| `work commit-ready` | ✓ | ~ | br/bv only |
| `assign <session>` | ✓ | ✓ | mux-backed pane resolution + muxDispatchDeliverer (bd-gziyw) |
| `rebalance <session>` | ✓ | ~ | shares assign mux paths; not fully e2e-verified on herdr |
| `review-queue <session>` | ✓ | ~ | shares assign mux paths; not fully e2e-verified on herdr |
| `beads` (br delegation) | — | — | delegates to br CLI; backend-agnostic |
| `coordinator <session>` | ✓ | ~ | CLI uses muxEnsureInstalled; runtime pane I/O herdr-aware (bd-0j1mq) |

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
| `changes <session>` | ✓ | ✗ | file changes from git diff |
| `conflicts <session>` | ✓ | ✗ | agent file conflict detection |

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
| `preflight <prompt>` | ✓ | ~ | prompt analysis; not fully verified on herdr |
| `redact` | — | — | backend-agnostic redaction lib |

## 7. Persistence & Recovery

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `checkpoint save <session>` | ✓ | ~ | capturer uses muxGetPanes / mux capture (bd-dzdva); layout fidelity reduced on herdr |
| `checkpoint list` | ✓ | ~ | storage-only; backend-agnostic list |
| `checkpoint restore` | ✓ | ~ | restore uses muxCreateSession / muxCreatePane (bd-dzdva) |
| `timeline list` | ✓ | ✗ | |
| `timeline show` | ✓ | ✗ | |
| `timeline export` | ✓ | ✗ | |
| `rollback <session>` | ✓ | ✗ | |
| `restore <session> <cp>` | ✓ | ✗ | |
| `archive <name>` | ✓ | ✗ | |
| `history` | ✓ | ✗ | |
| `handoff` | ✓ | ✗ | session handoff |
| `resume <session>` | ✓ | ✗ | |
| `replay [index]` | ✓ | ✗ | |
| `audit list` | ✓ | ~ | tamper-evident audit lib is backend-agnostic; CLI not fully verified |

## 8. Pipeline & Workflows

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `pipeline run <file>` | ✓ | ~ | Execute path uses muxPasteKeys / muxCapture (bd-dzdva); CLI still has some tmux EnsureInstalled call sites |
| `pipeline list` | ✓ | ~ | storage-only |
| `pipeline status <run>` | ✓ | ~ | |
| `pipeline resume <run>` | ✓ | ~ | shares Execute mux path |
| `pipeline cancel <run>` | ✓ | ~ | |
| `workflows list` | ✓ | ✗ | |
| `recipes list/show` | ✓ | ✗ | |
| `template list/show` | ✓ | ✗ | |
| `session-templates` | ✓ | ✗ | |
| `profile` / `profiles` | ✓ | ✗ | |

## 9. API & Integration

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `serve` | ✓ | ✗ | NTM REST API server |
| `openapi generate` | ✓ | ✗ | |
| `config show/edit` | — | — | backend-agnostic config |
| `deps` | ✓ | ✗ | dependency check |
| `upgrade` | ✓ | ✗ | |
| `init` / `setup` | ✓ | ✗ | project init |
| `version` | — | ✓ | |
| `completion <shell>` | — | ✓ | |
| `shell <shell>` | — | ✓ | shell integration |

## 10. Plugin & UI

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `dashboard <session>` | ✓ | ✗ | NTM TUI dashboard |
| `palette <session>` | ✓ | ✗ | command palette |
| `overlay <session>` | ✓ | ✗ | dashboard overlay |
| `plugins list` | ✓ | ✗ | NTM plugin system |
| `bind` | ✓ | ✗ | keybinding setup |
| `tutorial` | ✓ | ✗ | |
| Herdr sidebar status | — | ✓ | native Herdr TUI |
| Herdr pane attach | — | ✓ | native Herdr UI |

## 11. Robot / AI Agent Surfaces

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `--robot-status` | ✓ | ✗ | returns JSON with tmux state |
| `--robot-snapshot` | ✓ | ✗ | |
| `--robot-send` | ✓ | ✓ | uses send path (mux delivery) |
| `--robot-spawn` | ✓ | ~ | spawn path partial; agent-type argv wired |
| `--robot-help` | ✓ | ✗ | |
| `--robot-capabilities` | ✓ | ✗ | |
| `--robot-ack` | ✓ | ✗ | |
| `--robot-assign` | ✓ | ~ | assign path now mux-backed; robot surface not fully verified |
| `--robot-alerts` | ✓ | ✗ | |
| `--robot-mail-check` | ✓ | ✗ | |
| `--robot-bead-create` | ✓ | ✗ | |
| `--robot-pipeline-run` | ✓ | ✗ | |
| ~30 more robot flags | ✓ | ✗ | all need wiring |

## 12. Swarm & Ensemble

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `swarm` | ✓ | ✗ | weighted multi-project swarm |
| `ensemble [question]` | ✓ | ✗ | multi-model reasoning |
| `modes` | ✓ | ✗ | reasoning modes |
| `explain <mode>` | ✓ | ✗ | |
| `synthesize` | ✓ | ✗ | |

## 13. Git & IDE

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `git` | ✓ | ✗ | git coordination |
| `worktrees` / `worktree` | ✓ | ✓ | herdr has native worktree support |
| `repo` | ✓ | ✗ | |
| `hooks` | ✓ | ✗ | git hooks for quality |

## 14. Memory & Search

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `cass` | ✓ | ✗ | CASS session search |
| `memory` | ✓ | ✗ | CASS Memory (cm) |
| `search <query>` | ✓ | ✗ | CASS search |
| `context` | — | — | context packs |

## 15. Code Quality

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `bugs` | ✓ | ✗ | UBS integration |
| `scan [path]` | ✓ | ✗ | UBS scan |
| `scrub` | ✓ | ✗ | secret scanning |
| `doctor` | ✓ | ✗ | ecosystem health |

## Summary

Herdr-column cells only (`—` excluded from totals). Counts regenerated from the tables above.

| Area | Total | tmux ✓ | herdr ✓ | herdr ~ | herdr ✗ |
|---|---|---|---|---|---|
| Core Session Lifecycle | 32 | 32 | 17 | 3 | 12 |
| Agent Management | 24 | 24 | 8 | 5 | 11 |
| Monitoring & Output | 16 | 16 | 4 | 0 | 12 |
| Work Triage & Assign | 16 | 16 | 1 | 15 | 0 |
| Coordination (Mail) | 16 | 16 | 14 | 0 | 2 |
| Safety & Policy | 12 | 12 | 11 | 1 | 0 |
| Persistence & Recovery | 14 | 14 | 0 | 4 | 10 |
| Pipeline & Workflows | 10 | 10 | 0 | 5 | 5 |
| API & Integration | 8 | 5 | 3 | 0 | 5 |
| Plugin & UI | 8 | 6 | 2 | 0 | 6 |
| Robot Surfaces (listed) | 12 | 12 | 1 | 2 | 9 |
| Swarm & Ensemble | 5 | 5 | 0 | 0 | 5 |
| Git & IDE | 4 | 4 | 1 | 0 | 3 |
| Memory & Search | 3 | 3 | 0 | 0 | 3 |
| Code Quality | 4 | 4 | 0 | 0 | 4 |
| **Total (counted rows)** | **184** | **179** | **62** | **35** | **87** |
| N/A (`—`) rows | 4 | — | — | — | — |
| Robot bulk (`~30 more`) | ~30 | ~30 | 0 | 0 | ~30 |
| **Grand total (incl. robot bulk)** | **~214** | **~209** | **62** | **35** | **~117** |

**Current herdr parity: ~53%** of counted rows (97/184 at least partially working: 62 ✓ + 35 ~).  
Including robot bulk: **~45%** (97/214).  
Previous baseline was ~23% (48/210).  
Goal: 100% before deprecating tmux backend.

### Recent parity lifts (this pass)

| Bead | Area | Effect |
|---|---|---|
| bd-g5flx | spawn argv | cod/gmi/oc/agy/cursor/windsurf/aider/ollama → ✓ |
| bd-lqxdc | agent names | session-prefixed names; documented on spawn rows |
| bd-titsm | send selectors | herdr `wN:pM` → W.P fallback; mux delivery → more send ✓ |
| bd-biel8 | lifecycle | kill/interrupt/zoom → ✓ |
| bd-wg3js | monitoring | wait/health/capture → ✓ |
| bd-gziyw | work/assign | assign → ✓; work/* → ~ (agnostic br/bv) |
| bd-0j1mq | mail/coordinator | mail/message/locks/coordinator → ✓ |
| bd-dzdva | checkpoint/pipeline | checkpoint + pipeline core → ~ |
| bd-hkrjm | safety/policy | safety/policy/approve/guards → ✓ (verified agnostic) |
