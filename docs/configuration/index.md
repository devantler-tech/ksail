---
title: Configuration
layout: default
nav_order: 3
---

# Configuration

KSail can be configured in three ways:

1. **CLI Options**: Command-line options that can be passed to the KSail CLI.
2. **Declarative Config**: A YAML file that can be used to define your KSail configuration in a declarative manner.

The configuration is applied with the following precedence: `(1) CLI Options > (2) Declarative Config`. This means that any configuration set in the CLI options will override any configuration set the declarative config file.

It is suggested to use the declarative config file for most configurations, as it allows you to run the `ksail` command without any additional options. However, for quick tests or one-off runs, you can always use the CLI options to override the configuration.
