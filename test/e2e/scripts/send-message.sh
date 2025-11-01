#!/bin/bash

unamepwd="user2:crixcrax2"

user_count="$1"
message="$2"

user_list="["
for i in $(seq 1 "$user_count"); do
  [ $i -gt 1 ] && user_list="${user_list}, "
  user_list="${user_list}\"user${i}\""
done
user_list="${user_list}]"

data='{"whom":'"$user_list"',"what":"'"$message"'"}'

set -x
curl -i 'http://localhost:45678/api/message' -u "$unamepwd"  -d "$data"
set +x
