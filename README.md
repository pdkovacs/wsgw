# Web-socket gateway

Service taking care of the management (and use) of stateful web-socket connections on behalf of clustered applications with with stateless back-ends.

## Endpoints provided by the proxy service

* `GET /connect`
  
  For client devices to open a web-socket connection.

  Returns `{ connectionId: string }`
  where `connectionId` is the connection-id assigned by the proxy to the new web-socket connection.

* `POST /message/${connectionId}`

  For application back-ends to send message over a web-socket connection

  (Client devices send messages to the back-ends using the web-socket connections between them and the proxy service.)

## Endpoints the proxy service expects the application to provide

* `GET /ws/connect`

  The proxy service relays to this endpoint all requests coming in
  at its `GET /connect` end-point as they are. This endpoint
  is expected to authenticate the requests and return HTTP status `200` in the case of successful authentication (HTTP 401 in case of unsuccessful authentication).

* `POST /ws/disconnected`

  The proxy service notifies the application of connections lost via this end-point on a best-effort basis.

* `POST /ws/message`

  The proxy service relays to this end-point messages it receives from clients
