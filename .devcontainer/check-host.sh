#!/usr/bin/env bash
set -euo pipefail

# Check bind sources before Docker can create missing paths as directories.
# Never print or copy credential contents.
codex_binary="${HOME}/.codex/packages/standalone/current/bin/codex"
if [[ ! -x "$codex_binary" ]]; then
    printf 'Host Codex executable not found: %s\n' "$codex_binary" >&2
    exit 1
fi
"$codex_binary" --version >/dev/null

if [[ ! -f "${HOME}/.codex/auth.json" ]]; then
    printf 'Run codex login on the host before starting the container.\n' >&2
    exit 1
fi
if [[ ! -d "${HOME}/.config/gh" ]]; then
    printf 'Run gh auth login on the host before starting the container.\n' >&2
    exit 1
fi
