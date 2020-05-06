package service

import (
	"context"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"

	"gitlab.unanet.io/devops/eve-sch/internal/vault"
)

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

func (s *Scheduler) triggerFunction(ctx context.Context, secrets vault.Secrets, service *eve.DeployArtifact, plan *eve.NSDeploymentPlan) {
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
