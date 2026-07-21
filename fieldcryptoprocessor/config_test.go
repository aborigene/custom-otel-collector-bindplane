package fieldcryptoprocessor

import "testing"

func TestValidate_AcceptsNewPatternTypes(t *testing.T) {
	cfg := &Config{
		KeyProvider: providerDisk,
		Mask: MaskConfig{Patterns: []MaskPattern{
			{Field: "body", Type: patternTypeCPF},
			{Field: "body", Type: patternTypeCNPJ},
			{Field: "body", Type: patternTypeIBAN},
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestValidate_FieldPatternRequiresTokens(t *testing.T) {
	cfg := &Config{
		KeyProvider: providerDisk,
		Mask: MaskConfig{FieldPatterns: []MaskFieldPattern{
			{Field: "user.email", Pattern: "---"},
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation failure for template without A/X tokens")
	}
}
