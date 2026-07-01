// Package project provides CLI commands that operate solely on the GitOps
// project files (the on-disk project structure) without contacting a live
// cluster. It is the home for scaffolding and file-transforming commands such
// as project initialization and environment cloning, mirroring the taxonomy
// where `cluster` operates on the cluster and `workload` on its workloads.
package project
