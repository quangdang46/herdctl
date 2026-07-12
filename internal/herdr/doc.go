// Package herdr is a parallel backend for NTM-style orchestration on Herdr.
//
// Design goals:
//
//   - Do not modify internal/tmux. The tmux backend remains the default and the
//     source of truth until Herdr parity is proven feature-by-feature.
//   - Mirror the public shapes and method names used by internal/tmux so call
//     sites can later switch backends with minimal churn.
//   - Prefer Herdr CLI JSON output (same envelope the running server returns)
//     over raw socket framing for v1. Socket support can be layered later.
//
// Identity model:
//
//	NTM session name  →  Herdr workspace label (+ registry map to workspace_id)
//	NTM pane title    →  registry metadata (agent type/index/tags) keyed by pane_id
//	NTM pane id (%N)  →  Herdr pane_id (w1:p2)
//
// Unsupported tmux-only operations return ErrNotSupported instead of panicking
// or silently falling back to tmux.
package herdr
