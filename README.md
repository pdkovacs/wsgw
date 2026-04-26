# wsgw — WebSocket gateway

Stateful WebSocket connections don't compose well with stateless backends — they pin clients to specific instances and turn horizontal scaling into a routing problem. **wsgw** lifts that one concern out of the application: it owns the WebSocket connections so the backend can stay stateless, and it speaks plain HTTP to the backend in both directions.

It's a small, focused service for low- to medium-workload deployments that want to scale the application horizontally without taking on the operational weight of a heavyweight gateway.

## How it works

```
Client                  wsgw                       Backend
  |                      |                            |
  |--- GET /connect ---->|--- GET /ws/connect ------->|
  |                      |<--------- 200 OK ----------|
  |<-- 101 + ack {id} ---|                            |
  |                      |                            |
  |---- WS frame ------->|--- POST /ws/message ------>|
  |                      |<--------- 200 OK ----------|
  |                      |                            |
  |                      |<-- POST /message/{id} -----|
  |<--- WS frame --------|----------- 204 ----------->|
  |                      |                            |
  |--- WS close -------->|--- POST /ws/disconnected ->|
```

1. Client opens a WebSocket: `GET /connect`.
2. wsgw forwards the request as a regular HTTP `GET` to the backend's `/ws/connect` for authentication.
3. Backend returns `200` → wsgw upgrades the HTTP connection to a WebSocket and (optionally) sends an ack frame containing the assigned connection ID.
4. **Client → backend:** wsgw forwards each WS frame to the backend as `POST /ws/message`.
5. **Backend → client:** backend `POST`s to wsgw's `/message/{connectionId}`; wsgw forwards the body over the WebSocket.
6. Either side closes the WS → wsgw notifies the backend via `POST /ws/disconnected` (best-effort).

## Quick start

```bash
task build           # build ./cmd/wsgw
task test-all        # run integration suites
task watch           # rebuild + restart on file changes (needs fswatch)
```

To run a single instance against a local backend listening on `:45678`:

```bash
export WSGW_SERVER_PORT=45679
export WSGW_APP_BASE_URL=http://localhost:45678
export WSGW_ACK_NEW_CONN_WITH_CONN_ID=true
./cmd/wsgw
```

