package service

import (
	"context"
	"fmt"
	"strings"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
)

const (
	VaultEnvPrefix = "vault:"
)

func (s *Scheduler) getFunctionCode(ctx context.Context, function string) (string, error) {
	s.Logger(ctx).Debug("find function codes", zap.String("function", function))
	fnCodes, err := s.vault.GetKVSecrets(ctx, "fn_codes")
	if err != nil {
		return "", errors.Wrapf("failed to get fn_code secrets from vault for function: %s error: %v", function, err.Error())
	}

	if v, ok := fnCodes[function]; ok {
		return v, nil
	}
	return "", fmt.Errorf("failed to get the function code")
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
	fnCode, err := s.getFunctionCode(ctx, service.ArtifactFnPtr)
	if err != nil {
		fail(err, fmt.Sprintf("get function code failed"))
		return
	}

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
