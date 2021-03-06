# Configuration Format

**Note:** This is not the final configuration format and is subject to change at
any time.

## Example `monit` Configuration

bpm still sits on top of `monit` as part of the current BOSH job API.
However, the contents of the `monit` file now become simpler and less variable.
The amount of features used is minimized. BOSH would like to remove support
for `monit` eventually and so reducing the exposed feature area will make this
easier.

```
check process <job>
  with pidfile /var/vcap/sys/run/bpm/<job>/<job>.pid
  start program "/var/vcap/jobs/bpm/bin/bpm start <job>"
  stop program "/var/vcap/jobs/bpm/bin/bpm stop <job>"
  group vcap

check process <job>-<worker>
  with pidfile /var/vcap/sys/run/bpm/<job>/<worker>.pid
  start program "/var/vcap/jobs/bpm/bin/bpm start <job> -p <worker>"
  stop program "/var/vcap/jobs/bpm/bin/bpm stop <job> -p <worker>"
  group vcap
```

## Job Configuration

``` yaml
# /var/vcap/jobs/<job>/config/bpm.yml
processes:
  <job>:
    executable: /var/vcap/packages/program/bin/program-server
    args:
    - --port
    - 2424
    env:
      - FOO=BAR
    limits:
      memory: 3G
      processes: 10
      open_files: 100
    volumes:
    - /var/vcap/data/certificates
    hooks:
      pre_start: /var/vcap/jobs/<job>/bin/bpm-pre-start
  <worker>:
    executable: /var/vcap/packages/program/bin/program-worker
    args:
    - --queues
    - 4
    volumes:
    - name: /var/vcap/data/sockets
    hooks:
      pre_start: /var/vcap/jobs/<job>/bin/initialize
```

**Note:** The value of the `args:` are passed literally to the `executable:`.
Consider the following snippet:

```yaml
executable: /path/to/command

args:
- --some-flag="flag-value"
```

The value of `--some-flag` will be the string `"flag-value"` including quotes.

## Setting Sysctl Kernel Parameters

We recommend setting these parameters in your BOSH `pre-start` with the
following command:

```bash
sysctl -e -w net.ipv4.tcp_fin_timeout 10
sysctl -e -w net.ipv4.tcp_tw_reuse 1
```

You could set these in your bpm `pre_start` but since these affect the entire
host and not just the contained job we like to keep them separate.

## Hooks

Your startup hook must finish with time to spare before the `monit start`
timeout (30s by default). We're looking into ways to make this less vague.
