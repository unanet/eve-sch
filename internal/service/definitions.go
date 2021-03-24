package service

import (
	"bytes"
	"context"
	"html/template"

	"github.com/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func parseJobDefinition(definition []byte, job *eve.DeployJob, plan *eve.NSDeploymentPlan) ([]byte, error) {
	temp := template.New("definition")
	temp.Funcs(template.FuncMap{
		"replace": replace,
	})
	temp, err := temp.Parse(string(definition))
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	err = temp.Execute(&b, TemplateJobData{
		Plan: plan,
		Job:  job,
	})
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func parseServiceDefinition(definition []byte, service *eve.DeployService, plan *eve.NSDeploymentPlan) ([]byte, error) {
	temp := template.New("definition")
	temp.Funcs(template.FuncMap{
		"replace": replace,
	})
	temp, err := temp.Parse(string(definition))
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	err = temp.Execute(&b, TemplateServiceData{
		Plan:    plan,
		Service: service,
	})
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func (s *Scheduler) baseDefinition(
	ctx context.Context,
	definition *unstructured.Unstructured,
	crd eve.DefinitionResult,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) error {

	crdLabels, lblKeys := crd.Labels(eveDeployment)
	crdAnnotations, annoKeys := crd.Annotations(eveDeployment)

	if err := unstructured.SetNestedField(definition.Object, crd.APIVersion(), "apiVersion"); err != nil {
		return errors.Wrap(err, "failed to set apiVersion on k8s CRD")
	}

	if err := unstructured.SetNestedField(definition.Object, crd.Kind, "kind"); err != nil {
		return errors.Wrap(err, "failed to set kind on k8s CRD")
	}

	if err := unstructured.SetNestedField(definition.Object, eveDeployment.GetName(), "metadata", "name"); err != nil {
		return errors.Wrap(err, "failed to set metadata.name on k8s CRD")
	}

	if err := unstructured.SetNestedField(definition.Object, plan.Namespace.Name, "metadata", "namespace"); err != nil {
		return errors.Wrap(err, "failed to set metadata.namespace on k8s CRD")
	}

	if err := overrideMaps(definition, lblKeys, crdLabels); err != nil {
		return errors.Wrap(err, "failed to override labels")
	}

	if err := overrideMaps(definition, annoKeys, crdAnnotations); err != nil {
		return errors.Wrap(err, "failed to override annotations")
	}

	s.Logger(ctx).Debug("k8s base CRD set up", zap.Any("crd", crd), zap.Any("definition", definition))

	return nil
}
