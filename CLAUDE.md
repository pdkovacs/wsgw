# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

WSGW is a WebSocket gateway that manages stateful WebSocket connections on behalf of stateless backend applications. It acts as a proxy: clients connect via WebSocket, and backend apps communicate with clients via HTTP POST to the gateway.

## Commands

This project uses [Task](https://taskfile.dev/) (`task` CLI) for automation:

```bash
task build        # Build the Linux amd64 executable
task test-all     # Run all tests: go test -v -parallel 1 -timeout 600s ./test/...
task test-single  # Run a single suite (default: TestClusterSupportTestSuite)
task clean        # Clear test cache
task watch        # Watch and rebuild on file changes (requires fswatch)
task image        # Build Docker image
```

Running a specific test suite directly:
```bash
go test -v -parallel 1 -timeout 600s ./test/... -run '^TestConnectingTestSuite$'
```

Running a specific test within a suite:
```bash
go test -v -parallel 1 -timeout 600s ./test/... -run '^TestSendMessageTestSuite$' -testify.m '^TestSendAMessageFromApp$'
```

Tests require a `.test-env` file for environment configuration (see taskfile.yaml for details).

## Architecture

### Endpoints

- `GET /connect` — Client WebSocket connection initiation
- `POST /message/<connectionId>` — Backend sends a message to a specific client
- `GET /app-info` — Version/health info

### Connection Lifecycle

1. Client calls `GET /connect` → gateway relays to backend `GET /ws/connect` for authentication
2. On success, connection is registered locally (in-memory) and optionally in Redis (cluster mode)
3. HTTP is upgraded to WebSocket
4. **Client → Backend**: Messages forwarded via `POST /ws/message`
5. **Backend → Client**: Backend POSTs to `/message/<connectionId>`; gateway delivers via WebSocket
6. On disconnect, backend is notified via `POST /ws/disconnected` and connection deregistered

### Key Internal Packages

- **`internal/ws.go`** — `wsConnections` registry and `connection` struct; each connection has `fromClient`/`fromApp` channels, a rate limiter (1 msg/100ms, burst 8), and a 1024-message buffer
- **`internal/cluster.go`** — Optional Redis-backed cluster support; when `WSGW_REDIS_HOST` is set, connection ownership is stored in Redis and messages are relayed via HTTP between instances
- **`internal/config/`** — Environment-based config with `WSGW_` prefix (key vars: `SERVER_HOST`, `SERVER_PORT`, `APP_BASE_URL`, `REDIS_HOST`, `REDIS_PORT`)
- **`pkgs/logging/`** — Structured logging via zerolog
- **`pkgs/monitoring/`** — OpenTelemetry metrics and traces (OTLP export)

### Test Infrastructure

Tests live in `test/integration/` and `test/e2e/`. Integration tests use testify suites that spin up a real WSGW server instance alongside a mock backend app (`test/mockapp/`). Tests must run with `-parallel 1` for isolation. Suite names: `TestConnectingTestSuite`, `TestSendMessageTestSuite`, `TestClusterSupportTestSuite`.
