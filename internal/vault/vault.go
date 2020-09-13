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

	"github.com/kelseyhightower/envconfig"
	"gitlab.unanet.io/devops/eve/pkg/errors"
	ehttp "gitlab.unanet.io/devops/eve/pkg/http"
	"gitlab.unanet.io/devops/eve/pkg/log"
	"go.uber.org/zap"
)

type Secrets map[string]string

type Client struct {
	httpClient  *http.Client
	vaultAddr   string
	vaultRole   string
	vaultMount  string
	clusterName string
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
		log.Logger.Debug("vault kubernetes login token parser", zap.String("vault_role", c.vaultRole))
		data, err := ioutil.ReadFile(k8sTokenPath)
		if err != nil {
			return "", errors.Wrap(err)
		}

		if len(data) <= 10 {
			log.Logger.Warn("vault kubernetes login jwt token too short", zap.String("data", string(data)))
		}

		var jsonStr = []byte(fmt.Sprintf(`{"jwt":"%s", "role": "%s"}`, data, c.vaultRole))
		req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/v1/auth/%s/login", c.vaultAddr, c.vaultMount), bytes.NewBuffer(jsonStr))
		if err != nil {
			return "", errors.Wrap(err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", errors.Wrap(err)
		}
		defer resp.Body.Close()

		statusOK := resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices

		if !statusOK {
			log.Logger.Warn("vault kubernetes login failure", zap.String("status", resp.Status), zap.Int("status_code", resp.StatusCode), zap.String("vault_role", c.vaultRole))
		}

		var respMap map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&respMap)
		if err != nil {
			return "", errors.Wrapf("failed to json decode vault kubernetes login response: %v", err.Error())
		}

		if verrs, ok := respMap["errors"].([]string); ok {
			return "", errors.Wrapf("vault errors: %v", strings.Join(verrs, ","))
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

		log.Logger.Debug("vault kubernetes login sucess")
		return token, nil
	}
}

type Config struct {
	Timeout        time.Duration `split_words:"true" default:"10s"`
	Addr           string        `split_words:"true" default:"http://localhost:8200"`
	Role           string        `split_words:"true" default:"vault-auth"`
	K8sMount       string        `split_words:"true" default:"kubernetes"`
	K8sClusterName string        `split_words:"true" default:"devops-prod"`
}

func NewClient(tokenAuthenticators ...TokenParser) (*Client, error) {
	var config Config
	err := envconfig.Process("VAULT", &config)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	if strings.HasSuffix(config.Addr, "/") {
		config.Addr = strings.TrimSuffix(config.Addr, "/")
	}

	httpClient := &http.Client{
		Timeout:   config.Timeout,
		Transport: ehttp.LoggingTransport,
	}

	client := &Client{
		httpClient:  httpClient,
		vaultAddr:   config.Addr,
		vaultRole:   config.Role,
		vaultMount:  config.K8sMount,
		clusterName: config.K8sClusterName,
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
	log.Logger.Debug("vault get KvSecret", zap.String("path", path), zap.String("cluster_name", c.clusterName))
	token, err := c.tokenParser(ctx)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	// We are pretty restrictive on what vault data this code can get
	// each eve-sch lives in a cluster and has access to clusters/{{VAULT_K8S_CLUSTER_NAME}}/eve-sch
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/v1/kv/data/clusters/%s/eve-sch/%s", c.vaultAddr, c.clusterName, path), nil)
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
		return nil, errors.Wrapf("vault respMap['data'] property not found")
	}

	data, ok := respMap["data"].(map[string]interface{})
	if !ok {
		return nil, errors.Wrapf("vault respMap['data'].(map[string]interface{}) property not found")
	}

	if _, ok := data["data"]; !ok {
		return nil, errors.Wrapf("vault data['data'] property not found")
	}

	data, ok = data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.Wrapf("vault data['data'].(map[string]interface{}) property not found")
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
