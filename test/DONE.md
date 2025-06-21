# Manual

## Single test app instance

### Multiple users with multiple websocket connections

1. logged-as two distinct users using two WEB-browsers to the test app
2. connected  to WSGW with the same two users with multiple `wscat` instances for each user
3. sent messages (including an incremented number) using a simple WEB-client
4. observed the messages coming in in browser console and in the `wscat` outputs
