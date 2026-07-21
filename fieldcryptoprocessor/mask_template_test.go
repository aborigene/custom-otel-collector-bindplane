package fieldcryptoprocessor

import "testing"

func TestMaskWithTemplate_PreservesPunctuationAndMasksMinimum(t *testing.T) {
	in := "123.456.789-09"
	out := maskWithTemplate(in, "AAAXAA")
	if out != "123.XXX.XXX-09" {
		t.Fatalf("unexpected output: got %q want %q", out, "123.XXX.XXX-09")
	}
}

func TestMaskWithTemplate_EmailPattern(t *testing.T) {
	in := "joao.silva@somedomain.com"
	out := maskWithTemplate(in, "AAXXXXA@AAXXXAA")
	if len(out) != len(in) {
		t.Fatalf("masked output length changed: got %d want %d", len(out), len(in))
	}
	if out[4] != '.' || out[10] != '@' || out[21] != '.' {
		t.Fatalf("punctuation should be preserved: got %q", out)
	}
	masked := 0
	for i := 0; i < len(out); i++ {
		if out[i] == 'X' {
			masked++
		}
	}
	if masked < 9 {
		t.Fatalf("expected email masking to redact enough characters, got %d X in %q", masked, out)
	}
}

func TestMaskWithTemplate_EnforcesMin40Percent(t *testing.T) {
	in := "ABCDEFGHIJ"
	out := maskWithTemplate(in, "AAAAAAAAAA")
	masked := 0
	for i := 0; i < len(out); i++ {
		if out[i] == 'X' {
			masked++
		}
	}
	if masked < 4 {
		t.Fatalf("expected at least 40%% masked, got %d/%d in %q", masked, len(in), out)
	}
}
