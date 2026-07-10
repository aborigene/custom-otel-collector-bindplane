package fieldcryptoprocessor

import (
	"context"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	ctx := context.Background()
	kp, err := NewDiskKeyProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskKeyProvider: %v", err)
	}
	id, key, err := kp.CurrentKey(ctx)
	if err != nil {
		t.Fatalf("CurrentKey: %v", err)
	}
	if id == "" {
		t.Fatal("expected a non-empty current key id")
	}

	plaintext := "sensitive-value-12345"
	ct, err := EncryptAESGCM(key, []byte(plaintext))
	if err != nil {
		t.Fatalf("EncryptAESGCM: %v", err)
	}
	pt, err := DecryptAESGCM(key, ct)
	if err != nil {
		t.Fatalf("DecryptAESGCM: %v", err)
	}
	if string(pt) != plaintext {
		t.Fatalf("round-trip mismatch: got %q want %q", pt, plaintext)
	}
}

func TestUnknownKeyIDFails(t *testing.T) {
	kp, err := NewDiskKeyProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskKeyProvider: %v", err)
	}
	if _, err := kp.Key(context.Background(), "key-does-not-exist"); err == nil {
		t.Fatal("expected an error for an unknown key id")
	}
}

func TestCiphertextDiffersPerCall(t *testing.T) {
	kp, err := NewDiskKeyProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskKeyProvider: %v", err)
	}
	_, key, err := kp.CurrentKey(context.Background())
	if err != nil {
		t.Fatalf("CurrentKey: %v", err)
	}
	a, _ := EncryptAESGCM(key, []byte("same-input"))
	b, _ := EncryptAESGCM(key, []byte("same-input"))
	if a == b {
		t.Fatal("expected different ciphertext per call (fresh random nonce)")
	}
}

func TestRotateRetainsOldKeys(t *testing.T) {
	ctx := context.Background()
	kp, err := NewDiskKeyProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskKeyProvider: %v", err)
	}
	oldID, oldKey, err := kp.CurrentKey(ctx)
	if err != nil {
		t.Fatalf("CurrentKey: %v", err)
	}
	newID, err := kp.Rotate(ctx)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newID == oldID {
		t.Fatal("Rotate must produce a new key id")
	}
	// Old key must still be retrievable so historical data still decrypts.
	got, err := kp.Key(ctx, oldID)
	if err != nil {
		t.Fatalf("old key not retained after rotate: %v", err)
	}
	if string(got) != string(oldKey) {
		t.Fatal("retained old key material changed after rotate")
	}
}
