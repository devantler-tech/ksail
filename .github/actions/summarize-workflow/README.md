# Summarize Workflow Result Action

A GitHub composite action that summarizes the results of multiple jobs and fails if any job failed. Useful for creating status check jobs that aggregate results from matrix builds or parallel jobs.

## Why?

GitHub branch protection rules require a specific check to pass. When using matrix builds or conditional jobs, the check names can vary (e.g., `build (ubuntu, go-1.21)`, `build (macos, go-1.22)`). This action provides a single, stable check name that reports the aggregate status of all jobs.

## Usage

```yaml
jobs:
  build:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - run: echo "Building..."

  test:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - run: echo "Testing..."

  status:
    name: CI Status
    runs-on: ubuntu-latest
    needs: [build, test]
    if: ${{ always() }}
    steps:
      - uses: actions/checkout@v4
      - name: Summarize workflow result
        uses: ./.github/actions/summarize-workflow
        with:
          job-results: "${{ needs.build.result }} ${{ needs.test.result }}"
```

## Inputs

| Input             | Description                                  | Required | Default                                  |
| ----------------- | -------------------------------------------- | -------- | ---------------------------------------- |
| `job-results`     | Space-separated list of job results to check | Yes      | -                                        |
| `success-message` | Message when all jobs passed                 | No       | `✅ All jobs succeeded or were skipped.` |
| `failure-message` | Message when a job failed                    | No       | `❌ At least one job failed.`            |

## Job Result Values

GitHub provides these result values for jobs:

| Result      | Description                         | Treated as |
| ----------- | ----------------------------------- | ---------- |
| `success`   | Job completed successfully          | ✅ Pass    |
| `skipped`   | Job was skipped (condition not met) | ✅ Pass    |
| `failure`   | Job failed                          | ❌ Fail    |
| `cancelled` | Job was cancelled                   | ❌ Fail    |

## Example with Custom Messages

```yaml
- name: Summarize workflow result
  uses: ./.github/actions/summarize-workflow
  with:
    job-results: "${{ needs.build.result }} ${{ needs.test.result }} ${{ needs.deploy.result }}"
    success-message: "🎉 Pipeline completed successfully!"
    failure-message: "💥 Pipeline failed - check the logs above."
```

## Example with Many Jobs

```yaml
status:
  name: CI - MyProject
  runs-on: ubuntu-latest
  needs: [changes, build, lint, test, coverage, integration-test]
  if: ${{ always() }}
  steps:
    - uses: actions/checkout@v4
    - uses: ./.github/actions/summarize-workflow
      with:
        job-results: >-
          ${{ needs.changes.result }}
          ${{ needs.build.result }}
          ${{ needs.lint.result }}
          ${{ needs.test.result }}
          ${{ needs.coverage.result }}
          ${{ needs.integration-test.result }}
```

## Output

On success:

```text
✅ All jobs succeeded or were skipped.
```

On failure:

```text
❌ At least one job failed.
```
