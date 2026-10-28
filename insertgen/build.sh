#!/usr/bin/env bash
# Build insertgen.exe (Windows) tu source tren Mac/Linux. Can cai Go: https://go.dev/dl
set -euo pipefail
cd "$(dirname "$0")"
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o insertgen.exe .
echo "Xong: $(pwd)/insertgen.exe"
