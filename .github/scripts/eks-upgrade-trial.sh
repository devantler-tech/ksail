#!/usr/bin/env bash
# An opt-in trial on the cluster created by this workflow. Never prints credentials.
set +x
set -euo pipefail

fail() {
	echo "::error::$*" >&2
	exit 1
}

from="${EKS_UPGRADE_FROM:-}"
to="${EKS_UPGRADE_TO:-}"
if [[ "${1:-}" == --validate-versions && -z "$from$to" ]]; then
	exit 0
fi
if [[ ! "$from" =~ ^1\.[1-9][0-9]?$ || ! "$to" =~ ^1\.[1-9][0-9]?$ ]]; then
	fail 'Set both upgrade versions in 1.MINOR form, or leave both empty.'
fi
[[ "${to#1.}" -eq $((${from#1.} + 1)) ]] || fail 'The trial requires exactly one forward minor upgrade.'
[[ "${1:-}" != --validate-versions ]] || exit 0

: "${KSAIL_EKS_CLUSTER_NAME:?}" "${KSAIL_EKS_WORKDIR:?}" "${AWS_REGION:?}"
: "${AWS_OIDC_ROLE_ARN:?}" "${ACTIONS_ID_TOKEN_REQUEST_URL:?}" "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:?}"

umask 077
private_dir="$(mktemp -d "${RUNNER_TEMP:-/tmp}/ksail-eks-upgrade.XXXXXX")"
trap 'rm -rf "$private_dir"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cd "$KSAIL_EKS_WORKDIR"
export AWS_PAGER='' AWS_MAX_ATTEMPTS=3

describe_cluster() {
	aws eks describe-cluster --name "$KSAIL_EKS_CLUSTER_NAME" --region "$AWS_REGION" \
		--cli-connect-timeout 10 --cli-read-timeout 30 --output json
}
updates() {
	aws eks list-updates --name "$KSAIL_EKS_CLUSTER_NAME" --region "$AWS_REGION" \
		--cli-connect-timeout 10 --cli-read-timeout 30 --output json |
		jq -ce '.updateIds | if type == "array" then sort else error("missing update inventory") end'
}
assert_cluster() {
	local version="$1" actual
	actual="$(describe_cluster)" || fail 'Cannot inspect the EKS cluster.'
	jq -e --arg version "$version" --argjson baseline "$baseline" '
		.cluster.status == "ACTIVE" and .cluster.version == $version and
		.cluster.arn == $baseline.cluster.arn and .cluster.createdAt == $baseline.cluster.createdAt
	' <<<"$actual" >/dev/null || fail 'Cluster identity, status, or version did not match the trial.'
}

baseline="$(describe_cluster)" || fail 'Cannot read the starting EKS cluster.'
jq -e --arg version "$from" '
	.cluster.status == "ACTIVE" and .cluster.version == $version and
	(.cluster.arn | type == "string" and length > 0) and .cluster.createdAt != null
' <<<"$baseline" >/dev/null || fail 'Expected an ACTIVE cluster at the starting version.'
before_updates="$(updates)" || fail 'Cannot inventory existing EKS updates.'

# Keep the real STS expiration in a credential_process response. Exporting the
# access-key tuple alone loses that metadata and cannot exercise the positive path.
# https://docs.github.com/en/actions/reference/security/oidc#methods-for-requesting-the-oidc-token
curl --fail --silent --show-error --max-time 30 \
	-H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
	"${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=sts.amazonaws.com" |
	jq -er '.value | select(type == "string" and length > 0)' >"$private_dir/oidc"
aws sts assume-role-with-web-identity --role-arn "$AWS_OIDC_ROLE_ARN" \
	--role-session-name "ksail-upgrade-${GITHUB_RUN_ID:-trial}" --duration-seconds 7200 \
	--web-identity-token "file://$private_dir/oidc" --region "$AWS_REGION" \
	--cli-connect-timeout 10 --cli-read-timeout 30 --output json >"$private_dir/sts.json"
export KSAIL_EKS_TRIAL_CREDENTIALS="$private_dir/credentials.json"
jq -e '.Credentials | {Version:1,AccessKeyId,SecretAccessKey,SessionToken,Expiration} |
	if all(.AccessKeyId,.SecretAccessKey,.SessionToken,.Expiration; type == "string" and length > 0)
	then . else error("incomplete STS session") end' "$private_dir/sts.json" >"$KSAIL_EKS_TRIAL_CREDENTIALS" ||
	fail 'STS did not return a complete session with expiry metadata.'

# Use the same real session without expiry metadata for the negative control.
# A rejection for any other reason does not establish the lifetime guard.
if (
	export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
	AWS_ACCESS_KEY_ID="$(jq -r .AccessKeyId "$KSAIL_EKS_TRIAL_CREDENTIALS")"
	AWS_SECRET_ACCESS_KEY="$(jq -r .SecretAccessKey "$KSAIL_EKS_TRIAL_CREDENTIALS")"
	AWS_SESSION_TOKEN="$(jq -r .SessionToken "$KSAIL_EKS_TRIAL_CREDENTIALS")"
	unset AWS_PROFILE AWS_DEFAULT_PROFILE
	timeout 5m ksail cluster update --kubernetes-version "$to" --yes
) >"$private_dir/rejected.log" 2>&1; then
	fail 'Upgrade unexpectedly accepted temporary credentials without expiry metadata.'
fi
grep -Fq 'EKS upgrade credentials must remain valid through the bounded wait plus one minute' \
	"$private_dir/rejected.log" || fail 'Upgrade failed before reaching the credential lifetime guard.'
assert_cluster "$from"
[[ "$(updates)" == "$before_updates" ]] || fail 'The rejected upgrade submitted an AWS update.'
echo 'PASS: unknown-expiry session rejected without an AWS update'

# The subprocess receives an isolated provider chain; it cannot fall back to the
# configure-aws-credentials action's ambient tuple or a runner credential source.
cat >"$private_dir/provider" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat "$KSAIL_EKS_TRIAL_CREDENTIALS"
EOF
chmod 700 "$private_dir/provider"
export AWS_CONFIG_FILE="$private_dir/config" AWS_SHARED_CREDENTIALS_FILE="$private_dir/empty"
export AWS_PROFILE=ksail-upgrade-trial AWS_EC2_METADATA_DISABLED=true
printf '[profile ksail-upgrade-trial]\ncredential_process = "%s/provider"\n' "$private_dir" >"$AWS_CONFIG_FILE"
: >"$AWS_SHARED_CREDENTIALS_FILE"
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_SECURITY_TOKEN \
	AWS_DEFAULT_PROFILE AWS_ROLE_ARN AWS_WEB_IDENTITY_TOKEN_FILE \
	AWS_CONTAINER_CREDENTIALS_FULL_URI AWS_CONTAINER_CREDENTIALS_RELATIVE_URI

timeout 70m ksail cluster update --kubernetes-version "$to" --yes
assert_cluster "$to"
after_updates="$(updates)" || fail 'Cannot inventory EKS updates after the upgrade.'
update_id="$(jq -er --argjson before "$before_updates" '
	. - $before | if length == 1 then .[0] else error("expected one new update") end
' <<<"$after_updates")"
aws eks describe-update --name "$KSAIL_EKS_CLUSTER_NAME" --region "$AWS_REGION" \
	--update-id "$update_id" --cli-connect-timeout 10 --cli-read-timeout 30 --output json |
	jq -e --arg target "$to" --arg id "$update_id" '
		.update.id == $id and .update.type == "VersionUpdate" and .update.status == "Successful" and
		any(.update.params[]?; .type == "Version" and .value == $target)
	' >/dev/null || fail 'AWS did not confirm the exact version update succeeded.'
[[ "$(timeout 2m kubectl get --raw /readyz)" == ok ]] || fail 'Upgraded Kubernetes API is not ready.'

timeout 5m ksail cluster update --kubernetes-version "$to" --yes
assert_cluster "$to"
[[ "$(updates)" == "$after_updates" ]] || fail 'Repeating the target submitted another AWS update.'
echo 'PASS: one successful version update, preserved identity, ready API, and repeat no-op'
