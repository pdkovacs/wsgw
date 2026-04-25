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

### Three test app instances with multiple users with a single websocket connection each

Delivering 

* 128 messages to 12k destinations in total takes about 3 seconds

    ```
    time curl -i -u 'user1:crixcrax1' -X POST 'http://wsgw-e2e-client.internal/run?timeout=5m&user-count=128'
    ```

    ![alt text](image.png)

* 256 messages to 47k destinations in total takes about 1 minute 10 seconds

    ```
    time curl -i -u 'user1:crixcrax1' -X POST 'http://wsgw-e2e-client.internal/run?timeout=5m&user-count=256'

    ```

    ![alt text](image-1.png)

Loads having 512 messages to 195k destinations and larger require a large
total number of sockets where the limitations of stress testing on a local
machine with minikube start to show by failures to create connections with
the default range for ephemeral sockets most in the test application.
Increasing the the number of application instances helps. Here are some
results with 16 app instances:

* 512 messages to 193k destinations:

![alt text](image-2.png)

The time spent in WSGW as measured "inside" is still in the order of tens of microseconds.

----

Note

The histograms show durations which start when the test application is very close to having sent the push request
and end when the test client actually receives the message. The durations measurable in the Websocket Gateway are much shorter.
The following sample is fairly representative of the proportions between durations measured "inside" and "outside":

![alt text](image-6.png)

----

#### MILESTONE-2

With

* HTTP/2
* a few fixes

Random messages from 1024 users to ~780k destinations in total takes about 1 minute 15 seconds

```
time curl -i -u 'user1:crixcrax1' -X POST 'http://wsgw-e2e-client.internal/run?timeout=5m&user-count=1024'

```

##### Postgres as tracking store

![alt text](image.png)

* wsgw.push-message milliseconds:  
   n=102400 min=0.01 p50=48.13 mean=98.84 p95=375.02 p99=613.72 max=1554.43

##### Valkey as tracking store

![alt text](image-3.png)

* wsgw.push-message milliseconds:  
   n=102400 min=0.01 p50=58.85 mean=110.32 p95=401.04 p99=637.97 max=1833.89

## Tools

### Poor man's statistics

#### Extract durations from Grafana Tempo response

```
spanname="push-message"
jq -r '
  .traces[].spanSets[].spans[]
  | select(.name == "push-message")
  | (.durationNanos | tonumber) / 1e6
' response.json > durations_ms.txt
```

#### Create one-liner stats

```
python3 -c '
import sys, statistics
xs=[float(x) for x in sys.stdin]
xs_s=sorted(xs)
n=len(xs)
print(f"n={n} min={min(xs):.2f} p50={statistics.median(xs):.2f} \
mean={statistics.mean(xs):.2f} p95={xs_s[int(n*0.95)]:.2f} \
p99={xs_s[int(n*0.99)]:.2f} max={max(xs):.2f}")
' < durations_ms.txt
```

### Image file clean-up

Eyeball first:

```
comm -23 \
  <(ls -1 test/e2e/doc/image*.png | sed -nE 's|test/e2e/doc/||p' | sort) \
  <(sed -nE 's/[[:space:]]*!\[alt.text[^)]*(image-[0-9]+\.png)\)/\1/gp' test/e2e/doc/DONE.md | sort)
```

Do the cleanup:

```
cd test/e2e/doc
comm -23 <(ls -1 image*.png | sort) \
         <(sed -nE 's/[[:space:]]*!\[alt.text[^)]*(image-[0-9]+\.png)\)/\1/gp' DONE.md | sort) \
| xargs -r rm
```
