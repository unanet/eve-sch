// +build local

package vault_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.unanet.io/devops/eve-sch/internal/vault"
)

var (
	c *vault.Client
)

func client(t *testing.T) *vault.Client {
	if c != nil {
		return c
	}

	cl, err := vault.NewClient(vault.TokenParserForExistingToken(nil))
	require.NoError(t, err)
	c = cl
	require.NotNil(t, c)
	return c
}

func TestClient_GetKVSecret(t *testing.T) {
	resp, err := client(t).GetKVSecretString(context.TODO(), "devops/artifactory", "ci_readonly_username")
	require.NoError(t, err)
	require.Equal(t, "unanet-ci-r", resp)
}
