package service

import (
	"context"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"go.uber.org/zap"

	"gitlab.unanet.io/devops/eve-sch/internal/vault"
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

func (s *Scheduler) getFunctionCode(ctx context.Context, function string) string {
	fnCodes, err := s.vault.GetKVSecrets(ctx, "fn_codes")
	if err != nil {
		s.Logger(ctx).Warn("could not retrieve function codes from vault", zap.Error(err))
		return "empty"
	}

	if v, ok := fnCodes[function]; ok {
		return v
	}

	s.Logger(ctx).Warn("could not find function code", zap.String("function", function))
	return "empty"
}

func (s *Scheduler) triggerFunction(ctx context.Context, secrets vault.Secrets, service *eve.DeployService, plan *eve.NSDeploymentPlan) {
	payload := make(map[string]interface{})
	for k, v := range service.Metadata {
		payload[k] = v
	}

	for k, v := range secrets {
		payload[k] = v
	}

	payload["environment"] = plan.EnvironmentName
	payload["namespace"] = plan.Namespace.Alias
	payload["cluster"] = plan.Namespace.ClusterName
	payload["artifact_name"] = service.ArtifactName
	payload["artifact_version"] = service.AvailableVersion
	payload["artifact_repo"] = service.ArtifactoryFeed
	payload["artifact_path"] = service.ArtifactoryPath

	fnCode := s.getFunctionCode(ctx, service.ArtifactFnPtr)

	resp, err := s.fnTrigger.Post(ctx, service.ArtifactFnPtr, fnCode, payload)
	if err != nil {
		plan.Message("artifact deployment failed for: %s", service.ArtifactName)
		service.Result = eve.DeployArtifactResultFailed
		return
	}

	for _, x := range resp.Messages {
		if len(x) == 0 {
			continue
		}
		plan.Messages = append(plan.Messages, x)
	}

	service.Result = eve.ParseDeployArtifactResult(resp.Result)
}

func (s *Scheduler) deployNamespace(ctx context.Context, m *queue.M) error {
	plan, err := eve.UnMarshalNSDeploymentFromS3LocationBody(ctx, s.downloader, m.Body)
	if err != nil {
		return errors.Wrap(err)
	}

	secrets, err := s.vault.GetKVSecrets(ctx, plan.Namespace.ClusterName)
	if err != nil {
		return errors.Wrap(err)
	}

	for _, x := range plan.Services {
		if len(x.ArtifactFnPtr) > 0 {
			s.triggerFunction(ctx, secrets, x, plan)
		}
	}

	err = s.worker.DeleteMessage(ctx, m)
	if err != nil {
		return errors.Wrap(err)
	}

	if plan.Failed() {
		plan.Status = eve.DeploymentPlanStatusErrors
	} else {
		plan.Status = eve.DeploymentPlanStatusComplete
	}

	mBody, err := eve.MarshalNSDeploymentPlanToS3LocationBody(ctx, s.uploader, plan)
	if err != nil {
		return errors.Wrap(err)
	}

	err = s.worker.Message(ctx, s.apiQUrl, &queue.M{
		ID:      m.ID,
		ReqID:   queue.GetReqID(ctx),
		GroupID: GroupUpdateDeployment,
		Body:    mBody,
		Command: CommandUpdateDeployment,
	})
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}
