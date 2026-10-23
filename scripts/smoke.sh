#!/usr/bin/env bash
# Starts a LowLex binary with an empty data folder and checks the first-run flow:
# health, the default admin must change its password, then the API opens up.
#   scripts/smoke.sh path/to/lowlex
set -euo pipefail

bin="$1"
port="${PORT:-4799}"
dir="$(mktemp -d)"
base="http://127.0.0.1:${port}/api"
cookies="${dir}/cookies"

LOWLEX_PORT="$port" LOWLEX_DATA_DIR="${dir}/data" "$bin" serve >"${dir}/log" 2>&1 &
pid=$!
trap 'kill $pid 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
	curl -sf "${base}/health" >/dev/null && break
	sleep 0.2
done
"$bin" version
curl -sf "${base}/health"
echo

expect() { # expect <want> <got> <what>
	if [ "$1" != "$2" ]; then
		echo "FAIL: $3: got $2, want $1"
		cat "${dir}/log"
		exit 1
	fi
	echo "ok: $3"
}

login=$(curl -s -c "$cookies" -H 'Content-Type: application/json' -d '{"username":"admin","password":"lowlex"}' "${base}/auth/login")
expect true "$(echo "$login" | grep -o '"must_change_password":[a-z]*' | cut -d: -f2)" "default admin must change password"
expect 403 "$(curl -s -o /dev/null -w '%{http_code}' -b "$cookies" "${base}/projects")" "API blocked before the change"
curl -sf -b "$cookies" -H 'Content-Type: application/json' -H "Origin: http://127.0.0.1:${port}" \
	-d '{"current_password":"lowlex","new_password":"Smoke-Test-Password-1"}' "${base}/auth/password" >/dev/null
expect 200 "$(curl -s -o /dev/null -w '%{http_code}' -b "$cookies" "${base}/projects")" "API open after the change"
expect 200 "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${port}/login")" "web UI served"
echo "smoke test passed"
