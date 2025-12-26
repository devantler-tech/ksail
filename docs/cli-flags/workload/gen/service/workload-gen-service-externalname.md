---
title: "ksail workload gen service externalname"
parent: "ksail workload gen service"
grand_parent: "ksail workload gen"
---

# ksail workload gen service externalname

```text
Create an ExternalName service with the specified name.

 ExternalName service references to an external DNS address instead of only pods, which will allow application authors to reference services that exist off platform, on other clusters, or locally.

Usage:
  ksail workload gen service externalname NAME --external-name external.name [--dry-run=server|client|none] [flags]

Examples:
  # Create a new ExternalName service named my-ns
  kubectl create service externalname my-ns --external-name bar.com

Flags:
      --allow-missing-template-keys    If true, ignore any errors in templates when a field or map key is missing in the template. Only applies to golang and jsonpath output formats. (default true)
      --dry-run string[="unchanged"]   Must be "none", "server", or "client". If client strategy, only print the object that would be sent, without sending it. If server strategy, submit server-side request without persisting the resource. (default "none")
      --external-name string           External name of service
      --field-manager string           Name of the manager used to track field ownership. (default "kubectl-create")
  -h, --help                           help for externalname
  -o, --output string                  Output format. One of: (json, yaml, kyaml, name, go-template, go-template-file, template, templatefile, jsonpath, jsonpath-as-json, jsonpath-file).
      --save-config                    If true, the configuration of current object will be saved in its annotation. Otherwise, the annotation will be unchanged. This flag is useful when you want to perform kubectl apply on this object in the future.
      --show-managed-fields            If true, keep the managedFields when printing objects in JSON or YAML format.
      --tcp strings                    Port pairs can be specified as '<port>:<targetPort>'.
      --template string                Template string or path to template file to use when -o=go-template, -o=go-template-file. The template format is golang templates [http://golang.org/pkg/text/template/#pkg-overview].
      --validate string[="strict"]     Must be one of: strict (or true), warn, ignore (or false). "true" or "strict" will use a schema to validate the input and fail the request if invalid. It will perform server side validation if ServerSideFieldValidation is enabled on the api-server, but will fall back to less reliable client-side validation if not. "warn" will warn about unknown or duplicate fields without blocking the request if server-side field validation is enabled on the API server, and behave as "ignore" otherwise. "false" or "ignore" will not perform any schema validation, silently dropping any unknown or duplicate fields. (default "strict")

Global Flags:
      --timing   Show per-activity timing output
```
