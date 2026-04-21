//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/spf13/cobra"
)

func runCmd(cmd *cobra.Command, args []string) string {
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(os.Stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error running %v: %v\n", args, err)
		os.Exit(1)
	}

	return outBuf.String()
}

func writeSnap(path, name, content string) {
	entry := fmt.Sprintf("[%s - 1]\n%s\n---\n", name, content)
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s\n", path)
}

func appendSnap(path, name, content string) {
	entry := fmt.Sprintf("\n[%s - 1]\n%s\n---\n", name, content)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open %s: %v\n", path, err)
		os.Exit(1)
	}

	if _, err := f.WriteString(entry); err != nil {
		_ = f.Close()
		fmt.Fprintf(os.Stderr, "failed to write %s to %s: %v\n", name, path, err)
		os.Exit(1)
	}

	if err := f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to close %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Printf("appended %s to %s\n", name, path)
}

func main() { //nolint:cyclop,funlen
	snapDir := "pkg/cli/cmd/workload/gen/__snapshots__"
	testdataDir := "pkg/cli/cmd/workload/gen/testdata"

	rt := di.NewRuntime()

	writeSnap(snapDir+"/namespace_test.snap", "TestGenNamespace",
		runCmd(gen.NewNamespaceCmd(rt), []string{"test-namespace"}))

	writeSnap(
		snapDir+"/configmap_test.snap",
		"TestGenConfigMap",
		runCmd(
			gen.NewConfigMapCmd(rt),
			[]string{
				"test-config",
				"--from-literal=APP_ENV=production",
				"--from-literal=DEBUG=false",
			},
		),
	)

	writeSnap(
		snapDir+"/deployment_test.snap",
		"TestGenDeployment",
		runCmd(
			gen.NewDeploymentCmd(rt),
			[]string{"test-deployment", "--image=nginx:1.21", "--replicas=3"},
		),
	)

	writeSnap(snapDir+"/job_test.snap", "TestGenJob",
		runCmd(gen.NewJobCmd(rt), []string{"test-job", "--image=busybox:latest"}))

	writeSnap(
		snapDir+"/cronjob_test.snap",
		"TestGenCronJob",
		runCmd(
			gen.NewCronJobCmd(rt),
			[]string{
				"test-cronjob",
				"--image=busybox:latest",
				"--schedule=*/5 * * * *",
				"--restart=OnFailure",
			},
		),
	)

	writeSnap(snapDir+"/ingress_test.snap", "TestGenIngressSimple",
		runCmd(gen.NewIngressCmd(rt), []string{"test-ingress", "--rule=example.com/*=svc:80"}))
	appendSnap(
		snapDir+"/ingress_test.snap",
		"TestGenIngressWithTLS",
		runCmd(
			gen.NewIngressCmd(rt),
			[]string{"test-ingress-tls", "--rule=secure.example.com/*=svc:443,tls=my-tls-secret"},
		),
	)
	appendSnap(
		snapDir+"/ingress_test.snap",
		"TestGenIngressMultipleRules",
		runCmd(
			gen.NewIngressCmd(rt),
			[]string{
				"test-ingress-multi",
				"--rule=api.example.com/*=api-svc:8080",
				"--rule=web.example.com/*=web-svc:80",
			},
		),
	)

	writeSnap(
		snapDir+"/poddisruptionbudget_test.snap",
		"TestGenPodDisruptionBudget",
		runCmd(
			gen.NewPodDisruptionBudgetCmd(rt),
			[]string{"test-pdb", "--min-available=2", "--selector=app=test"},
		),
	)

	writeSnap(
		snapDir+"/priorityclass_test.snap",
		"TestGenPriorityClass",
		runCmd(
			gen.NewPriorityClassCmd(rt),
			[]string{"test-priority", "--value=1000", "--description=Test priority class"},
		),
	)

	writeSnap(snapDir+"/quota_test.snap", "TestGenQuota",
		runCmd(gen.NewQuotaCmd(rt), []string{"test-quota", "--hard=cpu=1,memory=1Gi,pods=10"}))

	writeSnap(
		snapDir+"/rolebinding_test.snap",
		"TestGenRoleBinding",
		runCmd(
			gen.NewRoleBindingCmd(rt),
			[]string{"test-rolebinding", "--role=test-role", "--user=test-user"},
		),
	)

	writeSnap(
		snapDir+"/clusterrolebinding_test.snap",
		"TestGenClusterRoleBinding",
		runCmd(
			gen.NewClusterRoleBindingCmd(rt),
			[]string{
				"test-clusterrolebinding",
				"--clusterrole=test-clusterrole",
				"--user=test-user",
			},
		),
	)

	writeSnap(snapDir+"/serviceaccount_test.snap", "TestGenServiceAccount",
		runCmd(gen.NewServiceAccountCmd(rt), []string{"test-sa"}))

	writeSnap(snapDir+"/service_test.snap", "TestGenServiceClusterIP",
		runCmd(gen.NewServiceCmd(rt), []string{"clusterip", "test-svc", "--tcp=80:8080"}))
	appendSnap(snapDir+"/service_test.snap", "TestGenServiceNodePort",
		runCmd(gen.NewServiceCmd(rt), []string{"nodeport", "test-svc", "--tcp=80:8080"}))
	appendSnap(snapDir+"/service_test.snap", "TestGenServiceLoadBalancer",
		runCmd(gen.NewServiceCmd(rt), []string{"loadbalancer", "test-svc", "--tcp=80:8080"}))
	appendSnap(
		snapDir+"/service_test.snap",
		"TestGenServiceExternalName",
		runCmd(
			gen.NewServiceCmd(rt),
			[]string{"externalname", "test-svc", "--external-name=example.com"},
		),
	)

	writeSnap(
		snapDir+"/secret_test.snap",
		"TestGenSecretGeneric",
		runCmd(
			gen.NewSecretCmd(rt),
			[]string{
				"generic",
				"test-secret",
				"--from-literal=key1=value1",
				"--from-literal=key2=value2",
			},
		),
	)
	appendSnap(
		snapDir+"/secret_test.snap",
		"TestGenSecretDockerRegistry",
		runCmd(
			gen.NewSecretCmd(rt),
			[]string{
				"docker-registry",
				"test-docker-secret",
				"--docker-server=https://registry.example.com",
				"--docker-username=testuser",
				"--docker-password=testpass123",
				"--docker-email=testuser@example.com",
			},
		),
	)

	// TLS secret uses the cert/key from testdata to stay in sync with secret_test.go.
	certFile := testdataDir + "/tls.crt"
	keyFile := testdataDir + "/tls.key"

	appendSnap(
		snapDir+"/secret_test.snap",
		"TestGenSecretTLS",
		runCmd(
			gen.NewSecretCmd(rt),
			[]string{"tls", "test-tls-secret", "--cert=" + certFile, "--key=" + keyFile},
		),
	)
}
