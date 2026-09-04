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
	base, err := parseEnvoyDuration(e.RetryBackOff.BaseInterval)
	if err != nil {
		return Policy{}, fmt.Errorf("base_interval: %w", err)
	}
	max, err := parseEnvoyDuration(e.RetryBackOff.MaxInterval)
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
	e.RetryBackOff.BaseInterval = formatEnvoyDuration(p.InitialDelay)
	e.RetryBackOff.MaxInterval = formatEnvoyDuration(p.MaxDelay)
	return e
}

// parseEnvoyDuration parses the string form of a google.protobuf.Duration
// as Envoy renders it in JSON: a decimal number of seconds with a
// trailing "s", e.g. "0.1s" or "20s".
func parseEnvoyDuration(s string) (time.Duration, error) {
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

func formatEnvoyDuration(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + "s"
}
