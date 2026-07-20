# Build and Release Strategy for This Repository

## Executive Summary

This repository should follow a two-lane model:

1. Development lane (fast local iteration): use OCB-style local builds for coding and smoke tests.
2. Release lane (BindPlane-managed production path): use ODB release artifacts and sync versions to BindPlane.

Recommended canonical path for this repo:

- Keep OCB for local developer speed.
- Use ODB release artifacts as the only source of truth for versions consumed by BindPlane.
- Drive release + sync from GitHub Actions, and make local release run the same pipeline steps.

This keeps one authoritative build process while still preserving fast local debugging.

## Why This Matters

In our current investigation, the custom agent reaches BindPlane and gets configuration assigned, but rollout remains pending and never transitions to current.

Observed behavior:

- Agent shows assigned + pending, but no current config state.
- Rollout shows pending=1, completed=0, errors=0, incompatible=0.
- Collector keeps running local pipeline behavior (debug exporter output visible).

This pattern means metadata compatibility is fixed, but config activation lifecycle is still not completing.

## ODB vs OCB: Clear Difference

### OCB (OpenTelemetry Collector Builder)

What it does:

- Builds a collector binary from a manifest (components list).
- Great for local development and custom component composition.

What it does not guarantee by itself:

- A complete BindPlane-managed runtime/release lifecycle equivalent to BDOT managed releases.

Use OCB when:

- You are coding processors/extensions.
- You want fast local test cycles.

### ODB (otel-distro-builder, BindPlane distro release flow)

What it does:

- Produces release artifacts (tar/rpm/sbom/checksums) and packaging expected by BindPlane release/sync model.
- Integrates cleanly with AgentVersion sync in BindPlane.

Use ODB when:

- You publish versions for BindPlane installation/management.
- You need reproducible release artifacts and a stable release contract.

## Recommended Path for This Repo

Use ODB as the release system of record.

- Release artifacts and version sync to BindPlane come from GitHub Actions release pipeline.
- Local image rebuilds are for development/testing only unless they originate from the same release inputs and version.

In short:

- OCB for development.
- ODB for release and BindPlane-managed lifecycle.

## Current Repo Assets (Good Foundation)

Existing workflows already align with this direction:

- Release artifacts workflow: [.github/workflows/release.yaml](.github/workflows/release.yaml)
- BindPlane sync workflow: [.github/workflows/bindplane-sync.yaml](.github/workflows/bindplane-sync.yaml)

Existing local/deploy script:

- Local image build/deploy helper: [build/build_image.sh](build/build_image.sh)

Version manifest:

- Component/version pins: [build/manifest.yaml](build/manifest.yaml)

## Single Place for Building Everything

### Target model

One version source and one release entry point:

1. Version is set in [build/manifest.yaml](build/manifest.yaml) and release tag.
2. GitHub Actions builds release artifacts and publishes release.
3. GitHub Actions syncs that released version into BindPlane.
4. Kubernetes images used by environments should map to the same released version.

### Practical standard to adopt

- All official versions are created by tag-triggered CI only.
- Local builds must use non-release tags (for example: dev-<date>-<sha>) and never overwrite official release tags.
- If a version is in BindPlane AgentVersion, there must be a matching GitHub release artifact for it.

## Detailed Process

## 1) Developer loop (local)

Use local build/deploy only for iteration:

- Update code.
- Build/test locally.
- Deploy to test namespace with temporary image tag.
- Validate behavior quickly.

Do not treat local images as official release outputs.

## 2) Release loop (official)

1. Update version to next target in [build/manifest.yaml](build/manifest.yaml) (for example: v0.4.2).
2. Create and push git tag (v0.4.2).
3. [release.yaml](.github/workflows/release.yaml) builds artifacts and publishes release.
4. [bindplane-sync.yaml](.github/workflows/bindplane-sync.yaml) syncs that version to BindPlane.
5. Deploy images tagged with the same release version.
6. Start/verify rollout in BindPlane.

## 3) BindPlane compatibility and assignment rules

For a configuration to appear compatible with a fleet:

- Configuration labels must match agent type/platform.
- Configuration selector must be satisfiable by fleet/agent labels.

For this repo, selector should remain aligned with fleet labels (example):

- attributes/os.type: linux
- attributes/service.name: custom-otel-collector-bindplane

Avoid synthetic selector labels that agents do not carry (example: configuration: <name>), unless you intentionally add those labels to agents.

## 4) Rollout verification checklist

After assignment and rollout start:

1. Fleet points to intended configuration.
2. Rollout moves pending -> completed.
3. Agent shows configurationStatus.current (not only assigned/pending).
4. Runtime behavior reflects remote config (not only local static config behavior).

## 5) Evidence capture checklist for troubleshooting

When rollout is stuck:

- Agent details (yaml/json)
- Rollout details/status
- Fleet and configuration yaml
- Available components hash/details
- Collector logs around startup/opamp/config apply

Keep these artifacts attached to each incident to avoid repeated guesswork.

## Repository Cleanup Plan

## Phase A: Documentation consistency

- Keep this file as the build/release policy source of truth.
- Update [README.md](README.md) and [DEPLOY.md](DEPLOY.md) to reference this policy.

## Phase B: Build flow consolidation

- Keep [release.yaml](.github/workflows/release.yaml) as canonical release build.
- Keep [bindplane-sync.yaml](.github/workflows/bindplane-sync.yaml) as canonical sync path.
- Restrict [build/build_image.sh](build/build_image.sh) usage to dev/test workflows.

## Phase C: Version governance

- Enforce version format consistency (prefer vX.Y.Z everywhere).
- Enforce tag-to-release-to-agent-version mapping as a release gate.

## Phase D: CI policy guardrails

- Block publishing official tags from local scripts.
- Optionally add CI check that compares manifest version, git tag, and release metadata.

## Immediate Next Steps

1. Keep official releases tag-driven and workflow-only.
2. Treat [release.yaml](.github/workflows/release.yaml) as the only release builder and [bindplane-sync.yaml](.github/workflows/bindplane-sync.yaml) as the only BindPlane sync path.
3. Keep [build/build_image.sh](build/build_image.sh) for development and smoke tests only.
4. Remove or update any docs that still describe local build steps as part of the release or sync path.

## Decision Record

For this repository, the recommended direction is:

- ODB-driven release artifacts and BindPlane sync as the official path.
- OCB/local image loops as development-only support path.

This gives the clearest operational model and the best chance of reproducible BindPlane-managed behavior across future implementations.
