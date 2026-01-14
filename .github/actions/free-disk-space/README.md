# Free Disk Space Action

A GitHub composite action that frees up disk space on GitHub-hosted runners by removing unused pre-installed tools and cleaning Docker.

## Why?

GitHub-hosted runners have limited disk space (~14GB free). Workflows that build or pull large container images, run Kubernetes clusters, or perform other disk-intensive operations can run out of space and fail with `DiskPressure` or `No space left on device` errors.

This action reclaims **~18GB** of disk space by removing tools that most CI workflows don't need.

## Usage

```yaml
steps:
  - uses: actions/checkout@v4

  - name: Free disk space
    uses: ./.github/actions/free-disk-space

  # Your disk-heavy steps here...
```

### With Custom Options

```yaml
- name: Free disk space
  uses: ./.github/actions/free-disk-space
  with:
    remove-dotnet: "true" # Remove .NET SDK (~1.5GB)
    remove-android: "true" # Remove Android SDK (~10GB)
    remove-haskell: "true" # Remove Haskell/GHC (~5GB)
    remove-codeql: "true" # Remove CodeQL (~1GB)
    clean-docker: "true" # Run docker system prune
```

## Inputs

| Input            | Description                                                   | Default |
|------------------|---------------------------------------------------------------|---------|
| `remove-dotnet`  | Remove .NET SDK (~1.5GB)                                      | `true`  |
| `remove-android` | Remove Android SDK (~10GB)                                    | `true`  |
| `remove-haskell` | Remove Haskell/GHC (~5GB)                                     | `true`  |
| `remove-codeql`  | Remove CodeQL (~1GB)                                          | `true`  |
| `clean-docker`   | Run `docker system prune` to remove unused images and volumes | `true`  |

## What Gets Removed

| Path                          | Size   | Description                            |
|-------------------------------|--------|----------------------------------------|
| `/usr/share/dotnet`           | ~1.5GB | .NET SDK and runtimes                  |
| `/usr/local/lib/android`      | ~10GB  | Android SDK                            |
| `/opt/ghc`                    | ~5GB   | Haskell GHC compiler                   |
| `/opt/hostedtoolcache/CodeQL` | ~1GB   | CodeQL analysis tools                  |
| Docker cleanup                | varies | Unused images, containers, and volumes |

## Example Output

```text
Disk space before cleanup:
Filesystem      Size  Used Avail Use% Mounted on
/dev/root        84G   63G   21G  75% /

Removing .NET SDK...
Removing Android SDK...
Removing Haskell/GHC...
Removing CodeQL...
Cleaning Docker...

Disk space after cleanup:
Filesystem      Size  Used Avail Use% Mounted on
/dev/root        84G   45G   39G  54% /
```
