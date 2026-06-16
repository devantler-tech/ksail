package backup_test

// Shared string constants for the backup engine tests. They satisfy goconst for
// literals repeated across the package's black-box test files and keep the test
// data single-sourced.
const (
	tProviderDocker = "Docker"
	tKindSecret     = "Secret"
	tClusterIPNone  = "None"
	tClusterIPType  = "ClusterIP"
	tClusterIP      = "clusterIP"
	tClusterIPs     = "clusterIPs"
	tLabelApp       = "app"
	tTypePods       = "pods"
	tTypeServices   = "services"
	tTypeDeploys    = "deployments"
	tTypeCRDs       = "customresourcedefinitions"
	tTypeNamespaces = "namespaces"
	tMetadataField  = "metadata"
	tControllerUID  = "controller-uid"
	tUIDValue       = "abc123"
	tAlreadyExists  = "already exists"
	tBackupName     = "my-backup"
	tClusterBackup  = "cluster-backup"
	tResourcesDir   = "resources/"
	tPodsYAMLPath   = "resources/pods.yaml"
	tEtcPasswd      = "/etc/passwd"
	tPolicyNone     = "none"
	tBatchUIDLabel  = "batch.kubernetes.io/controller-uid"
	tLabelValueApp  = "myapp"
	tTypeField      = "type"
)
