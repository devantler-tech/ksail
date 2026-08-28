package uiserver_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/uiserver"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/require"
)

// Every chat field the Settings page can submit is client input, so rejecting one has to answer 4xx
// rather than 5xx. The adapter maps that by wrapping api.ErrInvalid, and a field missing from that
// mapping is indistinguishable to the caller from the server failing to save.
func TestUpdateAppSettingsMapsChatValidationErrorsToInvalid(t *testing.T) {
	t.Parallel()

	for name, request := range map[string]api.AppSettings{
		"provider":         {Chat: api.ChatSettings{Provider: "unknown"}},
		"wire API":         {Chat: api.ChatSettings{WireAPI: "messages"}},
		"reasoning effort": {Chat: api.ChatSettings{ReasoningEffort: "extreme"}},
		"API key env var":  {Chat: api.ChatSettings{APIKeyEnvVar: "TEAM=KEY"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			manager, err := credentials.NewManager(credentials.NewMemoryStore())
			require.NoError(t, err)

			_, err = uiserver.UpdateAppSettingsForTest(context.Background(), manager, request)

			require.ErrorIs(t, err, api.ErrInvalid)
		})
	}
}
