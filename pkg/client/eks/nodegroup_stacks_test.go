package eks_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	stackResponseStart = `<ListStacksResponse xmlns="http://cloudformation.amazonaws.com/doc/2010-05-15/">` +
		`<ListStacksResult>`
	stackResponseEnd = `</ListStacksResult></ListStacksResponse>`
)

func TestNodegroupStackExistsFindsCollisionAfterFirstPage(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"CREATE_COMPLETE", "CREATE_IN_PROGRESS", "CREATE_FAILED", "DELETE_COMPLETE"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			pages := 0
			client := newNodegroupHTTPClient(
				t,
				func(writer http.ResponseWriter, request *http.Request) {
					pages++

					assert.Contains(
						t,
						request.Header.Get("Authorization"),
						"Credential=FROZENACCESS/",
					)
					assert.Contains(
						t,
						request.Header.Get("Authorization"),
						"/us-east-1/cloudformation/aws4_request",
					)

					if !assert.NoError(t, request.ParseForm()) {
						return
					}

					assert.Equal(t, "ListStacks", request.PostForm.Get("Action"))
					writer.Header().Set("Content-Type", "text/xml")

					if request.PostForm.Get("NextToken") == "" {
						_, _ = writer.Write(
							[]byte(
								stackResponseStart + `<StackSummaries/><NextToken>next</NextToken>` + stackResponseEnd,
							),
						)

						return
					}

					_, _ = writer.Write([]byte(stackResponseStart + `<StackSummaries><member>` +
						`<StackName>eksctl-demo-nodegroup-workers</StackName><StackStatus>` + status +
						`</StackStatus></member></StackSummaries>` + stackResponseEnd))
				},
			)
			exists, err := client.NodegroupStackExists(t.Context(), "demo", "workers")
			require.NoError(t, err)
			assert.Equal(t, status != "DELETE_COMPLETE", exists)
			assert.Equal(t, 2, pages)
		})
	}
}

func TestNodegroupStackExistsRejectsIncompleteInventory(t *testing.T) {
	t.Parallel()

	for _, response := range []string{
		``, `<NextToken>repeat</NextToken><StackSummaries/>`,
		`<StackSummaries><member><StackStatus>CREATE_COMPLETE</StackStatus></member></StackSummaries>`,
	} {
		t.Run(response, func(t *testing.T) {
			t.Parallel()
			client := newNodegroupHTTPClient(t, func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/xml")
				_, _ = writer.Write([]byte(stackResponseStart + response + stackResponseEnd))
			})
			exists, err := client.NodegroupStackExists(t.Context(), "demo", "workers")
			require.Error(t, err)
			assert.False(t, exists)
		})
	}
}
