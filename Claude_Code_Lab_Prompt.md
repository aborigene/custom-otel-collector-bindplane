# Claude Code Prompt — Field Mask + Encrypt Custom OTel Processor Lab (v2)

This document contains (1) a ready-to-paste prompt for Claude Code that builds the
entire lab, and (2) best practices for taking the result from POC to a maintained
customer project. v2 adds full BindPlane compatibility, a random log generator, a
pluggable KeyProvider (with a KMS stub), and an explicit two-stage CPF check.

---

## Before you paste: filling in the `# CHANGE` values

Every `# CHANGE` in the prompt is a name you own. Worked example for a fictional
"Acme Corp" — replace with your real values:

| Placeholder | What it is | Example value |
|---|---|---|
| module path | Go module = repo URL without `https://`. Used in `go mod init` and `dist.module`. | `github.com/acme-corp/otel-fieldcrypto` |
| dist name | Collector distribution name. MUST equal the BindPlane Agent Type `metadata.name`. | `acme-fieldcrypto-collector` |
| repo link | Full URL of the GitHub repo running the ODB build. | `https://github.com/acme-corp/otel-fieldcrypto` |

So wherever the prompt says `# CHANGE module path`, you would write
`github.com/acme-corp/otel-fieldcrypto`, and so on.

---

## PART 1 — The Claude Code Prompt

> Copy everything inside the fenced block into Claude Code as your initial instruction.

