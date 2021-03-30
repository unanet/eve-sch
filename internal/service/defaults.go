package service

import (
	"encoding/json"

	apiv1 "k8s.io/api/core/v1"

	"github.com/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	//apiv1 "k8s.io/api/core/v1"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var definitionSpecKeyMap = map[string][]string{
	"metadataAnnotations":           {"metadata", "annotations"},
	"metadataLabels":                {"metadata", "labels"},
	"replicas":                      {"spec", "replicas"},
	"sessionAffinity":               {"spec", "sessionAffinity"},
	"selectorApp":                   {"spec", "selector", "app"},
	"matchLabelsApp":                {"spec", "selector", "matchLabels", "app"},
	"templateMetaLabels":            {"spec", "template", "metadata", "labels"},
	"labelsNuance":                  {"spec", "template", "metadata", "labels", "nuance"},
	"templateMetaAnnotations":       {"spec", "template", "metadata", "annotations"},
	"nodeSelector":                  {"spec", "template", "spec", "nodeSelector"},
	"terminationGracePeriodSeconds": {"spec", "template", "spec", "terminationGracePeriodSeconds"},
	"serviceAccountName":            {"spec", "template", "spec", "serviceAccountName"},
	"securityContext":               {"spec", "template", "spec", "securityContext"},
	"fsGroup":                       {"spec", "template", "spec", "securityContext", "fsGroup"},
	"runAsUser":                     {"spec", "template", "spec", "securityContext", "runAsUser"},
	"runAsGroup":                    {"spec", "template", "spec", "securityContext", "runAsGroup"},
	"windowsOptions":                {"spec", "template", "spec", "securityContext", "windowsOptions"},
	"imagePullSecrets":              {"spec", "template", "spec", "imagePullSecrets"},
	"containers":                    {"spec", "template", "spec", "containers"},
	"restartPolicy":                 {"spec", "template", "spec", "restartPolicy"},
}

func defaultStickySessions(definition *unstructured.Unstructured, deployment eve.DeploymentSpec) error {
	if deployment.GetStickySessions() {
		_, found, err := unstructured.NestedString(definition.Object, definitionSpecKeyMap["sessionAffinity"]...)
		if err != nil {
			return errors.Wrap(err, "failed to find spec.sessionAffinity on k8s service CRD")
		}

		// sessionAffinity declared on the definition; use it
		if found {
			return nil
		}

		// Nothing declared, but for legacy reason (deployment.GetStickySessions()) we need to set it
		if err := unstructured.SetNestedField(definition.Object, string(apiv1.ServiceAffinityClientIP), definitionSpecKeyMap["sessionAffinity"]...); err != nil {
			return errors.Wrap(err, "failed to set spec.sessionAffinity on k8s service CRD")
		}
	}

	return nil
}

// TODO: remove after migration from eve service to definition
func defaultProbe(serviceValue []byte, definitionValue map[string]interface{}) map[string]interface{} {
	if definitionValue != nil {
		return definitionValue
	}

	if len(serviceValue) <= 5 {
		return nil
	}
	var probe = make(map[string]interface{})
	if err := json.Unmarshal(serviceValue, &probe); err != nil {
		return nil
	}

	return probe
}

func defaultReplicas(definition *unstructured.Unstructured, eveDeployment eve.DeploymentSpec) error {
	_, found, err := unstructured.NestedInt64(definition.Object, definitionSpecKeyMap["replicas"]...)
	if err != nil || !found {
		if err := unstructured.SetNestedField(definition.Object, int64(eveDeployment.GetDefaultCount()), definitionSpecKeyMap["replicas"]...); err != nil {
			return errors.Wrap(err, "failed to set spec.replicas on k8s deployment CRD")
		}
	}
	return nil
}

