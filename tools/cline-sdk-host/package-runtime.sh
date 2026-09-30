#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT=${1:-"$ROOT/../../build/cline-sdk-host"}

case "$(node --version 2>/dev/null || true)" in
  v22.*|v23.*|v24.*|v25.*|v26.*) ;;
  *) echo "Cline SDK host requires Node.js 22 or newer" >&2; exit 1 ;;
esac

rm -rf "$OUT"
mkdir -p "$OUT"
(cd "$ROOT" && npm ci && npm run build)
cp "$ROOT/package.json" "$ROOT/package-lock.json" "$ROOT/tsconfig.json" "$OUT/"
cp -R "$ROOT/dist" "$OUT/"
printf '%s\n' "Cline SDK host package created at $OUT"
