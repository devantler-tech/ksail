//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// testTLSCert and testTLSKey match the constants in secret_test.go.
const (
	testTLSCert = `-----BEGIN CERTIFICATE-----
MIIDDTCCAfWgAwIBAgIUQg2thFOmdEGn073/v2LH13oF0bIwDQYJKoZIhvcNAQEL
BQAwFjEUMBIGA1UEAwwLZXhhbXBsZS5jb20wHhcNMjUxMTAzMTk0MjIxWhcNMzUx
MTAxMTk0MjIxWjAWMRQwEgYDVQQDDAtleGFtcGxlLmNvbTCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAKmOXZrM0wpyfNPu40G2gsXXNPknjzB/RjtkDY8N
Ni/YfBK8MR41+ttjF3GRt2nkIDZ7gxMfDYBbQ5z4RQgarPLc+ADw+TnQ8aWAJGHN
TrUlqUsQaYMPi1nU066P0ctFvS0ezzJ9QblnJLDbhobvykK5wXp9pWGFvCyGBSGA
LoH3S1dZpRNazl7YexHVzo4aqDzu9B1mBDm9FP1aPgfCYX+o9ZfHFpFJkGT8Uutn
EYXSb/zedRHzYw2ya23zqCZ8fGlxWYD4+jwJyogJ2P5hPwZQ2t0biDNWhi0B5VuL
CCmNKEZsRQkOhWHH6rfmm1XqgM8wRIii+o3B4I3/9jbBGF0CAwEAAaNTMFEwHQYD
VR0OBBYEFL8A6pmICsjO1G+Y+UQ2ySAiCxK7MB8GA1UdIwQYMBaAFL8A6pmICsjO
1G+Y+UQ2ySAiCxK7MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
AErf6LUuXvuL/GLixJjOBADpM9Leju7dbB2t+sDh+kIPDpsHMj4EjPshSisBm26m
emE6geKA0vjD4fI8RL/kIvlPzPwojBDkbqOzNIxsAUF+7jlOxabuCmsQqpjZf4I7
zxomDNeSDndqUgcJIf/HjxyWK5Fi4N1wQuoid375EEixavXmzBIQvvXD2qT44rGY
vneGmModP5G4mcUIdNAd1oQoGYYKFpDPu7a1DiBGWTWb8sifjBwWjHhC1IsHMKg1
L1SjRMzmGtmQ7ckyJjq/cDDcvqei6tPKhN6oLjVezyfgb/j3feQM34RxOOMlm7IV
ZQL4GfN3z39LdLpniz7OuqU=
-----END CERTIFICATE-----
`

	testTLSKey = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCpjl2azNMKcnzT
7uNBtoLF1zT5J48wf0Y7ZA2PDTYv2HwSvDEeNfrbYxdxkbdp5CA2e4MTHw2AW0Oc
+EUIGqzy3PgA8Pk50PGlgCRhzU61JalLEGmDD4tZ1NOuj9HLRb0tHs8yfUG5ZySw
24aG78pCucF6faVhhbwshgUhgC6B90tXWaUTWs5e2HsR1c6OGqg87vQdZgQ5vRT9
Wj4HwmF/qPWXxxaRSZBk/FLrZxGF0m/83nUR82MNsmtt86gmfHxpcVmA+Po8CcqI
Cdj+YT8GUNrdG4gzVoYtAeVbiwgpjShGbEUJDoVhx+q35ptV6oDPMESIovqNweCN
//Y2wRhdAgMBAAECggEAVINLkM8rGff61EAsMiLgh/A+zTm0m3206gFy6KyzJ6IG
Jeh7qw1I3nVDyC3TeApnLADgUnWV6zaSOvlcny98qQkO7JkwAGtvJwj6GW2WH6CI
A4xIqzTiRoJYiJfTADjglE7ZA9d/HQSWOzkQks2OyTeBgqaB+lwIcUDT6eDUTZ6/
JtVY5EZmn+JEKylHJznnoEIIEyjYjJED33bQX4GszKojrD2tNY3ASgKhi+6cienm
PHt0I8l0EdoNUv1tCzxzZuyhqush9S4HY+EGcyFLj5drYzVDG8L8+EOndJd6cZ3L
IJGQDgUigKGKPwAR4+XnvKJMIBNBIZDzpWBprxWKmQKBgQDh0S+Nl/hsQjQsTyXt
qnRFYBTXneQrmCm/YHSBl4UGXK4z+XxqxGJEe8+0OVok7TIkSQyIF0tvcXJWjwUd
H9VYx1CLybldCdXlzSf4uM3inlWsgBRK/Ft0IDyw13bZ0L5XuY96O82ZWDR6/7gA
J2Sf1nVUqibBt9JRdXBWdO2HTwKBgQDAOBbguICAUCOQD959Z1063Kai0ChF1cXM
8wkA3iDTynJUOlxW+tn9n0PiHqcmjcLC3gQrxjb53qC7iakXnVw+rO1Xhfa9XzfK
slI5JBUueZtD+P45ZeRCaM1LhevIFSBoFOPPJYzninYOjawZZyttn7vzKFxVAr8a
DqOZjeO6kwKBgQDQVXzoxi8kOcQGqRLV/O9+XdF8x6eNbLn/XQ6/zLmmj/UL8H2P
xxTeF9gdbtgyvz8GaPqNx+gJrgGNyC8wmoDrgh9WiEpigsN7WtYoyt7v16I1Hoka
UU5SibdUc8Sr2cDyEDlFzUy2z8DDRY9NXQqhyGrBLKXLDTuVeaKlsQS/UwKBgQCd
KH7UByXRQzSAYekoIO3h5Ww86/IxfuH1erPeyL6QSxKE+R5sYzb+HUx0QVmqtPcL
OliwraRfUX2bN6dPznIQMHTxPW+KT6KfEIMXgv/qerTOs3Kv3TXuch9/4yPu+A8B
6iqEQBBfcx6pMX4HWwnv3EzgNxyeyNsUY+mw74jFDwKBgHT05YAt2RslNS4fmDvb
WhPIJGokCb27z32bH5jVVfr2Uq/GfWiIRY01KTWFLKSZseQA8SlOZ52q7NAckan5
ptRP8mvEaJVFBiIf95JlkTp76qUDLrEhI2ALJx1JjVx4H1M3Jjoeelm1qKGesaYz
H25Qf9zEQeJJCcSQPZ+iipaX
-----END PRIVATE KEY-----
`
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

	defer f.Close()
	_, _ = f.WriteString(entry)

	fmt.Printf("appended %s to %s\n", name, path)
}

func main() { //nolint:cyclop,funlen
	snapDir := "pkg/cli/cmd/workload/gen/__snapshots__"

	rt := di.NewRuntime()

	writeSnap(snapDir+"/namespace_test.snap", "TestGenNamespace",
		runCmd(gen.NewNamespaceCmd(rt), []string{"test-namespace"}))

	writeSnap(snapDir+"/configmap_test.snap", "TestGenConfigMap",
		runCmd(gen.NewConfigMapCmd(rt), []string{"test-config", "--from-literal=APP_ENV=production", "--from-literal=DEBUG=false"}))

	writeSnap(snapDir+"/deployment_test.snap", "TestGenDeployment",
		runCmd(gen.NewDeploymentCmd(rt), []string{"test-deployment", "--image=nginx:1.21", "--replicas=3"}))

	writeSnap(snapDir+"/job_test.snap", "TestGenJob",
		runCmd(gen.NewJobCmd(rt), []string{"test-job", "--image=busybox:latest"}))

	writeSnap(snapDir+"/cronjob_test.snap", "TestGenCronJob",
		runCmd(gen.NewCronJobCmd(rt), []string{"test-cronjob", "--image=busybox:latest", "--schedule=*/5 * * * *", "--restart=OnFailure"}))

	writeSnap(snapDir+"/ingress_test.snap", "TestGenIngressSimple",
		runCmd(gen.NewIngressCmd(rt), []string{"test-ingress", "--rule=example.com/*=svc:80"}))
	appendSnap(snapDir+"/ingress_test.snap", "TestGenIngressWithTLS",
		runCmd(gen.NewIngressCmd(rt), []string{"test-ingress-tls", "--rule=secure.example.com/*=svc:443,tls=my-tls-secret"}))
	appendSnap(snapDir+"/ingress_test.snap", "TestGenIngressMultipleRules",
		runCmd(gen.NewIngressCmd(rt), []string{"test-ingress-multi", "--rule=api.example.com/*=api-svc:8080", "--rule=web.example.com/*=web-svc:80"}))

	writeSnap(snapDir+"/poddisruptionbudget_test.snap", "TestGenPodDisruptionBudget",
		runCmd(gen.NewPodDisruptionBudgetCmd(rt), []string{"test-pdb", "--min-available=2", "--selector=app=test"}))

	writeSnap(snapDir+"/priorityclass_test.snap", "TestGenPriorityClass",
		runCmd(gen.NewPriorityClassCmd(rt), []string{"test-priority", "--value=1000", "--description=Test priority class"}))

	writeSnap(snapDir+"/quota_test.snap", "TestGenQuota",
		runCmd(gen.NewQuotaCmd(rt), []string{"test-quota", "--hard=cpu=1,memory=1Gi,pods=10"}))

	writeSnap(snapDir+"/rolebinding_test.snap", "TestGenRoleBinding",
		runCmd(gen.NewRoleBindingCmd(rt), []string{"test-rolebinding", "--role=test-role", "--user=test-user"}))

	writeSnap(snapDir+"/clusterrolebinding_test.snap", "TestGenClusterRoleBinding",
		runCmd(gen.NewClusterRoleBindingCmd(rt), []string{"test-clusterrolebinding", "--clusterrole=test-clusterrole", "--user=test-user"}))

	writeSnap(snapDir+"/serviceaccount_test.snap", "TestGenServiceAccount",
		runCmd(gen.NewServiceAccountCmd(rt), []string{"test-sa"}))

	writeSnap(snapDir+"/service_test.snap", "TestGenServiceClusterIP",
		runCmd(gen.NewServiceCmd(rt), []string{"clusterip", "test-svc", "--tcp=80:8080"}))
	appendSnap(snapDir+"/service_test.snap", "TestGenServiceNodePort",
		runCmd(gen.NewServiceCmd(rt), []string{"nodeport", "test-svc", "--tcp=80:8080"}))
	appendSnap(snapDir+"/service_test.snap", "TestGenServiceLoadBalancer",
		runCmd(gen.NewServiceCmd(rt), []string{"loadbalancer", "test-svc", "--tcp=80:8080"}))
	appendSnap(snapDir+"/service_test.snap", "TestGenServiceExternalName",
		runCmd(gen.NewServiceCmd(rt), []string{"externalname", "test-svc", "--external-name=example.com"}))

	writeSnap(snapDir+"/secret_test.snap", "TestGenSecretGeneric",
		runCmd(gen.NewSecretCmd(rt), []string{"generic", "test-secret", "--from-literal=key1=value1", "--from-literal=key2=value2"}))
	appendSnap(snapDir+"/secret_test.snap", "TestGenSecretDockerRegistry",
		runCmd(gen.NewSecretCmd(rt), []string{"docker-registry", "test-docker-secret", "--docker-server=https://registry.example.com", "--docker-username=testuser", "--docker-password=testpass123", "--docker-email=testuser@example.com"}))

	// TLS secret requires writing cert/key to temp files.
	dir, err := os.MkdirTemp("", "ksail-gen-snap-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	defer os.RemoveAll(dir)

	certFile := dir + "/tls.crt"
	keyFile := dir + "/tls.key"

	if writeErr := os.WriteFile(certFile, []byte(testTLSCert), 0o600); writeErr != nil {
		fmt.Fprintf(os.Stderr, "failed to write cert: %v\n", writeErr)
		os.Exit(1)
	}

	if writeErr := os.WriteFile(keyFile, []byte(testTLSKey), 0o600); writeErr != nil {
		fmt.Fprintf(os.Stderr, "failed to write key: %v\n", writeErr)
		os.Exit(1)
	}

	appendSnap(snapDir+"/secret_test.snap", "TestGenSecretTLS",
		runCmd(gen.NewSecretCmd(rt), []string{"tls", "test-tls-secret", "--cert=" + certFile, "--key=" + keyFile}))
}
