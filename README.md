# retry-policy-convert

Every system that retries failed requests describes the policy a little
differently. The AWS SDK writes retry config as `max_attempts` plus a
`retry_mode`; Envoy describes the same idea as `num_retries` plus a
`retry_back_off` with interval strings like `"20s"`. When you move a
service behind an Envoy sidecar, or read AWS retry settings back out of
a service mesh config, someone ends up translating attempt counts and
millisecond delays into Envoy's duration strings by hand. This tool
does that translation.

It currently supports two formats:

- `aws` — the retry section of an AWS SDK config
- `envoy` — Envoy's route-level `RetryPolicy`

## Usage

Read from a file, write to stdout:

```
retry-policy-convert --from aws --to envoy --input aws-retry.json
```

Read from stdin (useful in a pipeline), write to a file:

```
curl -s https://example.com/retry-config.json | \
  retry-policy-convert --from aws --to envoy --output envoy-retry.json
```

`--input` and `--output` both default to `-`, meaning stdin/stdout, so
the simplest invocation is just:

```
cat aws-retry.json | retry-policy-convert --from aws --to envoy
```

### Example

Input (`aws-retry.json`):

```json
{
  "max_attempts": 3,
  "retry_mode": "adaptive",
  "backoff": {
    "base_delay_ms": 100,
    "max_delay_ms": 20000
  }
}
```

Output (`--to envoy`):

```json
{
  "retry_on": "5xx,connect-failure",
  "num_retries": 2,
  "retry_back_off": {
    "base_interval": "0.1s",
    "max_interval": "20s"
  }
}
```

Note that `num_retries` is one less than `max_attempts`: AWS counts the
initial attempt, Envoy counts only the retries after it.

## Building

```
go build -o retry-policy-convert .
```

No third-party dependencies — standard library only.

## Status

Early. Only two formats are wired up and the mapping between them is
necessarily lossy in both directions (Envoy's `retry_on` conditions are
richer than AWS's retry modes, and vice versa for adaptive throttling
behavior). See the roadmap for what's next.
