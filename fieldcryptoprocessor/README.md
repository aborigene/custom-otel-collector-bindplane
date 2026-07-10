# fieldcryptoprocessor

A custom OpenTelemetry Collector processor (type `fieldcrypto`) that performs field-level
**masking** and reversible **encryption** inside the collector, so PII handling can be
centralized instead of implemented per-language in every service.

Signals: **logs, traces, metrics** (demo and tests focus on logs).
Capabilities: `MutatesData = true`. Operates only on **string-typed** values.

## Per-field precedence

For each field the processor applies, in order:

1. **encrypt** — if the field is listed in `encrypt.fields`, its whole value is replaced
   with AES-256-GCM ciphertext and the record attribute `encryption.key_id` records which
   key was used.
2. **mask** — else if listed in `mask.fields`, the whole value is replaced with `mask_value`.
3. **patterns** — else any `mask.patterns` targeting that field run (in-field masking).

`"body"` targets the log record body when it is a string.

## Configuration

```yaml
processors:
  fieldcrypto:
    key_dir: /var/keys          # keystore dir for the disk provider (default /var/keys)
    key_provider: disk          # "disk" (default) or "kms" (stub, see below)
    mask_value: "[MASKED]"      # default "[MASKED]"
    mask:
      fields:                   # whole-value replacement
        - user.email
      patterns:                 # in-field masking
        - { field: body, type: cpf }                     # checksum-validated CPF
        - { field: notes, type: regex, regex: "\\d{16}" } # every match masked
    encrypt:
      fields:                   # reversible encryption
        - user.document
        - user.card
```

`Validate()` fails if: `key_provider` is not `disk`/`kms`; a pattern `type` is not
`cpf`/`regex`; a `regex` pattern has no (or an invalid) `regex`; or a field appears in
**both** `mask.fields` and `encrypt.fields`.

## CPF masking — explicit two-stage validation (`cpf.go`)

CPF is the Brazilian individual taxpayer id (11 digits, last two are modulo-11 checksums).
Masking runs a two-stage check for performance:

- **Stage 1 — `hasCPFShape` (cheap gate):** strip non-digits, require exactly 11 digits
  that are not all identical. Short-circuits obvious junk with no arithmetic.
- **Stage 2 — `validChecksum`:** only when stage 1 passes, recompute both verifier digits.

`cpfCandidateRegex` (`\b(\d{3}[.\-]?\d{3}[.\-]?\d{3}[.\-]?\d{2})\b`) is the outer text
filter; `hasCPFShape` is the inner guard. Only candidates where `isValidCPF` is true are
masked — invalid CPF-shaped numbers are left intact. `cpf_bench_test.go` demonstrates the
short-circuit.

## Encryption & keys (`crypto.go`, `kms_provider.go`)

Encryption is behind a `KeyProvider` interface so production can swap the key backend
without touching the processor:

```go
type KeyProvider interface {
    CurrentKey(ctx) (keyID string, key []byte, err error) // encrypt
    Key(ctx, keyID) (key []byte, err error)               // decrypt
    Rotate(ctx) (keyID string, err error)
}
```

- **`DiskKeyProvider` (lab default):** keystore at `<key_dir>/keystore.json`, mode `0600`.
  Generates a 32-byte random key on first use; `Rotate()` adds a new current key and
  **never deletes old keys**, so historical data still decrypts.
- **`KMSKeyProvider` (stub):** compiles and satisfies the interface but returns
  "not implemented in POC". Documents the envelope-encryption path (KEK in KMS, per-record
  DEKs) for the production migration. Select with `key_provider: kms`.

Primitives are AES-256-GCM with a fresh 12-byte nonce; output is `base64(nonce || sealed)`.
`EncryptAESGCM` / `DecryptAESGCM` are exported and shared with the decryptor CLI.

**Keystore format:**

```json
{ "current": "key-<unix>-<hex>",
  "keys": { "key-<unix>-<hex>": { "key": "<base64 32B>", "created": "<RFC3339>" } } }
```

**Decryption contract:** the `encryption.key_id` attribute on a record names the key that
encrypted its fields. Never delete retired keys.

> Security: the processor never logs plaintext or key material — debug logs reference only
> `key_id` and field/counter metadata.
