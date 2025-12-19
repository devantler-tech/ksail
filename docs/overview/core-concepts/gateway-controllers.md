---
title: "Gateway Controllers"
parent: Core Concepts
grand_parent: Overview
nav_order: 6
---

# Gateway Controllers

Gateway controllers manage [Gateway API](https://gateway-api.sigs.k8s.io) resources.

> **Note:** Gateway controller configuration is not yet implemented in KSail. This document describes planned functionality.

## Planned Options

### Default

Will preserve whatever the distribution provides. Currently, neither Kind nor K3d install a gateway implementation by default.

### None

Will explicitly disable gateway installation.

## Current Status

Gateway API support is planned for a future release. For now, you can install gateway controllers manually using `ksail workload install`.
