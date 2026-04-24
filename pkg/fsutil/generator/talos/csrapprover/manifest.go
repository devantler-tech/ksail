// Package csrapprover provides the kubelet-serving-cert-approver manifest
// for embedding in Talos machine configs via cluster.inlineManifests.
//
// The upstream project (alex1989hu/kubelet-serving-cert-approver) recommends
// the :main image tag for deployments. Versioned images stopped at 0.6.1 in
// GHCR, but the :main tag receives continuous updates. The Dockerfile in this
// package tracks the image for documentation; Dependabot will update it if
// versioned images resume.
//
// See: https://github.com/alex1989hu/kubelet-serving-cert-approver
package csrapprover

import (
	_ "embed"
)

//go:embed Dockerfile
var dockerfile string

// manifestTemplate is the kubelet-serving-cert-approver standalone deployment manifest.
// Derived from https://github.com/alex1989hu/kubelet-serving-cert-approver/blob/main/deploy/standalone-install.yaml
// with the image reference replaced by manifestImagePlaceholder for substitution.
const manifestTemplate = `apiVersion: v1
kind: Namespace
metadata:
  labels:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/warn: restricted
  name: kubelet-serving-cert-approver
---
apiVersion: v1
kind: ServiceAccount
metadata:
  labels:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
  name: kubelet-serving-cert-approver
  namespace: kubelet-serving-cert-approver
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  labels:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
  name: certificates:kubelet-serving-cert-approver
rules:
- apiGroups:
  - certificates.k8s.io
  resources:
  - certificatesigningrequests
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - certificates.k8s.io
  resources:
  - certificatesigningrequests/approval
  verbs:
  - update
- apiGroups:
  - authorization.k8s.io
  resources:
  - subjectaccessreviews
  verbs:
  - create
- apiGroups:
  - certificates.k8s.io
  resourceNames:
  - kubernetes.io/kubelet-serving
  resources:
  - signers
  verbs:
  - approve
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  labels:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
  name: events:kubelet-serving-cert-approver
rules:
- apiGroups:
  - ""
  resources:
  - events
  verbs:
  - create
  - patch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  labels:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
  name: events:kubelet-serving-cert-approver
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: events:kubelet-serving-cert-approver
subjects:
- kind: ServiceAccount
  name: kubelet-serving-cert-approver
  namespace: kubelet-serving-cert-approver
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  labels:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
  name: kubelet-serving-cert-approver
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: certificates:kubelet-serving-cert-approver
subjects:
- kind: ServiceAccount
  name: kubelet-serving-cert-approver
  namespace: kubelet-serving-cert-approver
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
  name: kubelet-serving-cert-approver
  namespace: kubelet-serving-cert-approver
spec:
  ports:
  - name: metrics
    port: 9090
    protocol: TCP
    targetPort: metrics
  selector:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/instance: kubelet-serving-cert-approver
    app.kubernetes.io/name: kubelet-serving-cert-approver
  name: kubelet-serving-cert-approver
  namespace: kubelet-serving-cert-approver
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/instance: kubelet-serving-cert-approver
      app.kubernetes.io/name: kubelet-serving-cert-approver
  template:
    metadata:
      labels:
        app.kubernetes.io/instance: kubelet-serving-cert-approver
        app.kubernetes.io/name: kubelet-serving-cert-approver
    spec:
      affinity:
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - preference:
              matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: DoesNotExist
            weight: 100
      containers:
      - args:
        - serve
        env:
        - name: NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        image: ghcr.io/alex1989hu/kubelet-serving-cert-approver:main
        livenessProbe:
          httpGet:
            path: /healthz
            port: health
          initialDelaySeconds: 6
        name: cert-approver
        ports:
        - containerPort: 8080
          name: health
        - containerPort: 9090
          name: metrics
        readinessProbe:
          httpGet:
            path: /readyz
            port: health
          initialDelaySeconds: 3
        resources:
          limits:
            cpu: 250m
            memory: 32Mi
          requests:
            cpu: 10m
            memory: 16Mi
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          privileged: false
          readOnlyRootFilesystem: true
          runAsNonRoot: true
      priorityClassName: system-cluster-critical
      securityContext:
        fsGroup: 65534
        runAsGroup: 65534
        runAsUser: 65534
        seccompProfile:
          type: RuntimeDefault
      serviceAccountName: kubelet-serving-cert-approver
      tolerations:
      - effect: NoSchedule
        key: node.cloudprovider.kubernetes.io/uninitialized
        operator: Exists
      - effect: NoSchedule
        key: node-role.kubernetes.io/master
        operator: Exists
      - effect: NoSchedule
        key: node-role.kubernetes.io/control-plane
        operator: Exists
`

// Manifest returns the kubelet-serving-cert-approver manifest YAML.
// The manifest uses the upstream-recommended :main image tag.
func Manifest() string {
	return manifestTemplate
}
