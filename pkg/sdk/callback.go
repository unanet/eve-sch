package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kelseyhightower/envconfig"
	"gitlab.unanet.io/devops/go/pkg/errors"
	chttp "gitlab.unanet.io/devops/go/pkg/http"
)

type CallbackConfig struct {
	CallbackUrl string `split_words:"true" required:"true"`
}

type Callback struct {
	url       string
	timeout   time.Duration
	userAgent string
	client    *http.Client
}

func NewCallback() (*Callback, error) {
	c := CallbackConfig{}
	err := envconfig.Process("EVE", &c)
	if err != nil {
		return nil, errors.Wrapf("missing callback config")
	}

	api := &Callback{
		userAgent: "eve-sch/sdk",
		url:       c.CallbackUrl,
		timeout:   time.Duration(15) * time.Second,
	}

	api.client = &http.Client{
		Transport: chttp.LoggingTransport,
		Timeout:   api.timeout,
	}

	return api, nil
}

func (c *Callback) Message(messages ...string) error {
	messagesJson, err := json.Marshal(&CallbackMessage{Messages: messages})
	if err != nil {
		return errors.Wrap(err)
	}

	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewBuffer(messagesJson))
	if err != nil {
		return errors.Wrap(err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return errors.Wrap(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		return errors.Wrapf("failed to send callback message to eve-sch, status: %d", resp.StatusCode)
	}
	return nil
}

func (c *Callback) Messagef(message string, a ...interface{}) error {
	return c.Message(fmt.Sprintf(message, a...))
}
