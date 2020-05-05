package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dghubble/sling"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/json"
)

const (
	userAgent = "eve"
)

type FnResponse struct {
	Status   string   `json:"status"`
	Messages []string `json:"messages"`
}

type FnCall struct {
	sling *sling.Sling
}

func NewFnTrigger(timeout time.Duration) *FnCall {
	var httpClient = &http.Client{
		Timeout: timeout,
	}

	sling := sling.New().Client(httpClient).
		Add("User-Agent", userAgent).
		ResponseDecoder(json.NewJsonDecoder())
	return &FnCall{sling: sling}
}

func (c *FnCall) Post(ctx context.Context, url string, body interface{}) (*FnResponse, error) {
	var failure string
	var success FnResponse
	r, err := c.sling.New().Post(url).BodyJSON(body).Request()
	if err != nil {
		return nil, errors.Wrap(err)
	}
	resp, err := c.sling.Do(r.WithContext(ctx), &FnResponse{}, &failure)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	if http.StatusOK == resp.StatusCode {
		return &success, nil
	} else {
		return nil, errors.Wrap(fmt.Errorf(failure))
	}
}
