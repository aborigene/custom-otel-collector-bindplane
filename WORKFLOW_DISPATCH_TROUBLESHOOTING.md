# Workflow Dispatch Troubleshooting Guide

## Problem: BindPlane Sync Workflow Not Running After Release

When the `Build Collector Release` workflow completes successfully but the `Sync Agent to BindPlane` workflow does not trigger automatically.

## Root Causes and Solutions

### 1. Missing Repository Permissions

**Symptom:** Dispatch fails silently; no sync workflow runs.

**Solution:** 
1. Go to repository Settings > Actions > General
2. Under "Workflow permissions", select **Read and write permissions**
3. Enable **Allow GitHub Actions to create and approve pull requests** (optional, depends on workflow needs)
4. Save changes

### 2. Incorrect Variable Context

**Problem Found:** 
- Original code used `github.event.repository.default_branch` which is not available in the context of tag push events
- This caused the dispatch to fail silently because the variable was empty

**Solution Applied:**
- Changed to hardcoded `main` (verified as the repository's default branch)
- Can be parameterized in the future if multiple branches need support

**Files Changed:**
- `.github/workflows/release.yaml`: Updated trigger step to use correct branch reference

### 3. Incorrect gh workflow run Syntax

**Problem Found:**
- Used `--input` flag which is not valid for `gh workflow run`
- Should use `-f key=value` format for workflow_dispatch inputs

**Solution Applied:**
- Changed to `gh workflow run bindplane-sync.yaml -f version="$version"`

### 4. Inadequate Error Handling

**Problem:**
- Dispatch failures were silenced, making debugging difficult

**Solution Applied:**
- Added explicit if/else with clear messaging
- Logs show whether dispatch succeeded or failed
- Non-blocking (release completes even if dispatch fails)

## How to Validate the Fix

### Quick Validation

1. Create a test tag:
   ```bash
   git tag v0.4.6-test
   git push origin v0.4.6-test
   ```

2. Monitor the workflows:
   - Go to repository Actions tab
   - Watch `Build Collector Release` job
   - Once release step completes, you should see dispatch logs:
     ```
     Dispatching bindplane-sync workflow for version: 0.4.6-test on ref: main
     ✅ Workflow dispatch successful. Sync should start automatically.
     ```

3. Check that `Sync Agent to BindPlane` workflow starts automatically
   - Should appear in Actions tab within 30 seconds
   - Will sync version to BindPlane

### Full Validation (if test passes)

1. Verify in BindPlane:
   ```bash
   bindplane get agent-versions -o table | grep custom-otel-collector-bindplane
   ```
   
2. Confirm test version appears (e.g., `0.4.6-test`)

3. Clean up test tag:
   ```bash
   git tag -d v0.4.6-test
   git push origin :refs/tags/v0.4.6-test
   ```

### Fallback: Manual Sync

If automated dispatch still fails:

1. Go to repository Actions tab
2. Find `Sync Agent to BindPlane` workflow
3. Click "Run workflow"
4. Enter version (e.g., `0.4.6`)
5. Click "Run workflow"

## Debugging Checklist

If workflows still aren't running:

- [ ] Repository Settings > Actions > General has "Read and write permissions" enabled
- [ ] `.github/workflows/release.yaml` has `permissions: { actions: write, contents: write }`
- [ ] `.github/workflows/bindplane-sync.yaml` has `on: { workflow_dispatch: ... }`
- [ ] No branch protection rules blocking workflow changes
- [ ] Secrets `BINDPLANE_API_KEY` and `BINDPLANE_REMOTE_URL` are set and valid
- [ ] Tag follows semantic versioning format (e.g., `v0.4.6`, not `v0.4.6-test` with characters after patch)

## Related Files

- `.github/workflows/release.yaml` — Builds release artifacts and triggers sync
- `.github/workflows/bindplane-sync.yaml` — Syncs version to BindPlane
- `BINDPLANE_SYNC_SETUP.md` — General sync setup documentation

## Timeline of Changes

**Issue:** Sync workflow not running after release completion

**Root Cause:** 
1. Invalid context variable `github.event.repository.default_branch` 
2. Incorrect `gh workflow run` flag syntax

**Fix Applied:**
- Use hardcoded `main` branch (verified as default)
- Use `-f version="$value"` flag format
- Added better error messaging

**Status:** Ready for testing
