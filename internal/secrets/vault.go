package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os/user"
	"strings"
	"time"

	"github.com/hashicorp/vault/api"
	"gitlab.unanet.io/devops/eve/pkg/errors"
	ehttp "gitlab.unanet.io/devops/eve/pkg/http"
)

type VaultClient = api.Client

type Client struct {
	*VaultClient
	httpClient         *http.Client
	vaultAddr          string
	vaultRole          string
	tokenAuthenticator TokenAuthenticator
}

const (
	k8sTokenPath = "/run/secrets/kubernetes.io/serviceaccount/token"
)

type TokenAuthenticator func(c *Client) error

func TokenAuthenticatorExistingToken(c *Client) error {
	usr, err := user.Current()
	if err != nil {
		return errors.Wrap(err)
	}

	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile(fmt.Sprintf("%s/.vault-token", usr.HomeDir))
	if err != nil {
		return errors.Wrap(err)
	}

	c.SetToken(string(content))
	return nil
}

func TokenAuthenticatorK8s(c *Client) error {
	data, err := ioutil.ReadFile(k8sTokenPath)
	if err != nil {
		return errors.Wrap(err)
	}

	var jsonStr = []byte(fmt.Sprintf(`{"jwt":"%s", "role": "%s"}`, data, c.vaultRole))
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/v1/auth/kubernetes/login", c.vaultAddr), bytes.NewBuffer(jsonStr))
	if err != nil {
		return errors.Wrap(err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err)
	}
	defer resp.Body.Close()

	var respMap map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&respMap)
	if err != nil {
		return errors.Wrap(err)
	}

	if _, ok := respMap["auth"]; !ok {
		return errors.Wrapf("invalid vault response: %v", respMap)
	}

	auth, ok := respMap["auth"].(map[string]interface{})
	if !ok {
		return errors.Wrapf("invalid vault response [auth]: %vs", respMap)
	}
	token, ok := auth["client_token"].(string)
	if !ok {
		return errors.Wrapf("invalid vault response [client_token]: %vs", auth)
	}
	c.SetToken(token)
	return nil
}

type Config struct {
	VaultTimeout time.Duration `split_words:"true" default:"10s"`
	VaultAddr    string        `split_words:"true" default:"http://localhost:8200"`
	VaultRole    string        `split_words:"true" default:"vault-auth"`
}

func NewClient(config Config, tokenAuthenticators ...TokenAuthenticator) (*Client, error) {
	if strings.HasSuffix(config.VaultAddr, "/") {
		config.VaultAddr = strings.TrimSuffix(config.VaultAddr, "/")
	}

	httpClient := &http.Client{
		Timeout:   config.VaultTimeout,
		Transport: ehttp.LoggingTransport,
	}

	vaultClient, err := api.NewClient(&api.Config{Address: config.VaultAddr, HttpClient: httpClient})
	if err != nil {
		return nil, err
	}

	client := &Client{
		VaultClient: vaultClient,
		httpClient:  httpClient,
		vaultAddr:   config.VaultAddr,
		vaultRole:   config.VaultRole,
	}

	if len(tokenAuthenticators) == 0 {
		client.tokenAuthenticator = TokenAuthenticatorK8s
	} else {
		client.tokenAuthenticator = tokenAuthenticators[0]
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

func (c *Client) GetKVSecretString(path string, key string) (string, error) {
	if err := c.tokenAuthenticator(c); err != nil {
		return "", errors.Wrap(err)
	}
	data, err := c.Logical().Read(fmt.Sprintf("kv/data/%s", path))
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", NotFoundErrorf("key: %s, cannot be found", path)
	}

	return getKey(data.Data["data"].(map[string]interface{}), key), nil
}

func (c *Client) GetKVSecretMap(path string) (map[string]string, error) {
	if err := c.tokenAuthenticator(c); err != nil {
		return nil, errors.Wrap(err)
	}
	data, err := c.Logical().Read(fmt.Sprintf("kv/data/%s", path))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, NotFoundErrorf("key: %s, cannot be found", path)
	}

	return getMap(data.Data["data"].(map[string]interface{})), nil
}