```text
You are building a proof-of-concept lab for a custom OpenTelemetry Collector processor.
The goal is to demonstrate to a customer that field-level masking AND reversible
encryption can be centralized in the collector, replacing per-language client libraries.

Build everything in a single Git repository. Use Go 1.21+. Do not invent APIs — if
unsure about a pdata or processor API, read the installed module source under the Go
module cache before using it.

## VERSION PINNING (important — do this first)
This collector must be fully manageable by BindPlane, so it must be built from the same
component baseline as the BindPlane Distro for OpenTelemetry (BDOT), not bare OTel Contrib.
1. Look up the latest BDOT release at
   https://github.com/observIQ/bindplane-otel-collector/releases and note the OTel
   Collector/contrib version it tracks and the bindplane component version (e.g. as of
   mid-2026 this is roughly OTel v0.154.x and bindplane components v1.8.x — verify current).
2. Pin EVERY OpenTelemetry Collector module (core, contrib, pdata) to that same minor
   version. Pin bindplane components to the matching bindplane version. Mixed minors will
   not compile.
3. Record the exact versions you chose at the top of build/manifest.yaml in a comment.

## BINDPLANE COMPATIBILITY (required)
So BindPlane does not grey out features, the ODB manifest MUST include the components that
make a distribution BindPlane-manageable, in addition to your custom processor:
  - the bindplane extension: github.com/observiq/bindplane-otel-collector/extension/bindplaneextension
  - the healthcheck extension
  - the standard set of receivers/processors/exporters/connectors that BDOT ships (mirror
    the BindPlane "minimal compatible" skeleton manifest from their BYOC how-to guide so
    the UI exposes a normal component set), PLUS the custom fieldcryptoprocessor.
Also generate a BindPlane Agent Type resource file (deploy/bindplane-agent-type.yaml):
  apiVersion: bindplane.observiq.com/v1
  kind: AgentType
  metadata:
    name: custom-otel-collector-bindplane            # CHANGE dist name — MUST equal dist.name in manifest.yaml
  spec:
    repositoryLink: https://github.com/aborigene/custom-otel-collector-bindplane  # CHANGE repo link — full https URL of this GitHub repo
    platformArchSet:
      - { platform: linux,   arch: amd64 }
      - { platform: linux,   arch: arm64 }
      - { platform: darwin,  arch: arm64 }
      - { platform: windows, arch: amd64 }
Document in the README that ODB packages the OpAMP supervisor into the release, and that
the Agent Type must be applied to BindPlane via its CLI before connecting the collector.

## Repository layout
  fieldcryptoprocessor/
    config.go
    factory.go
    processor.go
    crypto.go                   # AES-256-GCM + KeyProvider interface + disk provider
    kms_provider.go             # KMS-backed KeyProvider STUB (compiles, not wired by default)
    cpf.go                      # CPF two-stage validation
    processor_test.go
    crypto_test.go
    cpf_test.go
    cpf_bench_test.go           # benchmark proving the format gate short-circuits
    README.md
  cmd/decryptor/main.go         # decryption CLI
  cmd/loggen/main.go            # random OTLP log generator for smoke tests
  build/
    manifest.yaml               # ODB manifest (BindPlane-compatible + custom processor)
    Dockerfile.collector
    Dockerfile.decryptor
    Dockerfile.loggen
  deploy/
    namespace.yaml
    pvc.yaml
    configmap-collector.yaml
    deployment-collector.yaml
    job-decryptor.yaml
    job-loggen.yaml
    bindplane-agent-type.yaml
    kustomization.yaml
  testdata/sample-logs.json
  Makefile
  README.md

## Processor behavior (type string "fieldcrypto")
Support logs, traces, metrics; focus demo + tests on LOGS. Capabilities MutatesData=true.

### Configuration (config.go)
  key_dir:    string   # keystore dir for the disk provider (default /var/keys)
  key_provider: string # "disk" (default) or "kms" (kms uses the stub)
  mask_value: string   # default "[MASKED]"
  mask:
    fields:   []string                 # whole-value replacement
    patterns:                          # in-field masking
      - { field: <attr or "body">, type: "cpf" }        # cpf uses checksum validation
      - { field: <attr or "body">, type: "regex", regex: "<re>" }
  encrypt:
    fields:   []string                 # reversible encryption
Add Validate(): error if regex pattern lacks regex, if type not in {cpf,regex}, or if a
field is in both mask.fields and encrypt.fields.

### CPF validation (cpf.go) — EXPLICIT TWO-STAGE for performance
Stage 1 (cheap format gate): stripNonDigits, then hasCPFShape(digits) which returns true
ONLY if len==11 AND not all 11 digits identical. This runs first and short-circuits.
Stage 2 (checksum): only if stage 1 passes, run the weighted modulo-11 verifier-digit
calculation (weights 10..2 over digits 0..8 for the first verifier; 11..2 over digits
0..9 for the second; remainder<2 => 0 else 11-remainder). isValidCPF returns stage1 && stage2.
Also expose the compiled candidate regex: \b(\d{3}[.\-]?\d{3}[.\-]?\d{3}[.\-]?\d{2})\b
The regex is the outer format filter when scanning free text; hasCPFShape is the inner
guard so the modulo arithmetic never runs on implausible candidates.

### Masking logic
  - mask.fields: replace whole string value with mask_value.
  - pattern type "cpf": regex-scan target, and for each match replace with mask_value ONLY
    if isValidCPF is true; leave everything else (and invalid CPF-shaped numbers) intact.
  - pattern type "regex": replace every match with mask_value.
  - "body" targets the log record body when it is a string.

### Encryption (crypto.go) — behind a KeyProvider interface
Define:
  type KeyProvider interface {
      CurrentKey(ctx context.Context) (keyID string, key []byte, err error) // for encrypt
      Key(ctx context.Context, keyID string) (key []byte, err error)        // for decrypt
      Rotate(ctx context.Context) (keyID string, err error)
  }
Implement DiskKeyProvider (the lab default):
  - keystore file <key_dir>/keystore.json:
    { "current":"<key_id>", "keys": { "<key_id>": {"key":"<base64 32B>","created":"<RFC3339>"} } }
  - on load: if empty, generate a 32-byte crypto/rand key, id "key-<unixSeconds>-<4 hex>",
    write file 0600, set current. Never overwrite/delete existing keys (rotation-safe).
  - Rotate() adds a new current key, keeping old ones.
Encryption primitives (pure functions, shared with the CLI):
  - AES-256-GCM. encrypt: fresh 12-byte random nonce; output base64(nonce||Seal(...)).
  - decrypt: split nonce, gcm.Open.
Processor: for each encrypt.field string value, get CurrentKey, encrypt, set the field to
the ciphertext, and set the record attribute "encryption.key_id" to the key id used.
NEVER log plaintext or key material; debug logs may reference key_id and field names only.

### KMS stub (kms_provider.go)
Provide KMSKeyProvider implementing KeyProvider as a COMPILING STUB using envelope
encryption shape: a comment block explaining KEK-in-KMS / DEK-per-record, method bodies
returning a clear "not implemented in POC" error, and TODOs showing where an AWS KMS
GenerateDataKey / Decrypt call would go. Selectable via key_provider: "kms" but defaulting
off. This proves the production migration path without requiring cloud creds in the lab.

### Factory (factory.go)
processor.NewFactory with WithLogs/WithTraces/WithMetrics (alpha). Construct the configured
KeyProvider in Start(). Traces/metrics apply attribute-level rules only (skip body patterns).

### Processor (processor.go)
Traverse pdata: logs (resource attrs, record attrs, body), traces (resource+span attrs),
metrics (resource + Gauge/Sum/Histogram datapoint attrs). Operate only on ValueTypeStr.
Per field precedence: encrypt if listed; else mask if listed; patterns apply to their targets.

## Decryptor CLI (cmd/decryptor/main.go)
Reuses crypto.go. Modes:
  decryptor --key-dir /var/keys --key-id key-... --value <b64>         # single value
  decryptor --key-dir /var/keys --input line.json --fields a,b,c       # reads encryption.key_id
Fail clearly if the key id is not in the keystore.

## Random log generator (cmd/loggen/main.go) — for smoke tests
A CLI that sends OTLP logs to a configurable endpoint. Flags:
  --endpoint (default localhost:4318), --protocol (http|grpc), --count, --rate (logs/sec),
  --valid-cpf-pct (share of logs whose body contains a VALID CPF), --seed.
Each generated log must randomly include a subset of: a body string that may embed a valid
CPF, an invalid-but-CPF-shaped number, an email, a user.document attribute (a valid CPF), a
user.card attribute, and benign noise attributes. Implement randomValidCPF() that generates
9 random digits then COMPUTES the two verifier digits with the same modulo-11 algorithm, so
generated CPFs are genuinely valid; implement randomInvalidCPFShaped() that produces an
11-digit number that fails the checksum. Print a summary at the end (how many valid CPFs,
invalid-shaped, emails were emitted) so smoke-test expectations are known up front.

## Tests
  - cpf_test.go: valid (e.g. 529.982.247-25, 111.444.777-35) and invalid (all-same-digit,
    wrong verifier, too short) tables.
  - cpf_bench_test.go: BenchmarkIsValidCPF over a mix; assert (in a comment) that the
    all-same-digit and wrong-length inputs skip the arithmetic via hasCPFShape.
  - crypto_test.go: encrypt/decrypt round-trip; wrong key id fails; ciphertext differs per call.
  - processor_test.go: plog.Logs fixture with a body CPF, a full-mask field, and an encrypt
    field; run ConsumeLogs against a mock consumer; assert in-body CPF masked, mask field
    replaced, encrypt field is base64 ciphertext, encryption.key_id set; then decrypt and
    assert equality with the original.
Run `go build ./...` and `go test ./...` and fix all failures before finishing.

## Build + K8s (deploy/)
  - manifest.yaml: BindPlane-compatible component set + fieldcryptoprocessor (local gomod
    replace to this repo). dist.module=github.com/aborigene/custom-otel-collector-bindplane, dist.name=custom-otel-collector-bindplane.
  - Dockerfiles for collector (via ODB), decryptor, loggen. Multi-stage, minimal, non-root.
  - namespace "fieldcrypto-lab"; pvc "keystore-pvc" (RWO 100Mi) mounted by collector AND
    decryptor at /var/keys; configmap with a collector config demonstrating ALL features
    (full-field mask user.email, in-field CPF mask on body, encrypt user.document+user.card),
    pipeline processors [memory_limiter, fieldcrypto, batch] (fieldcrypto before batch);
    deployment for the collector; job-decryptor and job-loggen; bindplane-agent-type.yaml;
    kustomization.yaml.

## README (top-level) — copy-paste runnable
  1. go test ./...
  2. local: build collector, run with a local --key-dir, start loggen against it, show the
     debug exporter output proving mas/encrypt + encryption.key_id, then decrypt one value.
  3. kind/minikube: build+load images, apply -k deploy/, port-forward, run job-loggen, run
     job-decryptor and read its logs.
  4. getting keys out: kubectl cp from a PVC-mounting pod, or run job-decryptor.
  5. BindPlane: apply bindplane-agent-type.yaml via BindPlane CLI, then connect the collector.
  Document the keystore.json format, the encryption.key_id contract, and the version pins.

Deliver working, compiling, tested code.
```

