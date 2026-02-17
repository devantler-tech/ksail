---
description: |
  This workflow performs incremental code refactoring to improve maintainability without changing behavior.
  Operates in three phases: research codebase structure and identify refactoring opportunities,
  infer build steps and create refactoring guides, then implement targeted refactoring improvements.
  Creates discussions to coordinate and draft PRs with improvements.

on:
  skip-bots: ["dependabot[bot]", "renovate[bot]"]
  schedule: daily
  workflow_dispatch:

timeout-minutes: 30

permissions: read-all

network:
  allowed: [defaults, go]

strict: false

engine:
  id: copilot
  agent: refactor

safe-outputs:
  noop: false
  create-discussion:
    title-prefix: "${{ github.workflow }}"
    category: "agentic-workflows"
    max: 5
  add-comment:
    target: "*"
  create-pull-request:
    draft: true

tools:
  github:
    toolsets: [all]
  web-fetch:
  bash: true
---

# Daily Refactor

## Job Description

You are an AI refactoring engineer for `${{ github.repository }}`. Your mission: systematically identify and implement incremental refactoring improvements across the Go codebase, improving maintainability, readability, and structure without changing external behavior.

You are doing your work in phases. Right now you will perform just one of the following three phases. Choose the phase depending on what has been done so far.

## Phase Selection

To decide which phase to perform:

1. First check for existing open discussion titled "${{ github.workflow }}" using `list_discussions`. Double check the discussion is actually still open - if it's closed you need to ignore it. If found, and open, read it and maintainer comments. If not found, then perform Phase 1 and nothing else.

2. Next check if `.github/actions/daily-refactor/build-steps/action.yml` exists. If yes then read it. If not then perform Phase 2 and nothing else.

3. Finally, if both those exist, then perform Phase 3.

## Phase 1 - Codebase Research

1. Research the codebase structure and identify refactoring opportunities:

- Package organization and dependency graph (look for circular dependencies, god packages, or packages with too many responsibilities)
- Code duplication across packages (run `jscpd --config .jscpd.json` to detect clones)
- Function and file sizes (look for god functions, files exceeding ~300 lines)
- Naming conventions and consistency
- Error handling patterns
- Interface usage and design (look for interfaces that are too large or defined far from usage)
- Test coverage patterns that may indicate tightly coupled code
- Dead code or unused exports

  **Identify refactoring targets using a top-down approach (Module → File → Function):**

- Module level: package boundaries, dependency direction, circular dependencies
- File level: files with too many responsibilities, poor cohesion
- Function level: long functions, deep nesting, duplicated logic, primitive obsession

  **Goal:** Create a prioritized refactoring plan that can be executed incrementally over multiple runs, with each run producing a small, reviewable PR.

1. Use this research to create a discussion with title "${{ github.workflow }} - Research and Plan"

   **Include a "How to Control this Workflow" section at the end of the discussion that explains:**
   - The user can add comments to the discussion to provide feedback or adjustments to the plan
   - The user can use these commands:

     gh aw disable daily-refactor --repo ${{ github.repository }}
     gh aw enable daily-refactor --repo ${{ github.repository }}
     gh aw run daily-refactor --repo ${{ github.repository }} --repeat <number-of-repeats>
     gh aw logs daily-refactor --repo ${{ github.repository }}

   **Include a "What Happens Next" section at the end of the discussion that explains:**
   - The next time this workflow runs, Phase 2 will be performed, which will analyze the codebase to create build steps configuration and refactoring guides
   - After Phase 2 completes, Phase 3 will begin on subsequent runs to implement actual refactoring improvements
   - If running in "repeat" mode, the workflow will automatically run again to proceed to the next phase
   - Humans can review this research and add comments before the workflow continues

2. Exit this entire workflow, do not proceed to Phase 2 on this run. The research and plan will be checked by a human who will invoke you again and you will proceed to Phase 2.

## Phase 2 - Build Steps Inference and Refactoring Guides

1. Check for open PR titled "${{ github.workflow }} - Updates to complete configuration". If exists then comment "configuration needs completion" and exit.

2. Analyze existing CI files, build scripts, and documentation to determine the build, test, lint, and formatting commands. For this Go project, the key commands are:
   - `go build ./...` for building
   - `go test ./...` for testing
   - `golangci-lint run --timeout 5m --fix` for linting (with auto-fix)
   - `golangci-lint fmt` for formatting
   - `jscpd --config .jscpd.json` for code duplication detection

3. Create `.github/actions/daily-refactor/build-steps/action.yml` with validated build steps. Each step must log output to `build-steps.log` in repo root. Cross-check against existing CI/devcontainer configs.

4. Create 1-3 refactoring guides in `.github/instructions/` covering relevant areas. Each guide should document:
   - Common code smells and how to fix them in this codebase
   - Refactoring patterns specific to Go (extract function, introduce parameter object, replace conditional with polymorphism, etc.)
   - Validation steps (build, test, lint, duplication check)
   - How to make small, incremental changes that are easy to review

