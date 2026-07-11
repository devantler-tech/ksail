package mirror_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSteeringRedirectValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		redirect mirror.SteeringRedirect
		wantErr  bool
	}{
		{
			name:     "valid ports",
			redirect: mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 9090},
		},
		{
			name:     "port bounds",
			redirect: mirror.SteeringRedirect{ServicePort: 1, InterceptPort: 65535},
		},
		{
			name:     "zero service port",
			redirect: mirror.SteeringRedirect{ServicePort: 0, InterceptPort: 9090},
			wantErr:  true,
		},
		{
			name:     "negative service port",
			redirect: mirror.SteeringRedirect{ServicePort: -1, InterceptPort: 9090},
			wantErr:  true,
		},
		{
			name:     "service port too high",
			redirect: mirror.SteeringRedirect{ServicePort: 65536, InterceptPort: 9090},
			wantErr:  true,
		},
		{
			name:     "zero intercept port",
			redirect: mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 0},
			wantErr:  true,
		},
		{
			name:     "intercept port too high",
			redirect: mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 70000},
			wantErr:  true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.redirect.Validate()

			if testCase.wantErr {
				require.ErrorIs(t, err, mirror.ErrSteeringPortInvalid)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSteeringRedirectInsertArgs(t *testing.T) {
	t.Parallel()

	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 9090}

	args, err := redirect.InsertArgs()

	require.NoError(t, err)
	assert.Equal(t, []string{
		"-t", "nat", "-I", "PREROUTING",
		"-p", "tcp",
		"--dport", "8080",
		"-m", "comment", "--comment", mirror.SteeringRuleComment,
		"-j", "REDIRECT",
		"--to-ports", "9090",
	}, args)
}

func TestSteeringRedirectDeleteArgs(t *testing.T) {
	t.Parallel()

	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 9090}

	args, err := redirect.DeleteArgs()

	require.NoError(t, err)
	assert.Equal(t, []string{
		"-t", "nat", "-D", "PREROUTING",
		"-p", "tcp",
		"--dport", "8080",
		"-m", "comment", "--comment", mirror.SteeringRuleComment,
		"-j", "REDIRECT",
		"--to-ports", "9090",
	}, args)
}

func TestSteeringRedirectDeleteIsExactInverseOfInsert(t *testing.T) {
	t.Parallel()

	// Reversibility is a hard requirement (#5839): iptables -D only matches a
	// byte-identical rule specification, so the delete vector must differ from
	// the insert vector in exactly the chain action and nothing else.
	redirect := mirror.SteeringRedirect{ServicePort: 443, InterceptPort: 18443}

	insert, err := redirect.InsertArgs()
	require.NoError(t, err)

	deleteArgs, err := redirect.DeleteArgs()
	require.NoError(t, err)

	require.Len(t, deleteArgs, len(insert))

	for index := range insert {
		if insert[index] == "-I" {
			assert.Equal(t, "-D", deleteArgs[index], "index %d", index)

			continue
		}

		assert.Equal(t, insert[index], deleteArgs[index], "index %d", index)
	}
}

func TestSteeringRedirectGuardInsertArgs(t *testing.T) {
	t.Parallel()

	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 9090}

	args, err := redirect.GuardInsertArgs()

	require.NoError(t, err)
	assert.Equal(t, []string{
		"-t", "filter", "-I", "INPUT",
		"-p", "tcp",
		"--dport", "9090",
		"-m", "conntrack", "!", "--ctstate", "DNAT",
		"-m", "comment", "--comment", mirror.SteeringRuleComment,
		"-j", "DROP",
	}, args)
}

func TestSteeringRedirectGuardDeleteArgs(t *testing.T) {
	t.Parallel()

	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 9090}

	args, err := redirect.GuardDeleteArgs()

	require.NoError(t, err)
	assert.Equal(t, []string{
		"-t", "filter", "-D", "INPUT",
		"-p", "tcp",
		"--dport", "9090",
		"-m", "conntrack", "!", "--ctstate", "DNAT",
		"-m", "comment", "--comment", mirror.SteeringRuleComment,
		"-j", "DROP",
	}, args)
}

func TestSteeringRedirectGuardDeleteIsExactInverseOfInsert(t *testing.T) {
	t.Parallel()

	// Same reversibility requirement as the redirect rule (#5839): iptables -D
	// only matches a byte-identical specification.
	redirect := mirror.SteeringRedirect{ServicePort: 443, InterceptPort: 18443}

	insert, err := redirect.GuardInsertArgs()
	require.NoError(t, err)

	deleteArgs, err := redirect.GuardDeleteArgs()
	require.NoError(t, err)

	require.Len(t, deleteArgs, len(insert))

	for index := range insert {
		if insert[index] == "-I" {
			assert.Equal(t, "-D", deleteArgs[index], "index %d", index)

			continue
		}

		assert.Equal(t, insert[index], deleteArgs[index], "index %d", index)
	}
}

func TestSteeringRedirectGuardArgsInvalid(t *testing.T) {
	t.Parallel()

	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 0}

	insert, err := redirect.GuardInsertArgs()
	require.ErrorIs(t, err, mirror.ErrSteeringPortInvalid)
	assert.Nil(t, insert)

	deleteArgs, err := redirect.GuardDeleteArgs()
	require.ErrorIs(t, err, mirror.ErrSteeringPortInvalid)
	assert.Nil(t, deleteArgs)
}

func TestSteeringRedirectArgsInvalid(t *testing.T) {
	t.Parallel()

	redirect := mirror.SteeringRedirect{ServicePort: 0, InterceptPort: 9090}

	insert, err := redirect.InsertArgs()
	require.ErrorIs(t, err, mirror.ErrSteeringPortInvalid)
	assert.Nil(t, insert)

	deleteArgs, err := redirect.DeleteArgs()
	require.ErrorIs(t, err, mirror.ErrSteeringPortInvalid)
	assert.Nil(t, deleteArgs)
}
