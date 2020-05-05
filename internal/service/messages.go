package service

import (
	"context"
	"encoding/json"
	"fmt"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/eve/pkg/s3"
	"go.uber.org/zap"
)

const (
	CommandUpdateDeployment string = "api-update-deployment"
	GroupUpdateDeployment   string = "api-update-deployment"
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
	err = json.Unmarshal(planText, &nsDeploymentPlan)
	if err != nil {
		return errors.Wrap(err)
	}

	err = s.worker.DeleteMessage(ctx, m)
	if err != nil {
		return errors.Wrap(err)
	}

	sMap, err := s.vault.GetKVSecretMap("eve-sch")
	if err != nil {
		return errors.Wrap(err)
	}

	s.Logger(ctx).Info("here's the map", zap.String("map", fmt.Sprintf("%v", sMap)))

	uLocation, err := s.uploader.Upload(ctx, fmt.Sprintf("%s-result", m.ID), planText)
	if err != nil {
		return errors.Wrap(err)
	}
	uLocationBytes, err := json.Marshal(uLocation)
	if err != nil {
		return errors.Wrap(err)
	}

	err = s.worker.Message(ctx, s.apiQUrl, &queue.M{
		ID:      m.ID,
		ReqID:   queue.GetReqID(ctx),
		GroupID: GroupUpdateDeployment,
		Body:    uLocationBytes,
		Command: CommandUpdateDeployment,
	})
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}
