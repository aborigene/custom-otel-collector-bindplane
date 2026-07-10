# Deploying the fieldcrypto POC to a real Kubernetes cluster (EKS Fargate)

Step-by-step guide to build the images, push them to Amazon ECR, point the manifests at
those images, and run the POC on a real EKS Fargate cluster.

> The POC is push-based (`loggen → OTLP → collector`) and uses an **emptyDir** keystore, so
> it needs **no DaemonSet and no EBS/EFS** — it runs on Fargate as-is. The only real
> adjustments are: build **linux/amd64** images (Fargate is x86_64 only), push them to a
> registry the cluster can pull, and make sure a **Fargate profile** covers the namespace.

---

## 0. Prerequisites

- **AWS CLI v2**, **Docker** (with `buildx`), **kubectl**, and **eksctl** installed.
- An existing **EKS cluster** with `kubectl` pointed at it (`kubectl config current-context`).
- Permissions to create ECR repositories and (if needed) a Fargate profile.
- **Architecture:** EKS Fargate runs **linux/amd64 only**. On Apple Silicon (M-series) you
  MUST build amd64 images or pods fail with `exec format error`. This guide does that.

---

## 1. Set shared environment variables

```bash
export AWS_REGION=us-east-1
export CLUSTER_NAME=my-eks-cluster                     # <-- your cluster name
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
export ECR=${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com
export IMAGE_TAG=v1                                     # use an immutable tag per build
```

---

## 2. Create the ECR repositories

Only **two** images are needed — the `decryptor` binary is baked into the collector image.

```bash
for repo in fieldcrypto-collector fieldcrypto-loggen; do
  aws ecr describe-repositories --repository-names "$repo" --region "$AWS_REGION" >/dev/null 2>&1 \
    || aws ecr create-repository --repository-name "$repo" --region "$AWS_REGION" \
         --image-scanning-configuration scanOnPush=true
done
```

---

## 3. Authenticate Docker to ECR

```bash
aws ecr get-login-password --region "$AWS_REGION" \
  | docker login --username AWS --password-stdin "$ECR"
```

> **Using ECR Public instead?** Repos live under `public.ecr.aws/<your-alias>/...` and login
> is always against `us-east-1`:
> `aws ecr-public get-login-password --region us-east-1 | docker login --username AWS --password-stdin public.ecr.aws`
> Then substitute that registry for `$ECR` below.

---

## 4. Build (amd64) and push the images

Build-and-push in one step with buildx. The collector is built via ocb and may take a few
minutes on the first run (it downloads OTel modules).

```bash
# Collector (includes the OTLP receiver, fieldcrypto processor, and the /decryptor binary)
docker buildx build --platform linux/amd64 \
  -f build/Dockerfile.collector \
  -t "$ECR/fieldcrypto-collector:$IMAGE_TAG" --push .

# Log generator (smoke-test client)
docker buildx build --platform linux/amd64 \
  -f build/Dockerfile.loggen \
  -t "$ECR/fieldcrypto-loggen:$IMAGE_TAG" --push .
```

Verify the pushes:

```bash
aws ecr describe-images --repository-name fieldcrypto-collector --region "$AWS_REGION" \
  --query 'imageDetails[].imageTags' --output text
aws ecr describe-images --repository-name fieldcrypto-loggen --region "$AWS_REGION" \
  --query 'imageDetails[].imageTags' --output text
```

> **Alternative (build locally with the Makefile, then tag + push):**
> ```bash
> export DOCKER_DEFAULT_PLATFORM=linux/amd64
> make collector loggen                       # builds fieldcrypto-collector:dev, fieldcrypto-loggen:dev
> docker tag fieldcrypto-collector:dev "$ECR/fieldcrypto-collector:$IMAGE_TAG"
> docker tag fieldcrypto-loggen:dev    "$ECR/fieldcrypto-loggen:$IMAGE_TAG"
> docker push "$ECR/fieldcrypto-collector:$IMAGE_TAG"
> docker push "$ECR/fieldcrypto-loggen:$IMAGE_TAG"
> ```

---

## 5. Point the manifests at your ECR images

Use the kustomize `images:` transformer so you don't hand-edit the Deployment/Job. Append
this block to [deploy/kustomization.yaml](deploy/kustomization.yaml), substituting your
registry (echo `$ECR` and `$IMAGE_TAG` to get the literal values):

```yaml
images:
  - name: fieldcrypto-collector
    newName: <ACCOUNT>.dkr.ecr.<REGION>.amazonaws.com/fieldcrypto-collector
    newTag: v1
  - name: fieldcrypto-loggen
    newName: <ACCOUNT>.dkr.ecr.<REGION>.amazonaws.com/fieldcrypto-loggen
    newTag: v1
```

