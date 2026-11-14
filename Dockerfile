# Use distroless static image for minimal attack surface
FROM gcr.io/distroless/static:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931

# MCP Registry ownership verification
LABEL io.modelcontextprotocol.server.name="io.github.devantler-tech/ksail"

# GoReleaser v2 provides TARGETPLATFORM (e.g., linux/amd64, linux/arm64)
ARG TARGETPLATFORM

# Copy the binary from the platform-specific subdirectory
# GoReleaser v2 places binaries in ${TARGETPLATFORM}/ subdirectories
COPY ${TARGETPLATFORM}/ksail /ksail

# Use the distroless nonroot user by its numeric id. A name is not resolvable
# without /etc/passwd, and Kubernetes runAsNonRoot can only verify a numeric id.
USER 65532:65532

# Add a simple healthcheck compatible with distroless (exec form only)
# This verifies the binary is present and runnable.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
	CMD ["/ksail", "--version"]

# Set entrypoint
ENTRYPOINT ["/ksail"]
