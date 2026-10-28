#!/usr/bin/env bash
# Build comp_<yyyymmdd_hhmmss>.exe (Windows) tu source tren Mac/Linux. Can cai Go: https://go.dev/dl
set -euo pipefail
cd "$(dirname "$0")"
out="comp_$(date +%Y%m%d_%H%M%S).exe"
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "$out" .
echo "Xong: $(pwd)/$out"
