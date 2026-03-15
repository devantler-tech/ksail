package gen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

// testTLSCert and testTLSKey are a self-signed certificate/key pair for CN=example.com,
// generated offline and embedded here so TLS secret tests are deterministic and
// do not require external tooling.
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

func execSecret(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := gen.NewSecretCmd(rt)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return outBuf.String(), errBuf.String(), err
}

func TestGenSecretGeneric(t *testing.T) {
	t.Parallel()

	output, _, err := execSecret(t, []string{
		"generic", "test-secret",
		"--from-literal=key1=value1",
		"--from-literal=key2=value2",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenSecretTLS(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")

	require.NoError(t, os.WriteFile(certFile, []byte(testTLSCert), 0o600))
	require.NoError(t, os.WriteFile(keyFile, []byte(testTLSKey), 0o600))

	output, _, err := execSecret(t, []string{
		"tls", "test-tls-secret",
		"--cert=" + certFile,
		"--key=" + keyFile,
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenSecretDockerRegistry(t *testing.T) {
	t.Parallel()

	output, _, err := execSecret(t, []string{
		"docker-registry", "test-docker-secret",
		"--docker-server=https://registry.example.com",
		"--docker-username=testuser",
		"--docker-password=testpass123",
		"--docker-email=testuser@example.com",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}
