package secrets

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os/user"
	"time"

	"github.com/hashicorp/vault/api"
	"go.uber.org/zap"

	ehttp "gitlab.unanet.io/devops/eve/pkg/http"
	"gitlab.unanet.io/devops/eve/pkg/log"
)

type Client struct {
	api.Client
}

type Argument func(*api.Client)

func VaultToken() Argument {
	return func(c *api.Client) {
		usr, err := user.Current()
		if err != nil {
			log.Logger.Error("unable to determine user from user.Current()", zap.Error(err))
			return
		}

		// Read entire file content, giving us little control but
		// making it very simple. No need to close the file.
		content, err := ioutil.ReadFile(fmt.Sprintf("%s/.vault-token", usr.HomeDir))
		if err != nil {
			log.Logger.Error("unable to load vault token from home directory", zap.Error(err))
			return
		}

		c.SetToken(string(content))
	}
}

type Config struct {
	VaultTimeout time.Duration `split_words:"true" default:"10s"`
	VaultAddr    string        `split_words:"true" default:"http://localhost:8200"`
}

func NewClient(config Config, arguments ...Argument) (*Client, error) {
	client, err := api.NewClient(&api.Config{Address: config.VaultAddr, HttpClient: &http.Client{
		Timeout:   config.VaultTimeout,
		Transport: ehttp.LoggingTransport,
	}})
	if err != nil {
		return nil, err
	}

	if len(arguments) == 0 {
		VaultToken()(client)
	}

	return &Client{*client}, nil
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

func (c Client) GetKVSecret(path string, key string) (string, error) {
	data, err := c.Logical().Read(fmt.Sprintf("kv/data/%s", path))
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", NotFoundErrorf("key: %s, cannot be found", path)
	}

	return getKey(data.Data["data"].(map[string]interface{}), key), nil
}
