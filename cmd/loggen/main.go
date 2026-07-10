// cmd/loggen — a random OTLP log generator for smoke-testing the fieldcrypto processor.
//
// It emits logs that contain a realistic mix of sensitive data so you can verify the
// processor's masking and encryption end to end:
//   - a body string that may embed a VALID CPF or an invalid-but-CPF-shaped number
//   - a user.document attribute (a valid CPF, unformatted)
//   - a user.card attribute (a 16-digit number)
//   - an email, plus benign noise attributes
//
// generated CPFs are genuinely valid: randomValidCPF computes the two verifier digits
// with the same modulo-11 algorithm the processor uses to validate them. At the end it
// prints a summary so smoke-test expectations are known up front.
//
// Usage:
//
//	loggen --endpoint localhost:4318 --protocol http --count 100 --rate 20 \
//	       --valid-cpf-pct 50 --seed 1
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type summary struct {
	total       int
	validCPFs   int
	invalidCPFs int
	emails      int
	documents   int
	cards       int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "loggen: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		endpoint    = flag.String("endpoint", "localhost:4318", "OTLP endpoint host:port")
		protocol    = flag.String("protocol", "http", "http or grpc")
		count       = flag.Int("count", 50, "number of log records to send")
		rate        = flag.Float64("rate", 10, "logs per second (<=0 means as fast as possible)")
		validCPFPct = flag.Int("valid-cpf-pct", 50, "percent of logs whose body embeds a VALID CPF")
		seed        = flag.Int64("seed", time.Now().UnixNano(), "PRNG seed for reproducible runs")
	)
	flag.Parse()

	rng := rand.New(rand.NewSource(*seed))
	ctx := context.Background()

	send, closeFn, err := newSender(ctx, *protocol, *endpoint)
	if err != nil {
		return err
	}
	defer closeFn()

	var interval time.Duration
	if *rate > 0 {
		interval = time.Duration(float64(time.Second) / *rate)
	}

	var s summary
	for i := 0; i < *count; i++ {
		ld := buildLog(rng, *validCPFPct, &s)
		if err := send(ctx, ld); err != nil {
			return fmt.Errorf("send log %d: %w", i, err)
		}
		s.total++
		if interval > 0 {
			time.Sleep(interval)
		}
	}

	fmt.Printf("sent %d logs (seed=%d): valid_cpf=%d invalid_shaped=%d emails=%d documents=%d cards=%d\n",
		s.total, *seed, s.validCPFs, s.invalidCPFs, s.emails, s.documents, s.cards)
	return nil
}

// buildLog creates a single-record plog.Logs and updates the summary counters.
func buildLog(rng *rand.Rand, validCPFPct int, s *summary) plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "loggen")
	lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	lr.SetSeverityText("INFO")

	// Body: embed a valid or invalid-shaped CPF, or neither.
	switch {
	case rng.Intn(100) < validCPFPct:
		cpf := randomValidCPF(rng)
		lr.Body().SetStr("customer CPF " + cpf + " completed checkout")
		s.validCPFs++
	case rng.Intn(2) == 0:
		lr.Body().SetStr("rejected CPF " + randomInvalidCPFShaped(rng) + " on submit")
		s.invalidCPFs++
	default:
		lr.Body().SetStr("routine event, no PII")
	}

	attrs := lr.Attributes()
	// user.document — a valid, unformatted CPF (target for encryption).
	if rng.Intn(2) == 0 {
		attrs.PutStr("user.document", digitsOnly(randomValidCPF(rng)))
		s.documents++
	}
	// user.card — a 16-digit card-like number (target for encryption).
	if rng.Intn(2) == 0 {
		attrs.PutStr("user.card", randomDigits(rng, 16))
		s.cards++
	}
	// email — target for whole-value masking.
	if rng.Intn(2) == 0 {
		attrs.PutStr("user.email", randomEmail(rng))
		s.emails++
	}
	// benign noise
	attrs.PutStr("http.method", []string{"GET", "POST", "PUT"}[rng.Intn(3)])
	attrs.PutInt("http.status_code", int64([]int{200, 201, 400, 500}[rng.Intn(4)]))
	return ld
}

