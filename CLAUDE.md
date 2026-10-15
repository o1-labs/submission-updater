# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Submission Updater is a Go application that wraps the Mina Protocol's [Stateless verifier tool](https://github.com/MinaProtocol/mina/tree/develop/src/app/delegation_verify). It communicates with either Cassandra/AWS Keyspaces or PostgreSQL databases to:

1. Select submissions within a specified time range
2. Feed them to the stateless verifier for validation
3. Update submissions with verification results

The application supports two storage backends:
- **Cassandra/AWS Keyspaces**: Full submission data including raw blocks
- **PostgreSQL**: Used with uptime-service-validation coordinator (no raw_block storage)

## Development Environment Setup

**Prerequisites**: Nix package manager

```bash
nix-shell  # Enter development environment with Go 1.21 and dependencies
```

## Common Commands

**Build**:
```bash
make build          # Build binary to ./result/bin/submission-updater
nix-shell --run "make build"  # Build within nix-shell
```

**Test**:
```bash
make test           # Run Go tests
nix-shell --run "make test"   # Run tests within nix-shell
```

**Clean**:
```bash
make clean          # Remove result directory
```

**Dependency Management**:
```bash
make tidy           # Run go mod tidy in src directory
```

**Docker Images**:
```bash
# Standalone submission-updater only
TAG=1.0 make docker-standalone

# With stateless verifier binary included
TAG=1.0 MINA_VERSION=4.0.2-84b8d96 LEDGER_CONFIG_VERSION=4.0.0-6850301 make docker-delegation-verify

# Custom image names (defaults to ghcr.io/o1-labs/submission-updater)
IMAGE_NAME=custom-registry/my-image TAG=1.0 make docker-standalone
```

## Code Architecture

**Core Components**:
- `main.go`: Entry point, orchestrates the submission verification workflow
- `app_config.go`: Environment variable loading and configuration management
- `app_context.go`: Application context with storage and AWS client initialization
- `submission.go`: Submission data structures and JSON marshaling
- `operation.go`: Core business logic for submission processing

**Storage Backends**:
- `cassandra.go`: Cassandra/AWS Keyspaces implementation with SSL and authentication
- `postgres.go`: PostgreSQL implementation for coordinator integration
- `s3.go`: AWS S3 client for downloading missing blocks

**External Tool Integration**:
- `command.go`: Wrapper for executing the stateless verifier binary via stdin/stdout
- `shards.go`: Block hash to S3 shard mapping utilities

**Configuration**:
The application uses environment variables for all configuration. Key variables:
- `DELEGATION_VERIFY_BIN_PATH`: Path to stateless verifier binary (required)
- `SUBMISSION_STORAGE`: "POSTGRES" (default) or "CASSANDRA"
- `NETWORK_NAME`, `AWS_S3_BUCKET`: For S3 block retrieval
- Storage-specific vars for Cassandra or PostgreSQL connections

**Data Flow**:
1. Parse time range arguments
2. Load environment configuration
3. Initialize storage backend (Cassandra or PostgreSQL) and AWS S3 client
4. Query submissions in time range
5. Download missing blocks from S3 if needed
6. Marshal submissions to JSON and pipe to stateless verifier
7. Parse verification results and update storage

## Testing

Tests are located alongside source files with `_test.go` suffix:
- `command_test.go`: Tests for stateless verifier execution
- `cassandra_test.go`: Cassandra storage backend tests
- `shards_test.go`: S3 shard mapping tests

Run single test file:
```bash
cd src && go test -run TestFunctionName
```

## Docker Deployment

The project supports two Docker configurations:

1. **Standalone**: Only submission-updater binary, requires external stateless verifier
2. **Delegation-verify**: submission-updater plus the stateless verifier and network config installed from released Mina debs (stable apt repo)

The delegation-verify image is used in production and requires `MINA_VERSION` (Mina release deb version). See the Docker section of README.md.