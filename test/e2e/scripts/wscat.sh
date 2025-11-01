#!/bin/bash

userid=$1

while true; do
  wscat --auth "user${userid}:crixcrax${userid}" --connect ws://localhost:45679/connect
  echo "WebSocket connection closed. Reconnecting in 5 seconds..."
  sleep 5
done
