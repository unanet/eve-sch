package service

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func mergeM(standard map[string]interface{}, definition map[string]interface{}) map[string]interface{} {
	var merged = make(map[string]interface{})
	if definition != nil {
		for k, v := range definition {
			merged[k] = v
		}
	}

	for k, v := range standard {
		merged[k] = v
	}

	return merged
}

func getDeploymentContainerPorts(eveDeployment eve.DeploymentSpec) []interface{} {
	var result = make([]interface{}, 0)
	// Setup the Service Port
	if eveDeployment.GetServicePort() != 0 {
		result = append(result, map[string]interface{}{
			"name":          "http",
			"containerPort": int64(eveDeployment.GetServicePort()),
			"protocol":      string(apiv1.ProtocolTCP),
		})
	}
	// Setup the Metrics Port
	if eveDeployment.GetMetricsPort() != 0 {
		result = append(result, map[string]interface{}{
			"name":          "metrics",
			"containerPort": int64(eveDeployment.GetMetricsPort()),
			"protocol":      string(apiv1.ProtocolTCP),
		})
	}
	return result
}

func getDockerImageName(artifact *eve.DeployArtifact) string {
	return fmt.Sprintf("%s/%s:%s", fmt.Sprintf(DockerRepoFormat, artifact.ArtifactoryFeed), artifact.ArtifactoryPath, artifact.EvalImageTag())
}

func defaultContainerEnvVars(deploymentID uuid.UUID, artifact *eve.DeployArtifact) []interface{} {
	c := config.GetConfig()
	artifact.Metadata["EVE_CALLBACK_URL"] = fmt.Sprintf("http://eve-sch-v1.%s:%d/callback?id=%s", c.Namespace, c.Port, deploymentID.String())
	artifact.Metadata["EVE_IMAGE_NAME"] = getDockerImageName(artifact)

	var containerEnvVars = make([]interface{}, 0)
	for k, v := range artifact.Metadata {
		value, ok := v.(string)
		if !ok {
			continue
		}

		containerEnvVars = append(containerEnvVars, map[string]interface{}{
			"name":  k,
			"value": value,
		})
	}

	return containerEnvVars
}

func overrideImagePullSecrets(definition *unstructured.Unstructured) error {
	// extract spec image pull secret
	ips, found, err := unstructured.NestedSlice(definition.Object, definitionSpecKeyMap["imagePullSecrets"]...)
	if err != nil || !found || ips == nil {
		if err := unstructured.SetNestedField(definition.Object, []interface{}{map[string]interface{}{"name": "docker-cfg"}}, definitionSpecKeyMap["imagePullSecrets"]...); err != nil {
			return errors.Wrap(err, "failed to update imagePullSecrets")
		}
		return nil
	}

	if err := unstructured.SetNestedField(ips[0].(map[string]interface{}), "docker-cfg", "name"); err != nil {
		return errors.Wrap(err, "failed to update image pull secrets with docker-cfg")
	}
	if err := unstructured.SetNestedField(definition.Object, ips, definitionSpecKeyMap["imagePullSecrets"]...); err != nil {
		return errors.Wrap(err, "failed to update image pull secrets")
	}
	return nil
}

func overrideContainers(definition *unstructured.Unstructured, eveDeployment eve.DeploymentSpec, plan *eve.NSDeploymentPlan) error {
	var defContainerErrs error

	// these values should always be set
	// base layer overrides so that the client (api/eve) can't try and set them
	var baseContainer = map[string]interface{}{
		"name":            eveDeployment.GetArtifact().ArtifactName,
		"image":           getDockerImageName(eveDeployment.GetArtifact()),
		"imagePullPolicy": string(apiv1.PullAlways),
		"ports":           getDeploymentContainerPorts(eveDeployment),
		"env":             defaultContainerEnvVars(plan.DeploymentID, eveDeployment.GetArtifact()),
	}

	// First try and find/extract the "containers" key in the definition
	// if 1 (or more) are found, then use that as the Definition spec (i.e. take what was supplied from eve-api definition)
	containers, found, err := unstructured.NestedSlice(definition.Object, definitionSpecKeyMap["containers"]...)
	if err != nil || !found || containers == nil || len(containers) == 0 {
		ctr := baseContainer
		ctr["readinessProbe"] = defaultProbe(eveDeployment.GetReadiness(), nil)
		ctr["livenessProbe"] = defaultProbe(eveDeployment.GetLiveness(), nil)
		ctr["resources"] = defaultPodResources(eveDeployment.GetResources(), nil)
		containers = []interface{}{ctr}
	} else {
		for k, v := range baseContainer {
			if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), v, k); err != nil {
				defContainerErrs = multierror.Append(defContainerErrs, errors.Wrap(err, "failed to override base container key val"))
			}
		}

		if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), defaultProbe(eveDeployment.GetReadiness(), containers[0].(map[string]interface{}))["readinessProbe"], "readinessProbe"); err != nil {
			defContainerErrs = multierror.Append(defContainerErrs, errors.Wrap(err, "failed to override container readinessProbe"))
		}

		if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), defaultProbe(eveDeployment.GetLiveness(), containers[0].(map[string]interface{}))["livenessProbe"], "livenessProbe"); err != nil {
			defContainerErrs = multierror.Append(defContainerErrs, errors.Wrap(err, "failed to override container readinessProbe"))
		}

		if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), defaultPodResources(eveDeployment.GetResources(), containers[0].(map[string]interface{}))["resources"], "resources"); err != nil {
			defContainerErrs = multierror.Append(defContainerErrs, errors.Wrap(err, "failed to override container readinessProbe"))
		}

	}

	if err := unstructured.SetNestedSlice(definition.Object, containers, definitionSpecKeyMap["containers"]...); err != nil {
		defContainerErrs = multierror.Append(defContainerErrs, errors.Wrap(err, "failed to override container values..."))
	}

	return defContainerErrs
}

func (s *Scheduler) overrideMaps(ctx context.Context, definition *unstructured.Unstructured, keyFields []string, baseLabels map[string]interface{}) error {

	s.Logger(ctx).Debug("override maps", zap.Strings("keys", keyFields), zap.Any("base", baseLabels))

	defLabels, found, err := unstructured.NestedMap(definition.Object, keyFields...)
	if err != nil {
		return errors.Wrapf(err, "failed to find the map labels by key: %v", keyFields)
	}
	if found {
		s.Logger(ctx).Debug("labels defined on the definition", zap.Any("defLabels", defLabels))
	}
	if defLabels != nil && len(defLabels) > 0 {
		s.Logger(ctx).Debug("merging defined labels with base (legacy) labels", zap.Any("defLabels", defLabels), zap.Any("baseLabels", baseLabels))
		baseLabels = mergeM(baseLabels, defLabels)
	}

	if err := unstructured.SetNestedMap(definition.Object, baseLabels, keyFields...); err != nil {
		return errors.Wrap(err, "failed to set map on k8s deployment CRD")
	}

	return nil
}
