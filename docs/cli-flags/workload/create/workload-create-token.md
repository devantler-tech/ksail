---
title: "ksail workload create token"
parent: "ksail workload create"
grand_parent: "ksail workload"
---

# ksail workload create token

```text
Request a service account token.

Usage:
  ksail workload create token SERVICE_ACCOUNT_NAME

Examples:
  # Request a token to authenticate to the kube-apiserver as the service account "myapp" in the current namespace
  kubectl create token myapp
  
  # Request a token for a service account in a custom namespace
  kubectl create token myapp --namespace myns
  
  # Request a token with a custom expiration
  kubectl create token myapp --duration 10m
  
  # Request a token with a custom audience
  kubectl create token myapp --audience https://example.com
  
  # Request a token bound to an instance of a Secret object
  kubectl create token myapp --bound-object-kind Secret --bound-object-name mysecret
  
  # Request a token bound to an instance of a Secret object with a specific UID
  kubectl create token myapp --bound-object-kind Secret --bound-object-name mysecret --bound-object-uid 0d4691ed-659b-4935-a832-355f77ee47cc

Flags:
      --allow-missing-template-keys   If true, ignore any errors in templates when a field or map key is missing in the template. Only applies to golang and jsonpath output formats. (default true)
      --audience stringArray          Audience of the requested token. If unset, defaults to requesting a token for use with the Kubernetes API server. May be repeated to request a token valid for multiple audiences.
      --bound-object-kind string      Kind of an object to bind the token to. Supported kinds are Node, Pod, Secret. If set, --bound-object-name must be provided.
      --bound-object-name string      Name of an object to bind the token to. The token will expire when the object is deleted. Requires --bound-object-kind.
      --bound-object-uid string       UID of an object to bind the token to. Requires --bound-object-kind and --bound-object-name. If unset, the UID of the existing object is used.
      --duration duration             Requested lifetime of the issued token. If not set or if set to 0, the lifetime will be determined by the server automatically. The server may return a token with a longer or shorter lifetime.
  -h, --help                          help for token
  -o, --output string                 Output format. One of: (json, yaml, kyaml, name, go-template, go-template-file, template, templatefile, jsonpath, jsonpath-as-json, jsonpath-file).
      --show-managed-fields           If true, keep the managedFields when printing objects in JSON or YAML format.
      --template string               Template string or path to template file to use when -o=go-template, -o=go-template-file. The template format is golang templates [http://golang.org/pkg/text/template/#pkg-overview].

Global Flags:
      --timing   Show per-activity timing output
```
