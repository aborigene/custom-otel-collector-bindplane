package fieldcryptoprocessor

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
)

// nopLogsConsumer is a minimal downstream consumer for tests. The processor mutates the
// plog.Logs in place before calling us, so tests inspect the same object afterwards.
type nopLogsConsumer struct{}

func (nopLogsConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}
func (nopLogsConsumer) ConsumeLogs(context.Context, plog.Logs) error { return nil }

func TestConsumeLogs_MaskEncryptAndDecrypt(t *testing.T) {
	ctx := context.Background()
	keyDir := t.TempDir()

	cfg := &Config{
		KeyDir:      keyDir,
		KeyProvider: providerDisk,
		MaskValue:   "[MASKED]",
		Mask: MaskConfig{
			Fields:   []string{"user.email"},
			Patterns: []MaskPattern{{Field: "body", Type: patternTypeCPF}},
		},
		Encrypt: EncryptConfig{Fields: []string{"user.document"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	p := &logsProcessor{fieldCrypto: newFieldCrypto(cfg, zap.NewNop()), next: nopLogsConsumer{}}
	if err := p.Start(ctx, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	const (
		origBodyCPF = "529.982.247-25"
		origEmail   = "alice@example.com"
		origDoc     = "12345678901"
	)

	ld := plog.NewLogs()
	lr := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lr.Body().SetStr("customer CPF " + origBodyCPF + " on file")
	lr.Attributes().PutStr("user.email", origEmail)
	lr.Attributes().PutStr("user.document", origDoc)

	if err := p.ConsumeLogs(ctx, ld); err != nil {
		t.Fatalf("ConsumeLogs: %v", err)
	}

	got := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)

	// 1. In-body CPF masked.
	body := got.Body().Str()
	if want := "customer CPF [MASKED] on file"; body != want {
		t.Fatalf("body: got %q want %q", body, want)
	}

	// 2. Whole-value mask field replaced.
	email, _ := got.Attributes().Get("user.email")
	if email.Str() != "[MASKED]" {
		t.Fatalf("email: got %q want [MASKED]", email.Str())
	}

	// 3. Encrypt field is base64 ciphertext (not the plaintext) and key id recorded.
	doc, _ := got.Attributes().Get("user.document")
	if doc.Str() == origDoc {
		t.Fatal("user.document was not encrypted")
	}
	keyID, ok := got.Attributes().Get(keyIDAttr)
	if !ok || keyID.Str() == "" {
		t.Fatalf("%s attribute not set", keyIDAttr)
	}

	// 4. Decrypt round-trip equals the original.
	kp, err := NewDiskKeyProvider(keyDir)
	if err != nil {
		t.Fatalf("NewDiskKeyProvider: %v", err)
	}
	key, err := kp.Key(ctx, keyID.Str())
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	pt, err := DecryptAESGCM(key, doc.Str())
	if err != nil {
		t.Fatalf("DecryptAESGCM: %v", err)
	}
	if string(pt) != origDoc {
		t.Fatalf("decrypted: got %q want %q", pt, origDoc)
	}
}

func TestConsumeLogs_InvalidCPFLeftIntact(t *testing.T) {
	ctx := context.Background()
	cfg := &Config{
		KeyDir:      t.TempDir(),
		KeyProvider: providerDisk,
		MaskValue:   "[MASKED]",
		Mask:        MaskConfig{Patterns: []MaskPattern{{Field: "body", Type: patternTypeCPF}}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	p := &logsProcessor{fieldCrypto: newFieldCrypto(cfg, zap.NewNop()), next: nopLogsConsumer{}}
	if err := p.Start(ctx, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ld := plog.NewLogs()
	lr := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	// 111.111.111-11 is CPF-shaped but invalid -> must be left intact.
	lr.Body().SetStr("fake 111.111.111-11 number")

	if err := p.ConsumeLogs(ctx, ld); err != nil {
		t.Fatalf("ConsumeLogs: %v", err)
	}
	got := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().Str()
	if want := "fake 111.111.111-11 number"; got != want {
		t.Fatalf("invalid CPF should be intact: got %q want %q", got, want)
	}
}
