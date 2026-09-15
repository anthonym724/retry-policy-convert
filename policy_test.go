package main

import (
	"testing"
	"time"
)

func TestFromAWS(t *testing.T) {
	cases := []struct {
		name string
		in   awsPolicy
		want Policy
	}{
		{
			name: "standard mode has no connect-failure retry",
			in: awsPolicy{
				MaxAttempts: 3,
				RetryMode:   "standard",
			},
			want: Policy{
				MaxAttempts:           3,
				RetryOnServerErrors:   true,
				RetryOnConnectFailure: false,
			},
		},
		{
			name: "adaptive mode retries connect failures too",
			in: awsPolicy{
				MaxAttempts: 5,
				RetryMode:   "adaptive",
			},
			want: Policy{
				MaxAttempts:           5,
				RetryOnServerErrors:   true,
				RetryOnConnectFailure: true,
			},
		},
		{
			name: "backoff delays convert from milliseconds",
			in: awsPolicy{
				MaxAttempts: 1,
				RetryMode:   "standard",
				Backoff: struct {
					BaseDelayMs int `json:"base_delay_ms"`
					MaxDelayMs  int `json:"max_delay_ms"`
				}{BaseDelayMs: 100, MaxDelayMs: 20000},
			},
			want: Policy{
				MaxAttempts:         1,
				InitialDelay:        100 * time.Millisecond,
				MaxDelay:            20 * time.Second,
				RetryOnServerErrors: true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fromAWS(tc.in)
			if got != tc.want {
				t.Errorf("fromAWS(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestToAWS(t *testing.T) {
	cases := []struct {
		name string
		in   Policy
		want awsPolicy
	}{
		{
			name: "connect failure retry maps to adaptive mode",
			in: Policy{
				MaxAttempts:           4,
				RetryOnConnectFailure: true,
			},
			want: awsPolicy{MaxAttempts: 4, RetryMode: "adaptive"},
		},
		{
			name: "no connect failure retry maps to standard mode",
			in: Policy{
				MaxAttempts:           4,
				RetryOnConnectFailure: false,
			},
			want: awsPolicy{MaxAttempts: 4, RetryMode: "standard"},
		},
		{
			name: "delays convert to whole milliseconds",
			in: Policy{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     20 * time.Second,
			},
			want: awsPolicy{
				RetryMode: "standard",
				Backoff: struct {
					BaseDelayMs int `json:"base_delay_ms"`
					MaxDelayMs  int `json:"max_delay_ms"`
				}{BaseDelayMs: 100, MaxDelayMs: 20000},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toAWS(tc.in)
			if got != tc.want {
				t.Errorf("toAWS(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFromEnvoy(t *testing.T) {
	cases := []struct {
		name    string
		in      envoyPolicy
		want    Policy
		wantErr bool
	}{
		{
			name: "num_retries is one less than max_attempts",
			in: envoyPolicy{
				RetryOn:    "5xx,connect-failure",
				NumRetries: 2,
				RetryBackOff: struct {
					BaseInterval string `json:"base_interval"`
					MaxInterval  string `json:"max_interval"`
				}{BaseInterval: "0.1s", MaxInterval: "20s"},
			},
			want: Policy{
				MaxAttempts:           3,
				InitialDelay:          100 * time.Millisecond,
				MaxDelay:              20 * time.Second,
				RetryOnServerErrors:   true,
				RetryOnConnectFailure: true,
			},
		},
		{
			name: "retry_on with only one condition",
			in: envoyPolicy{
				RetryOn:    "connect-failure",
				NumRetries: 0,
				RetryBackOff: struct {
					BaseInterval string `json:"base_interval"`
					MaxInterval  string `json:"max_interval"`
				}{BaseInterval: "0s", MaxInterval: "0s"},
			},
			want: Policy{
				MaxAttempts:           1,
				RetryOnServerErrors:   false,
				RetryOnConnectFailure: true,
			},
		},
		{
			name: "invalid base_interval is an error",
			in: envoyPolicy{
				RetryBackOff: struct {
					BaseInterval string `json:"base_interval"`
					MaxInterval  string `json:"max_interval"`
				}{BaseInterval: "not-a-duration", MaxInterval: "0s"},
			},
			wantErr: true,
		},
		{
			name: "invalid max_interval is an error",
			in: envoyPolicy{
				RetryBackOff: struct {
					BaseInterval string `json:"base_interval"`
					MaxInterval  string `json:"max_interval"`
				}{BaseInterval: "0s", MaxInterval: "not-a-duration"},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fromEnvoy(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("fromEnvoy(%+v) = nil error, want one", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("fromEnvoy(%+v) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("fromEnvoy(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestToEnvoy(t *testing.T) {
	cases := []struct {
		name string
		in   Policy
		want envoyPolicy
	}{
		{
			name: "both conditions produce a comma-joined retry_on",
			in: Policy{
				MaxAttempts:           3,
				RetryOnServerErrors:   true,
				RetryOnConnectFailure: true,
			},
			want: envoyPolicy{
				RetryOn:    "5xx,connect-failure",
				NumRetries: 2,
			},
		},
		{
			name: "no conditions produce an empty retry_on",
			in: Policy{
				MaxAttempts: 1,
			},
			want: envoyPolicy{
				RetryOn:    "",
				NumRetries: 0,
			},
		},
		{
			name: "max_attempts of zero does not go negative",
			in: Policy{
				MaxAttempts: 0,
			},
			want: envoyPolicy{
				NumRetries: 0,
			},
		},
		{
			name: "delays format as second strings",
			in: Policy{
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     20 * time.Second,
			},
			want: envoyPolicy{
				RetryBackOff: struct {
					BaseInterval string `json:"base_interval"`
					MaxInterval  string `json:"max_interval"`
				}{BaseInterval: "0.1s", MaxInterval: "20s"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toEnvoy(tc.in)
			if got != tc.want {
				t.Errorf("toEnvoy(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFromGRPC(t *testing.T) {
	cases := []struct {
		name    string
		in      grpcPolicy
		want    Policy
		wantErr bool
	}{
		{
			name: "both status codes map to both retry conditions",
			in: grpcPolicy{
				MaxAttempts:          4,
				InitialBackoff:       "0.1s",
				MaxBackoff:           "1s",
				BackoffMultiplier:    2,
				RetryableStatusCodes: []string{"INTERNAL", "UNAVAILABLE"},
			},
			want: Policy{
				MaxAttempts:           4,
				InitialDelay:          100 * time.Millisecond,
				MaxDelay:              1 * time.Second,
				RetryOnServerErrors:   true,
				RetryOnConnectFailure: true,
			},
		},
		{
			name: "unrecognized status codes are ignored",
			in: grpcPolicy{
				MaxAttempts:          2,
				InitialBackoff:       "0s",
				MaxBackoff:           "0s",
				RetryableStatusCodes: []string{"DEADLINE_EXCEEDED", "RESOURCE_EXHAUSTED"},
			},
			want: Policy{
				MaxAttempts: 2,
			},
		},
		{
			name: "invalid initialBackoff is an error",
			in: grpcPolicy{
				InitialBackoff: "not-a-duration",
				MaxBackoff:     "0s",
			},
			wantErr: true,
		},
		{
			name: "invalid maxBackoff is an error",
			in: grpcPolicy{
				InitialBackoff: "0s",
				MaxBackoff:     "not-a-duration",
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fromGRPC(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("fromGRPC(%+v) = nil error, want one", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("fromGRPC(%+v) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("fromGRPC(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestToGRPC(t *testing.T) {
	cases := []struct {
		name string
		in   Policy
		want grpcPolicy
	}{
		{
			name: "both conditions produce both status codes",
			in: Policy{
				MaxAttempts:           4,
				InitialDelay:          100 * time.Millisecond,
				MaxDelay:              1 * time.Second,
				RetryOnServerErrors:   true,
				RetryOnConnectFailure: true,
			},
			want: grpcPolicy{
				MaxAttempts:          4,
				InitialBackoff:       "0.1s",
				MaxBackoff:           "1s",
				BackoffMultiplier:    2,
				RetryableStatusCodes: []string{"INTERNAL", "UNAVAILABLE"},
			},
		},
		{
			name: "no conditions produce no status codes",
			in: Policy{
				MaxAttempts: 1,
			},
			want: grpcPolicy{
				MaxAttempts:       1,
				InitialBackoff:    "0s",
				MaxBackoff:        "0s",
				BackoffMultiplier: 2,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toGRPC(tc.in)
			if got.MaxAttempts != tc.want.MaxAttempts ||
				got.InitialBackoff != tc.want.InitialBackoff ||
				got.MaxBackoff != tc.want.MaxBackoff ||
				got.BackoffMultiplier != tc.want.BackoffMultiplier ||
				!slicesEqual(got.RetryableStatusCodes, tc.want.RetryableStatusCodes) {
				t.Errorf("toGRPC(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseProtoDuration(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{name: "whole seconds", in: "20s", want: 20 * time.Second},
		{name: "fractional seconds", in: "0.1s", want: 100 * time.Millisecond},
		{name: "zero", in: "0s", want: 0},
		{name: "surrounding whitespace is trimmed", in: "  5s  ", want: 5 * time.Second},
		{name: "missing s suffix is an error", in: "20", wantErr: true},
		{name: "non-numeric value is an error", in: "abcs", wantErr: true},
		{name: "empty string is an error", in: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProtoDuration(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseProtoDuration(%q) = %v, nil error, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProtoDuration(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseProtoDuration(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatProtoDuration(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "whole seconds", in: 20 * time.Second, want: "20s"},
		{name: "fractional seconds", in: 100 * time.Millisecond, want: "0.1s"},
		{name: "zero", in: 0, want: "0s"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatProtoDuration(tc.in); got != tc.want {
				t.Errorf("formatProtoDuration(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestProtoDurationRoundTrip(t *testing.T) {
	durations := []time.Duration{0, 100 * time.Millisecond, 20 * time.Second, 90 * time.Minute}
	for _, d := range durations {
		s := formatProtoDuration(d)
		got, err := parseProtoDuration(s)
		if err != nil {
			t.Fatalf("parseProtoDuration(%q) unexpected error: %v", s, err)
		}
		if got != d {
			t.Errorf("round trip through %q: got %v, want %v", s, got, d)
		}
	}
}
