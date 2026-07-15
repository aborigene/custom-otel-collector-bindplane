# custom-otel-collector-bindplane

A proof-of-concept lab showing that **field-level masking AND reversible encryption** can
be centralized in a custom OpenTelemetry Collector processor (`fieldcrypto`) — replacing
per-language client libraries — packaged as a **BindPlane-manageable** collector distro.

- Custom processor: [`fieldcryptoprocessor/`](fieldcryptoprocessor/README.md) — masks
  emails/CPFs and reversibly encrypts fields (AES-256-GCM behind a `KeyProvider`).
- Decryptor CLI: [`cmd/decryptor`](cmd/decryptor/main.go) — on-demand reversal.
- Log generator: [`cmd/loggen`](cmd/loggen/main.go) — random OTLP logs for smoke tests.
- ODB build + Kubernetes manifests + BindPlane Agent Type.

## Build Strategy

Build/release policy, ODB vs OCB guidance, and CI consolidation plan are documented in
[BUILD_STRATEGY.md](BUILD_STRATEGY.md).

## Version pins

Built from the **BDOT v1.103.0** baseline (the BindPlane Distro for OpenTelemetry), so the
distro is BindPlane-manageable. Every OTel module is pinned to the same minor:

| Component | Version |
|---|---|
| OTel Collector / contrib (unstable modules) | `v0.155.0` |
| OTel Collector core stable (pdata, component, consumer, processor) | `v1.61.0` |
| bindplane components (bindplane-otel-contrib / collector) | `v1.9.0` / `v1.103.0` |

Recorded at the top of [build/manifest.yaml](build/manifest.yaml). Bump the whole set
together. ODB packages the OpAMP supervisor into the release; the BindPlane **Agent Type**
must be applied via the BindPlane CLI before connecting the collector.

## 1. Test

```bash
go test ./...
go test -race ./...
go test -run '^$' -bench BenchmarkIsValidCPF -benchmem ./fieldcryptoprocessor/
```

## 2. Local end-to-end

Build the collector via ocb, run it with a **local** keystore, generate logs, watch the
debug exporter mask/encrypt them, then decrypt one value.

```bash
# a) build + run the collector with a local ./.keys keystore (see the Makefile target)
make run-collector-local          # serves OTLP on :4317 (grpc) and :4318 (http)

# b) in another terminal, send logs
go run ./cmd/loggen --endpoint localhost:4318 --protocol http \
  --count 50 --rate 20 --valid-cpf-pct 50 --seed 1
# prints a summary, e.g.:
#   sent 50 logs (seed=1): valid_cpf=27 invalid_shaped=12 emails=24 documents=23 cards=26
```

In the collector output you will see:
- `user.email` → `[MASKED]`
- valid CPFs in the body → `[MASKED]`; invalid CPF-shaped numbers left intact
- `user.document` / `user.card` → base64 ciphertext, plus an `encryption.key_id` attribute

Decrypt one value (copy a `key_id` and a ciphertext from the output):

```bash
go run ./cmd/decryptor --key-dir ./.keys --key-id key-XXXX --value <base64-ciphertext>
```

## 3. Kubernetes (kind / minikube / EKS Fargate)

> Deploying to a **real** cluster (build → push to ECR → run on EKS Fargate)? Follow the
> step-by-step guide in [DEPLOY.md](DEPLOY.md). The steps below are for a local cluster.

The keystore is an **emptyDir** inside the collector pod — no PVC — so this runs on **EKS
Fargate** (no EBS/EFS, no DaemonSet). `loggen` pushes OTLP straight to the collector
Service, so no host log-file access is needed. On Fargate, ensure a **Fargate profile**
covers the `fieldcrypto-lab` namespace, and push images to a registry your cluster can
pull (e.g. ECR) instead of `kind load`.

```bash
# build images (kind/minikube: load locally; Fargate: push to ECR and update image refs)
make images
kind load docker-image fieldcrypto-collector:dev fieldcrypto-loggen:dev
#   (minikube: `minikube image load <img>` for each)

kubectl apply -k deploy/
kubectl -n fieldcrypto-lab rollout status deploy/fieldcrypto-collector

# generate load (Job targets the collector Service on :4318)
kubectl -n fieldcrypto-lab create -f deploy/job-loggen.yaml
kubectl -n fieldcrypto-lab logs -l app=fieldcrypto-collector -f   # watch mask/encrypt

# decrypt on-demand: exec the /decryptor baked into the collector image (no separate Job,
# because emptyDir keys live only in this pod). Copy a key-id + ciphertext from the logs:
kubectl -n fieldcrypto-lab exec deploy/fieldcrypto-collector -c collector -- \
  /decryptor --key-dir /var/keys --key-id key-XXXX --value <base64-ciphertext>
```

## 4. Getting keys / plaintext out

With emptyDir the keystore lives at `/var/keys` **inside the collector pod** and is lost
when the pod restarts (a fresh key is generated on next start) — acceptable for the POC.
Decrypt by exec-ing the in-image `/decryptor` (shown above); `kubectl cp` won't work on the
distroless image (no `tar`). For durable keys across restarts / a separate decryptor
workload, use a shared volume (PVC on EBS, or EFS on Fargate) — see the production notes.

**Keystore format** and the **`encryption.key_id` contract** are documented in
[fieldcryptoprocessor/README.md](fieldcryptoprocessor/README.md). Never delete retired keys.

## 5. BindPlane

```bash
bindplane apply -f deploy/bindplane-agent-type.yaml   # metadata.name == dist.name
# then connect the collector; BindPlane manages it like BDOT.
```

## POC → Project (summary)

- **Keys:** the disk keystore is POC-only. Swap `DiskKeyProvider` for a KMS/Vault
  (envelope encryption; only wrapped DEKs touch disk) via the `KeyProvider` seam — the
  `kms_provider.go` stub is where that lands. Never have the processor write keys to a PVC
  in production.
- **BindPlane ops:** manage the mask/encrypt rules as versioned config with progressive
  rollouts; keep `platformArchSet`/`repositoryLink` accurate; bump OTel versions as a set.
- **Quality/CI:** add `metadata.yaml` + mdatagen, emit metrics (fields masked/encrypted,
  CPF validated vs rejected), `golangci-lint`, `go test -race`, image scan + SBOM.
- **Security:** never log plaintext/keys; run non-root, read-only rootfs, dropped caps;
  list target/excluded fields explicitly. Not covered: numeric-typed fields, encoded blobs,
  and data exported before the processor was deployed.
