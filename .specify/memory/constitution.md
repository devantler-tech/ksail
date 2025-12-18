<!--
Sync Impact Report:
- Version: 1.0.3 → 1.0.4
- Modified principles:
  - V. Test-First Development: Strengthened black-box requirement (public APIs only) and clarified package-level testing convention
- Added sections:
  - Development Workflow & Validation: added VS Code workspace navigation guidance
- Removed sections:
  - Serena Semantic Tools (references removed; tooling guidance updated)
- Templates requiring updates:
  ✅ .specify/templates/plan-template.md
  ✅ .specify/templates/spec-template.md
  ✅ .specify/templates/tasks-template.md
  ⚠ .specify/templates/commands/*.md (not present in this repository)
- Follow-up TODOs: None
-->

# KSail-Go Constitution

## Core Principles

### I. KISS (Keep It Simple, Stupid)

**Simplicity over complexity.** Prefer straightforward solutions that are easy to understand and maintain. Avoid over-engineering or adding unnecessary abstraction layers. If a simple approach solves the problem effectively, use it.

**Rationale**: Complex code is harder to understand, maintain, test, and debug. Simple solutions reduce cognitive load and make the codebase accessible to all contributors.

### II. DRY (Don't Repeat Yourself)

**Eliminate duplication.** Extract common logic into reusable functions, packages, or interfaces. Every piece of knowledge MUST have a single, unambiguous representation in the codebase. Use Go's interface-based design to share behavior without duplicating code.

**Rationale**: Duplication leads to inconsistent behavior, maintenance burden, and bugs when one instance is updated but others are forgotten.

### III. YAGNI (You Aren't Gonna Need It)

**Implement only what's needed now.** Do NOT add functionality based on speculative future requirements. Focus on current, well-defined requirements. Additional features can be added later when they're actually needed.

**Rationale**: Speculative features add complexity, increase maintenance burden, and often go unused. Build what's needed, when it's needed.

### IV. Interface-Based Design (NON-NEGOTIABLE)

**Depend on abstractions, not concrete implementations.** All major components MUST be defined as interfaces first. Use Go's implicit interface satisfaction. Keep interfaces small and focused (Interface Segregation Principle).

**Requirements**:

- Define interface contracts before implementations
- Use mockery to generate test mocks from interfaces
- Each interface should have a clear, single purpose
- Implementations must honor interface contracts (Liskov Substitution Principle)

**Rationale**: Interfaces enable testability, flexibility, and clear contracts between components. They allow implementations to be swapped without breaking consumers.

### V. Test-First Development (NON-NEGOTIABLE)

**Tests MUST be written before implementation.** Follow the Red-Green-Refactor cycle: Write failing tests → Implement minimal code to pass → Refactor while keeping tests green.

**Requirements**:

- Tests MUST validate behavior via public APIs only (black-box): exported identifiers and CLI surface
- Tests MUST be written as black-box tests using external test packages (`package <name>_test`) by default
- Tests MUST NOT assert on unexported functions, unexported fields, or internal implementation details
- Refactors that do not change public behavior SHOULD NOT require test rewrites
- `go test ./...` is the baseline test runner; system tests may exist in CI for end-to-end validation
- Snapshot testing is allowed for CLI output consistency
- Pre-commit hooks MUST pass: `mockery`, `go test ./...`, `golangci-lint run`

**Rationale**: Test-first ensures testable code, prevents regressions, provides living documentation, and enables confident refactoring.

### VI. Package-First Architecture

**Every feature starts as a well-defined package.** Packages MUST be self-contained, independently testable, and documented. Organize by domain concern, not technical layer.

**Requirements**:

- Core business logic in `pkg/` packages
- CLI commands in `cmd/` (thin wrappers around `pkg/` logic)
- Avoid `internal/` in KSail-Go to keep functionality importable by external projects
- Clear package purposes with godoc comments
- No circular dependencies

**Rationale**: Package-first design promotes modularity, reusability, and clear separation of concerns. It makes code easier to test and understand.

### VII. Code Quality Standards (NON-NEGOTIABLE)

**All code MUST pass quality gates.** Use automated tooling to enforce standards consistently across the codebase.

**Requirements**:

- `golangci-lint run` MUST pass (timeout: 120+ seconds)
- `go test ./...` MUST pass (timeout: 90+ seconds)
- `mockery` MUST generate all mocks successfully
- Pre-commit hooks enabled and passing
- Code coverage tracked via codecov.io
- NEVER CANCEL long-running builds or tests

**Rationale**: Automated quality gates catch issues early, ensure consistency, and maintain high code quality without manual oversight.

## Design Patterns & Refactoring

### Design Pattern Application

Apply design patterns judiciously—only when they solve a real problem, not for the sake of using patterns.

**Approved Patterns for KSail-Go**:

- **Factory Method**: Creating provisioners based on distribution type (Kind, K3d, EKS)
- **Strategy**: Different validation/provisioning strategies per distribution
- **Adapter**: Wrapping external tools (Kind, K3d, eksctl) with unified interfaces
- **Decorator**: Adding logging, metrics, retry logic to core operations
- **Facade**: Simplified high-level operations hiding complex workflows
- **Command**: CLI command structure (via Cobra framework)

**Pattern Usage Rules**:

- Use when pattern solves a current, concrete problem
- Use when pattern improves code clarity and maintainability
- Avoid using patterns "just because" or for speculative needs
- Avoid forcing patterns where simple solutions work better

### Code Smells & Refactoring

**Refactor immediately when encountering**:

- Long functions (>50-100 lines) → Extract smaller, focused functions
- Large structs with too many responsibilities → Split into focused types
- Duplicate code → Extract shared logic into reusable packages
- Primitive obsession → Create domain types (`type ClusterName string`)
- Switch/type assertions on types → Use interface-based polymorphism

**Code Smell Categories** (see `.github/copilot-instructions.md` for full catalog):

- **Bloaters**: Long methods, large structs, primitive obsession, long parameter lists, data clumps
- **Object-Orientation Abusers**: Type switches, temporary fields, refused bequest, inconsistent interfaces
- **Change Preventers**: Divergent change, shotgun surgery, parallel hierarchies
- **Dispensables**: Redundant comments, duplicate code, lazy packages, dead code, speculative generality
- **Couplers**: Feature envy, inappropriate intimacy, message chains, middle men

**When to Accept Code Smells**:

- Code that rarely changes and is well-tested
- Refactoring provides little benefit
- Document reason with comment explaining acceptance

## Development Workflow & Validation

### Build & Validation Requirements

**Pre-Commit Checklist** (MUST pass before any commit):

```bash
mockery                    # Generate fresh mocks (~1.2s)
go test ./...             # Run all tests (~37s, timeout: 90+s)
golangci-lint run         # Lint code (~1m16s, timeout: 120+s)
go build -o ksail .       # Ensure clean build (~1.4s)
```

**CI Pipeline Requirements**:

- Standard Go CI: build, test, lint via reusable workflows
- System tests: matrix testing across Kind, K3d, EKS distributions
- Full lifecycle validation: init → create → info → list → start → stop → delete
- All tests MUST pass before merge

**Manual Testing Requirements** (post-implementation):

- Basic CLI validation (`./ksail --help`, `./ksail --version`)
- Complete cluster lifecycle test in `/tmp/ksail-test`
- Alternative distribution testing as applicable

### Timing Expectations (NEVER CANCEL)

**Critical**: Long-running operations need time to complete. Premature cancellation causes incomplete builds and false failures.

**Standard Timings**:

- `go mod download`: ~0.045s (cached)
- `go build ./...`: ~2.1s
- `go build -o ksail .`: ~1.4s
- `mockery`: ~1.2s
- `go test ./...`: ~37s (timeout: 90+s)
- `golangci-lint run`: ~1m16s (timeout: 120+s)
- `mega-linter-runner -f go`: ~5+ minutes (timeout: 600+s)

**Timeout Rules**:

- Build commands: 60+ seconds minimum
- Test commands: 90+ seconds minimum
- Linter commands: 120+ seconds minimum
- Mega-linter: 600+ seconds minimum

### VS Code Workspace Navigation (CRITICAL)

**For ALL analysis, investigation, and code understanding tasks, use targeted workspace tools first.**

**Standard Workflow**:

1. Locate relevant files quickly with glob search (e.g., `file_search`) before opening large files
2. Find symbol usage sites via reference lookup (e.g., `list_code_usages`) when changing APIs
3. Prefer semantic or regex search (e.g., `semantic_search`, `grep_search`) over scanning entire files
4. Read only the necessary sections (targeted reads) to keep context tight and decisions precise

**Rationale**: Targeted navigation is faster and reduces accidental changes. Avoid reading entire files when symbol- or pattern-level search suffices.

## Governance

This constitution supersedes all other development practices. All pull requests and code reviews MUST verify compliance with these principles.

**Amendment Process**:

- Amendments require documentation of rationale and impact
- Version bump follows semantic versioning:
  - **MAJOR**: Backward-incompatible principle changes
  - **MINOR**: New principles or materially expanded guidance
  - **PATCH**: Clarifications, wording, typo fixes
- Update `.github/copilot-instructions.md` to reflect changes
- Update all templates in `.specify/templates/` for consistency

**Compliance Review**:

- Constitution violations must be addressed in code review
- Complexity must be justified with clear rationale
- Refer to `.github/copilot-instructions.md` for detailed runtime guidance
- Use `.specify/templates/` for structured development workflows

**Task Suitability for GitHub Copilot**:

- ✅ Well-suited: Bug fixes, test improvements, documentation, refactoring, dependency updates, CLI enhancements, technical debt
- ❌ Human-required: Architecture decisions, complex integrations, security-critical changes, production incidents, business logic

**Version**: 1.0.4 | **Ratified**: 2025-11-15 | **Last Amended**: 2025-12-13
