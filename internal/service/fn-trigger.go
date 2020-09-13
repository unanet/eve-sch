package service

import (
	"context"
	"fmt"
	"strings"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
)

const (
	VaultEnvPrefix = "vault:"
)

func (s *Scheduler) getFunctionCode(ctx context.Context, function string) string {
	s.Logger(ctx).Debug("find function codes", zap.String("function", function))
	fnCodes, err := s.vault.GetKVSecrets(ctx, "fn_codes")
	if err != nil {
		s.Logger(ctx).Error("could not retrieve function codes from vault", zap.Error(err), zap.String("function", function))
		return "empty"
	}

	if v, ok := fnCodes[function]; ok {
		return v
	}

	s.Logger(ctx).Warn("could not find function code", zap.String("function", function))
	return "empty"
}

func (s *Scheduler) setSecrets(ctx context.Context, metadata map[string]interface{}) {
	for k, v := range metadata {
		ms, ok := v.(string)
		if !ok {
			continue
		}
		if !strings.HasPrefix(ms, VaultEnvPrefix) {
			continue
		}

		ms = strings.TrimPrefix(ms, VaultEnvPrefix)
		valueSplit := strings.Split(ms, "#")
		if len(valueSplit) != 2 {
			s.Logger(ctx).Error("invalid vault env reference", zap.String("key", k), zap.String("value", ms))
			continue
		}

		ps, err := s.vault.GetKVSecretString(ctx, valueSplit[0], valueSplit[1])
		if err != nil {
			s.Logger(ctx).Error("failed to load secrets", zap.Error(err))
			continue
		}

		metadata[k] = ps
	}
}

func (s *Scheduler) triggerFunction(ctx context.Context, optName string, service *eve.DeployArtifact, plan *eve.NSDeploymentPlan) {
	s.Logger(ctx).Debug("trigger function", zap.String("optName", optName))
	fail := s.failAndLogFn(ctx, optName, service, plan)
	payload := make(map[string]interface{})
	for k, v := range service.Metadata {
		payload[k] = v
	}
	s.setSecrets(ctx, payload)
	fnCode := s.getFunctionCode(ctx, service.ArtifactFnPtr)

	resp, err := s.fnTrigger.Post(ctx, service.ArtifactFnPtr, fnCode, payload)
	if err != nil {
		fail(err, fmt.Sprintf("fnCode: %s trigger failed", fnCode))
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
