# Agentic Workflows Instructions

## Threat Detection Reliability

**Context**: The detection job runs Copilot CLI with restricted tools to analyze agent output for threats. Silent failures (exit code 1 with no output) have occurred.

**Requirements**:

1. **Always include diagnostic output before copilot CLI invocation:**
   - Print copilot version
   - List artifact files with sizes
   - Validate prompt file content (size, encoding)
   - Check environment variables

2. **Implement robust error handling for copilot CLI calls:**
   - Use `set -euo pipefail` at script start
   - Capture both stdout and stderr separately
   - Add timeout protection (e.g., `timeout 10m copilot ...`); tune based on expected analysis duration
   - Upload all logs on failure, not just detection.log

3. **Test with minimal configuration first:**
   - Before adding `--disable-builtin-mcps` and tool restrictions, verify copilot works with defaults
   - Add restrictions incrementally with validation

4. **Provide fallback mechanisms:**
   - If detection fails, capture full state (environment, logs, artifacts) for post-mortem analysis
   - Detection is security-critical; failures should block workflow completion by default
   - Only downgrade to a warning when the failure is clearly an infrastructure issue (e.g., CLI install failure, network timeout), not a potential security concern
