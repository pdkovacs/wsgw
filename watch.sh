#!/bin/bash
READLINK=$(which greadlink >/dev/null 2>&1 && echo greadlink || echo readlink)
fswatch_pid_file="$($READLINK -f "fswatch.pid")"

unset app_pid
unset fswatch_pid
unset stopping

kill_app() {
  if [ -n "$app_pid" ]; then
    kill -9 $app_pid && echo "killed app with pid $app_pid"
  fi
  if [ -n "$tail_pid" ]; then
    kill -9 $tail_pid && echo "killed tail with pid $tail_pid"
  fi
}

cleanup() {
  stopping=true
  kill_app
  if [ -n "$fswatch_pid" ]; then
    if kill -9 $fswatch_pid; then
      echo "killed fswatch with pid $fswatch_pid";
      rm $fswatch_pid_file
    fi
  fi
}

trap cleanup EXIT

build_and_run() {
  if task build; then
    "./cmd/${WSGW_EXEC_NAME}" >"$WSGW_LOG_FILE" 2>"$WSGW_LOG_FILE" &
    app_pid=$!
    tail -f "${WSGW_LOG_FILE}" &
    tail_pid=$!
  fi
}

while true; do
  kill_app
  build_and_run

  set -x
  fswatch -r -1 --event Created --event Updated --event Removed cmd internal &
  set +x
  fswatch_pid=$!
  echo $fswatch_pid >"$fswatch_pid_file"
  wait $fswatch_pid

  set -x
  [ "$stopping" = "true" ] && exit
  set +x
done 
