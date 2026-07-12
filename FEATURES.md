# Herdctl Feature Parity

Source of truth for migrating NTM features from tmux → Herdr backend.

**Legend:** ✓ = works · ~ = partial · ✗ = not yet · — = N/A (Herdr-native or tmux-agnostic)

---

## 1. Core Session Lifecycle

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `spawn <name> --cc=N` | ✓ | ✓ | agent.start argv |
| `spawn <name> --cod=N` | ✓ | ~ | only --cc wired; need cod/gmi/oc/agy argv |
| `spawn <name> --gmi=N` | ✓ | ✗ | |
| `spawn <name> --agy=N` | ✓ | ✗ | |
| `spawn <name> --cursor=N` | ✓ | ✗ | |
| `spawn <name> --windsurf=N` | ✓ | ✗ | |
| `spawn <name> --aider=N` | ✓ | ✗ | |
| `spawn <name> --oc=N` | ✓ | ✗ | |
| `spawn <name> --ollama=N` | ✓ | ✗ | |
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
| `kill <session>` | ✓ | ✓ | with confirm prompt |
| `view <session>` | ✓ | ✗ | tile all panes |
| `zoom <session> <index>` | ✓ | ✗ | zoom pane |
| `interrupt <session>` | ✓ | ✗ | send Ctrl-C (herdr agent.send has `ctrl+c`) |
| `wait <session>` | ✓ | ✗ | wait for agent status (herdr `agent.wait` exists) |
| `session list` | ✓ | ~ | via sessions subcommand |
| `session stop` | ✓ | ✗ | |
| `session delete` | ✓ | ✗ | |
| `sessions` (save mgmt) | ✓ | ✗ | managed session save/load |

## 2. Agent Management

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `send <session> --cc "..."` | ✓ | ✓ | |
| `send <session> --cod "..."` | ✓ | ~ | routing via registry works; need test |
| `send <session> --gmi "..."` | ✓ | ~ | |
| `send <session> --all` | ✓ | ~ | |
| `send --pane N` | ✓ | ~ | selector via numeric W.P |
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
| `agent wait` | ✓ | ~ | herdr `agent.wait` |
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
| `health <session>` | ✓ | ~ | relies on capture-pane screen parsing |
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
| `capture-pane` | ✓ | ✓ | via herdr pane read |
| `pane read` (visible) | ✓ | ✓ | via herdr pane read --source visible |
| `pane read` (recent) | ✓ | ✓ | via herdr pane read --source recent |

## 4. Work Triage & Assignment

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `work triage` | ✓ | ✗ | bridges br/bv; backend-agnostic in theory |
| `work next` | ✓ | ✗ | |
| `work search <q>` | ✓ | ✗ | |
| `work queue-dry` | ✓ | ✗ | |
| `work graph` | ✓ | ✗ | dependency graph |
| `work impact <paths>` | ✓ | ✗ | |
| `work forecast` | ✓ | ✗ | |
| `work burndown` | ✓ | ✗ | |
| `work alerts` | ✓ | ✗ | |
| `work label-flow` | ✓ | ✗ | |
| `work label-health` | ✓ | ✗ | |
| `work commit-ready` | ✓ | ✗ | |
| `assign <session>` | ✓ | ✗ | bridges br/bv + tmux |
| `rebalance <session>` | ✓ | ✗ | |
| `review-queue <session>` | ✓ | ✗ | |
| `beads` (br delegation) | — | — | delegates to br CLI; backend-agnostic |
| `coordinator <session>` | ✓ | ✗ | |

