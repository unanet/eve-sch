package service

import (
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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

func overrideMaps(definition *unstructured.Unstructured, keyFields []string, baseLabels map[string]interface{}) error {

	defLabels, found, err := unstructured.NestedMap(definition.Object, keyFields...)
	if err == nil && found && defLabels != nil {
		baseLabels = mergeM(baseLabels, defLabels)
	}

	if err := unstructured.SetNestedMap(definition.Object, baseLabels, keyFields...); err != nil {
		return errors.Wrap(err, "failed to set map on k8s deployment CRD")
	}

	return nil
}
