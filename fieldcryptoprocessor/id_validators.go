package fieldcryptoprocessor

import (
	"regexp"
	"strings"
)

var cnpjCandidateRegex = regexp.MustCompile(`\b(\d{2}[./-]?\d{3}[./-]?\d{3}[/-]?\d{4}[./-]?\d{2})\b`)

var ibanCandidateRegex = regexp.MustCompile(`(?i)\b[A-Z]{2}[0-9]{2}(?:[ -]?[A-Z0-9]{4}){2,7}(?:[ -]?[A-Z0-9]{1,4})?\b`)

func isValidCNPJ(candidate string) bool {
	digits := stripNonDigits(candidate)
	if !hasCNPJShape(digits) {
		return false
	}
	return validCNPJChecksum(digits)
}

func hasCNPJShape(digits string) bool {
	if len(digits) != 14 {
		return false
	}
	for i := 1; i < len(digits); i++ {
		if digits[i] != digits[0] {
			return true
		}
	}
	return false
}

func validCNPJChecksum(digits string) bool {
	w1 := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	w2 := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}

	sum := 0
	for i := 0; i < 12; i++ {
		sum += int(digits[i]-'0') * w1[i]
	}
	d1 := 11 - (sum % 11)
	if d1 >= 10 {
		d1 = 0
	}
	if d1 != int(digits[12]-'0') {
		return false
	}

	sum = 0
	for i := 0; i < 13; i++ {
		sum += int(digits[i]-'0') * w2[i]
	}
	d2 := 11 - (sum % 11)
	if d2 >= 10 {
		d2 = 0
	}
	return d2 == int(digits[13]-'0')
}

func isValidIBAN(candidate string) bool {
	norm := normalizeIBAN(candidate)
	if len(norm) < 15 || len(norm) > 34 {
		return false
	}
	if norm[0] < 'A' || norm[0] > 'Z' || norm[1] < 'A' || norm[1] > 'Z' {
		return false
	}
	if norm[2] < '0' || norm[2] > '9' || norm[3] < '0' || norm[3] > '9' {
		return false
	}

	rearranged := norm[4:] + norm[:4]
	mod := 0
	for i := 0; i < len(rearranged); i++ {
		ch := rearranged[i]
		switch {
		case ch >= '0' && ch <= '9':
			mod = (mod*10 + int(ch-'0')) % 97
		case ch >= 'A' && ch <= 'Z':
			v := int(ch-'A') + 10
			mod = (mod*10 + (v / 10)) % 97
			mod = (mod*10 + (v % 10)) % 97
		default:
			return false
		}
	}
	return mod == 1
}

func normalizeIBAN(s string) string {
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}