## 5. Coordination (Agent Mail)

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `mail send <session>` | ✓ | ~ | HTTP to Agent Mail API; backend-agnostic |
| `mail inbox <session>` | ✓ | ~ | |
| `mail ack <msg-id>` | ✓ | ~ | |
| `message send <to> <body>` | ✓ | ~ | |
| `message inbox` | ✓ | ~ | |
| `message read <id>` | ✓ | ~ | |
| `lock <session> <paths>` | ✓ | ~ | |
| `locks list <session>` | ✓ | ~ | |
| `unlock <session> <paths>` | ✓ | ~ | |
| `force-release` | ✓ | ~ | |
| `locks renew <session>` | ✓ | ~ | |
| `coordinator status` | ✓ | ~ | |
| `coordinator digest` | ✓ | ~ | |
| `coordinator conflicts` | ✓ | ~ | |
| `changes <session>` | ✓ | ✗ | file changes from git diff |
| `conflicts <session>` | ✓ | ✗ | agent file conflict detection |

## 6. Safety & Policy

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `safety status` | ✓ | ✗ | yaml policy check |
| `safety check <cmd>` | ✓ | ✗ | |
| `safety install` | ✓ | ✗ | install DCG hooks |
| `safety simulate` | ✓ | ✗ | |
| `safety blocked` | ✓ | ✗ | |
| `policy show` | ✓ | ✗ | |
| `policy edit` | ✓ | ✗ | |
| `policy validate` | ✓ | ✗ | |
| `policy automation` | ✓ | ✗ | |
| `approve list/show/deny` | ✓ | ✗ | |
| `guards` | ✓ | ✗ | Agent Mail pre-commit guards |
| `preflight <prompt>` | ✓ | ✗ | |
| `redact` | — | — | backend-agnostic redaction lib |

## 7. Persistence & Recovery

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `checkpoint save <session>` | ✓ | ✗ | captures tmux pane state |
| `checkpoint list` | ✓ | ✗ | |
| `checkpoint restore` | ✓ | ✗ | |
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
| `audit list` | ✓ | ✗ | tamper-evident audit (backend-agnostic lib) |

## 8. Pipeline & Workflows

| Feature | tmux | herdr | Notes |
|---|---|---|---|
| `pipeline run <file>` | ✓ | ✗ | depends on tmux panes |
| `pipeline list` | ✓ | ✗ | |
| `pipeline status <run>` | ✓ | ✗ | |
| `pipeline resume <run>` | ✓ | ✗ | |
| `pipeline cancel <run>` | ✓ | ✗ | |
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
| `--robot-send` | ✓ | ✓ | uses send path |
| `--robot-spawn` | ✓ | ~ | |
| `--robot-help` | ✓ | ✗ | |
| `--robot-capabilities` | ✓ | ✗ | |
| `--robot-ack` | ✓ | ✗ | |
| `--robot-assign` | ✓ | ✗ | |
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

| Area | Total | tmux ✓ | herdr ✓ | herdr ~ | herdr ✗ |
|---|---|---|---|---|---|
| Core Session Lifecycle | 27 | 27 | 7 | 6 | 14 |
| Agent Management | 26 | 25 | 5 | 8 | 13 |
| Monitoring & Output | 15 | 15 | 3 | 2 | 10 |
| Work Triage & Assign | 18 | 17 | 0 | 0 | 17 |
| Coordination (Mail) | 14 | 14 | 0 | 10 | 4 |
| Safety & Policy | 14 | 13 | 0 | 0 | 13 |
| Persistence & Recovery | 16 | 16 | 0 | 0 | 16 |
| Pipeline & Workflows | 10 | 10 | 0 | 0 | 10 |
| API & Integration | 9 | 7 | 2 | 0 | 5 |
| Plugin & UI | 8 | 5 | 2 | 0 | 3 |
| Robot Surfaces | ~35 | 35 | 1 | 1 | 33 |
| Swarm & Ensemble | 6 | 6 | 0 | 0 | 6 |
| Git & IDE | 5 | 4 | 1 | 0 | 1 |
| Memory & Search | 4 | 3 | 0 | 0 | 3 |
| Code Quality | 4 | 4 | 0 | 0 | 4 |
| **Total** | **~210** | **196** | **21** | **27** | **162** |

**Current herdr parity: ~23%** (48/210 items at least partially working).  
Goal: 100% before deprecating tmux backend.
