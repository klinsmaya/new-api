#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export GOCACHE="$PWD/.directpay-tools/build-cache"
export GOPATH="$PWD/.directpay-tools/gopath"
go_bin="${DIRECTPAY_GO:-$PWD/.directpay-tools/go1.26.8/go/bin/go}"
# No merchant configuration is read by these tests. Protocol transports never
# contact gateways. External DB DSNs are opt-in, dedicated local fixtures only.
if [[ -n "${DIRECTPAY_TEST_DSN:-}" && "${DIRECTPAY_TEST_DSN}" != *127.0.0.1* ]]; then
  echo 'Refusing non-loopback payment test database' >&2
  exit 2
fi
"$go_bin" test ./model ./controller -run DirectPay -count=1
"$go_bin" test ./service/directpay/... -count=1
(cd web && node node_modules/vitest/vitest.mjs run src/features/wallet/direct-pay/__tests__/payment.test.tsx)
