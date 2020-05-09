package service

import (
	"context"
	"strings"

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

func (s *Scheduler) getSecrets(ctx context.Context, paths string) vault.Secrets {
	var secrets vault.Secrets = make(map[string]string)
	if len(paths) == 0 {
		return secrets
	}
	secretPaths := strings.Split(paths, ",")
	for _, x := range secretPaths {
		ps, err := s.vault.GetKVSecrets(ctx, x)
		if err != nil {
			s.Logger(ctx).Error("failed to load secrets", zap.String("path", x), zap.Error(err))
			continue
		}
		for k, v := range ps {
			secrets[k] = v
		}
	}

	return secrets
}

func (s *Scheduler) triggerFunction(ctx context.Context, service *eve.DeployArtifact, plan *eve.NSDeploymentPlan) {
	fail := s.failAndLogFn(ctx, service, plan)
	secrets := s.getSecrets(ctx, service.InjectVaultPaths)
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
		fail(err, "function trigger for artifact failed")
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