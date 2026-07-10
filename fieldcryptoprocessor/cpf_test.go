package fieldcryptoprocessor

import "testing"

func TestIsValidCPF(t *testing.T) {
	valid := []string{
		"529.982.247-25",
		"111.444.777-35",
		"52998224725", // same as first, unformatted
	}
	for _, c := range valid {
		if !isValidCPF(c) {
			t.Errorf("expected %q to be a valid CPF", c)
		}
	}

	invalid := []string{
		"111.111.111-11", // all-same-digit: rejected by hasCPFShape (stage 1)
		"000.000.000-00", // all-same-digit
		"529.982.247-24", // wrong verifier digit
		"123.456.789-00", // wrong verifier digits
		"529.982.247",    // too short
		"5299822472555",  // too long
		"",               // empty
		"not-a-cpf",      // no digits
	}
	for _, c := range invalid {
		if isValidCPF(c) {
			t.Errorf("expected %q to be an INVALID CPF", c)
		}
	}
}

func TestHasCPFShape(t *testing.T) {
	if hasCPFShape("11111111111") {
		t.Error("all-same-digit strings must fail the stage-1 shape gate")
	}
	if hasCPFShape("123") {
		t.Error("short strings must fail the stage-1 shape gate")
	}
	if !hasCPFShape("52998224725") {
		t.Error("a plausible 11-digit string must pass the stage-1 shape gate")
	}
}
