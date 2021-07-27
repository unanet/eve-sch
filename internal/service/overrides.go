package service

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/unanet/eve-sch/internal/config"
	"github.com/unanet/eve/pkg/eve"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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

func (s *Scheduler) overrideImagePullSecrets(ctx context.Context, definition *unstructured.Unstructured) error {
	// extract spec image pull secret
	ips, found, err := unstructured.NestedSlice(definition.Object, "spec", "template", "spec", "imagePullSecrets")
	if err != nil || !found || ips == nil {
		s.Logger(ctx).Warn("missing definition image pull secrets")
		if err := unstructured.SetNestedField(definition.Object, []interface{}{map[string]interface{}{"name": "docker-cfg"}}, "spec", "template", "spec", "imagePullSecrets"); err != nil {
			return errors.Wrap(err, "failed to update imagePullSecrets")
		}
		return nil
	}

	if err := unstructured.SetNestedField(ips[0].(map[string]interface{}), "docker-cfg", "name"); err != nil {
		return errors.Wrap(err, "failed to update image pull secrets with docker-cfg")
	}
	if err := unstructured.SetNestedField(definition.Object, ips, "spec", "template", "spec", "imagePullSecrets"); err != nil {
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
	containers, found, err := unstructured.NestedSlice(definition.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || containers == nil || len(containers) == 0 {
		containers = []interface{}{baseContainer}
	} else {
		for k, v := range baseContainer {
			if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), v, k); err != nil {
				defContainerErrs = multierror.Append(defContainerErrs, errors.Wrap(err, "failed to override base container key val"))
			}
		}
	}

	if err := unstructured.SetNestedSlice(definition.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		defContainerErrs = multierror.Append(defContainerErrs, errors.Wrap(err, "failed to override container values..."))
	}

	return defContainerErrs
}
