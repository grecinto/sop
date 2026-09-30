#!/bin/sh
set -eu

case "$(node --version 2>/dev/null || true)" in
  v22.*|v23.*|v24.*|v25.*|v26.*) ;;
  *) echo "Cline SDK host requires Node.js 22 or newer" >&2; exit 1 ;;
esac

npm ci
npm run build
printf '%s\n' "Cline SDK host built at $(pwd)/dist/host.js"