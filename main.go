// Command retry-policy-convert translates a retry policy between the
// JSON shapes used by different systems (currently the AWS SDK retry
// config and Envoy's route-level RetryPolicy), so migrating a service
// from one to the other doesn't mean re-deriving backoff numbers by hand.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

const usage = `retry-policy-convert --from FORMAT --to FORMAT [--input PATH] [--output PATH]

Formats: aws, envoy, grpc

--input and --output default to "-", meaning stdin/stdout. Either can
also be a file path.

Examples:
  cat aws-retry.json | retry-policy-convert --from aws --to envoy
  retry-policy-convert --from envoy --to aws --input in.json --output out.json
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "retry-policy-convert:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("retry-policy-convert", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	from := fs.String("from", "", "source format: aws or envoy")
	to := fs.String("to", "", "target format: aws or envoy")
	input := fs.String("input", "-", "input file, or - for stdin")
	output := fs.String("output", "-", "output file, or - for stdout")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		fs.Usage()
		return fmt.Errorf("--from and --to are required")
	}

	in, err := openInput(*input, stdin)
	if err != nil {
		return err
	}
	defer in.Close()

	raw, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	policy, err := decode(*from, raw)
	if err != nil {
		return fmt.Errorf("decoding %s input: %w", *from, err)
	}

	out, err := encode(*to, policy)
	if err != nil {
		return fmt.Errorf("encoding %s output: %w", *to, err)
	}

	w, closeFn, err := openOutput(*output, stdout)
	if err != nil {
		return err
	}
	defer closeFn()

	if _, err := w.Write(out); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

func decode(format string, raw []byte) (Policy, error) {
	switch format {
	case "aws":
		var a awsPolicy
		if err := json.Unmarshal(raw, &a); err != nil {
			return Policy{}, err
		}
		return fromAWS(a), nil
	case "envoy":
		var e envoyPolicy
		if err := json.Unmarshal(raw, &e); err != nil {
			return Policy{}, err
		}
		return fromEnvoy(e)
	case "grpc":
		var g grpcPolicy
		if err := json.Unmarshal(raw, &g); err != nil {
			return Policy{}, err
		}
		return fromGRPC(g)
	default:
		return Policy{}, fmt.Errorf("unknown format %q (want aws, envoy, or grpc)", format)
	}
}

func encode(format string, p Policy) ([]byte, error) {
	var v any
	switch format {
	case "aws":
		v = toAWS(p)
	case "envoy":
		v = toEnvoy(p)
	case "grpc":
		v = toGRPC(p)
	default:
		return nil, fmt.Errorf("unknown format %q (want aws, envoy, or grpc)", format)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func openInput(path string, stdin io.Reader) (io.ReadCloser, error) {
	if path == "-" {
		return io.NopCloser(stdin), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening input: %w", err)
	}
	return f, nil
}

func openOutput(path string, stdout io.Writer) (io.Writer, func() error, error) {
	if path == "-" {
		return stdout, func() error { return nil }, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("opening output: %w", err)
	}
	return f, f.Close, nil
}
