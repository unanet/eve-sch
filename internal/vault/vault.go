package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os/user"
	"strings"
	"time"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	ehttp "gitlab.unanet.io/devops/eve/pkg/http"
)

type Secrets map[string]string

type Client struct {
	httpClient  *http.Client
	vaultAddr   string
	vaultRole   string
	tokenParser TokenParser
}

//noinspection SpellCheckingInspection
const (
	k8sTokenPath = "/run/secrets/kubernetes.io/serviceaccount/token"
)

type TokenParser func(ctx context.Context) (string, error)

func TokenParserForExistingToken(_ *Client) TokenParser {
	return func(ctx context.Context) (string, error) {
		usr, err := user.Current()
		if err != nil {
			return "", errors.Wrap(err)
		}

		// Read entire file content, giving us little control but
		// making it very simple. No need to close the file.
		content, err := ioutil.ReadFile(fmt.Sprintf("%s/.vault-token", usr.HomeDir))
		if err != nil {
			return "", errors.Wrap(err)
		}

		return string(content), nil
	}
}

func TokenParserK8s(c *Client) TokenParser {
	return func(ctx context.Context) (string, error) {
		data, err := ioutil.ReadFile(k8sTokenPath)
		if err != nil {
			return "", errors.Wrap(err)
		}

		var jsonStr = []byte(fmt.Sprintf(`{"jwt":"%s", "role": "%s"}`, data, c.vaultRole))
		req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/v1/auth/kubernetes/login", c.vaultAddr), bytes.NewBuffer(jsonStr))
		if err != nil {
			return "", errors.Wrap(err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", errors.Wrap(err)
		}
		defer resp.Body.Close()

		var respMap map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&respMap)
		if err != nil {
			return "", errors.Wrap(err)
		}

		if _, ok := respMap["auth"]; !ok {
			return "", errors.Wrapf("/auth property not found in vault response")
		}

		auth, ok := respMap["auth"].(map[string]interface{})
		if !ok {
			return "", errors.Wrapf("/auth property not a map")
		}

		if _, ok := auth["client_token"]; !ok {
			return "", errors.Wrapf("/auth/client_token property not found")
		}

		token, ok := auth["client_token"].(string)
		if !ok {
			return "", errors.Wrapf("/auth/client_token property not a string")
		}
		return token, nil
	}
}

type Config struct {
	VaultTimeout time.Duration `split_words:"true" default:"10s"`
	VaultAddr    string        `split_words:"true" default:"http://localhost:8200"`
	VaultRole    string        `split_words:"true" default:"vault-auth"`
}

func NewClient(config Config, tokenAuthenticators ...TokenParser) (*Client, error) {
	if strings.HasSuffix(config.VaultAddr, "/") {
		config.VaultAddr = strings.TrimSuffix(config.VaultAddr, "/")
	}

	httpClient := &http.Client{
		Timeout:   config.VaultTimeout,
		Transport: ehttp.LoggingTransport,
	}

	client := &Client{
		httpClient: httpClient,
		vaultAddr:  config.VaultAddr,
		vaultRole:  config.VaultRole,
	}

	if len(tokenAuthenticators) == 0 {
		client.tokenParser = TokenParserK8s(client)
	} else {
		client.tokenParser = tokenAuthenticators[0]
	}

	return client, nil
}

func getKey(data map[string]interface{}, key string) string {
	value, ok := data[key]
	if !ok {
		return ""
	}

	if rs, ok := value.(string); ok {
		return rs
	}

	return ""
}

func getMap(data map[string]interface{}) map[string]string {
	var returnMap = make(map[string]string)
	for k := range data {
		returnMap[k] = getKey(data, k)
	}
	return returnMap
}

func (c *Client) getKvSecret(ctx context.Context, path string) (map[string]interface{}, error) {
	token, err := c.tokenParser(ctx)
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/v1/kv/data/%s", c.vaultAddr, path), nil)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	req.Header.Add("X-Vault-Token", token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	defer resp.Body.Close()

	var respMap map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&respMap)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	if _, ok := respMap["data"]; !ok {
		return nil, errors.Wrapf("/data property not found")
	}

	data, ok := respMap["data"].(map[string]interface{})
	if !ok {
		return nil, errors.Wrapf("/data property not a map")
	}

	if _, ok := data["data"]; !ok {
		return nil, errors.Wrapf("/data/data property not found")
	}

	data, ok = data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.Wrapf("/data/data property not a map")
	}

	return data, nil
}

func (c *Client) GetKVSecretString(ctx context.Context, path string, key string) (string, error) {
	secretMap, err := c.getKvSecret(ctx, path)
	if err != nil {
		return "", errors.Wrap(err)
	}

	return getKey(secretMap, key), nil
}

func (c *Client) GetKVSecrets(ctx context.Context, path string) (Secrets, error) {
	secretMap, err := c.getKvSecret(ctx, path)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return getMap(secretMap), nil
}
