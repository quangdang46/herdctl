---
name: ntm
description: >-
  Run NTM for multi-agent tmux orchestration, work triage, robot mode, safety,
  coordination, and local APIs. Use when spawning swarms, dispatching work, or
  operating `herdctl` as an agent or human operator.
---

<!-- TOC: Quick Start | Session Orchestration | Dispatch & Reusable Assets | Work Intelligence | Coordination | Safety | Robot Mode | Serve API | Project Resolution | References | Related Skills -->

# NTM — Named Tmux Manager

> **Core capability:** Turn `tmux` into a structured, recoverable multi-agent workspace.

> **Read the repo first.** If the target repository has `AGENTS.md` or `README.md`, read those before applying this skill. Repo-local instructions override generic NTM advice.

> **Interactive vs automation:**
> - `herdctl dashboard`, `herdctl palette`, and other TUI surfaces are for humans.
> - For machine-readable automation, prefer `--robot-*`.
> - Non-interactive CLI commands such as `herdctl send`, `herdctl work triage`, `herdctl locks list`, `herdctl pipeline status`, and `herdctl serve` are fine when they are the clearest tool.

> **Coordination and isolation:**
> - Agent Mail reservations are the default coordination primitive.
> - `--worktrees` and `herdctl worktrees ...` are supported isolation tools when the repo policy allows them.
> - If a repo `AGENTS.md` prefers reservations-only or has worktree-specific rules, follow that repo.

## Quick Start

```bash
# Install / sanity check
curl -fsSL "https://raw.githubusercontent.com/Dicklesworthstone/ntm/main/install.sh?$(date +%s)" | bash -s -- --easy-mode
herdctl deps -v

# Create or resolve a project
herdctl quick myproject --template=go

# Launch a mixed swarm
herdctl spawn myproject --cc=2 --cod=1 --agy=1

# Dispatch work
herdctl send myproject --cc "Map the auth layer and propose a refactor plan."

# Inspect the current work graph and system state
herdctl work triage --format=markdown
herdctl --robot-snapshot
```

## Session Orchestration

Use these for day-to-day session lifecycle management:

```bash
herdctl spawn myproject --cc=3 --cod=2 --agy=1
herdctl spawn myproject --label frontend --cc=2
herdctl spawn myproject --label backend --cc=2 --worktrees
herdctl add myproject --cc=1
herdctl add myproject --label frontend --cod=1
herdctl list
herdctl status myproject
herdctl view myproject
herdctl zoom myproject 3
herdctl attach myproject
herdctl dashboard myproject
herdctl palette myproject
```

Useful spawn patterns:

```bash
herdctl spawn myproject --prompt "Read AGENTS.md and start on ready work"
herdctl spawn myproject -r full-stack
herdctl spawn myproject -t red-green
herdctl spawn myproject --persona=architect --persona=implementer:2
herdctl spawn myproject --stagger-mode=smart --cc=6 --cod=4
```

## Dispatch and Reusable Assets

High-leverage NTM usage is not just `spawn` plus `send`. The real power shows up when
you combine richer dispatch patterns with reusable session and prompt assets.

```bash
herdctl send myproject --all "Checkpoint and summarize blockers."
herdctl send myproject --pane=2 "Own the auth migration."
herdctl send --project myproject "Sync to main and report conflicts."
herdctl send myproject -c internal/auth/service.go "Review this subsystem"
herdctl send myproject -t fix --var issue="nil pointer" --file internal/auth/service.go
herdctl send myproject --smart --route=affinity "Take the auth follow-up"
herdctl send myproject --distribute --dist-strategy=dependency

herdctl recipes list
herdctl recipes show full-stack
herdctl workflows list
herdctl workflows show red-green
herdctl template list
herdctl template show refactor
herdctl session-templates list
herdctl session-templates show refactor
```

User-level and project-level assets both matter. NTM can resolve configuration from
`~/.config/ntm/...` and project-local `.ntm/...` trees, so check the repo before
assuming defaults.

## Work Intelligence

NTM is no longer just a pane launcher. It has first-class work selection and assignment:

```bash
herdctl work triage
herdctl work triage --by-track
herdctl work alerts
herdctl work search "JWT auth"
herdctl work impact internal/api/auth.go
herdctl work next
herdctl work graph

herdctl assign myproject --auto --strategy=dependency
herdctl assign myproject --beads=br-123,br-124 --agent=codex
```

Use `herdctl work ...` when you want NTM to wrap `bv` and present work in operator-friendly form.
Use raw `bv --robot-*` when you specifically want the graph engine's native robot output.

## Coordination and Recovery

NTM now exposes the surrounding coordination stack directly:

