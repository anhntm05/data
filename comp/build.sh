#!/usr/bin/env bash
# Build comp.exe (Windows) tu source tren Mac/Linux. Can cai Go: https://go.dev/dl
set -euo pipefail
cd "$(dirname "$0")"
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o comp.exe .
echo "Xong: $(pwd)/comp.exe"
