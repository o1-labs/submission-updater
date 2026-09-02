# Submission Updater

[![Build](https://github.com/MinaFoundation/cassandra-updater/actions/workflows/build.yml/badge.svg)](https://github.com/MinaFoundation/cassandra-updater/actions/workflows/build.yml)

This is a wrapper over the [Stateless verifier tool](https://github.com/MinaProtocol/mina/tree/develop/src/app/delegation_verify) that is responsible for communication with Cassandra database. It will select a range of submissions from Cassandra, feed `stateless_verifier` with it, collect results and update submissions with gathered data. In order to work as expected the program requires `DELEGATION_VERIFY_BIN_PATH` env variable to be set.

## Build
```
$ nix-shell
$ make
```


## Configuration

**1. Runtime Configuration**:

  - `DELEGATION_VERIFY_BIN_PATH` - path to [Stateless verifier tool](https://github.com/MinaProtocol/mina/tree/develop/src/app/delegation_verify) binary.
  - `NO_CHECKS` - if set to `1`, stateless verifier tool will run with `--no-checks` flag
  - `TOLERATE_SOK_MISMATCH` - if set to `1`, a submission whose only verification failure is the snark-work sok-digest check is **re-verified with its snark work stripped**, and the result of that second run replaces the first. The verifier then skips the snark-work path entirely and verifies the block on its own, so the block proof must still verify for the submission to count; a submission that fails the retry keeps the retry's error. Submissions failing for any other reason are never retried, and with the flag off behaviour is unchanged. Both era-specific spellings of the failure are recognised, because the check lives in a different place in each: pre-fork binaries (Berkeley, and the 3.5.0 mainnet stop-slot release) compare inside `Transaction_snark.verify` and report `Transaction_snark.verify: Mismatched sok_message`, while post-fork binaries (4.0.0 Mesa) use an explicit check in `delegation_verify.ml` and report `proof's sok message digest does not match the sok message`. Matching only the latter would leave the waiver inert against the failures occurring on mainnet today and against the pre-fork partition in dual-verifier mode. Daemons from 3.4.0 onward emit uptime snark work carrying the default sok digest on the zkApp-segment path ([MinaProtocol/mina#19299](https://github.com/MinaProtocol/mina/issues/19299)); the already-released mainnet Mesa artifacts all carry that path, so post-fork these submissions would fail verification at scale. The sok binding exists to prevent fee/prover misattribution in the snark pool, where work is paid - uptime snark work is never pooled or paid, so waiving only this check for the hard-fork window has no security cost for uptime attestation, which rests on the (still fully verified) block proof. The retry - rather than simply marking the record valid - is what makes the waiver count for scoring: `delegation_verify` short-circuits on the first error, so a sok-failed record comes back with a NULL `state_hash`, and the coordinator awards a point only when a submission's `state_hash` appears in its shortlist (NULL hashes are dropped before that comparison). Re-verifying without snark work restores the full payload - `state_hash`, `parent`, `height`, `slot` - so the submission scores normally. Temporary; remove after the daemon fleet carries [MinaProtocol/mina#19313](https://github.com/MinaProtocol/mina/pull/19313).
  - `SUBMISSION_STORAGE` - Storage where submissions are kept. Valid options: `POSTGRES` or `CASSANDRA`. Default: `POSTGRES`.
  - `GENESIS_LEDGER_FILE` - file path to genesis ledger file. This is input for stateless_verifier `--config-file` option. In principle it is optional, if set, stateless_verifier will be run with `--config-file GENESIS_LEDGER_FILE` option.
  - `FORK_CUTOVER_TIME` - optional RFC3339 timestamp (e.g. `2026-09-03T00:00:00Z`) marking a hard fork cutover. When set, dual-verifier mode is enabled: submissions with `submitted_at >= FORK_CUTOVER_TIME` are verified with the post-fork binary (`DELEGATION_VERIFY_BIN_PATH_POST_FORK`), while submissions with `submitted_at < FORK_CUTOVER_TIME` are verified with the pre-fork binary (`DELEGATION_VERIFY_BIN_PATH`). When unset, behavior is unchanged and only `DELEGATION_VERIFY_BIN_PATH` is used.
  - `DELEGATION_VERIFY_BIN_PATH_POST_FORK` - path to the post-fork stateless verifier tool binary. Required when `FORK_CUTOVER_TIME` is set.
  - `GENESIS_LEDGER_FILE_POST_FORK` - file path to the post-fork genesis ledger file, passed as `--config-file` to the post-fork binary. Required whenever `FORK_CUTOVER_TIME` is set: the post-fork verification keys derive from the runtime config's fork constants and there is no compile-time fallback, so without it the post-fork binary would silently fail every post-fork submission. Both post-fork paths are validated at startup (the binary must exist and be executable, the ledger file must exist), so a bad path surfaces at deploy time rather than on fork day. `GENESIS_LEDGER_FILE` remains the pre-fork config.

**2. AWS Keyspaces/Cassandra Configuration**:

  **Mandatory/common env vars:**
  - `AWS_KEYSPACE` - Your Keyspace name.
  - `SSL_CERTFILE` - The path to your SSL certificate.

  **Depending on way of connecting:**

  _Service level connection:_
  - `CASSANDRA_HOST` - Cassandra host (e.g. cassandra.us-west-2.amazonaws.com).
  - `CASSANDRA_PORT` - Cassandra port (e.g. 9142).
  - `CASSANDRA_USERNAME` - Cassandra service user.
  - `CASSANDRA_PASSWORD` - Cassandra service password.

  _AWS access key / web identity token:_
  - `AWS_REGION` - The AWS region (same as used for S3).
  - `AWS_WEB_IDENTITY_TOKEN_FILE` - AWS web identity token file.
  - `AWS_ROLE_SESSION_NAME` - AWS role session name.
  - `AWS_ROLE_ARN` - AWS role arn.
  - `AWS_ACCESS_KEY_ID` - Your AWS Access Key ID. No need to set if `AWS_WEB_IDENTITY_TOKEN_FILE`, `AWS_ROLE_SESSION_NAME` and `AWS_ROLE_ARN` are set.
  - `AWS_SECRET_ACCESS_KEY` - Your AWS Secret Access Key. No need to set if `AWS_WEB_IDENTITY_TOKEN_FILE`, `AWS_ROLE_SESSION_NAME` and `AWS_ROLE_ARN` are set.

**3. AWS S3 Configuration**:

  - `AWS_S3_BUCKET` - AWS S3 Bucket where blocks and submissions are stored.
  - `NETWORK_NAME` - Network name (in case block does not exist in Cassandra we attempt to download it from AWS S3 from `AWS_S3_BUCKET`\\`NETWORK_NAME`\blocks)
  - `AWS_REGION` - The AWS region where your S3 bucket is located. While this is automatically retrieved, it can also be explicitly set through environment variables or AWS configuration files.
  - `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` - Your AWS credentials. These are automatically retrieved from your environment or AWS configuration files but should be securely stored and accessible in your deployment environment.

**4. PostgreSQL Configuration**

If this storage backend is configured it is assumed that submissions are written into `submissions` table in the uptime-service-validation (coordinator) component. In this mode we are not storing `raw_block` in the database.

- `POSTGRES_HOST` - Hostname or IP address where your PostgreSQL server is running.
- `POSTGRES_PORT` - Port number on which PostgreSQL is listening.
- `POSTGRES_DB` - The name of the database to connect to. This is the uptime-service-validation database.
- `POSTGRES_USER` - The username with which to connect to the database.
- `POSTGRES_PASSWORD` - The password for the database user.
- `POSTGRES_SSLMODE` - The mode for SSL connectivity (e.g., `disable`, `require`, `verify-ca`, `verify-full`). Default is `require` for secure setups.

## Run

```
$ ./result/bin/cassandra-updater "2024-03-04 09:38:54.0+0000" "2024-03-04 09:45:55.0+0000"
```

## Docker

We can build docker image containing both `submission-updater` and [Stateless verifier tool](https://github.com/MinaProtocol/mina/tree/develop/src/app/delegation_verify). For that we need to feed build with `DUNE_PROFILE` and `MINA_BRANCH` env variables. `DUNE_PROFILE` is the profile in which the tool will be built (typically `devnet`). `MINA_BRANCH` indicates which branch of [Mina](https://github.com/MinaProtocol/mina) repository we want to build the tool from.

The docker image already has set: 
 - `DELEGATION_VERIFY_BIN_PATH`
 - `SSL_CERTFILE` 
 - `GENESIS_LEDGER_FILE` with mainnet genesis_ledger file. In case different ledger file is required one can override it by passing GENESIS_LEDGER_FILE to the docker container via `-e GENESIS_LEDGER_FILE=/different/path/genesis.json`. 
 - `DELEGATION_VERIFY_BIN_PATH_POST_FORK`, `GENESIS_LEDGER_FILE_POST_FORK` and `FORK_CUTOVER_TIME` for dual-binary (pre/post-hard-fork) images — see below.

**Build**:

```
$ nix-shell
$ TAG=1.0 \
  DUNE_PROFILE=devnet \
  MINA_BRANCH=delegation_verify_over_stdin_rc_base \
  make docker-delegation-verify
```

### Dual-binary (pre/post-hard-fork) builds

For the Mina "Mesa" hard fork a single image can carry **two** `delegation-verify` binaries — the pre-fork one (built from `MINA_BRANCH`, exactly as before) and a post-fork one. The post-fork build is controlled by three optional env variables (threaded through as docker build args):

 - `MINA_BRANCH_POST_FORK` - branch (or tagged release) of [Mina](https://github.com/MinaProtocol/mina) to build the post-fork binary from. **Empty (the default) disables the post-fork build entirely** and the resulting image is identical to a single-binary build (plus an empty placeholder file at the post-fork binary path).
 - `DUNE_PROFILE_POST_FORK` - dune profile for the post-fork build. **Defaults to `DUNE_PROFILE`**, so both binaries in one image are built on the same profile unless you deliberately override it.
 - `FORK_CUTOVER_TIME` - RFC3339 timestamp of the hard fork, baked into the image as the `FORK_CUTOVER_TIME` env variable. Empty (the default) keeps dual mode off — the wrapper behaves exactly as today.

Passing `FORK_CUTOVER_TIME` *to the build* bakes an armed cutover into the image, so the build refuses to proceed unless the image can actually serve it: `MINA_BRANCH_POST_FORK` must be set, `genesis_ledgers/mainnet-post-fork.json` must exist, and the timestamp must parse (checked with GNU `date -d`; skipped on BSD/macOS `date`). This is the fork-day rebuild path. To arm an image you built earlier instead, leave it out here and set it at deploy time — see [Arming the cutover on an already-built image](#arming-the-cutover-on-an-already-built-image).

The same three knobs are exposed as optional `workflow_dispatch` inputs of the Publish workflow (`mina_branch_post_fork`, `dune_profile_post_fork`, `fork_cutover_time`).

Resulting image layout:

 - `/bin/delegation-verify` - pre-fork binary (`DELEGATION_VERIFY_BIN_PATH`)
 - `/bin/delegation-verify-post-fork` - post-fork binary (`DELEGATION_VERIFY_BIN_PATH_POST_FORK`)
 - `/root/genesis_ledgers/mainnet.json` - pre-fork config file (`GENESIS_LEDGER_FILE`)
 - `/root/genesis_ledgers/mainnet-post-fork.json` - post-fork config file (`GENESIS_LEDGER_FILE_POST_FORK`)

The whole `genesis_ledgers/` directory is copied into the image, so `mainnet-post-fork.json` rides along automatically if it is committed *before* the build. Usually it will not be: that file carries a `proof.fork` block (`state_hash`, `blockchain_length`, `global_slot_since_genesis`) describing the actual fork block, so it cannot exist until the fork has happened. An image built ahead of the fork therefore ships without it, and the post-fork ledger has to be supplied at deploy time instead — see [Arming the cutover on an already-built image](#arming-the-cutover-on-an-already-built-image) below.

Until the cutover is armed, dual-binary images can already be built and deployed with `FORK_CUTOVER_TIME` unset, in which case only the pre-fork binary is used.

Example dual-binary build:

```
$ nix-shell
$ TAG=1.0 \
  DUNE_PROFILE=devnet \
  MINA_BRANCH=delegation_verify_over_stdin_rc_base \
  MINA_BRANCH_POST_FORK=mesa \
  FORK_CUTOVER_TIME=2026-09-03T00:00:00Z \
  make docker-delegation-verify
```

Both binaries above are built on the `devnet` profile: `DUNE_PROFILE_POST_FORK` is unset and therefore inherits `DUNE_PROFILE`.

### Arming the cutover on an already-built image

This is the sequence the dual-binary image exists for: build and deploy *before* the fork, arm *after* it, with no rebuild or image swap on fork day.

Setting `FORK_CUTOVER_TIME` is not sufficient on its own. The routing wrapper stats **both** the post-fork binary and the post-fork genesis ledger at startup and exits if either is missing, so arming an image that was built before the ledger existed fails immediately with something like:

```
GENESIS_LEDGER_FILE_POST_FORK: cannot stat /root/genesis_ledgers/mainnet-post-fork.json
```

The binary is already in the image (it was built from `MINA_BRANCH_POST_FORK`). The ledger is not, and could not have been — see above — so mount it in at deploy time.

**Docker**, mounting at the path already baked into the image, so no env override is needed:

```
docker run --rm \
  -e FORK_CUTOVER_TIME=2026-09-03T00:00:00Z \
  -v /path/to/mainnet-post-fork.json:/root/genesis_ledgers/mainnet-post-fork.json:ro \
  ghcr.io/o1-labs/submission-updater:1.0 \
  "2026-09-03 00:00:00.0+0000" "2026-09-03 00:05:00.0+0000"
```

Or mount it anywhere and point the variable at it:

```
docker run --rm \
  -e FORK_CUTOVER_TIME=2026-09-03T00:00:00Z \
  -e GENESIS_LEDGER_FILE_POST_FORK=/mnt/ledgers/mainnet-post-fork.json \
  -v /path/to/ledgers:/mnt/ledgers:ro \
  ghcr.io/o1-labs/submission-updater:1.0 \
  "2026-09-03 00:00:00.0+0000" "2026-09-03 00:05:00.0+0000"
```

**Kubernetes**. The ledger file is small — it references ledger data by hash rather than inlining it, so the committed `mainnet.json` is under 1 kB — which makes a ConfigMap a comfortable fit:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mina-post-fork-ledger
data:
  mainnet-post-fork.json: |
    { ... contents of the published post-fork genesis ledger ... }
```

Mounted on the worker Job/pod spec:

```yaml
spec:
  containers:
    - name: submission-updater
      env:
        - name: FORK_CUTOVER_TIME
          value: "2026-09-03T00:00:00Z"
      volumeMounts:
        - name: post-fork-ledger
          mountPath: /root/genesis_ledgers/mainnet-post-fork.json
          subPath: mainnet-post-fork.json
          readOnly: true
  volumes:
    - name: post-fork-ledger
      configMap:
        name: mina-post-fork-ledger
```

`subPath` mounts the single file without shadowing the rest of `/root/genesis_ledgers/`, so the pre-fork `mainnet.json` baked into the image stays visible. Swap the ConfigMap for a Secret if the ledger should be treated as sensitive — the volume wiring is unchanged.

This is also why building with `MINA_BRANCH_POST_FORK` set while `FORK_CUTOVER_TIME` is left unset is legal and expected: that is precisely the image you deploy ahead of the fork, dormant, with the cutover time and the ledger both supplied later.

**Run**:

```
docker run --rm \
  -e AWS_KEYSPACE \
  -e AWS_REGION \
  -e AWS_ACCESS_KEY_ID \
  -e AWS_SECRET_ACCESS_KEY \
  673156464838.dkr.ecr.us-west-2.amazonaws.com/delegation-verify:1.0 \
  "2024-03-15 13:12:12.0+0000" "2024-03-15 13:12:13.0+0000"
```
