#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if ! command -v docker >/dev/null 2>&1; then
  echo "请先安装 Docker。" >&2
  exit 1
fi
exec docker build --pull -t "${JUKU_IMAGE:-guoguo-juku:local}" --build-arg "GO_IMAGE=${JUKU_GO_IMAGE:-golang:1.26-bookworm}" --build-arg "RUNTIME_IMAGE=${JUKU_RUNTIME_IMAGE:-debian:bookworm-slim}" "$@" "$project_dir"
