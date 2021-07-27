package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/unanet/go/pkg/errors"
	chttp "github.com/unanet/go/pkg/http"
	"go.uber.org/zap"
)

type CallbackConfig struct {
	CallbackUrl string `split_words:"true" required:"true"`
}

type Callback struct {
	url       string
	timeout   time.Duration
	userAgent string
	client    *http.Client
	l         *zap.Logger
}

func NewCallback(logger *zap.Logger) (*Callback, error) {
	c := CallbackConfig{}
	err := envconfig.Process("EVE", &c)
	if err != nil {
		return nil, errors.Wrapf("missing callback config")
	}

	api := &Callback{
		userAgent: "eve-sch/sdk",
		url:       c.CallbackUrl,
		timeout:   time.Duration(15) * time.Second,
		l:         logger,
	}

	api.client = &http.Client{
		Transport: chttp.LoggingTransport,
		Timeout:   api.timeout,
	}

	return api, nil
}

func (c *Callback) Message(messages ...string) {
	failFn := func(err error) {
		c.l.Error("callback message failed", zap.Error(err), zap.String("messages", fmt.Sprintf("%v", messages)))
	}
	messagesJson, err := json.Marshal(&CallbackMessage{Messages: messages})
	if err != nil {
		failFn(err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewBuffer(messagesJson))
	if err != nil {
		failFn(err)
		return
	}

	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		failFn(err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		failFn(errors.Wrapf("failed to send callback message to eve-sch, status: %d", resp.StatusCode))
		return
	}
}

func (c *Callback) Messagef(message string, a ...interface{}) {
	c.Message(fmt.Sprintf(message, a...))
}