Or generate it automatically with kustomize:

```bash
cd deploy
kustomize edit set image \
  fieldcrypto-collector=$ECR/fieldcrypto-collector:$IMAGE_TAG \
  fieldcrypto-loggen=$ECR/fieldcrypto-loggen:$IMAGE_TAG
cd ..
```

> The manifests keep `imagePullPolicy: IfNotPresent`, which is correct for **immutable**
> tags (`v1`, a git SHA, …). If you ever re-push the **same** tag, set the collector/loggen
> `imagePullPolicy` to `Always` so nodes re-pull.
>
> **Direct edit alternative:** change the `image:` field in
> [deploy/deployment-collector.yaml](deploy/deployment-collector.yaml) (container
> `collector`) and [deploy/job-loggen.yaml](deploy/job-loggen.yaml) to the full ECR refs.

---

## 6. Ensure a Fargate profile covers the namespace

Fargate only schedules pods whose namespace/labels match a Fargate profile. Create one for
`fieldcrypto-lab` (the profile's pod execution role gets ECR pull perms automatically):

```bash
eksctl create fargateprofile \
  --cluster "$CLUSTER_NAME" --region "$AWS_REGION" \
  --name fieldcrypto-lab --namespace fieldcrypto-lab
```

> If the cluster also has managed node groups, you can skip this and let the pods run on
> nodes instead — but on a Fargate-only cluster this step is required or the pods stay
> `Pending`.

---

## 7. Deploy

```bash
kubectl apply -k deploy/
kubectl -n fieldcrypto-lab rollout status deploy/fieldcrypto-collector --timeout=300s
```

> Fargate pods start slower than node pods (each gets its own microVM) — the 300s timeout
> gives them room. `kubectl apply -k` also creates the `fieldcrypto-loggen` Job, so an
> initial batch of load is sent automatically once the collector is Ready.

---

## 8. Run the POC and verify

```bash
# Watch the collector mask + encrypt the incoming logs
kubectl -n fieldcrypto-lab logs -l app=fieldcrypto-collector -f
```

In the debug output you should see, for each record:
- `user.email` → `[MASKED]`
- valid CPFs in the body → `[MASKED]`; invalid CPF-shaped numbers left intact
- `user.document` / `user.card` → base64 ciphertext, plus an `encryption.key_id` attribute

**Re-run the load generator** (the Job only runs once):

```bash
kubectl -n fieldcrypto-lab delete job fieldcrypto-loggen --ignore-not-found
kubectl -n fieldcrypto-lab create -f deploy/job-loggen.yaml
kubectl -n fieldcrypto-lab logs job/fieldcrypto-loggen        # prints the emitted-PII summary
```

**Decrypt on-demand** — copy a `key_id` and a ciphertext value from the collector logs,
then exec the `/decryptor` baked into the collector image (no shell needed):

```bash
kubectl -n fieldcrypto-lab exec deploy/fieldcrypto-collector -c collector -- \
  /decryptor --key-dir /var/keys --key-id key-XXXX --value <base64-ciphertext>
```

---

## 9. (Optional) Register with BindPlane

```bash
bindplane apply -f deploy/bindplane-agent-type.yaml   # metadata.name == dist.name
# then connect the collector; BindPlane manages it like BDOT.
```

---

## 10. Cleanup

```bash
kubectl delete -k deploy/
eksctl delete fargateprofile --cluster "$CLUSTER_NAME" --name fieldcrypto-lab --region "$AWS_REGION"
# optionally remove the images
aws ecr delete-repository --repository-name fieldcrypto-collector --region "$AWS_REGION" --force
aws ecr delete-repository --repository-name fieldcrypto-loggen --region "$AWS_REGION" --force
```

---

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| Pods stuck `Pending` on Fargate | No Fargate profile for `fieldcrypto-lab` (step 6), or namespace/labels don't match. |
| `exec format error` in pod logs | Image built for arm64. Rebuild with `--platform linux/amd64` (step 4). |
| `ImagePullBackOff` / `ErrImagePull` | Wrong image ref (step 5), ECR repo missing (step 2), or the Fargate pod execution role lacks ECR pull perms. |
| Collector `CrashLoopBackOff` | Check `kubectl -n fieldcrypto-lab logs deploy/fieldcrypto-collector`; usually a config typo in the ConfigMap. |
| loggen `connection refused` | Collector not Ready yet, or Service name/port changed. It targets `fieldcrypto-collector:4318`. |
| Keys gone after a restart | Expected with emptyDir — a fresh key is generated on start. For durable keys use a shared volume (EFS on Fargate) or the KMS provider. |
