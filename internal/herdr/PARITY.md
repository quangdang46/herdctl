# Herdr backend parity vs `internal/tmux`

`internal/herdr` is a **parallel** backend. `internal/tmux` is untouched and remains the default until this checklist is green under `backend=herdr` (wiring not landed yet).

Live reference: Herdr **0.7.3** CLI JSON envelopes (`{"id","result"}`).

## Identity mapping

| NTM / tmux | Herdr adapter |
|---|---|
| session name | workspace **label** + `registry.json` → `workspace_id` |
| pane title `sess__cc_1[tag]` | registry `PaneMeta` + `pane rename` label |
| pane id `%12` / `session:0.1` | Herdr `w1:p2` |
| window index | best-effort from `tab_id` (`w1:t2` → 2) |

Registry default path: `~/.ntm/herdr/registry.json`  
Override: `HERDCTL_HERDR_REGISTRY`

## Method parity

| API | Status | Herdr mapping |
|---|---|---|
| `IsInstalled` / `EnsureInstalled` | done | `HERDR_BIN_PATH` / `herdr` on PATH |
| `ValidateSessionName` | done | same rules as tmux |
| `SessionExists` | done | registry + `workspace list` |
| `ListSessions` | done | registry ∩ live workspaces |
| `CreateSession` / `WithHistoryLimit` | done | `workspace create --label --cwd --no-focus` (history ignored) |
| `KillSession` | done | `workspace close` + registry drop |
| `GetSession` | done | registry + panes |
| `GetPanes` / `GetPanesContext` | done | `pane list --workspace` + registry merge |
| `KillPane` | done | `pane close` |
| `SetPaneTitle` | done | registry + `pane rename` |
| `SplitWindow` | partial | `pane split` + list diff for new id |
| `StartAgent` | done (**herdr-only extra**) | `agent start --workspace --cwd -- argv` |
| `SendKeys` / `WithDelay` | done | `pane run` / `send-text` / `send-keys` |
| `SendKeysForAgent*` | done | delay defaults; no tmux buffer path |
| `AgentSend` | done (**herdr-only extra**) | `agent send` |
| `SendInterrupt` / `SendEOF` | done | `pane send-keys ctrl+c/d` |
| `CapturePaneOutput*` | done | `pane read --source recent` |
| `CapturePaneVisible` | done | `pane read --source visible` |
| `ZoomPane` | done | `pane zoom --on` |
| `DisplayMessage` | partial | `notification show` |
| `GetCurrentSession` | done | focused workspace → registry |
| `ApplyTiledLayout` | partial | best-effort `UnzoomAllPanes` then `ErrNotSupported` (no select-layout tiled) |
| `AttachOrSwitch` | stub | `ErrNotSupported` (Herdr client attach model) |
| `SetPaneBorderStyle*` | stub | `ErrNotSupported` |
| `PasteKeys` / `SendBuffer*` | not yet | use `SendKeys` / `pane run` |
| `SendKeysForAgentDoubleEnter` | not yet | |
| `GetPanesWithActivity*` | not yet | |
| `PaneStreamer` / pipe-pane | not yet | poll `pane read` / events later |
| `Client.Remote` SSH tmux | not yet | different Herdr remote model |
| `InTmux` | n/a | `InHerdr()` |

## Do not delete tmux until

- [ ] `spawn --cc=N --cod=M` path wired to `StartAgent` + works live
- [ ] `send --cc/--all` resolves registry panes
- [ ] `status` / `list` / capture health paths
- [ ] `kill` session/pane
- [ ] assign/work resolve Herdr pane ids
- [ ] full suite still green with tmux backend
- [ ] no production path requires raw tmux titles without registry

## Live test

```bash
HERDCTL_HERDR_LIVE=1 go test ./internal/herdr/ -run Live -v
```

## Wiring (future, not in this scaffold)

1. Keep calling `tmux.*` by default.
2. Add env/config `NTM_BACKEND=herdr|tmux` (name TBD).
3. Thin dispatch in spawn/send only — do not mass-replace call sites until core green.
