#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
# SPDX-License-Identifier: MPL-2.0
set -euo pipefail
cd "$(dirname "$0")/.."
work_dir=$(mktemp -d)
fixture="kuma-provider-test-$$"
export KUMA_TEST_CONTAINER="$fixture"
read -r -a engine <<< "${CONTAINER_ENGINE:-docker}"
cleanup() {
    result=$?
    trap - EXIT
    "${engine[@]}" rm --force "$fixture" >/dev/null 2>&1 || true
    rm -rf -- "$work_dir"
    exit "$result"
}
trap cleanup EXIT
mkdir "$work_dir/data"
export KUMA_TEST_PASSWORD_FILE="$work_dir/password"
python3 - <<'PY'
import os, secrets
with open(os.open(os.environ['KUMA_TEST_PASSWORD_FILE'], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as f:
    f.write(secrets.token_urlsafe(36))
PY
"${engine[@]}" run --detach --name "$fixture" --publish 127.0.0.1::3001 --env UPTIME_KUMA_DB_TYPE=sqlite \
    --volume "$work_dir/data:/app/data" "$(cat tests/kuma-image.txt)" >/dev/null
port=$("${engine[@]}" port "$fixture" 3001/tcp | head -1 | sed 's/.*://')
export KUMA_TEST_ENDPOINT="http://127.0.0.1:$port"
ready=0
for ((i=0; i<90; i++)); do
    if curl --silent --fail "$KUMA_TEST_ENDPOINT" >/dev/null; then ready=1; break; fi
    sleep 1
done
((ready == 1))
go run ./tests/bootstrap.go
go test -race -p=2 -tags=integration -count=1 -v .
