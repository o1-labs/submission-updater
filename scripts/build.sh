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
    if [[ "$DUNE_PROFILE" == "" ]]; then
      echo "Specify DUNE_PROFILE env variable. (e.g. devnet)"
      exit 1
    fi
    if [[ "$MINA_BRANCH" == "" ]]; then
      echo "Specify MINA_BRANCH env variable. (The branch to build the delegation-verify binary from)."
      exit 1
    fi
    if [[ "$FORK_CUTOVER_TIME" != "" && "$MINA_BRANCH_POST_FORK" == "" ]]; then
      echo "FORK_CUTOVER_TIME is set but MINA_BRANCH_POST_FORK is empty; refusing to build a dual-mode image without a post-fork binary."
      exit 1
    fi
    if [[ "$FORK_CUTOVER_TIME" != "" && ! -f genesis_ledgers/mainnet-post-fork.json ]]; then
      echo "FORK_CUTOVER_TIME is set but genesis_ledgers/mainnet-post-fork.json does not exist; refusing to build a dual-mode image whose GENESIS_LEDGER_FILE_POST_FORK would point at a missing file. Commit the post-fork genesis ledger first."
      exit 1
    fi
    # Validate the cutover format up front, so a typo fails in the first second
    # rather than after the multi-hour build and again at container startup.
    #
    # The shape check is a literal RFC3339 match rather than `date -d`, because
    # the consumer is Go's time.RFC3339 and GNU date is far more permissive than
    # that: it happily accepts "Sept 3" and "2026-09-03 00:00:00", which are the
    # typos most worth catching. `date -d` is then used only as a secondary
    # semantic check (e.g. month 13) where it exists - BSD/macOS date has no -d,
    # so that probe skips rather than erroring.
    RFC3339_RE='^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$'
    if [[ "$FORK_CUTOVER_TIME" != "" ]]; then
      if [[ ! "$FORK_CUTOVER_TIME" =~ $RFC3339_RE ]]; then
        echo "FORK_CUTOVER_TIME='$FORK_CUTOVER_TIME' is not RFC3339; expected e.g. 2026-09-03T00:00:00Z."
        exit 1
      fi
      if date -d "1970-01-01T00:00:00Z" >/dev/null 2>&1 && ! date -d "$FORK_CUTOVER_TIME" >/dev/null 2>&1; then
        echo "FORK_CUTOVER_TIME='$FORK_CUTOVER_TIME' is RFC3339-shaped but not a real timestamp."
        exit 1
      fi
    fi
    # set default image name for GitHub Container Registry if IMAGE_NAME is not set
    IMAGE_NAME=${IMAGE_NAME:-ghcr.io/o1-labs/submission-updater}
    # Optional post-hard-fork (Mesa) build args. When MINA_BRANCH_POST_FORK is
    # empty the post-fork builder stage is skipped and the image is identical
    # to a single-binary build. DUNE_PROFILE_POST_FORK defaults to DUNE_PROFILE
    # so both binaries in one image cannot silently end up on different profiles.
    docker build \
      --build-arg "MINA_BRANCH=$MINA_BRANCH" \
      --build-arg "DUNE_PROFILE=$DUNE_PROFILE" \
      --build-arg "MINA_BRANCH_POST_FORK=${MINA_BRANCH_POST_FORK:-}" \
      --build-arg "DUNE_PROFILE_POST_FORK=${DUNE_PROFILE_POST_FORK:-$DUNE_PROFILE}" \
      --build-arg "FORK_CUTOVER_TIME=${FORK_CUTOVER_TIME:-}" \
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
