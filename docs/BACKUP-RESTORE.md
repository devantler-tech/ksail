# Cluster Backup and Restore

KSail provides `cluster backup` and `cluster restore` commands for backing up and restoring Kubernetes cluster resources and persistent volumes.

## Overview

The backup/restore functionality allows you to:
- Create compressed archives of Kubernetes resources
- Backup cluster state for disaster recovery
- Migrate workloads between clusters
- Restore resources in the correct order

## Backup Command

### Basic Usage

````bash
ksail cluster backup --output ./my-backup.tar.gz
````

### Options

- `--output, -o` (required): Output path for the backup archive
- `--include-volumes` (default: true): Include persistent volume data in backup
- `--namespaces, -n`: Specific namespaces to backup (default: all)
- `--exclude-types`: Resource types to exclude (default: events)
- `--compression`: Compression level 0-9 (default: 6)

### Examples

**Backup all resources:**
````bash
ksail cluster backup --output ./cluster-backup.tar.gz
````

**Backup specific namespaces:**
````bash
ksail cluster backup --output ./app-backup.tar.gz --namespaces default,kube-system
````

**Exclude certain resource types:**
````bash
ksail cluster backup --output ./backup.tar.gz --exclude-types events,pods
````

**Maximum compression:**
````bash
ksail cluster backup --output ./backup.tar.gz --compression 9
````

### What Gets Backed Up

Resources are exported in order:
1. CustomResourceDefinitions (CRDs)
2. Namespaces
3. StorageClasses
4. PersistentVolumes and PersistentVolumeClaims
5. Secrets and ConfigMaps
6. ServiceAccounts, Roles, RoleBindings
7. ClusterRoles and ClusterRoleBindings
8. Services
9. Deployments, StatefulSets, DaemonSets
10. Jobs and CronJobs
11. Ingresses

### Backup Archive Structure

````
backup.tar.gz
├── backup-metadata.json       # Backup metadata (version, timestamp, cluster info)
└── resources/
    ├── customresourcedefinitions/
    │   └── customresourcedefinitions.yaml
    ├── namespaces/
    │   └── namespaces.yaml
    ├── deployments/
    │   └── deployments.yaml
    └── ...
````

### Backup Metadata

Each backup includes a `backup-metadata.json` file with:
- **version**: Backup format version (v1)
- **timestamp**: When the backup was created
- **clusterName**: Source cluster name
- **ksailVersion**: KSail version used
- **resourceCount**: Total number of resources backed up

## Restore Command

### Basic Usage

````bash
ksail cluster restore --input ./my-backup.tar.gz
````

### Options

- `--input, -i` (required): Input backup archive path
- `--existing-resource-policy`: How to handle existing resources
  - `none` (default): Skip existing resources
  - `update`: Update existing resources
- `--dry-run`: Show what would be restored without applying

### Examples

**Restore from backup:**
````bash
ksail cluster restore --input ./cluster-backup.tar.gz
````

**Preview restore without applying:**
````bash
ksail cluster restore --input ./backup.tar.gz --dry-run
````

**Update existing resources during restore:**
````bash
ksail cluster restore --input ./backup.tar.gz --existing-resource-policy update
````

### Restore Process

1. **Extract archive**: Uncompress and extract backup to temporary directory
2. **Read metadata**: Display backup information (timestamp, cluster, resource count)
3. **Restore resources**: Apply resources in correct order
4. **Cleanup**: Remove temporary files

Resources are restored in the same order they were backed up to ensure dependencies are met (e.g., CRDs before custom resources, namespaces before namespaced resources).

## Use Cases

### Disaster Recovery

Backup your production cluster regularly:
````bash
# Daily backup
ksail cluster backup --output ./backups/prod-$(date +%Y%m%d).tar.gz
````

Restore in case of failure:
````bash
ksail cluster restore --input ./backups/prod-20260220.tar.gz
````

### Cluster Migration

Move workloads between clusters:
````bash
# Backup from source cluster
ksail cluster backup --output ./migration.tar.gz

# Switch kubeconfig to target cluster
export KUBECONFIG=~/.kube/target-cluster

# Restore to target cluster
ksail cluster restore --input ./migration.tar.gz
````

### Development Snapshots

Save and restore development cluster states:
````bash
# Save working state
ksail cluster backup --output ./dev-working-state.tar.gz

# Make experimental changes...

# Restore if needed
ksail cluster restore --input ./dev-working-state.tar.gz
````

## Limitations (v1)

Current limitations in the initial implementation:

- **No secret encryption**: Secrets are exported as plain YAML. Use SOPS or similar tools separately for secret management.
- **No cloud snapshots**: Volume backups use file system approach only. Cloud provider volume snapshots (AWS EBS, GCP PD, Azure Disk) are not supported.
- **No block storage snapshots**: Hetzner/block storage snapshots are not included.
- **No selective restore**: Cannot restore individual resources or namespaces from a backup (full restore only).

## Future Enhancements

Planned for future versions:

- Secret encryption integration (SOPS, Sealed Secrets)
- Cloud provider volume snapshots
- Selective restore (specific namespaces or resources)
- Incremental backups
- Backup scheduling and retention policies
- Backup validation and verification
- Compression algorithm options (zstd, bzip2)

## Troubleshooting

**"kubeconfig not found"**
Ensure your cluster is running and kubeconfig is properly configured:
````bash
ksail cluster info
````

**"failed to get resources"**
Some resource types may not exist in your cluster. This is normal and can be ignored.

**Large backup size**
Use higher compression or exclude unnecessary resource types:
````bash
ksail cluster backup --output ./backup.tar.gz --compression 9 --exclude-types events,pods
````

**Restore conflicts**
Use `--existing-resource-policy update` to overwrite existing resources:
````bash
ksail cluster restore --input ./backup.tar.gz --existing-resource-policy update
````