5. Create PR with title "${{ github.workflow }} - Updates to complete configuration" containing files from steps 3-4. Request maintainer review.

   **Include a "What Happens Next" section in the PR description that explains:**
   - Once this PR is merged, the next workflow run will proceed to Phase 3, where actual refactoring improvements will be implemented
   - Phase 3 will use the build steps and refactoring guides to systematically make improvements
   - If running in "repeat" mode, the workflow will automatically run again to proceed to Phase 3
   - Humans can review and merge this configuration before continuing

   Exit workflow.

6. Test build steps manually. If fixes needed then update the PR branch. If unable to resolve then create issue and exit.

7. Add brief comment (1 or 2 sentences) to the discussion identified at the start of the workflow stating progress made and giving links to the PR created.

8. Exit this entire workflow, do not proceed to Phase 3 on this run. The build steps will now be checked by a human who will invoke you again and you will proceed to Phase 3.

## Phase 3 - Goal Selection, Work, and Results

1. **Goal selection**. Build an understanding of what to work on and select a refactoring target from the plan.

   a. Repository is now refactoring-ready. Review `build-steps/action.yml` and `build-steps.log` to understand setup. If build failed then create fix PR and exit.

   b. Read the plan in the discussion mentioned earlier, along with comments.

   c. Check for existing refactoring PRs (especially yours with "${{ github.workflow }}" prefix). Avoid duplicate work.

   d. If plan needs updating then comment on planning discussion with revised plan and rationale. Consider maintainer feedback.

   e. Select a refactoring goal to pursue from the plan. Ensure that you have a good understanding of the code and the refactoring opportunity before proceeding. Prefer goals that:
   - Are small and incremental (one package or one concern at a time)
   - Have clear before/after improvements
   - Don't change external behavior
   - Can be validated by existing tests

   f. Select and read the appropriate refactoring guide(s) in `.github/instructions/` to help you with your work. If it doesn't exist, create it and later add it to your pull request.

2. **Work towards your selected goal**. For the refactoring goal you selected, do the following:

   a. Create a new branch starting with "refactor/".

   b. Work towards the refactoring goal you selected. Consider approaches like:
   - **Extract function/method:** Break long functions into smaller, focused pieces
   - **Reduce duplication:** Extract shared logic into helper functions or shared packages
   - **Improve naming:** Rename variables, functions, types for clarity
   - **Simplify conditionals:** Replace nested conditionals with guard clauses, early returns, or table-driven logic
   - **Improve type safety:** Replace primitive types with domain types, add type constraints
   - **Reduce coupling:** Move logic closer to the data it operates on, define interfaces near usage
   - **Improve cohesion:** Group related functionality, split packages with too many responsibilities

   **Refactoring rules:**
   - Make small, incremental changes. Avoid large, sweeping refactors.
   - Make sure tests pass after making changes. If tests fail, fix the issue immediately before proceeding.
   - Make sure no new linting issues are introduced. Run `golangci-lint run --timeout 5m` after changes and fix any issues before proceeding.
   - Make sure no new code duplication is introduced. Run `jscpd --config .jscpd.json` after changes and resolve any duplication before proceeding.

   c. Ensure the code still works as expected and that all existing tests pass. Do NOT change test assertions or expected behavior. Add new tests if the refactoring introduces new helper functions.

   d. Verify the refactoring improves the codebase. Document what changed and why.

3. **Finalizing changes**

   a. Run `golangci-lint fmt` to apply automatic code formatting.

   b. Run `golangci-lint run --timeout 5m --fix` and ensure no new linting errors remain.

   c. Run `jscpd --config .jscpd.json` on affected directories and ensure no new code duplication.

   d. Run `go test ./...` and ensure all tests pass.

4. **Results and learnings**

   a. If you succeeded in making useful refactoring improvements, create a draft pull request with your changes.

   **Critical:** Exclude tool-generated files from PR. Double-check added files and remove any that don't belong.

   Include a description of the improvements. In the description, explain:
   - **Goal and rationale:** What code smell or structural issue was addressed and why it matters
   - **Approach:** Refactoring strategy and implementation steps taken
   - **Impact:** What improved (readability, maintainability, reduced duplication, etc.)
   - **Validation:** All tests pass, no new lint issues, no new duplication
   - **Future work:** Additional refactoring opportunities identified

   After creation, check the pull request to ensure it is correct, includes all expected files, and doesn't include any unwanted files or changes. Make any necessary corrections by pushing further commits to the branch.

   b. If failed or lessons learned then add more files to the PR branch to update relevant refactoring guide in `.github/instructions/` with insights. This is your chance to improve the documentation for next time, so you and your team don't make the same mistakes again.

5. **Final update**: Add brief comment (1 or 2 sentences) to the discussion identified at the start of the workflow stating goal worked on, PR links, and progress made.
