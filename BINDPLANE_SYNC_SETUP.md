atal: Not a valid object name main
❌ Release aborted: Tag 'v0.4.6-test' is not on main branch
   Only tags pushed from main branch can trigger releases
Error: Process completed with exit code 1.
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

### Preferred Release Flow (GitHub Actions)

Use the repository workflows as the source of truth for every release and sync.

1. Update the version in [build/manifest.yaml](build/manifest.yaml) and create a git tag:
   ```bash
   git tag v0.2.0
   git push origin v0.2.0
   ```

2. GitHub Actions runs [Build Collector Release](.github/workflows/release.yaml) automatically for the tag push, builds the distribution artifacts, and publishes the GitHub Release.

3. The published release triggers [Sync Agent to BindPlane](.github/workflows/bindplane-sync.yaml) automatically, which syncs the new agent version into BindPlane.

4. Verify the version in BindPlane:
   ```bash
   bindplane get agent-versions -o table | grep custom-otel-collector-bindplane
   ```

### Manual Release or Sync via GitHub Actions

If you need to run the release or sync step manually, use GitHub Actions instead of local build commands:

1. Open the repository Actions tab.
2. Run [Build Collector Release](.github/workflows/release.yaml) with the desired version input when you need to build a release from a branch.
3. Run [Sync Agent to BindPlane](.github/workflows/bindplane-sync.yaml) if you need to resync an already published release or sync a specific version.
4. Verify the version in BindPlane with `bindplane get agent-versions -o table | grep custom-otel-collector-bindplane`.

### Troubleshooting

#### Workflow Failed: "Permission Denied"
- Verify the BindPlane API key has **Organization Admin** role
- Re-run the workflow from the Actions tab after confirming the secret values

#### Workflow Failed: "Unable to Connect"
- Verify `BINDPLANE_REMOTE_URL` is correct and accessible
- Check network connectivity from GitHub Actions runners

#### Version Not Appearing in BindPlane
- Verify the release tag matches semantic versioning (e.g., `v0.2.0`)
- Check release artifacts are present on GitHub
- Re-run [Sync Agent to BindPlane](.github/workflows/bindplane-sync.yaml) from the Actions tab for the target version

#### Rate Limiting Issues
- GitHub Actions runners may hit rate limits downloading BindPlane CLI
- Solution: Pre-build and cache the CLI binary (advanced)

## Release Workflow Summary

1. **Code → Build → Test**
   - Make code changes
   - Run the normal local test suite for development only

2. **Tag & Push**
   ```bash
   git tag v0.2.0
   git push origin v0.2.0
   ```

3. **GitHub Actions Build** (automatic)
   - [release.yaml](.github/workflows/release.yaml) builds collector artifacts
   - The workflow creates or updates the GitHub Release for that tag

4. **GitHub Actions Sync** (automatic)
   - [bindplane-sync.yaml](.github/workflows/bindplane-sync.yaml) runs when the release is published
   - The workflow syncs the released agent version into BindPlane

5. **Install Agent** (manual, in BindPlane UI)
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
