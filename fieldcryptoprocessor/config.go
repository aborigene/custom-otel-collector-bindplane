// config.go — configuration schema and validation for the fieldcrypto processor.
package fieldcryptoprocessor

import (
	"fmt"
	"regexp"
)

// providerDisk / providerKMS are the accepted values for Config.KeyProvider.
const (
	providerDisk = "disk"
	providerKMS  = "kms"

	patternTypeCPF   = "cpf"
	patternTypeCNPJ  = "cnpj"
	patternTypeIBAN  = "iban"
	patternTypeRegex = "regex"
)

// MaskPattern is an in-field (partial) masking rule targeting one field.
type MaskPattern struct {
	// Field is the attribute name to scan, or the literal "body" for the log body.
	Field string `mapstructure:"field"`
	// Type is "cpf" (checksum-validated) or "regex" (every match masked).
	Type string `mapstructure:"type"`
	// Regex is required when Type == "regex"; ignored otherwise.
	Regex string `mapstructure:"regex"`
}

// MaskConfig holds whole-value and in-field masking rules.
type MaskConfig struct {
	// Fields are masked by whole-value replacement with MaskValue.
	Fields []string `mapstructure:"fields"`
	// FieldPatterns are whole-field masks driven by an A/X template.
	FieldPatterns []MaskFieldPattern `mapstructure:"field_patterns"`
	// Patterns are in-field masking rules (cpf / regex).
	Patterns []MaskPattern `mapstructure:"patterns"`
}

// MaskFieldPattern masks a full field value using an A/X template.
// A keeps one original alphanumeric character visible.
// X masks one or more alphanumeric characters.
// Non-alphanumeric characters (dot, dash, slash, spaces) are preserved.
type MaskFieldPattern struct {
	Field   string `mapstructure:"field"`
	Pattern string `mapstructure:"pattern"`
}

// EncryptConfig lists fields to reversibly encrypt.
type EncryptConfig struct {
	Fields []string `mapstructure:"fields"`
}

// Config is the top-level processor configuration.
type Config struct {
	// KeyDir is the keystore directory used by the disk provider.
	KeyDir string `mapstructure:"key_dir"`
	// KeyProvider selects the KeyProvider implementation: "disk" (default) or "kms".
	KeyProvider string `mapstructure:"key_provider"`
	// MaskValue is the replacement token for masked values.
	MaskValue string `mapstructure:"mask_value"`
	// Mask holds whole-value and in-field masking rules.
	Mask MaskConfig `mapstructure:"mask"`
	// Encrypt lists fields to reversibly encrypt.
	Encrypt EncryptConfig `mapstructure:"encrypt"`
}

// Validate enforces the invariants described in the lab prompt:
//   - key_provider must be "disk" or "kms"
//   - every pattern type must be "cpf", "cnpj", "iban", or "regex"
//   - a "regex" pattern must supply a compilable regex
//   - a field may not appear in BOTH mask.fields and encrypt.fields
func (c *Config) Validate() error {
	switch c.KeyProvider {
	case "", providerDisk, providerKMS:
		// ok ("" defaults to disk in createDefaultConfig)
	default:
		return fmt.Errorf("invalid key_provider %q: must be %q or %q", c.KeyProvider, providerDisk, providerKMS)
	}

	for i, p := range c.Mask.Patterns {
		switch p.Type {
		case patternTypeCPF:
			// no regex needed
		case patternTypeCNPJ:
			// no regex needed
		case patternTypeIBAN:
			// no regex needed
		case patternTypeRegex:
			if p.Regex == "" {
				return fmt.Errorf("mask.patterns[%d] (field %q): type %q requires a regex", i, p.Field, patternTypeRegex)
			}
			if _, err := regexp.Compile(p.Regex); err != nil {
				return fmt.Errorf("mask.patterns[%d] (field %q): invalid regex: %w", i, p.Field, err)
			}
		default:
			return fmt.Errorf("mask.patterns[%d] (field %q): invalid type %q: must be %q, %q, %q, or %q",
				i, p.Field, p.Type, patternTypeCPF, patternTypeCNPJ, patternTypeIBAN, patternTypeRegex)
		}
	}

	for i, p := range c.Mask.FieldPatterns {
		if p.Field == "" {
			return fmt.Errorf("mask.field_patterns[%d]: field is required", i)
		}
		if p.Pattern == "" {
			return fmt.Errorf("mask.field_patterns[%d] (field %q): pattern is required", i, p.Field)
		}
		if !hasTemplateTokens(p.Pattern) {
			return fmt.Errorf("mask.field_patterns[%d] (field %q): pattern must include at least one A or X token", i, p.Field)
		}
	}

	maskSet := make(map[string]struct{}, len(c.Mask.Fields))
	for _, f := range c.Mask.Fields {
		maskSet[f] = struct{}{}
	}
	for _, f := range c.Encrypt.Fields {
		if _, dup := maskSet[f]; dup {
			return fmt.Errorf("field %q appears in both mask.fields and encrypt.fields", f)
		}
	}
	return nil
}
