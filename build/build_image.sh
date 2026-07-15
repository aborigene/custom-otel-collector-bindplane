#!/bin/bash
set -euo pipefail

cd /home/ec2-user/custom-otel-collector-bindplane

usage() {
  cat <<'EOF'
Usage: ./build/build_image.sh [--build-only | --build-deploy] [--image-tag TAG] [--region REGION] [collector source flags]

Modes:
  --build-only     Build and push images only.
  --build-deploy   Build, push, then update/apply Kubernetes manifests. (default)

Options:
  --image-tag TAG  Image tag to use (default: v2)
  --region REGION  AWS region to use (default: us-east-1)
  --collector-from-github-release
                   Build collector image from a prebuilt GitHub release tarball
                   instead of compiling it in Docker.
  --release-version VERSION
                   Release version to download for collector binary (default: IMAGE_TAG).
                   Accepts both 0.4.2 and v0.4.2.
  --github-repo OWNER/REPO
                   GitHub repo hosting release artifacts
                   (default: aborigene/custom-otel-collector-bindplane)
  -h, --help       Show this help text
EOF
}

MODE="build-deploy"
AWS_REGION="us-east-1"
IMAGE_TAG="v2"
COLLECTOR_FROM_GITHUB_RELEASE="false"
RELEASE_VERSION=""
GITHUB_REPO="aborigene/custom-otel-collector-bindplane"

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
    --collector-from-github-release)
      COLLECTOR_FROM_GITHUB_RELEASE="true"
      shift
      ;;
    --release-version)
      RELEASE_VERSION="$2"
      shift 2
      ;;
    --github-repo)
      GITHUB_REPO="$2"
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
echo "COLLECTOR_FROM_GITHUB_RELEASE=$COLLECTOR_FROM_GITHUB_RELEASE"
echo "GITHUB_REPO=$GITHUB_REPO"

build_collector_image_local() {
  docker buildx build --platform linux/amd64 \
    -f build/Dockerfile.collector \
    -t "$ECR/fieldcrypto-collector:$IMAGE_TAG" \
    --push .
}

build_collector_image_from_github_release() {
  local version release_tag asset_name base_url tmpdir tarball checksum_file checksum_line sha

  version="$RELEASE_VERSION"
  if [[ -z "$version" ]]; then
    version="$IMAGE_TAG"
  fi

  if [[ "$version" != v* ]]; then
    release_tag="v$version"
  else
    release_tag="$version"
  fi

  asset_name="custom-otel-collector-bindplane_${release_tag}_linux_amd64.tar.gz"
  base_url="https://github.com/${GITHUB_REPO}/releases/download/${release_tag}"

  tmpdir="$(mktemp -d)"
  tarball="${tmpdir}/${asset_name}"
  checksum_file="${tmpdir}/checksums.txt"

  echo "Downloading collector binary from GitHub release: ${base_url}/${asset_name}"
  curl -fL "${base_url}/${asset_name}" -o "$tarball"

  # Best-effort checksum validation when checksums asset exists.
  if curl -fsSL "${base_url}/custom-otel-collector-bindplane_${release_tag}_checksums.txt" -o "$checksum_file"; then
    checksum_line="$(grep " ${asset_name}$" "$checksum_file" || true)"
    if [[ -n "$checksum_line" ]]; then
      sha="$(echo "$checksum_line" | awk '{print $1}')"
      echo "${sha}  ${tarball}" | sha256sum -c -
    else
      echo "WARN: Could not find ${asset_name} in checksums file. Continuing without checksum verification."
    fi
  else
    echo "WARN: Checksums file not found for ${release_tag}. Continuing without checksum verification."
  fi

  tar -xzf "$tarball" -C "$tmpdir"

  if [[ ! -f "${tmpdir}/custom-otel-collector-bindplane" ]]; then
    echo "ERROR: Release artifact did not contain custom-otel-collector-bindplane binary." >&2
    rm -rf "$tmpdir"
    exit 1
  fi

  cat > "${tmpdir}/Dockerfile.collector.prebuilt" <<'EOF'
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY custom-otel-collector-bindplane /custom-otel-collector-bindplane
USER 10001:10001
ENTRYPOINT ["/custom-otel-collector-bindplane"]
CMD ["--config", "/etc/otel/config.yaml"]
EOF

  echo "Building collector image from prebuilt release binary (no local compile)."
  echo "NOTE: This mode ships only the collector binary; /decryptor is not included."
  docker buildx build --platform linux/amd64 \
    -f "${tmpdir}/Dockerfile.collector.prebuilt" \
    -t "$ECR/fieldcrypto-collector:$IMAGE_TAG" \
    --push "$tmpdir"

  rm -rf "$tmpdir"
}

# 2) Ensure ECR repos exist
for repo in fieldcrypto-collector fieldcrypto-loggen; do
  aws ecr describe-repositories --repository-names "$repo" --region "$AWS_REGION" >/dev/null 2>&1 \
    || aws ecr create-repository --repository-name "$repo" --region "$AWS_REGION"
done

# 3) Login Docker to ECR
aws ecr get-login-password --region "$AWS_REGION" \
  | docker login --username AWS --password-stdin "$ECR"

# 4) Build + push images (NOTE: correct Dockerfile path is build/...)
if [[ "$COLLECTOR_FROM_GITHUB_RELEASE" == "true" ]]; then
  build_collector_image_from_github_release
else
  build_collector_image_local
fi

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