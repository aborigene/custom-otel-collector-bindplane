// crypto.go — KeyProvider interface, disk-backed provider, and AES-256-GCM primitives.
// This is a reference sketch to review the shape; Claude Code will generate the full,
// tested version from the prompt.

package fieldcryptoprocessor

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// KeyProvider abstracts where encryption keys come from. The lab ships a disk-backed
// implementation; production swaps in KMSKeyProvider (or Vault) WITHOUT touching the
// processor. This single seam is what makes the POC -> Project migration a drop-in.
type KeyProvider interface {
	// CurrentKey returns the active key id and material, used for encryption.
	CurrentKey(ctx context.Context) (keyID string, key []byte, err error)
	// Key returns the material for a specific key id, used for decryption.
	Key(ctx context.Context, keyID string) (key []byte, err error)
	// Rotate creates a new current key and returns its id. Old keys are retained.
	Rotate(ctx context.Context) (keyID string, err error)
}

// ─── AES-256-GCM primitives (shared by processor and decryptor CLI) ──────────────

// EncryptAESGCM is the exported wrapper used by the decryptor CLI and any external
// tooling. It delegates to the internal primitive so the processor and CLI share
// exactly one implementation.
func EncryptAESGCM(key, plaintext []byte) (string, error) { return encryptAESGCM(key, plaintext) }

// DecryptAESGCM is the exported wrapper used by the decryptor CLI.
func DecryptAESGCM(key []byte, b64 string) ([]byte, error) { return decryptAESGCM(key, b64) }

func encryptAESGCM(key, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key) // key must be 32 bytes for AES-256
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	// Seal appends ciphertext+tag to nonce; store nonce||ciphertext together.
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptAESGCM(key []byte, b64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := raw[:ns], raw[ns:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// ─── Disk-backed provider (lab default) ──────────────────────────────────────────

type keystoreEntry struct {
	Key     string `json:"key"`     // base64, 32 bytes
	Created string `json:"created"` // RFC3339
}

type keystoreFile struct {
	Current string                   `json:"current"`
	Keys    map[string]keystoreEntry `json:"keys"`
}

type DiskKeyProvider struct {
	mu   sync.RWMutex
	dir  string
	data keystoreFile
}

func NewDiskKeyProvider(dir string) (*DiskKeyProvider, error) {
	p := &DiskKeyProvider{dir: dir, data: keystoreFile{Keys: map[string]keystoreEntry{}}}
	if err := p.load(); err != nil {
		return nil, err
	}
	if p.data.Current == "" {
		if _, err := p.Rotate(context.Background()); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *DiskKeyProvider) path() string { return filepath.Join(p.dir, "keystore.json") }

func (p *DiskKeyProvider) load() error {
	b, err := os.ReadFile(p.path())
	if os.IsNotExist(err) {
		return nil // fresh keystore; Rotate will create one
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &p.data)
}

func (p *DiskKeyProvider) save() error {
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path(), b, 0o600) // 0600: owner read/write only
}

func (p *DiskKeyProvider) CurrentKey(_ context.Context) (string, []byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	id := p.data.Current
	entry, ok := p.data.Keys[id]
	if !ok {
		return "", nil, fmt.Errorf("no current key")
	}
	key, err := base64.StdEncoding.DecodeString(entry.Key)
	return id, key, err
}

func (p *DiskKeyProvider) Key(_ context.Context, keyID string) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	entry, ok := p.data.Keys[keyID]
	if !ok {
		return nil, fmt.Errorf("unknown key id %q", keyID)
	}
	return base64.StdEncoding.DecodeString(entry.Key)
}

func (p *DiskKeyProvider) Rotate(_ context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", err
	}
	suffix := make([]byte, 4)
	_, _ = io.ReadFull(rand.Reader, suffix)
	id := fmt.Sprintf("key-%d-%s", time.Now().Unix(), hex.EncodeToString(suffix))
	if p.data.Keys == nil {
		p.data.Keys = map[string]keystoreEntry{}
	}
	p.data.Keys[id] = keystoreEntry{
		Key:     base64.StdEncoding.EncodeToString(key),
		Created: time.Now().UTC().Format(time.RFC3339),
	}
	p.data.Current = id // old keys retained so historical data still decrypts
	return id, p.save()
}
