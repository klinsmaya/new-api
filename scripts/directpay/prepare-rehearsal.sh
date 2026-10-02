#!/usr/bin/env bash
# Build only local fixture images. No push, release tag, host security change,
# merchant configuration or production database is used.
set -euo pipefail
cd "$(dirname "$0")/../.."
root="$PWD"
go_bin="${DIRECTPAY_GO:-$root/.directpay-tools/go1.26.8/go/bin/go}"
export GOCACHE="$root/.directpay-tools/build-cache" GOPATH="$root/.directpay-tools/gopath"
export DOCKER_CONFIG="$root/.directpay-tools/docker-config"
mkdir -p "$DOCKER_CONFIG" .directpay-tools/image
# The delivered Git bundle preserves these development-only intermediate refs.
for item in 'baseline:1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5' 'old:796f44088f3dae9e2248e059ad0ae9a4fd6863ce' 'compatible:1d40b4abc'; do
  label="${item%%:*}"
  revision="${item#*:}"
  destination="$(mktemp -d "$root/.directpay-tools/rehearsal-source.XXXXXX")"
  git archive "$revision" | tar -x -C "$destination"
  # Only API/migration behavior of prior binaries is tested; the browser uses current UI.
  cp -r web/dist "$destination/web/dist"
  output="$root/.directpay-tools/new-api-$label"
  [[ "$label" != old ]] || output="$root/.directpay-tools/new-api"
  [[ "$label" != compatible ]] || output="$root/.directpay-tools/new-api-compatible-rollback"
  (cd "$destination" && "$go_bin" build -o "$output" .)
done
"$go_bin" build -o .directpay-tools/new-api-next .
cp .directpay-tools/new-api-baseline .directpay-tools/image/baseline
cp .directpay-tools/new-api-compatible-rollback .directpay-tools/image/compatible
cp .directpay-tools/new-api-next .directpay-tools/image/current
# Linux amd64 fixture runtime, not the distribution Dockerfile. No package pulls.
cp /lib/x86_64-linux-gnu/libc.so.6 /lib64/ld-linux-x86-64.so.2 .directpay-tools/image/
cp -r /usr/share/zoneinfo .directpay-tools/image/
cat > .directpay-tools/image/Dockerfile <<'DOCKERFILE'
FROM scratch
ARG BINARY
COPY --chmod=0755 ${BINARY} /new-api
COPY --chmod=0755 libc.so.6 /lib/x86_64-linux-gnu/libc.so.6
COPY --chmod=0755 ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2
COPY --chmod=0755 zoneinfo /usr/share/zoneinfo
ENTRYPOINT ["/new-api"]
DOCKERFILE
for label in baseline compatible current; do
  docker build --build-arg BINARY="$label" -t "newapi-directpay-rehearsal:$label" .directpay-tools/image
done
# Start a dedicated Redis fixture and network separately, as in LOCAL_REHEARSAL.md.
