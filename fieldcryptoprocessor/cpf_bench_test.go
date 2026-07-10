package fieldcryptoprocessor

import "testing"

// BenchmarkIsValidCPF exercises a mix of inputs. The all-same-digit and wrong-length
// inputs are rejected by hasCPFShape (STAGE 1) BEFORE any modulo-11 arithmetic runs
// (STAGE 2) — only "529.982.247-25" reaches validChecksum. This is the whole point of
// the two-stage design: the cheap format gate short-circuits implausible candidates.
func BenchmarkIsValidCPF(b *testing.B) {
	mix := []string{
		"529.982.247-25", // valid -> runs stage 1 + stage 2
		"111.111.111-11", // all-same-digit -> stage 1 rejects, NO arithmetic
		"529.982.247",    // too short -> stage 1 rejects, NO arithmetic
		"123.456.789-00", // wrong verifier -> stage 1 passes, stage 2 rejects
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range mix {
			_ = isValidCPF(c)
		}
	}
}
