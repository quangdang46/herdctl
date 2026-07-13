package robot

// tmuxIsInstalledReal delegates to the active backend (tmux or herdr).
func tmuxIsInstalledReal() bool {
	return backendIsInstalled()
}

// tmuxSessionExistsReal delegates to the active backend.
func tmuxSessionExistsReal(name string) bool {
	return backendSessionExists(name)
}

// tmuxGetPanesReal lists panes via the active backend and converts to tmuxPaneInfo.
func tmuxGetPanesReal(session string) []tmuxPaneInfo {
	panes, err := backendGetPanes(session)
	if err != nil {
		return nil
	}
	result := make([]tmuxPaneInfo, len(panes))
	for i, p := range panes {
		result[i] = tmuxPaneInfo{Index: p.Index, Title: p.Title, Type: p.Type}
	}
	return result
}