A working backend implementation suitable for poking at locally lives in [`test/mockapp/`](test/mockapp/) (Go) and the [`pdkovacs/wsgw-node-ref`](https://github.com/pdkovacs/wsgw-node-ref) companion repository (Node.js).

## Endpoint reference

### Provided to clients and backends

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/connect` | Client opens a WebSocket. Returns `101` on success, `401` if the backend rejects auth, `500` otherwise. The first WS text frame is the connect-ack (see below) when `WSGW_ACK_NEW_CONN_WITH_CONN_ID=true`. |
| `POST` | `/message/{connectionId}` | Backend sends a message to a specific client. Body is opaque (delivered to the WebSocket as-is). Returns `204` on success, `404` if the connection is unknown, `503` if the per-connection buffer is saturated, `400`/`500` on input/internal errors. |
| `GET`  | `/app-info` | Build/version info. |

### Expected from the backend

The backend must serve three endpoints under whatever base URL is configured via `WSGW_APP_BASE_URL`:

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/ws/connect` | Authenticate a new connection. Return `200` to accept, `401` to reject, anything else is treated as an internal error. The original client headers (including `Authorization`) are passed through. wsgw also adds `X-WSGW-CONNECTION-ID`. |
| `POST` | `/ws/message` | Receive a frame the client sent. Return `200` to acknowledge; a non-`200` response causes wsgw to forward the response body back to the client over the WebSocket. The connection ID is in the `X-WSGW-CONNECTION-ID` header. |
| `POST` | `/ws/disconnected` | Notification that a client disconnected. Best-effort: wsgw does not retry, and the response status is logged but not acted on. |

### Headers and protocol notes

- **`X-WSGW-CONNECTION-ID`** — set by wsgw on every request to the backend. Carries the gateway-assigned connection ID.
- **`Authorization`** — passed through from the client's `GET /connect` to the backend's `GET /ws/connect` unchanged. wsgw does no auth itself.
- **Connect-ack frame** — when `WSGW_ACK_NEW_CONN_WITH_CONN_ID=true`, the first WS text frame the client receives after upgrade is `{"connectionId":"<id>"}`. Clients that need the ID for later out-of-band correlation should read this frame before processing application traffic.
- **Per-connection rate limiting** — incoming client frames are rate-limited at 1 msg / 100 ms with a burst of 8, with a 1024-message buffer. Sustained overload causes the backend's `POST /message/{id}` to receive `503`.

## Configuration

All configuration is via environment variables, prefixed `WSGW_`.

| Variable | Default | Description |
|---|---|---|
| `WSGW_SERVER_HOST` | `""` (all interfaces) | Bind address. |
| `WSGW_SERVER_PORT` | — | Listening port. **Required.** |
| `WSGW_APP_BASE_URL` | — | Base URL of the backend (e.g. `http://app:8080`). **Required.** |
| `WSGW_HTTP2` | `false` | Enable H2C between wsgw and the backend. |
| `WSGW_ACK_NEW_CONN_WITH_CONN_ID` | `false` | Send the connect-ack frame after upgrade. |
| `WSGW_LOAD_BALANCER_ADDRESS` | `""` | Allowed `Origin` for the WS handshake. *Slated for removal.* |
| `WSGW_OTLP_ENDPOINT` | `""` | OTLP/HTTP exporter endpoint for traces & metrics. Empty disables OTel export. |
| `WSGW_OTLP_SERVICE_NAMESPACE` | `""` | OTel `service.namespace` resource attribute. |
| `WSGW_OTLP_SERVICE_NAME` | `wsgw` | OTel `service.name` resource attribute. |
| `WSGW_OTLP_SERVICE_INSTANCE_ID` | hostname | OTel `service.instance.id` resource attribute. |
| `WSGW_OTLP_TRACE_SAMPLE_ALL` | `false` | Sample every trace (otherwise the SDK default). |

## Observability

wsgw is instrumented with OpenTelemetry traces and metrics, exported via OTLP/HTTP (set `WSGW_OTLP_ENDPOINT`). Notable metrics include active connections, deliveries, read/write errors, and per-connection backpressure. Traces cover the connect, push, and disconnect paths.

Logs are structured JSON via zerolog. A LogQL example for the [`test/e2e/`](test/e2e/) harness:

```
{filename="/mnt/workspace/logs/e2e-app"} | json | line_format `{{.time}} [{{.method}}] {{.message}} {{.req_url}}`
```

## Deployment

Reference Kubernetes manifests live in [`deploy/k8s/`](deploy/k8s/). The `image` and `k8s` tasks in [`taskfile.yaml`](taskfile.yaml) build the container image and apply the manifests against a local cluster (minikube friendly).

## Reference implementations

Two working backends demonstrate the contract end-to-end:

- [`test/mockapp/`](test/mockapp/) — Go, used by the integration suite. Useful as a precise spec.
- [`pdkovacs/wsgw-node-ref`](https://github.com/pdkovacs/wsgw-node-ref) — Node.js companion repo. More accessible if you're not coming from Go.

## Non-goals

- **Authentication.** Delegated entirely to the backend's `/ws/connect`.
- **TLS termination.** Expected to be handled by a load balancer or sidecar.
- **Message persistence or delivery guarantees.** Frames not delivered to the WebSocket (closed connection, overloaded buffer) surface as HTTP errors to the backend; retry/durability is the backend's concern.
- **Horizontal scaling of wsgw itself.** wsgw is intended to run as a single instance per application. Scale the application; treat wsgw as a small piece of stateful glue.

## Status

Early-stage. The wire contract is stable enough for the integration and e2e suites; expect breaking changes elsewhere. Built with Go 1.25. Released under the MIT License — see [LICENSE](LICENSE).