func defaultNodeSelector(definition *unstructured.Unstructured) error {
	_, found, err := unstructured.NestedMap(definition.Object, definitionSpecKeyMap["nodeSelector"]...)
	if err != nil || !found {
		if err := unstructured.SetNestedMap(definition.Object, map[string]interface{}{"node-group": "shared"}, definitionSpecKeyMap["nodeSelector"]...); err != nil {
			return errors.Wrap(err, "failed to update nodeSelector")
		}
	}
	return nil
}

func defaultTerminationGracePeriod(definition *unstructured.Unstructured) error {
	_, found, err := unstructured.NestedInt64(definition.Object, definitionSpecKeyMap["terminationGracePeriodSeconds"]...)
	if err != nil || !found {
		if err := unstructured.SetNestedField(definition.Object, int64(300), definitionSpecKeyMap["terminationGracePeriodSeconds"]...); err != nil {
			return errors.Wrap(err, "failed to update terminationGracePeriodSeconds")
		}
	}
	return nil
}

func defaultServiceAccountName(definition *unstructured.Unstructured, eveDeployment eve.DeploymentSpec) error {
	_, found, err := unstructured.NestedInt64(definition.Object, definitionSpecKeyMap["serviceAccountName"]...)
	if err != nil || !found {
		if err := unstructured.SetNestedField(definition.Object, eveDeployment.GetDefaultServiceAccount(), definitionSpecKeyMap["serviceAccountName"]...); err != nil {
			return errors.Wrap(err, "failed to update serviceAccountName")
		}
	}
	return nil
}

func defaultSecurityContext(definition *unstructured.Unstructured, eveDeployment eve.DeploymentSpec) error {

	_, found, err := unstructured.NestedMap(definition.Object, definitionSpecKeyMap["windowsOptions"]...)
	if err != nil {
		return errors.Wrap(err, "failed to find windowsOptions")
	}

	// If we are explicitly setting in the definition, use it
	if found {
		return nil
	}

	_, found, err = unstructured.NestedInt64(definition.Object, definitionSpecKeyMap["fsGroup"]...)
	if err != nil || !found {
		if err := unstructured.SetNestedField(definition.Object, int64(65534), definitionSpecKeyMap["fsGroup"]...); err != nil {
			return errors.Wrap(err, "failed to update fsGroup")
		}
	}

	_, found, err = unstructured.NestedInt64(definition.Object, definitionSpecKeyMap["runAsUser"]...)
	if err != nil || !found {
		if err := unstructured.SetNestedField(definition.Object, int64(eveDeployment.GetDefaultRunAs()), definitionSpecKeyMap["runAsUser"]...); err != nil {
			return errors.Wrap(err, "failed to update runAsUser")
		}
	}

	_, found, err = unstructured.NestedInt64(definition.Object, definitionSpecKeyMap["runAsGroup"]...)
	if err != nil || !found {
		if err := unstructured.SetNestedField(definition.Object, int64(eveDeployment.GetDefaultRunAs()), definitionSpecKeyMap["runAsGroup"]...); err != nil {
			return errors.Wrap(err, "failed to update runAsGroup")
		}
	}

	return nil
}

func defaultServicePort(definition *unstructured.Unstructured, eveDeployment eve.DeploymentSpec) error {
	if eveDeployment.GetServicePort() > 0 {
		svcPorts, found, err := unstructured.NestedSlice(definition.Object, "spec", "ports")
		if err != nil || !found || svcPorts == nil || len(svcPorts) == 0 {
			portEntry := map[string]interface{}{"port": int64(eveDeployment.GetServicePort())}
			if err := unstructured.SetNestedSlice(definition.Object, []interface{}{portEntry}, "spec", "ports"); err != nil {
				return errors.Wrap(err, "failed to set spec.selector.matchLabels.app on k8s CRD")
			}
		}
	}
	return nil
}

// TODO: Move this into eve-api
func defaultHPADef() eve.DefinitionResult {
	return eve.DefinitionResult{
		Class:   "autoscaling",
		Version: "v2beta2",
		Kind:    "HorizontalPodAutoscaler",
		Order:   "post",
		Data: map[string]interface{}{
			"spec": map[string]interface{}{},
		},
	}
}
