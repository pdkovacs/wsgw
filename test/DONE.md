# Single WebSocket Gateway instance

## Manual

### Single test app instance

#### Multiple users with multiple websocket connections each

1. logged in as two distinct users using two WEB-browsers to the test app
2. connected  to WSGW with the same two users with multiple `wscat` instances for each user
3. sent messages (including an incremented number) using a simple WEB-client
4. verified the messages coming in in browser console and in the `wscat` outputs

### Multiple test app instances

#### Single user with a simple WEB-client with a single websocket connection

This is really just for sanity testing the setup of multiple test-app instances

1. logged in with a simple WEB-client
3. sent messages to the logged in user
4. verified the messages coming in in browser console
5. verified looking at metrics in Grafana that incoming message requests
   are distributed evenly over the test application instances

## Automated

<!-- ### Multiple test app instances with multiple users with a single websocket connection each -->


