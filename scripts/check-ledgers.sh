#!/usr/bin/env bash
# Fails unless every s3_data_hash in a Mina runtime config matches a ledger tarball
# (by sha3-256) in a directory. The verifier trusts local tarballs as is and silently
# downloads missing ones from S3, so a gap would only show up as egress.
#
# usage: check-ledgers.sh <config.json> <dir>
set -euo pipefail
shopt -s nullglob

cfg=${1:?config file}
dir=${2:?ledger directory}

hashes="$(jq -r '[.ledger, .epoch_data.staking, .epoch_data.next] | .[] | .s3_data_hash // empty' "$cfg" | sort -u)"
[[ -n "$hashes" ]] || { echo "no s3_data_hash in ${cfg}" >&2; exit 1; }

declare -A have
for f in "$dir"/*.tar.gz; do
  have[$(openssl dgst -sha3-256 -r "$f" | cut -d' ' -f1)]="$f"
done

for want in $hashes; do
  [[ -n "${have[$want]:-}" ]] || { echo "no ledger tarball with sha3-256 ${want} (from ${cfg})" >&2; exit 1; }
  echo "ledger ${want}: ${have[$want]}"
done
