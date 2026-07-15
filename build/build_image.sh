#!/bin/bash
set -euo pipefail

cd /home/ec2-user/custom-otel-collector-bindplane

usage() {
  cat <<'EOF'
Usage: ./build/build_image.sh [--build-only | --build-deploy] [--image-tag TAG] [--region REGION]

Modes:
  --build-only     Build and push images only.
  --build-deploy   Build, push, then update/apply Kubernetes manifests. (default)

Options:
  --image-tag TAG  Image tag to use (default: v2)
  --region REGION  AWS region to use (default: us-east-1)
  -h, --help       Show this help text
EOF
}

MODE="build-deploy"
AWS_REGION="us-east-1"
IMAGE_TAG="v2"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --build-only)
      MODE="build-only"
      shift
      ;;
    --build-deploy)
      MODE="build-deploy"
      shift
      ;;
    --image-tag)
      IMAGE_TAG="$2"
      shift 2
      ;;
    --region)
      AWS_REGION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

# 1) Define all required vars
export AWS_REGION
export IMAGE_TAG
export AWS_ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"
export ECR="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com"

echo "AWS_ACCOUNT_ID=$AWS_ACCOUNT_ID"
echo "ECR=$ECR"
echo "IMAGE_TAG=$IMAGE_TAG"
echo "MODE=$MODE"

# 2) Ensure ECR repos exist
for repo in fieldcrypto-collector fieldcrypto-loggen; do
  aws ecr describe-repositories --repository-names "$repo" --region "$AWS_REGION" >/dev/null 2>&1 \
    || aws ecr create-repository --repository-name "$repo" --region "$AWS_REGION"
done

# 3) Login Docker to ECR
aws ecr get-login-password --region "$AWS_REGION" \
  | docker login --username AWS --password-stdin "$ECR"

# 4) Build + push images (NOTE: correct Dockerfile path is build/...)
docker buildx build --platform linux/amd64 \
  -f build/Dockerfile.collector \
  -t "$ECR/fieldcrypto-collector:$IMAGE_TAG" \
  --push .

docker buildx build --platform linux/amd64 \
  -f build/Dockerfile.loggen \
  -t "$ECR/fieldcrypto-loggen:$IMAGE_TAG" \
  --push .

if [[ "$MODE" == "build-only" ]]; then
  echo "Build-only mode complete. Skipping deploy."
  exit 0
fi

# Refresh ECR pull credentials in-cluster (tokens expire periodically).
kubectl -n fieldcrypto-lab delete secret ecr-regcred --ignore-not-found
kubectl -n fieldcrypto-lab create secret docker-registry ecr-regcred \
  --docker-server="$ECR" \
  --docker-username=AWS \
  --docker-password="$(aws ecr get-login-password --region "$AWS_REGION")"
kubectl -n fieldcrypto-lab patch serviceaccount default \
  -p '{"imagePullSecrets":[{"name":"ecr-regcred"}]}'

# 5) Update deploy image refs
KUSTOMIZATION_FILE="deploy/kustomization.yaml"
cp "$KUSTOMIZATION_FILE" "${KUSTOMIZATION_FILE}.bak"

# Update collector image name/tag
awk -v ecr="$ECR" -v tag="$IMAGE_TAG" '
  /^[[:space:]]*-[[:space:]]name:[[:space:]]*fieldcrypto-collector[[:space:]]*$/ { inblk=1; print; next }
  inblk && /^[[:space:]]*newName:[[:space:]]*/ { print "    newName: " ecr "/fieldcrypto-collector"; next }
  inblk && /^[[:space:]]*newTag:[[:space:]]*/ { print "    newTag: " tag; inblk=0; next }
  { print }
' "$KUSTOMIZATION_FILE" > "${KUSTOMIZATION_FILE}.tmp" && mv "${KUSTOMIZATION_FILE}.tmp" "$KUSTOMIZATION_FILE"

# Update loggen image name/tag
awk -v ecr="$ECR" -v tag="$IMAGE_TAG" '
  /^[[:space:]]*-[[:space:]]name:[[:space:]]*fieldcrypto-loggen[[:space:]]*$/ { inblk=1; print; next }
  inblk && /^[[:space:]]*newName:[[:space:]]*/ { print "    newName: " ecr "/fieldcrypto-loggen"; next }
  inblk && /^[[:space:]]*newTag:[[:space:]]*/ { print "    newTag: " tag; inblk=0; next }
  { print }
' "$KUSTOMIZATION_FILE" > "${KUSTOMIZATION_FILE}.tmp" && mv "${KUSTOMIZATION_FILE}.tmp" "$KUSTOMIZATION_FILE"

# 6) Apply secret + manifests and restart collector
kubectl apply -f deploy/secret-bindplane-agent.yaml

# Render once, then apply non-Job resources first. This avoids starting the load
# generator while the collector is still provisioning on Fargate.
RENDERED_MANIFEST="$(mktemp)"
NON_JOB_MANIFEST="$(mktemp)"
JOB_MANIFEST="$(mktemp)"

kubectl kustomize deploy > "$RENDERED_MANIFEST"

awk 'BEGIN{RS="---"; ORS="---\n"} $0 !~ /kind:[[:space:]]*Job/ {print}' "$RENDERED_MANIFEST" > "$NON_JOB_MANIFEST"
awk 'BEGIN{RS="---"; ORS="---\n"} $0 ~ /kind:[[:space:]]*Job/ && $0 ~ /name:[[:space:]]*fieldcrypto-loggen/ {print}' "$RENDERED_MANIFEST" > "$JOB_MANIFEST"

kubectl apply -f "$NON_JOB_MANIFEST"
kubectl rollout restart deployment/fieldcrypto-collector -n fieldcrypto-lab
kubectl rollout status deployment/fieldcrypto-collector -n fieldcrypto-lab --timeout=300s

# Recreate the loggen Job only after collector is ready.
kubectl -n fieldcrypto-lab delete job fieldcrypto-loggen --ignore-not-found
if [[ -s "$JOB_MANIFEST" ]]; then
  kubectl apply -f "$JOB_MANIFEST"
else
  echo "WARN: fieldcrypto-loggen Job manifest was not found in rendered kustomize output."
fi

rm -f "$RENDERED_MANIFEST" "$NON_JOB_MANIFEST" "$JOB_MANIFEST"

# 7) Verify
kubectl get pods -n fieldcrypto-lab
kubectl logs -n fieldcrypto-lab deploy/fieldcrypto-collector --tail=200