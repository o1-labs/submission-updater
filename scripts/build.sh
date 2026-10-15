#!/usr/bin/env bash

set -e

if [[ "$OUT" == "" ]]; then
  OUT="$PWD/result"
fi

case "$1" in
  test)
    cd src
    $GO test
    ;;
  docker-delegation-verify)
    if [[ "$TAG" == "" ]]; then
      echo "Specify TAG env variable."
      exit 1
    fi
    if [[ "$MINA_VERSION" == "" ]]; then
      echo "Specify MINA_VERSION env variable (Mina release deb version, e.g. 4.0.2-84b8d96)."
      exit 1
    fi
    IMAGE_NAME=${IMAGE_NAME:-ghcr.io/o1-labs/submission-updater}
    docker build \
      --build-arg "MINA_VERSION=$MINA_VERSION" \
      --build-arg "MINA_NETWORK=${MINA_NETWORK:-mainnet}" \
      --build-arg "MINA_PROFILE=${MINA_PROFILE:-${MINA_NETWORK:-mainnet}}" \
      --build-arg "LEDGER_CONFIG_VERSION=${LEDGER_CONFIG_VERSION:-}" \
      -f dockerfiles/Dockerfile-delegation-verify -t "$IMAGE_NAME:$TAG" .
    ;;
  docker-standalone)
    if [[ "$TAG" == "" ]]; then
      echo "Specify TAG env variable."
      exit 1
    fi
    # set default image name for GitHub Container Registry if IMAGE_NAME is not set
    IMAGE_NAME=${IMAGE_NAME:-ghcr.io/o1-labs/submission-updater}
    docker build -f dockerfiles/Dockerfile-standalone -t "$IMAGE_NAME:$TAG" .
    ;;
  "")
    cd src
    $GO build -o "$OUT/bin/submission-updater"
    ;;
  *)
    echo "unknown command $1"
    exit 2
    ;;
esac
