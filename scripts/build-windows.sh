#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

export GOOS=windows
export GOARCH=amd64
export CGO_ENABLED=1
export CC=x86_64-w64-mingw32-gcc
export CXX=x86_64-w64-mingw32-g++

wails build -platform windows/amd64 -skipbindings -webview2 error -o asr_test_go.exe -ldflags "-X main.appVersion=${ASR_APP_VERSION:-0.0.1}"
if [[ ! -e build/bin/appsettings.json ]]; then
  cp appsettings.example.json build/bin/appsettings.json
fi
