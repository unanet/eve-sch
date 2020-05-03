package service

import (
	"context"
	"encoding/json"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/eve/pkg/s3"
)

func (s *Scheduler) handleMessage(ctx context.Context, m *queue.M) error {
	switch m.Command {
	case CommandDeployNamespace:
		return s.deployNamespace(ctx, m)
	default:
		return errors.Wrapf("unrecognized command: %s", m.Command)
	}
}

func (s *Scheduler) deployNamespace(ctx context.Context, m *queue.M) error {
	var location s3.Location
	err := json.Unmarshal(m.Body, &location)
	if err != nil {
		return errors.Wrap(err)
	}

	planText, err := s.downloader.Download(ctx, &location)
	if err != nil {
		return errors.Wrap(err)
	}

	var nsDeploymentPlan eve.NSDeploymentPlan
	err = json.Unmarshal(planText,  &nsDeploymentPlan)

	return nil
}

