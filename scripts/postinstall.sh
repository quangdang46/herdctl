#!/bin/sh
echo ""
echo "herdctl installed successfully!"
echo ""
echo "Add to your shell rc file:"
echo '  eval "$(herdctl shell bash)"  # for bash'
echo '  eval "$(herdctl shell zsh)"   # for zsh'
echo ""
echo "Compat: the `ntm` command remains available as a symlink."
echo "Run 'herdctl tutorial' to get started!"
echo ""
