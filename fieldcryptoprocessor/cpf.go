// cpf.go — EXPLICIT TWO-STAGE CPF validation.
//
// A CPF (Cadastro de Pessoas Físicas) is the Brazilian individual taxpayer id. It has
// 11 digits, the last two being modulo-11 verifier digits. Validating one naively runs
// the (relatively expensive) weighted-sum arithmetic on every candidate. This file
// splits the work into two stages so the arithmetic only runs on plausible candidates:
//
//	Stage 1 — cheap format gate (hasCPFShape): strip non-digits, then require exactly
//	          11 digits that are NOT all identical. This short-circuits obvious junk
//	          (wrong length, "111.111.111-11", etc.) without any multiplication.
//	Stage 2 — checksum (validChecksum): only runs when stage 1 passes. It recomputes
//	          both verifier digits with the weighted modulo-11 algorithm.
//
// The candidate regex is the OUTER filter used when scanning free text; hasCPFShape is
// the INNER guard so the modulo arithmetic never runs on implausible candidates.
package fieldcryptoprocessor

import (
	"regexp"
	"strings"
)

// cpfCandidateRegex matches CPF-SHAPED substrings in free text. It intentionally matches
// invalid numbers too; validity is decided by isValidCPF afterwards.
var cpfCandidateRegex = regexp.MustCompile(`\b(\d{3}[.\-]?\d{3}[.\-]?\d{3}[.\-]?\d{2})\b`)

// stripNonDigits removes everything that is not 0-9. Cheap and allocation-light.
func stripNonDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// hasCPFShape is STAGE 1: the cheap format gate. Returns true only if the digit string
// is exactly 11 digits long AND not all 11 digits are identical (all-same-digit strings
// pass the checksum math but are never valid CPFs, so we reject them here up front).
func hasCPFShape(digits string) bool {
	if len(digits) != 11 {
		return false
	}
	for i := 1; i < 11; i++ {
		if digits[i] != digits[0] {
			return true // found a differing digit -> plausible shape
		}
	}
	return false // all 11 digits identical
}

// validChecksum is STAGE 2: the weighted modulo-11 verifier-digit calculation. It assumes
// digits is exactly 11 numeric characters (guaranteed by hasCPFShape running first).
func validChecksum(digits string) bool {
	// First verifier digit: weights 10..2 over digits 0..8.
	sum := 0
	for i := 0; i < 9; i++ {
		sum += int(digits[i]-'0') * (10 - i)
	}
	v1 := sum % 11
	if v1 < 2 {
		v1 = 0
	} else {
		v1 = 11 - v1
	}
	if v1 != int(digits[9]-'0') {
		return false
	}

	// Second verifier digit: weights 11..2 over digits 0..9.
	sum = 0
	for i := 0; i < 10; i++ {
		sum += int(digits[i]-'0') * (11 - i)
	}
	v2 := sum % 11
	if v2 < 2 {
		v2 = 0
	} else {
		v2 = 11 - v2
	}
	return v2 == int(digits[10]-'0')
}

// isValidCPF returns stage1 && stage2. Stage 1 short-circuits so stage 2's arithmetic
// only runs on plausible candidates.
func isValidCPF(candidate string) bool {
	digits := stripNonDigits(candidate)
	if !hasCPFShape(digits) { // STAGE 1
		return false
	}
	return validChecksum(digits) // STAGE 2
}
