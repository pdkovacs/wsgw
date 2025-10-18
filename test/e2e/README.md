# End to end tests

The architecture diagram is available [here](https://github.com/pdkovacs/wsgw/tree/main/doc/design/diagrams).

## Test application

### Conversation support

Conversations (correlated messages) between the client and the server are supported to support test scenarios more complex than just sending unrelated messages from and to the client.

STOMP was considered, but, given its more general functionality, it is likely to create more overhead than needed.

The test adds a custom "overlay" data structure to the messages to identify a conversations:

```
{
    thread: {
        initiator: "cli" | "app";
        id: number;
        itemId: number
    }
}
```
.

### Message format

```
{
    thread: {
        initiator: "cli" | "app";
        id: number;
        itemId: number
    };
    payload: {
        messageType: string;
        data: any;
    };
}
```

### Message types accepted by the test application

* `generateMessageToRandomClients`
    
    data:
    
    ```
    { numberOfTargetClients: number }
    ```
    the number of clients to generate message to. The test application figures out the details and sends them to the client in an `expectMessages` type message.

### Message types sent by the test application

* `expectMessages`

    ```
    { clientIds: string[]; messageCommonPart: string }
    ```
    the app is going to send a message
    with the content of `${connectionId}-${messageCommonPart}-${clientId}` to the clients
    in the `clientIds` list.
