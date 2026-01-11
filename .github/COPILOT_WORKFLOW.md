# Copilot Workflow Automation

This document describes the automated workflow for GitHub Copilot integration.

## Workflow Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Issue with 'copilot' label              │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  copilot-auto-assign.yml (every 5 min)                     │
│  - Checks if Copilot has open PR                           │
│  - If not, assigns Copilot to highest priority issue       │
│  - Mentions @copilot to start work                         │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Copilot creates PR from issue                             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  auto-approve-workflows.yml (every 2 min)                  │
│  - Finds "action_required" workflow runs                   │
│  - Triggers CI via workflow_dispatch (bypasses approval)   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  CI runs (lint, test, build)                               │
└─────────────────────────────────────────────────────────────┘
                              │
                    ┌─────────┴─────────┐
                    ▼                   ▼
              ✅ CI passes         ❌ CI fails
                    │                   │
                    │                   ▼
                    │   ┌─────────────────────────────────────┐
                    │   │  copilot-ci-feedback.yml            │
                    │   │  - Posts @copilot with error details│
                    │   │  - Tracks fix attempts (max 2)      │
                    │   └─────────────────────────────────────┘
                    │                   │
                    │         ┌─────────┴─────────┐
                    │         ▼                   ▼
                    │   Attempt 1-2          Attempt 3+
                    │   (Copilot fixes)      (Escalate)
                    │         │                   │
                    │         ▼                   ▼
                    │   ┌───────────┐   ┌─────────────────────┐
                    │   │ CI reruns │   │ copilot-escalate.yml│
                    │   └───────────┘   │ - Adds label        │
                    │                   │   'needs-manual-help'│
                    │                   │ - cc @m0nk1111      │
                    │                   └─────────────────────┘
                    │                             │
                    │                             ▼
                    │                   ┌─────────────────────┐
                    │                   │ MANUAL INTERVENTION │
                    │                   │ Discuss in VS Code  │
                    │                   │ Copilot Chat        │
                    │                   └─────────────────────┘
                    │                             │
                    ▼                             ▼
┌─────────────────────────────────────────────────────────────┐
│  PR merged                                                  │
└─────────────────────────────────────────────────────────────┘
```

## Workflows

| Workflow | Purpose | Trigger |
|----------|---------|---------|
| `copilot-auto-assign.yml` | Assign Copilot to issues with `copilot` label | Every 5 min, or when PR closed |
| `auto-approve-workflows.yml` | Bypass first-time contributor approval | Every 2 min |
| `copilot-ci-feedback.yml` | Notify Copilot of CI failures | On CI completion |
| `copilot-escalate.yml` | Label stuck PRs for manual help | Every 5 min |
| `copilot-rebase.yml` | Auto-rebase Copilot branches | On push to main |
| `copilot-review-feedback.yml` | Apply Copilot review suggestions | On review comment |

## Escalation Policy

After **2 failed CI fix attempts** within 2 hours:
1. PR is labeled `needs-manual-help`
2. @m0nk1111 is notified
3. Auto-fix loop stops

**Manual intervention:**
1. Open VS Code
2. Start Copilot Chat (this agent)
3. Discuss the PR and remaining issues
4. Agent fixes the loose ends

## Labels

| Label | Meaning |
|-------|---------|
| `copilot` | Issue is eligible for Copilot assignment |
| `needs-manual-help` | Copilot couldn't fix CI after 2 attempts |

## Self-Hosted Runner

All workflows run on: `[self-hosted, Linux, ci, go]`

Runner: `oelala-storage-runner`
- Go 1.24+ pre-installed
- golangci-lint pre-installed
- No billing (self-hosted = free)
