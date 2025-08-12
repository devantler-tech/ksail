---
title: Configuration
nav_order: 3
---

# Configuration

KSail can be configured in two ways:

1. **CLI Options**: Command-line options that can be passed to the KSail CLI.
2. **Declarative Config**: YAML files that can be used to define the configuration for KSail, your chosen distribution, and more.

The configuration is applied with the following precedence: `(1) CLI Options > (2) Declarative Config`. This means that any configuration set in the CLI options will override any configuration set in declarative config files.

It is suggested to use the declarative config files for most configurations, as it allows you to run the `ksail` commands without any additional options. However, for quick tests or one-off runs, you can always use the CLI options to override your configuration, or to run a `ksail` command without any declarative config files.