```bash
herdctl mail send myproject --all "Report blockers and current file focus."
herdctl mail inbox myproject
herdctl locks list myproject --all-agents
herdctl locks renew myproject
herdctl locks force-release myproject 42 --note "agent inactive"
herdctl coordinator status myproject
herdctl coordinator digest myproject
herdctl coordinator conflicts myproject
herdctl checkpoint save myproject -m "before risky refactor"
herdctl checkpoint list myproject
herdctl checkpoint restore myproject
herdctl timeline list
herdctl timeline show <session-id>
herdctl history search "authentication error"
herdctl audit show myproject
herdctl changes conflicts myproject
herdctl resume myproject
```

Treat `herdctl checkpoint save` as a routine cadence, not just a pre-disaster snapshot. Good points to checkpoint: once prompts are confirmed received after a spawn or restore, after an investigation isolates a root cause, before risky edits, after significant uncommitted work but before verification, after a green verification, and before a merge/cleanup/handoff. Cheap checkpoints make any later `herdctl checkpoint restore` land on a known-good state.

Note that Agent Mail may run as an external MCP or service-manager process **outside** the tmux session. If Agent Mail and tmux appear to fail together, don't assume tmux took Agent Mail down — check the service manager, process/cgroup ancestry, and OOM/memory signals at the service boundary before relaunching workers.

Isolation options:

```bash
# Coordination-first
herdctl locks list myproject

# Isolation-first when policy allows it
herdctl spawn myproject --cc=3 --worktrees
herdctl worktrees list
herdctl worktrees merge claude_1
```

## Safety and Approvals

NTM has built-in safety, policy, and approval surfaces. Use them instead of ad hoc shell habits:

```bash
herdctl safety status
herdctl safety check -- git reset --hard
herdctl safety blocked --hours 24
herdctl safety install

herdctl policy show --all
herdctl policy validate
herdctl policy edit
herdctl policy automation

herdctl approve list
herdctl approve show abc123
herdctl approve abc123
herdctl approve deny abc123 --reason "wrong target branch"
```

If the repo instructions require offloading builds or tests through another tool such as `rch`, obey the repo instructions.

## Canonical Robot Mode

Start with these:

```bash
herdctl --robot-help
herdctl --robot-capabilities
herdctl --robot-status
herdctl --robot-snapshot
herdctl --robot-plan
herdctl --robot-dashboard
herdctl --robot-markdown --md-compact
herdctl --robot-terse
```

Common task-specific robot surfaces:

```bash
herdctl --robot-send=myproject --msg="Summarize blockers." --type=claude
herdctl --robot-ack=myproject --ack-timeout=30s
herdctl --robot-tail=myproject --lines=50
herdctl --robot-mail-check --mail-project=myproject --urgent-only
herdctl --robot-cass-search="authentication error"
herdctl --robot-beads-list --beads-status=open
herdctl --robot-bead-claim=br-123 --bead-assignee=agent1
herdctl --robot-bead-close=br-123 --bead-close-reason="Completed"
```

Operator loop:

```text
1. Bootstrap with --robot-snapshot
2. Tend with --robot-attention or --robot-wait
3. Act with --robot-send, herdctl send, herdctl assign, herdctl locks, or herdctl mail
4. Re-bootstrap with --robot-snapshot if the cursor expires
```

Prefer `--robot-*` when another agent or script needs structured output.

## Serve API and Pipeline Surfaces

NTM also exposes local API and durable workflow surfaces:

```bash
herdctl serve --port 7337
herdctl openapi generate
herdctl pipeline run .ntm/pipelines/review.yaml --session myproject
herdctl pipeline status run-20241230-123456-abcd
herdctl pipeline list
herdctl pipeline resume run-20241230-123456-abcd
herdctl pipeline cleanup --older=7d
```

Use `herdctl serve` for long-lived local integrations. Use `--robot-*` for single-shot agent control.

## Project Resolution

`herdctl spawn` needs a project directory that NTM can resolve.

```bash
herdctl config get projects_base
herdctl quick myproject --template=go

# Or point projects_base at an existing repo layout / create a symlink when needed
```

The session name usually matches the project directory name. Labels extend the session name as `project--label`.

## Reference Index

Read these when you need deeper detail without bloating the main skill body:

| Topic | Reference |
| --- | --- |
| High-leverage command patterns, output capture, monitoring, reusable assets | [COMMANDS.md](references/COMMANDS.md) |
| Attention feed, robot output formats, wait conditions, mail/cass/bead robot flows | [ROBOT-MODE.md](references/ROBOT-MODE.md) |
| Human dashboard, palette, keybindings, and TUI implementation notes | [DASHBOARD.md](references/DASHBOARD.md) |
| Project resolution, `projects_base`, config paths, and project-local assets | [CONFIG.md](references/CONFIG.md) |

## Related Skills

- `agent-mail` for inboxes, contact handshakes, and file reservations
- `br` for bead state changes and syncing
- `bv` for graph-aware task prioritization
- `cass` for prior-session retrieval
