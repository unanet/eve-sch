package service

import (
	"context"
	"encoding/json"

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

func (s *Scheduler) getSecrets(ctx context.Context, paths []string) vault.Secrets {
	var secrets vault.Secrets = make(map[string]string)
	for _, x := range paths {
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

func (s *Scheduler) triggerFunction(ctx context.Context, service *eve.DeployArtifact, plan *eve.NSDeploymentPlan, vaultPaths []string) {
	fail := s.failAndLogFn(ctx, service, plan)
	secrets := s.getSecrets(ctx, vaultPaths)
	payload := make(map[string]interface{})
	for k, v := range service.Metadata {
		payload[k] = v
	}

	for k, v := range secrets {
		payload[k] = v
	}

	json, _ := json.Marshal(&payload)
	s.Logger(ctx).Info("###################### PAYLOAD ######################", zap.ByteString("payload", json))

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
