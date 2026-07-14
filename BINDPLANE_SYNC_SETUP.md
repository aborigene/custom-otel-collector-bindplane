# BindPlane Auto-Sync Setup Guide

This guide walks through setting up automatic BindPlane synchronization for the custom agent on every release.

## Overview

The workflow automatically syncs your custom agent to BindPlane whenever:
1. A new release is published on GitHub (automatic)
2. You manually trigger the workflow with a specific version (manual)

## Setup Steps

### Step 1: Generate BindPlane API Key

1. Log in to your BindPlane enterprise environment
2. Navigate to **Settings** → **API Keys** (or **Organization** → **API Keys**)
3. Create a new API key with **Organization Admin** permissions
4. Copy the full API key (format: `bp_01XXXX...`)

### Step 2: Configure GitHub Secrets

1. Go to your repository: https://github.com/aborigene/custom-otel-collector-bindplane
2. Click **Settings** → **Secrets and variables** → **Actions**
3. Create two repository secrets:

#### Secret 1: `BINDPLANE_API_KEY`
- **Name:** `BINDPLANE_API_KEY`
- **Value:** Paste the API key from Step 1

#### Secret 2: `BINDPLANE_REMOTE_URL`
- **Name:** `BINDPLANE_REMOTE_URL`
- **Value:** Your BindPlane URL (e.g., `https://app.bindplane.com`)

### Step 3: Verify Workflow Setup

The workflow file `.github/workflows/bindplane-sync.yaml` is already in the repository. It will:
- Trigger automatically on every GitHub release published
- Extract the version from the release tag
- Call `bindplane sync agent-version` with that version
- Verify the sync succeeded

## Usage

### Automatic Sync (On Release Published)

1. Build your collector:
   ```bash
   make build
   ```

2. Create and push a new git tag:
   ```bash
   git tag v0.2.0
   git push origin v0.2.0
   ```

3. Create a GitHub Release:
   - Go to https://github.com/aborigene/custom-otel-collector-bindplane/releases
   - Click "Draft a new release"
   - Select tag `v0.2.0`
   - Add title and description
   - Click "Publish release"

4. The workflow automatically runs and syncs the version to BindPlane

5. Verify in BindPlane:
   ```bash
   bindplane get agent-versions -o table | grep custom-otel-collector-bindplane
   ```

### Manual Sync (Workflow Dispatch)

If you need to sync a specific version manually:

1. Go to https://github.com/aborigene/custom-otel-collector-bindplane/actions
2. Click **Sync Agent to BindPlane** workflow
3. Click **Run workflow**
4. Enter the version (without `v` prefix, e.g., `0.2.0`) or leave empty for `latest`
5. Click **Run workflow**

### Troubleshooting

#### Workflow Failed: "Permission Denied"
- Verify the BindPlane API key has **Organization Admin** role
- Test locally: `bindplane sync agent-version --agent-type custom-otel-collector-bindplane --version latest`

#### Workflow Failed: "Unable to Connect"
- Verify `BINDPLANE_REMOTE_URL` is correct and accessible
- Check network connectivity from GitHub Actions runners

#### Version Not Appearing in BindPlane
- Verify the release tag matches semantic versioning (e.g., `v0.2.0`)
- Check release artifacts are present on GitHub
- Try manual sync with `--all`: `bindplane sync agent-version --agent-type custom-otel-collector-bindplane --all`

#### Rate Limiting Issues
- GitHub Actions runners may hit rate limits downloading BindPlane CLI
- Solution: Pre-build and cache the CLI binary (advanced)

## Advanced: Manual Sync Commands

For one-time or emergency syncs without workflow:

```bash
# Set up BindPlane profile (one-time)
bindplane profile set \
  --api-key YOUR_API_KEY \
  --remote-url https://app.bindplane.com

# Sync latest version
bindplane sync agent-version \
  --agent-type custom-otel-collector-bindplane \
  --version latest

# Sync specific version
bindplane sync agent-version \
  --agent-type custom-otel-collector-bindplane \
  --version 0.2.0

# Sync all missing versions
bindplane sync agent-version \
  --agent-type custom-otel-collector-bindplane \
  --all

# Verify synced versions
bindplane get agent-versions -o table | grep custom-otel-collector-bindplane
```

## Release Workflow Summary

1. **Code → Build → Test**
   - Make code changes
   - Run `make build` locally to test

2. **Tag & Push**
   ```bash
   git tag v0.2.0
   git push origin v0.2.0
   ```

3. **GitHub Actions Build** (automatic)
   - Build collector binaries
   - Create release with artifacts

4. **GitHub Release** (manual)
   - Publish release on GitHub UI
   - Triggers `bindplane-sync.yaml` workflow

5. **BindPlane Sync** (automatic)
   - Workflow pulls release artifacts
   - Syncs version to BindPlane
   - Agent is now installable

6. **Install Agent** (manual, in BindPlane UI)
   - Go to Install Agents
   - Select "Custom OTel Collector (FieldCrypto)"
   - Choose synced version
   - Configure and deploy

## Next Steps

1. Generate the BindPlane API key (see Step 1)
2. Add GitHub secrets (see Step 2)
3. Publish a test release to verify the workflow runs
4. Check BindPlane to confirm version appears

Questions? Refer to the workflow logs at: https://github.com/aborigene/custom-otel-collector-bindplane/actions
