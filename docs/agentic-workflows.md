---
marp: true
theme: default
paginate: true
backgroundColor: #0d1117
color: #e6edf3
style: |
  section {
    font-family: 'Segoe UI', system-ui, sans-serif;
  }
  h1, h2, h3 {
    color: #58a6ff;
  }
  strong {
    color: #f0883e;
  }
  em {
    color: #8b949e;
  }
  code {
    background: #161b22;
    color: #79c0ff;
    padding: 2px 6px;
    border-radius: 4px;
  }
  a {
    color: #58a6ff;
  }
  section.lead h1 {
    color: #f0883e;
    font-size: 2.2em;
  }
  section.lead h2 {
    color: #8b949e;
    font-size: 1.1em;
    font-weight: normal;
  }
  table {
    font-size: 0.75em;
    color: #e6edf3;
  }
  th {
    background: #161b22;
    color: #58a6ff;
  }
  td {
    background: #0d1117;
    border-color: #30363d;
  }
  mermaid {
    font-size: 0.7em;
  }
---

<!-- _class: lead -->

# 🤖 Autonomous OSS Development with GitHub Agentic Workflows

## How KSail runs itself — and I just review PRs

---

# What is KSail?

A **Go CLI** and SDK for spinning up local Kubernetes clusters with GitOps built in.

- Embeds kubectl, helm, kind, k3d, vcluster, flux, argocd as **Go libraries**
- Only requires **Docker** — no tool installation
- Supports Vanilla, K3s, Talos, and VCluster distributions

> 💡 One binary, full local GitOps — from `ksail cluster init` to a running cluster.

---

# The Agentic Workflow Pipeline

| Layer | Workflow | Schedule | What it does |
|-------|----------|----------|-------------|
| **Strategy** | Weekly Strategy | Mon / Wed | Roadmap, competitive analysis, content |
| **Planning** | Repo Assist | Every 12h | Translates roadmap → issues → PRs |
| **Docs** | Daily Docs | Daily | Syncs documentation with code changes |
| **Infra** | Workflow Maintenance | Daily | Updates CI, optimizes workflows |
| **Safety** | CI Doctor | On failure | Investigates CI failures, files issues |
| **Cleanup** | Agentics Maintenance | Every 2h | Expires stale discussions, issues, PRs |

---

# How It All Connects

```mermaid
flowchart TD
    A["🗺️ Weekly Strategy\nMarket research → Now / Next / Later Roadmap"] --> B
    B["📋 Repo Assist\nRoadmap → Issues → Draft PRs"] --> C
    C["⚙️ CI Pipeline\nLint → Build → Unit Tests → E2E → Benchmarks"] --> D
    D["👨‍💻 Me: Promote Draft → In Review"] --> E
    E["🤖 Agent Merge via Skills\nRebase, fix CI, address review, merge"]
```

---

# AI Guardrails

```mermaid
flowchart TD
    A["🚨 Agent opens PR"] --> B
    B["🛡️ GHAS Security & CodeQL\n(vulnerability scanning)"] --> C
    C["🔒 StepSecurity\n(egress policy auditing)"] --> D
    D["🧹 Linting\nMegaLinter + golangci-lint"] --> E
    E["🧪 Unit Test Suite\ngo test ./... + Codecov"] --> F
    F["🚀 E2E / System Test Suite\nKind × K3d × Talos × VCluster"] --> G
    G["✅ Agent Merge via Skills\nRebase, fix, merge"]
```

*Every layer must pass before an agent PR can merge.*

---

# My Role: Minimal but Intentional

🖥️ A **Mac Mini runs 24/7 at home**, firing scheduled prompts that trigger agents to work on KSail autonomously.

### What I actually do:

- ✅ **Promote PRs** from Draft → In Review *(the main gate)*
- 👀 **Occasional check-ins** to review agent decisions
- 🛠️ **Build things myself** when I want to — I hook into the same process

### What the agents handle:

- 🗺️ Roadmap creation and competitive analysis
- 📋 Issue creation and prioritization
- 💻 Code changes, tests, and documentation
- 🔄 CI failure investigation and resolution
- 🧹 Stale content cleanup

> The workflow is designed so **nothing merges without my approval**.

---

<!-- _class: lead -->

# 🚀 The Result

## A single developer maintaining a complex Kubernetes tool
## with an army of autonomous agents — and a Mac Mini.

**github.com/devantler-tech/ksail**
