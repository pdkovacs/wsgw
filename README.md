# Web-socket gateway

Service taking care of the management (and use) of stateful web-socket connections on behalf of clustered applications with with stateless back-ends.

## Endpoints provided by the gateway

* `GET /connect`
  
  For client devices to open a web-socket connection.

  The gateway sends a text message of the form of `{ connectionId: string }` over
  the just opened websocket connection to inform the client of the connection-id assigned
  by the proxy to the connection.

* `POST /message/${connectionId}`

  For application back-ends to send message over a web-socket connection

  (Client devices send messages to the back-ends using the web-socket connections between them and the gateway.)

## Endpoints the gateway expects the application to provide

* `GET /ws/connect`

  The gateway relays to this endpoint all requests coming in
  at its `GET /connect` end-point as they are. This endpoint
  is expected to authenticate the requests and return HTTP status `200` in the case of successful authentication (HTTP 401 in case of unsuccessful authentication).

* `POST /ws/disconnected`

  The gateway notifies the application of connections lost via this end-point on a best-effort basis.

* `POST /ws/message`

  The gateway relays to this end-point messages it receives from clients

# Operation

## Logging

Sample LogQL:

```
{filename="/mnt/workspace/logs/e2e-app"} | json | line_format `{{.time}} [{{.method}}] {{.message}} {{.req_url}}`
```
