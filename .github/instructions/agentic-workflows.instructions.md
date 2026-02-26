# Agentic Workflows Instructions

## Threat Detection Reliability

**Context**: The detection job runs Copilot CLI with restricted tools to analyze agent output for threats. Silent failures (exit code 1 with no output) have occurred.

**Requirements**:

1. **Always include diagnostic output before Copilot CLI invocation:**
   - Print Copilot version
   - List artifact files with sizes
   - Validate prompt file content (size, encoding, hash) without echoing full contents
   - Verify presence of required environment variables by name only; never print secret values

2. **Implement robust error handling for Copilot CLI calls:**
   - Use `set -euo pipefail` at script start
   - Capture both stdout and stderr separately
   - Add timeout protection (e.g., `timeout 10m copilot ...`); tune based on expected analysis duration
   - Upload all logs on failure, not just detection.log

3. **Test with minimal configuration first:**
   - Before adding `--disable-builtin-mcps` and tool restrictions, verify Copilot CLI works with defaults
   - Add restrictions incrementally with validation

4. **Provide fallback mechanisms:**
   - If detection fails, capture sufficient non-sensitive diagnostics (logs, artifact metadata, and a redacted subset of environment variables) for post-mortem analysis; never dump or upload the full, unredacted environment, and ensure GitHub Actions secrets and tokens are masked before logging
   - Detection is security-critical; failures should block workflow completion by default
   - Only downgrade to a warning when the failure is clearly an infrastructure issue (e.g., CLI install failure, network timeout), not a potential security concern
