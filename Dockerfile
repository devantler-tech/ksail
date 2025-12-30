# Use distroless static image for minimal attack surface
FROM gcr.io/distroless/static:nonroot

# Copy the binary
# GoReleaser automatically copies the correct binary for each platform
COPY ksail /ksail

# Use nonroot user from distroless
USER nonroot:nonroot

# Add a simple healthcheck compatible with distroless (exec form only)
# This verifies the binary is present and runnable.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
	CMD ["/ksail", "--version"]

# Set entrypoint
ENTRYPOINT ["/ksail"]
