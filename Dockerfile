# Use distroless static image for minimal attack surface
FROM gcr.io/distroless/static:nonroot@sha256:e3f945647ffb95b5839c07038d64f9811adf17308b9121d8a2b87b6a22a80a39

# MCP Registry ownership verification
LABEL io.modelcontextprotocol.server.name="io.github.devantler-tech/ksail"

# GoReleaser v2 provides TARGETPLATFORM (e.g., linux/amd64, linux/arm64)
ARG TARGETPLATFORM

# Copy the binary from the platform-specific subdirectory
# GoReleaser v2 places binaries in ${TARGETPLATFORM}/ subdirectories
COPY ${TARGETPLATFORM}/ksail /ksail

# Use nonroot user from distroless
USER nonroot:nonroot

# Add a simple healthcheck compatible with distroless (exec form only)
# This verifies the binary is present and runnable.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
	CMD ["/ksail", "--version"]

# Set entrypoint
ENTRYPOINT ["/ksail"]
