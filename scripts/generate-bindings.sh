#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
wails build -platform linux/amd64 -tags webkit2_41 -s
