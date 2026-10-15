#!/usr/bin/env bash
# Cases for check-ledgers.sh. Needs bash 4+, jq and an openssl with sha3-256.
set -euo pipefail

check="$(cd "$(dirname "$0")" && pwd)/check-ledgers.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail=0

sha3() { openssl dgst -sha3-256 -r "$1" | cut -d' ' -f1; }

# expect <exit 0|nonzero> <name> <config> <dir>
expect() {
  local want=$1 name=$2 rc=0
  "$check" "$3" "$4" >/dev/null 2>&1 || rc=$?
  if [[ $want == 0 && $rc == 0 ]] || [[ $want != 0 && $rc != 0 ]]; then
    echo "ok   $name"
  else
    echo "FAIL $name (exit $rc)"; fail=1
  fi
}

mkdir "$tmp/d"
echo a > "$tmp/d/genesis_ledger_a.tar.gz"
echo b > "$tmp/d/epoch_ledger_b.tar.gz"
ha=$(sha3 "$tmp/d/genesis_ledger_a.tar.gz")
hb=$(sha3 "$tmp/d/epoch_ledger_b.tar.gz")
hz=$(printf 'z%.0s' {1..64})

cat > "$tmp/good.json" <<JSON
{"ledger":{"s3_data_hash":"$ha"},"epoch_data":{"staking":{"s3_data_hash":"$hb"},"next":{"s3_data_hash":"$hb"}}}
JSON
cat > "$tmp/missing.json" <<JSON
{"ledger":{"s3_data_hash":"$ha"},"epoch_data":{"staking":{"s3_data_hash":"$hb"},"next":{"s3_data_hash":"$hz"}}}
JSON
echo '{"ledger":{"name":"x"}}' > "$tmp/nohash.json"

expect 1 "config file missing"         "$tmp/nope.json"    "$tmp/d"
expect 1 "config with no s3_data_hash" "$tmp/nohash.json"  "$tmp/d"
expect 1 "hash without a tarball"      "$tmp/missing.json" "$tmp/d"
expect 1 "empty ledger directory"      "$tmp/good.json"    "$tmp/empty"
expect 0 "every hash has a tarball"    "$tmp/good.json"    "$tmp/d"

exit $fail
