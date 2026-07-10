// kms_provider.go — KMS-backed KeyProvider STUB.
//
// This is the production migration target. It compiles and satisfies KeyProvider,
// but the method bodies return a "not implemented in POC" error. It documents the
// envelope-encryption design so moving from the disk provider to a real KMS is a
// drop-in swap (change key_provider: "kms" in config, fill in the TODOs).
//
// Envelope encryption model:
//   - The KMS holds a long-lived Key Encryption Key (KEK). It never leaves the KMS.
//   - For encryption, the processor asks the KMS to generate a Data Encryption Key
//     (DEK). The KMS returns BOTH the plaintext DEK (used immediately for AES-GCM)
//     and the DEK wrapped/encrypted under the KEK.
//   - The wrapped DEK is what gets persisted / referenced by key id. Plaintext DEKs
//     are used and discarded; they are never written to disk.
//   - For decryption, the processor sends the wrapped DEK to the KMS, which unwraps
//     it (only the KMS can, since only it holds the KEK), returning the plaintext DEK.
//
// With this model, "key id" identifies a stored wrapped-DEK record; the plaintext key
// material only ever lives in memory for the duration of one operation.

package fieldcryptoprocessor

import (
	"context"
	"errors"
)

// errNotImplemented keeps the stub honest: selecting the KMS provider in the POC
// fails loudly rather than silently doing the wrong thing.
var errNotImplemented = errors.New("kms key provider not implemented in POC; use key_provider: disk")

// KMSKeyProvider is a stub. In production, embed a KMS client, e.g.:
//
//	type KMSKeyProvider struct {
//	    client   *kms.Client        // AWS SDK v2, or GCP/Azure/Vault equivalent
//	    kekARN   string             // the Key Encryption Key identifier
//	    store    WrappedDEKStore    // where wrapped DEKs live (Secret, DynamoDB, etc.)
//	    cache    map[string][]byte  // short-lived in-memory plaintext DEK cache
//	}
type KMSKeyProvider struct {
	// KEKRef is the KMS key identifier (e.g. an AWS KMS key ARN). Wired from config.
	KEKRef string
}

// NewKMSKeyProvider is where you would construct the KMS client from ambient cloud
// credentials (IRSA / workload identity), never from static keys in config.
func NewKMSKeyProvider(kekRef string) (*KMSKeyProvider, error) {
	return &KMSKeyProvider{KEKRef: kekRef}, nil
}

func (p *KMSKeyProvider) CurrentKey(ctx context.Context) (keyID string, key []byte, err error) {
	// TODO(prod): call KMS GenerateDataKey(KeyId=KEKRef, KeySpec=AES_256).
	// Persist the returned CiphertextBlob (wrapped DEK) under a new key id in the store.
	// Return (keyID, plaintextDEK). Discard the plaintext DEK after the encrypt call.
	return "", nil, errNotImplemented
}

func (p *KMSKeyProvider) Key(ctx context.Context, keyID string) (key []byte, err error) {
	// TODO(prod): look up the wrapped DEK for keyID in the store, then call
	// KMS Decrypt(CiphertextBlob=wrappedDEK) to recover the plaintext DEK.
	// Optionally cache the plaintext DEK briefly to reduce KMS calls.
	return nil, errNotImplemented
}

func (p *KMSKeyProvider) Rotate(ctx context.Context) (keyID string, err error) {
	// TODO(prod): generate a fresh DEK via GenerateDataKey and mark it current.
	// KEK rotation itself is managed in the KMS and is transparent to this code.
	return "", errNotImplemented
}

// Compile-time assertion that the stub satisfies the interface.
var _ KeyProvider = (*KMSKeyProvider)(nil)
