package credentials

import "testing"

func TestSelectorEqualsIgnoresRotatedSecrets(t *testing.T) {
	t.Parallel()

	pinned := AWSResolution{
		Profile:         "original",
		AccessKeyID:     "AKIAORIGINAL",
		SecretAccessKey: "old-secret",
		SessionToken:    "old-token",
		Region:          "us-east-1",
		sourceEnvVars: [4]string{
			"AWS_ACCESS_KEY_ID",
			"AWS_SECRET_ACCESS_KEY",
			"AWS_SESSION_TOKEN",
			"AWS_PROFILE",
		},
		sourceRegionEnvVar: "AWS_REGION",
		frozen:             true,
	}
	current := pinned
	current.SecretAccessKey = "rotated-secret"
	current.SessionToken = "rotated-token"

	current.frozen = false
	if !pinned.SelectorEquals(current) {
		t.Fatal("SelectorEquals must ignore rotated secrets and the frozen flag")
	}
}

func TestSelectorEqualsDetectsProfileChange(t *testing.T) {
	t.Parallel()

	pinned := AWSResolution{Profile: "original", Region: "us-east-1"}

	current := AWSResolution{Profile: "repointed", Region: "us-east-1"}
	if pinned.SelectorEquals(current) {
		t.Fatal("SelectorEquals must refuse a profile change")
	}
}

func TestSelectorEqualsDetectsAccessKeyChange(t *testing.T) {
	t.Parallel()

	pinned := AWSResolution{AccessKeyID: "AKIAORIGINAL", Region: "us-east-1"}

	current := AWSResolution{AccessKeyID: "AKIAREPOINTED", Region: "us-east-1"}
	if pinned.SelectorEquals(current) {
		t.Fatal("SelectorEquals must refuse an access-key change")
	}
}