---

## PART 2 — POC → Project Best Practices

### Key management (the biggest one)
The disk keystore is fine for a POC and wrong for production. The `KeyProvider` interface in
the prompt exists precisely so the swap is a drop-in:
- Back it with a real KMS/Vault (AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault) using
  envelope encryption — the KMS holds the key-encryption-key (KEK), the processor generates
  data-encryption-keys (DEKs), and only wrapped DEKs ever touch disk. The `kms_provider.go`
  stub is where this lands.
- If keys must stay in-cluster, mount read-only Kubernetes Secrets populated by the External
  Secrets Operator or Vault Agent Injector — never have the processor write keys to a PVC.
- Define a rotation policy and prove old data still decrypts (the `encryption.key_id`
  contract guarantees this — never delete retired keys).
- The decryptor is a separate, audited, on-demand workload with tight RBAC — not standing access.

### BindPlane operations
- Register via BYOC; keep the Agent Type's `platformArchSet` and `repositoryLink` accurate so
  release detection and rollouts work like they do for BDOT.
- Manage the fieldcrypto rules (which fields to mask/encrypt) as versioned BindPlane config
  with progressive rollouts, so a bad rule is rolled back quickly.
- Keep the manifest aligned to the current BDOT baseline; when you bump OTel versions, bump
  the whole set together.

### Component quality
- Add metadata.yaml + mdatagen so the processor reports a stability level and structured
  internal telemetry.
- Emit metrics: fields masked, fields encrypted, CPF candidates validated vs rejected, errors.
- Move stability alpha -> beta only after load testing.

### Testing and CI
- Keep unit tests; add the CPF benchmark, a fuzz test on the CPF/regex matchers, and a
  golden-file pipeline test.
- CI: golangci-lint, `go test -race`, ODB build, and image scan (Trivy/Grype). Sign images
  and publish an SBOM — security customers will ask.
- Automate version bumps with Renovate/Dependabot; pin OTel modules together.

### Security hardening
- Never log plaintext or key material — enforce in review and lint.
- Run non-root, read-only rootfs, dropped capabilities.
- List target/excluded fields explicitly so new instrumentation can't silently leak a field.
- Document what is NOT covered: numeric-typed fields, encoded blobs, and data exported before
  the processor was deployed.

### Handoff
- The README is the artifact that survives the engagement: architecture, keystore/key_id
  contract, config reference, decryption runbook, KMS migration plan, version pins.
- Agree ownership up front: who rotates keys, who reviews rules, who bumps OTel versions.
