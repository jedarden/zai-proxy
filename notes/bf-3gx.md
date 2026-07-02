# Task bf-3gx: Retire old container source dirs in ardenone-cluster

## Summary

Successfully retired old container source directories from ardenone-cluster after migration to the new zai-proxy repo.

## Changes Made

### 1. declarative-config (commit 18bac86)
- Removed `k8s/iad-ci/argo-workflows/zai-proxy-build.yml`
- Removed `k8s/iad-ci/argo-workflows/zai-proxy-dashboard-build.yml`
- These old templates still referenced `git://git.ardenone.com/jedarden/zai-proxy.git`
- The correct templates are `*-workflowtemplate.yml` which use Forgejo (jedarden/zai-proxy)

### 2. ardenone-cluster (commit 4b4468842)
- Removed `containers/zai-proxy/` directory (315 files deleted)
- Removed `containers/zai-proxy-dashboard/` directory
- Source code now lives in the new jedarden/zai-proxy repo

## Current State

- New CI WorkflowTemplates: `zai-proxy-build-workflowtemplate.yml` and `zai-proxy-dashboard-build-workflowtemplate.yml`
- These templates:
  - Use Forgejo: `jedarden/zai-proxy` repo
  - Build from `proxy/` and `dashboard/` subdirectories
  - Push images to Docker Hub: `ronaldraygun/zai-proxy` and `ronaldraygun/zai-proxy-dashboard`

## Verification

No remaining references to `git://git.ardenone.com/jedarden/zai-proxy.git` in declarative-config.

## Related

- Migration plan: docs/plan/plan.md § Migration Status (item 6)
- Depends on: bf-3kr (push Argo Workflow templates to declarative-config and verify builds)
