#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
if [[ $# != 2 ]]; then echo 'usage: upstream-impact.sh OLD_FULL_SHA NEW_FULL_SHA' >&2; exit 2; fi
for revision in "$@"; do
  [[ "$revision" =~ ^[0-9a-f]{40}$ ]] || { echo 'full commit SHA required' >&2; exit 2; }
  git cat-file -e "$revision^{commit}"
done
git diff --stat "$1" "$2" -- go.mod go.sum model common controller router setting web/src main.go
# This report is a review aid, not proof of semantic compatibility.
git diff "$1" "$2" -- model/topup.go model/user.go model/user_cache.go model/user_auth_cache.go model/quota_reserve.go model/main.go common/quota_math.go common/redis.go controller/topup.go router/api-router.go setting/operation_setting web/src/features/wallet web/package.json
