package fieldcryptoprocessor

import "testing"

func TestIsValidCNPJ(t *testing.T) {
	valid := []string{
		"11.444.777/0001-61",
		"11444777000161",
	}
	for _, c := range valid {
		if !isValidCNPJ(c) {
			t.Errorf("expected valid CNPJ: %q", c)
		}
	}

	invalid := []string{
		"11.444.777/0001-60",
		"11.111.111/1111-11",
		"123",
		"",
	}
	for _, c := range invalid {
		if isValidCNPJ(c) {
			t.Errorf("expected invalid CNPJ: %q", c)
		}
	}
}

func TestIsValidIBAN(t *testing.T) {
	valid := []string{
		"GB82 WEST 1234 5698 7654 32",
		"DE89370400440532013000",
	}
	for _, c := range valid {
		if !isValidIBAN(c) {
			t.Errorf("expected valid IBAN: %q", c)
		}
	}

	invalid := []string{
		"GB82 WEST 1234 5698 7654 31",
		"ZZ00INVALID",
		"",
	}
	for _, c := range invalid {
		if isValidIBAN(c) {
			t.Errorf("expected invalid IBAN: %q", c)
		}
	}
}

func TestIBANCandidateRegex(t *testing.T) {
	in := "iban GB82 WEST 1234 5698 7654 32 end"
	m := ibanCandidateRegex.FindString(in)
	if m == "" {
		t.Fatalf("expected iban regex to match, got empty")
	}
	if !isValidIBAN(m) {
		t.Fatalf("expected matched iban candidate to validate: %q", m)
	}
}
