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
  - `TOLERATE_SOK_MISMATCH` - if set to `1`, a submission whose only verification failure is the snark-work sok-digest check is **re-verified with its snark work stripped**, and the result of that second run replaces the first. The verifier then skips the snark-work path entirely and verifies the block on its own, so the block proof must still verify for the submission to count; a submission that fails the retry keeps the retry's error. Submissions failing for any other reason are never retried, and with the flag off behaviour is unchanged. The waived failure is `proof's sok message digest does not match the sok message` from `delegation_verify.ml`. Daemons from 3.4.0 onward emit uptime snark work carrying the default sok digest on the zkApp-segment path ([MinaProtocol/mina#19299](https://github.com/MinaProtocol/mina/issues/19299)); the already-released mainnet Mesa artifacts all carry that path, so post-fork these submissions would fail verification at scale. The sok binding exists to prevent fee/prover misattribution in the snark pool, where work is paid - uptime snark work is never pooled or paid, so waiving only this check has no security cost for uptime attestation, which rests on the (still fully verified) block proof. The retry - rather than simply marking the record valid - is what makes the waiver count for scoring: `delegation_verify` short-circuits on the first error, so a sok-failed record comes back with a NULL `state_hash`, and the coordinator awards a point only when a submission's `state_hash` appears in its shortlist (NULL hashes are dropped before that comparison). Re-verifying without snark work restores the full payload - `state_hash`, `parent`, `height`, `slot` - so the submission scores normally. Temporary; remove after the daemon fleet carries [MinaProtocol/mina#19313](https://github.com/MinaProtocol/mina/pull/19313).
  - `SUBMISSION_STORAGE` - Storage where submissions are kept. Valid options: `POSTGRES` or `CASSANDRA`. Default: `POSTGRES`.
  - `GENESIS_LEDGER_FILE` - file path to genesis ledger file. This is input for stateless_verifier `--config-file` option. In principle it is optional, if set, stateless_verifier will be run with `--config-file GENESIS_LEDGER_FILE` option.

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

`dockerfiles/Dockerfile-delegation-verify` builds `submission_updater` from `src/` and installs the [Stateless verifier tool](https://github.com/MinaProtocol/mina/tree/develop/src/app/delegation_verify) and the network config from a released Mina version in the signed apt repo (`stable.apt.packages.minaprotocol.com`, bookworm). Nothing is compiled from the Mina tree.

Build args (env variables of `make docker-delegation-verify`):

 - `MINA_VERSION` - required. Deb version of the Mina release, e.g. `4.0.2-84b8d96`. Installs `mina-delegation-verify` and `mina-<network>-config` at that version.
 - `MINA_NETWORK` - default `mainnet`. Selects `mina-<network>-config`.
 - `MINA_PROFILE` - default `MINA_NETWORK`. The verifier deb is profile-generic and reads it at run time.
 - `LEDGER_CONFIG_VERSION` - default `MINA_VERSION`. Version of `mina-<network>-config` that ships the ledger tarballs. Config debs carry them from MinaProtocol/mina#19737 on. For 4.0.1 and 4.0.2 use `4.0.0-6850301`, which has the same mainnet ledgers.

The verifier looks for the genesis and epoch ledgers under `/var/lib/coda` first and downloads them from S3 otherwise; files found locally are not hash-checked. The build therefore fails unless every `s3_data_hash` in `/var/lib/coda/<network>.json` matches a tarball in `/var/lib/coda` (`scripts/check-ledgers.sh`, tested by `scripts/test-check-ledgers.sh`).

The image sets:

 - `DELEGATION_VERIFY_BIN_PATH` - `/usr/local/bin/mina-delegation-verify`
 - `GENESIS_LEDGER_FILE` - `/var/lib/coda/<network>.json`
 - `MINA_PROFILE`, `SSL_CERTFILE`

The verifier comes from one Mina release, so the image verifies only submissions from that release's chain. For mainnet 4.x that is the Mesa chain: do not re-run windows before the 2026-09-03T18:00Z hard fork with it.

**Build**:

```
$ nix-shell
$ TAG=4.0.2 \
  MINA_VERSION=4.0.2-84b8d96 \
  LEDGER_CONFIG_VERSION=4.0.0-6850301 \
  make docker-delegation-verify
```

The same args are the `mina_version`, `mina_network` and `ledger_config_version` inputs of the Publish workflow.

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
