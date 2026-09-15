package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Policy is the in-memory representation every supported format is
// translated through. Adding a third format only means writing two
// functions (toX/fromX) against this struct, not a new conversion
// path for every existing pair.
type Policy struct {
	MaxAttempts           int
	InitialDelay          time.Duration
	MaxDelay              time.Duration
	RetryOnServerErrors   bool
	RetryOnConnectFailure bool
}

// awsPolicy mirrors the retry section of an AWS SDK config, e.g. what
// aws.Config.Retryer produces when marshaled to JSON.
type awsPolicy struct {
	MaxAttempts int    `json:"max_attempts"`
	RetryMode   string `json:"retry_mode"`
	Backoff     struct {
		BaseDelayMs int `json:"base_delay_ms"`
		MaxDelayMs  int `json:"max_delay_ms"`
	} `json:"backoff"`
}

// envoyPolicy mirrors Envoy's route-level RetryPolicy JSON.
type envoyPolicy struct {
	RetryOn       string `json:"retry_on"`
	NumRetries    int    `json:"num_retries"`
	PerTryTimeout string `json:"per_try_timeout,omitempty"`
	RetryBackOff  struct {
		BaseInterval string `json:"base_interval"`
		MaxInterval  string `json:"max_interval"`
	} `json:"retry_back_off"`
}

// grpcPolicy mirrors the retryPolicy object nested in a methodConfig entry
// of a gRPC service config, as described in
// https://github.com/grpc/grpc/blob/master/doc/service_config.md. This is
// the retryPolicy object on its own, not the methodConfig wrapper around
// it, matching how awsPolicy and envoyPolicy skip their own wrappers.
type grpcPolicy struct {
	MaxAttempts          int      `json:"maxAttempts"`
	InitialBackoff       string   `json:"initialBackoff"`
	MaxBackoff           string   `json:"maxBackoff"`
	BackoffMultiplier    float64  `json:"backoffMultiplier"`
	RetryableStatusCodes []string `json:"retryableStatusCodes"`
}

func fromAWS(a awsPolicy) Policy {
	return Policy{
		MaxAttempts: a.MaxAttempts,
		InitialDelay: time.Duration(a.Backoff.BaseDelayMs) * time.Millisecond,
		MaxDelay:     time.Duration(a.Backoff.MaxDelayMs) * time.Millisecond,
		// AWS SDK retryers always cover 5xx/throttling responses; only
		// "adaptive" mode additionally backs off on connect failures.
		RetryOnServerErrors:   true,
		RetryOnConnectFailure: a.RetryMode == "adaptive",
	}
}

func toAWS(p Policy) awsPolicy {
	var a awsPolicy
	a.MaxAttempts = p.MaxAttempts
	if p.RetryOnConnectFailure {
		a.RetryMode = "adaptive"
	} else {
		a.RetryMode = "standard"
	}
	a.Backoff.BaseDelayMs = int(p.InitialDelay / time.Millisecond)
	a.Backoff.MaxDelayMs = int(p.MaxDelay / time.Millisecond)
	return a
}

func fromEnvoy(e envoyPolicy) (Policy, error) {
	base, err := parseProtoDuration(e.RetryBackOff.BaseInterval)
	if err != nil {
		return Policy{}, fmt.Errorf("base_interval: %w", err)
	}
	max, err := parseProtoDuration(e.RetryBackOff.MaxInterval)
	if err != nil {
		return Policy{}, fmt.Errorf("max_interval: %w", err)
	}
	return Policy{
		// Envoy's num_retries counts retries after the initial attempt.
		MaxAttempts:           e.NumRetries + 1,
		InitialDelay:          base,
		MaxDelay:              max,
		RetryOnServerErrors:   strings.Contains(e.RetryOn, "5xx"),
		RetryOnConnectFailure: strings.Contains(e.RetryOn, "connect-failure"),
	}, nil
}

func toEnvoy(p Policy) envoyPolicy {
	var conditions []string
	if p.RetryOnServerErrors {
		conditions = append(conditions, "5xx")
	}
	if p.RetryOnConnectFailure {
		conditions = append(conditions, "connect-failure")
	}

	var e envoyPolicy
	e.RetryOn = strings.Join(conditions, ",")
	e.NumRetries = p.MaxAttempts - 1
	if e.NumRetries < 0 {
		e.NumRetries = 0
	}
	e.RetryBackOff.BaseInterval = formatProtoDuration(p.InitialDelay)
	e.RetryBackOff.MaxInterval = formatProtoDuration(p.MaxDelay)
	return e
}

// defaultBackoffMultiplier is used whenever converting into gRPC's format.
// Neither AWS's nor Envoy's retry shapes expose a configurable backoff
// multiplier, and 2 is the doubling behavior both of them implement, so
// there's no better source for the value than a fixed default.
const defaultBackoffMultiplier = 2

// gRPC service config status codes representing the same two retry
// conditions AWS and Envoy expose. UNAVAILABLE is gRPC's status for a
// connection-level failure; INTERNAL is its closest analog to an AWS/Envoy
// server error response. Any other status code in a gRPC policy's
// retryableStatusCodes is dropped on the way into Policy, and no other
// code is ever added on the way out.
const (
	grpcServerErrorCode    = "INTERNAL"
	grpcConnectFailureCode = "UNAVAILABLE"
)

func fromGRPC(g grpcPolicy) (Policy, error) {
	initial, err := parseProtoDuration(g.InitialBackoff)
	if err != nil {
		return Policy{}, fmt.Errorf("initialBackoff: %w", err)
	}
	max, err := parseProtoDuration(g.MaxBackoff)
	if err != nil {
		return Policy{}, fmt.Errorf("maxBackoff: %w", err)
	}

	var serverErrors, connectFailure bool
	for _, code := range g.RetryableStatusCodes {
		switch code {
		case grpcServerErrorCode:
			serverErrors = true
		case grpcConnectFailureCode:
			connectFailure = true
		}
	}

	return Policy{
		MaxAttempts:           g.MaxAttempts,
		InitialDelay:          initial,
		MaxDelay:              max,
		RetryOnServerErrors:   serverErrors,
		RetryOnConnectFailure: connectFailure,
	}, nil
}

func toGRPC(p Policy) grpcPolicy {
	var codes []string
	if p.RetryOnServerErrors {
		codes = append(codes, grpcServerErrorCode)
	}
	if p.RetryOnConnectFailure {
		codes = append(codes, grpcConnectFailureCode)
	}

	return grpcPolicy{
		MaxAttempts:          p.MaxAttempts,
		InitialBackoff:       formatProtoDuration(p.InitialDelay),
		MaxBackoff:           formatProtoDuration(p.MaxDelay),
		BackoffMultiplier:    defaultBackoffMultiplier,
		RetryableStatusCodes: codes,
	}
}

// parseProtoDuration parses the string form of a google.protobuf.Duration,
// which both Envoy and gRPC service config use for their backoff interval
// fields: a decimal number of seconds with a trailing "s", e.g. "0.1s" or
// "20s".
func parseProtoDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "s") {
		return 0, fmt.Errorf("invalid duration %q: must end in \"s\"", s)
	}
	secs, err := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

func formatProtoDuration(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + "s"
}