// ─── senders ───────────────────────────────────────────────────────────────────────

type sendFunc func(ctx context.Context, ld plog.Logs) error

func newSender(ctx context.Context, protocol, endpoint string) (sendFunc, func(), error) {
	switch protocol {
	case "http":
		url := "http://" + endpoint + "/v1/logs"
		client := &http.Client{Timeout: 10 * time.Second}
		send := func(ctx context.Context, ld plog.Logs) error {
			req := plogotlp.NewExportRequestFromLogs(ld)
			body, err := req.MarshalProto()
			if err != nil {
				return err
			}
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return err
			}
			httpReq.Header.Set("Content-Type", "application/x-protobuf")
			resp, err := client.Do(httpReq)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 300 {
				return fmt.Errorf("unexpected status %s", resp.Status)
			}
			return nil
		}
		return send, func() {}, nil

	case "grpc":
		conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		client := plogotlp.NewGRPCClient(conn)
		send := func(ctx context.Context, ld plog.Logs) error {
			_, err := client.Export(ctx, plogotlp.NewExportRequestFromLogs(ld))
			return err
		}
		return send, func() { _ = conn.Close() }, nil

	default:
		return nil, nil, fmt.Errorf("invalid --protocol %q: use http or grpc", protocol)
	}
}

// ─── random generators ──────────────────────────────────────────────────────────────

// randomValidCPF generates 9 random digits then COMPUTES the two verifier digits with
// the modulo-11 algorithm, so the result is a genuinely valid CPF (formatted).
func randomValidCPF(rng *rand.Rand) string {
	var d [11]int
	// Avoid the all-same-digit case (invalid despite a matching checksum).
	for {
		for i := 0; i < 9; i++ {
			d[i] = rng.Intn(10)
		}
		same := true
		for i := 1; i < 9; i++ {
			if d[i] != d[0] {
				same = false
				break
			}
		}
		if !same {
			break
		}
	}
	d[9] = verifier(d[:9], 10)
	d[10] = verifier(d[:10], 11)
	return fmt.Sprintf("%d%d%d.%d%d%d.%d%d%d-%d%d",
		d[0], d[1], d[2], d[3], d[4], d[5], d[6], d[7], d[8], d[9], d[10])
}

// randomInvalidCPFShaped produces an 11-digit number that FAILS the checksum (and is
// not all-same-digit), so the processor should leave it intact.
func randomInvalidCPFShaped(rng *rand.Rand) string {
	var d [11]int
	for i := 0; i < 9; i++ {
		d[i] = rng.Intn(10)
	}
	v1 := verifier(d[:9], 10)
	v2 := verifier(append(d[:9:9], v1), 11)
	// Deliberately corrupt the first verifier digit.
	d[9] = (v1 + 1) % 10
	d[10] = v2
	return fmt.Sprintf("%d%d%d.%d%d%d.%d%d%d-%d%d",
		d[0], d[1], d[2], d[3], d[4], d[5], d[6], d[7], d[8], d[9], d[10])
}

// verifier computes one modulo-11 verifier digit over digits with descending weights
// starting at startWeight.
func verifier(digits []int, startWeight int) int {
	sum := 0
	w := startWeight
	for _, dv := range digits {
		sum += dv * w
		w--
	}
	r := sum % 11
	if r < 2 {
		return 0
	}
	return 11 - r
}

func digitsOnly(formatted string) string {
	out := make([]byte, 0, len(formatted))
	for i := 0; i < len(formatted); i++ {
		if formatted[i] >= '0' && formatted[i] <= '9' {
			out = append(out, formatted[i])
		}
	}
	return string(out)
}

func randomDigits(rng *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('0' + rng.Intn(10))
	}
	return string(b)
}

func randomEmail(rng *rand.Rand) string {
	names := []string{"alice", "bob", "carol", "dave", "erin"}
	domains := []string{"example.com", "test.org", "acme.io"}
	return fmt.Sprintf("%s%d@%s", names[rng.Intn(len(names))], rng.Intn(1000), domains[rng.Intn(len(domains))])
}
