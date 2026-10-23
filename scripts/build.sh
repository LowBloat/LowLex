#!/usr/bin/env bash
# Builds the web UI, then one static LowLex binary per platform into dist/.
#   scripts/build.sh            # every platform
#   scripts/build.sh linux/amd64 windows/amd64
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
TARGETS=("$@")
if [ ${#TARGETS[@]} -eq 0 ]; then
	TARGETS=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)
fi

echo "web UI"
(cd web && bun install --frozen-lockfile && bun run build)

mkdir -p dist
for target in "${TARGETS[@]}"; do
	os="${target%/*}"
	arch="${target#*/}"
	name="lowlex-${VERSION}-${os}-${arch}"
	ext=""
	[ "$os" = windows ] && ext=".exe"
	echo "$name"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
		-ldflags "-s -w -X github.com/LowBloat/LowLex/internal/server.Version=${VERSION}" \
		-o "dist/${name}/lowlex${ext}" ./cmd/lowlex
	cp .env.example README.md LICENSE "dist/${name}/"
	if [ "$os" = windows ]; then
		(cd dist && rm -f "${name}.zip" && (command -v zip >/dev/null && zip -qr "${name}.zip" "${name}" || powershell -NoProfile -Command "Compress-Archive -Force -Path '${name}' -DestinationPath '${name}.zip'"))
	else
		# explicit modes: a binary built on Windows has no exec bit on disk
		tar -C dist --mode=0755 --no-recursion -cf "dist/${name}.tar" "${name}" "${name}/lowlex"
		tar -C dist --mode=0644 -rf "dist/${name}.tar" "${name}/.env.example" "${name}/README.md" "${name}/LICENSE"
		gzip -f "dist/${name}.tar"
	fi
done
(cd dist && (sha256sum lowlex-*.tar.gz lowlex-*.zip 2>/dev/null || true) > SHA256SUMS)
echo "done: dist/"
