# Replay a KSail release

A completed release can need another delivery attempt when a downstream service
was unavailable. CD can reuse its verified immutable publication instead of
rebuilding the CLI and desktop binaries and trying to upload them again.

## Supported recovery

Use a **full rerun of the original CD run**, for a tag whose CD workflow contains
the replay preflight and admission guard. The first attempt must have successfully
published all GitHub release assets, both signed container images and the signed
operator chart. MCP, documentation deployment or the Homebrew handoff may still
need to finish.

1. Open the original **CD** run in GitHub Actions. Confirm its tag and source are
   the release you intend to recover.
2. Select **Re-run all jobs**. With the GitHub CLI, use:

   ```bash
   gh run rerun RUN_ID --repo devantler-tech/ksail
   ```

3. Check **Validate release ref**. It must bind the original run, tag and source,
   verify the complete GitHub release attestation and asset set, and verify each
   OCI publication's signature against the original workflow and source commit.
   A conflict, missing publication or failed read stops the attempt here.
4. The CLI, desktop and chart jobs report reuse of the verified publication.
   The existing VS Code Marketplace path still runs, retaining its current
   optional-publish policy. Documentation is rebuilt, MCP uses its existing
   identical-version verification, and Homebrew retains its protected handoff.
5. **Publish Release** independently repeats the publication verification and
   compares the GitHub asset IDs/digests and OCI manifest digests with preflight.
   An older replay also leaves the newer documentation and cask pointers intact.
6. Wait for the complete CD run, then check the intended downstream delivery.
   An immutable GitHub release by itself does not prove MCP, documentation or
   Homebrew delivery succeeded.

Use **all jobs**, rather than only failed jobs or an individual producer. Every
consumer checks that its admission belongs to the current attempt. A partial
rerun carrying an earlier attempt's decision stops before building or changing a
publication; rerun the complete workflow to obtain a fresh decision.

## What stays protected

Replay never deletes or unlocks an immutable release, moves its tag, replaces its
assets, rebuilds its published Go executables or overwrites its OCI publications.
Missing or conflicting immutable content needs a separate reviewed recovery.
Normal first publication retains the complete build, signing, asset-completeness
and publication gates. CD keeps its existing serialized release queue and cask
ordering checks.

## Historical runs

GitHub reruns use the original event's commit and ref. An older tag whose workflow
lacks these guards will therefore continue using its original release behavior;
rerunning it does not load this repair from `main`. Publish a new reviewed source
version, or prepare a separate version-specific recovery. Do not change a
published tag or delete a release to make an old run pass.

References: [GitHub rerun behavior](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/re-run-workflows-and-jobs),
[immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)
and [release verification](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/secure-your-dependencies/verify-release-integrity).
