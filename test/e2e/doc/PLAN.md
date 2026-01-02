### Test-type codes

#### Scope

**I** — *integration tests*

   * always automated for now

**E** — *end-to-end tests*

   * complete with a web-client running a web-browser
   * always manual for now

#### Scalability

**S** — *single WSGW instance tests*

**C** — *clustered WSGW tests*

----

# Test cases

## Connection lifecycle

### Create WS connection

  * successful **(I + E)(S + C)**
  * failed due to
      * authentication failure **(I + E)(S + C)**

### Client reconnects after losing connection

Browsers should be able to reconnect to a/the WSGW instance after being disconnected for whatever reason. **E(S + C)**

### Application disconnect

  * successful **(I + E)(S + C)**



## Client sending and receiving messages

### Client sending

  * Single client sends message **(I + E)(S + C)**
  * Multiple clients send messages in parallel **(I + E)(S + C)**

### Client receiving

#### Messages to a single client

  * Single client recieves a message **(I + E)(S + C)**
  * Multiple clients receive one message each in parallel **(I + E)(S + C)**

#### Same message in multiple clients

  * Same message to multiple clients **(I + E)(S + C)**
  * Multiple messages each to multiple clients **(I + E)(S + C)**




# Considerations for testing clustered deployements

## Effectiveness

Testing clustered deployments is effective (compared to single instance deployments), if messages traverse WSGW instances (messages are passed between clients connected to different WSGW instances).

### Measuring effectiveness

Maintain a rate-type metric indicating the percentage of messages (sent or received by clients) traversering WSGW instances.

### Approaches to ensure effectiveness

#### Random message destinations with threshold

Define a mininum threshold for instance-traversing messages and have tests below this threshold fail — or repeat tests until threshold is reached.

#### "Hand-picked" message destinations to meet predefined traversal rate

1. Make sure the test components arre implemented such that
   1. the association of clients and their home WSGW-instance is tracked and
   2. the test driver can easly access this information
2. Construct the test input such that the rate of traversing messages is equal
   to a predefined value.
