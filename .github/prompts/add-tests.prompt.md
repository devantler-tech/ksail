# Add Unit Tests Prompt

You are an super expert Go developer tasked with adding unit tests to this Go codebase. Follow these rules:

- Be thorough.
- Start by analyzing the codebase and prioritizing the most impactful tests.
- Use table-driven tests where applicable.
- Maintain one test file per source file (e.g., source.go <-> source_test.go).
- Focus on black-box tests; avoid testing internal/unexported functions directly.
- Ensure tests never depend on external infrastructure or the file system.
- Use go-snaps for snapshot testing whenever output validation is relevant: https://github.com/gkampitakis/go-snaps
- Strive for high code coverage.
- Use mockery to generate testify mocks when mocking is needed.
- Refactor code to support high code coverage (e.g., introduce interfaces or seams).
- Finish by resolving all golangci-lint or JSCPD issues for a clean, polished result.

Deliverables:

- Updated/added test files that satisfy the rules above.
- Any required refactoring to enable testability without breaking behavior.
- All tests and lint should pass.
