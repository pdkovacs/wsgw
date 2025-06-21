# End to end tests

## Test application

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
