# retry-policy-convert

Every system that retries failed requests describes the policy a little
differently. The AWS SDK writes retry config as `max_attempts` plus a
`retry_mode`; Envoy describes the same idea as `num_retries` plus a
`retry_back_off` with interval strings like `"20s"`. When you move a
service behind an Envoy sidecar, or read AWS retry settings back out of
a service mesh config, someone ends up translating attempt counts and
millisecond delays into Envoy's duration strings by hand. This tool
does that translation.

It currently supports three formats:

- `aws` — the retry section of an AWS SDK config
- `envoy` — Envoy's route-level `RetryPolicy`
- `grpc` — the `retryPolicy` object from a gRPC service config `methodConfig` entry

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

Output (`--to grpc`):

```json
{
  "maxAttempts": 3,
  "initialBackoff": "0.1s",
  "maxBackoff": "20s",
  "backoffMultiplier": 2,
  "retryableStatusCodes": [
    "INTERNAL",
    "UNAVAILABLE"
  ]
}
```

Note that `num_retries` is one less than `max_attempts`: AWS counts the
initial attempt, Envoy counts only the retries after it. gRPC's
`maxAttempts` counts the initial attempt too, matching AWS.

`grpc`'s `retryableStatusCodes` is a list of named gRPC status codes,
not a boolean per condition. This tool only recognizes two of them:
`UNAVAILABLE`, gRPC's status for a connection-level failure, and
`INTERNAL`, its closest analog to an AWS/Envoy server error response.
Any other status code in an input file is dropped, and no other code
is ever produced in output.

## Building

```
go build -o retry-policy-convert .
```

No third-party dependencies — standard library only.

## Status

Early. The mapping between formats is necessarily lossy in places: each
format's retry conditions carve up the space of "what's retryable"
differently (Envoy's `retry_on` values, AWS's retry modes, and gRPC's
per-status-code list don't line up one to one), and gRPC's
`backoffMultiplier` has no equivalent to read from when converting into
it, so it's always written as a fixed default.

## Roadmap

- Preserve fields a format doesn't share with `Policy` (e.g. Envoy's
  `per_try_timeout`) instead of dropping them on conversion.
- Add a `--validate` mode that only checks whether a file parses as a
  given format, without converting it.
- Package a release binary and add install instructions here.
