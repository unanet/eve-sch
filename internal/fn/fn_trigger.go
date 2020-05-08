package fn

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dghubble/sling"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	ehttp "gitlab.unanet.io/devops/eve/pkg/http"
	"gitlab.unanet.io/devops/eve/pkg/json"
)

const (
	userAgent = "eve"
)

type Response struct {
	Result   string   `json:"result"`
	Messages []string `json:"messages"`
}

type Trigger struct {
	sling *sling.Sling
}

func NewTrigger(timeout time.Duration) *Trigger {
	var httpClient = &http.Client{
		Timeout:   timeout,
		Transport: ehttp.LoggingTransport,
	}

	sling := sling.New().Client(httpClient).
		Add("User-Agent", userAgent).
		ResponseDecoder(json.NewJsonDecoder())
	return &Trigger{sling: sling}
}

func (c *Trigger) Post(ctx context.Context, url string, code string, body interface{}) (*Response, error) {
	var failure string
	var success Response
	r, err := c.sling.New().Post(url).BodyJSON(body).QueryStruct(struct {
		Code string `json:"code"`
	}{
		Code: code,
	}).Request()
	if err != nil {
		return nil, errors.Wrap(err)
	}
	resp, err := c.sling.Do(r.WithContext(ctx), &success, &failure)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	if http.StatusOK == resp.StatusCode {
		return &success, nil
	} else {
		return nil, errors.Wrap(fmt.Errorf(failure))
	}
}
