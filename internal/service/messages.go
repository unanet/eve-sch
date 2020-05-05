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

	vaultSecrets, err := s.vault.GetKVSecretMap(nsDeploymentPlan.Namespace.ClusterName)
	if err != nil {
		return errors.Wrap(err)
	}

	for _, x := range nsDeploymentPlan.Services {
		if len(x.ArtifactFnPtr) == 0 {
			continue
		}

		payload := make(map[string]interface{})
		for k, v := range x.Metadata {
			payload[k] = v
		}

		for k, v := range vaultSecrets {
			payload[k] = v
		}

		payload["environment"] = nsDeploymentPlan.EnvironmentName
		payload["namespace"] = nsDeploymentPlan.Namespace.Name
		payload["cluster"] = nsDeploymentPlan.Namespace.ClusterName
		payload["artifact_name"] = x.ArtifactName
		payload["artifact_version"] = x.AvailableVersion
		payload["artifact_repo"] = x.ArtifactoryFeed
		payload["artifact_path"] = x.ArtifactoryPath

		resp, err := s.fnTrigger.Post(ctx, x.ArtifactFnPtr, payload)
		if err != nil {
			return errors.Wrap(err)
		}

		s.Logger(ctx).Info("response from function call", zap.String("resp", fmt.Sprintf("%v", resp)))
	}

	err = s.worker.DeleteMessage(ctx, m)
	if err != nil {
		return errors.Wrap(err)
	}

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
