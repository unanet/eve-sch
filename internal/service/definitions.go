package service

import (
	"bytes"
	"context"
	"html/template"
	"reflect"

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

// merge incoming definition values with defaults/standard values
// TODO: Migrate these to eve in the definition
func (s *Scheduler) mergeDefStandardMaps(ctx context.Context, defs, standard map[string]interface{}) map[string]interface{} {
	s.Logger(ctx).Debug("merge def standard maps", zap.Any("defs", defs), zap.Any("standard", standard))
	var result = make(map[string]interface{})
	if defs != nil && len(defs) > 0 {
		for k, v := range defs {
			result[k] = v
		}
	}

	// Apply the Standard Labels last
	for k, v := range standard {
		result[k] = v
	}

	return result
}
func withZapFields(deployment eve.DeploymentSpec, crd eve.DefinitionResult, std, def map[string]interface{}) []zap.Field {
	return []zap.Field{
		zap.String("name", deployment.GetName()),
		zap.String("kind", crd.Kind),
		zap.Strings("keys", crd.LabelKeys()),
		zap.Any("standard", std),
		zap.Any("definition", def),
	}
}

func (s *Scheduler) baseAnnotations(ctx context.Context, definition *unstructured.Unstructured, crd eve.DefinitionResult, deployment eve.DeploymentSpec) error {
	definitionAnnotations, found, err := unstructured.NestedMap(definition.Object, crd.AnnotationKeys()...)
	if err != nil {
		return errors.Wrapf(err, "failed to find the map by key: %v", crd.AnnotationKeys())
	}
	if !found || definitionAnnotations == nil {
		definitionAnnotations = make(map[string]interface{})
	}

	stdAnns := crd.StandardAnnotations(deployment)

	// CRD doesnt have any "standard" annotations (aka legacy)
	// but definition already does...nothing to do...let's bail
	if len(stdAnns) == 0 && len(definitionAnnotations) > 0 {
		return nil
	}

	if !reflect.DeepEqual(definitionAnnotations, stdAnns) {
		s.Logger(ctx).Warn("std annotations mismatch definition annotations", withZapFields(deployment, crd, stdAnns, definitionAnnotations)...)
	}

	if err := unstructured.SetNestedMap(definition.Object, s.mergeDefStandardMaps(ctx, definitionAnnotations, stdAnns), crd.AnnotationKeys()...); err != nil {
		return errors.Wrap(err, "failed to set CRD Labels")
	}
	return nil
}

func (s *Scheduler) baseLabels(ctx context.Context, definition *unstructured.Unstructured, crd eve.DefinitionResult, deployment eve.DeploymentSpec) error {
	definitionLabels, found, err := unstructured.NestedMap(definition.Object, crd.LabelKeys()...)
	if err != nil {
		return errors.Wrapf(err, "failed to find the map by key: %v", crd.LabelKeys())
	}
	if !found || definitionLabels == nil {
		definitionLabels = make(map[string]interface{})
	}

	stdLbls := crd.StandardLabels(deployment)

	// CRD doesnt have any "standard" labels (aka legacy)
	// but definition already does...nothing to do...let's bail
	if len(stdLbls) == 0 && len(definitionLabels) > 0 {
		return nil
	}

	// TODO: remove this once the data in eve is correct
	// using this as a way to see "warnings" for legacy settings
	if !reflect.DeepEqual(definitionLabels, stdLbls) {
		s.Logger(ctx).Warn("std labels mismatch definition labels", withZapFields(deployment, crd, stdLbls, definitionLabels)...)
	}

	if err := unstructured.SetNestedMap(definition.Object, s.mergeDefStandardMaps(ctx, definitionLabels, stdLbls), crd.LabelKeys()...); err != nil {
		return errors.Wrap(err, "failed to set CRD Labels")
	}
	return nil
}

func (s *Scheduler) baseDefinition(
	ctx context.Context,
	definition *unstructured.Unstructured,
	crd eve.DefinitionResult,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) error {

	s.Logger(ctx).Debug("incoming k8s base CRD def", zap.Any("crd", crd), zap.Any("definition", definition))

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

	if err := s.baseLabels(ctx, definition, crd, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to override labels")
	}

	if err := s.baseAnnotations(ctx, definition, crd, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to override labels")
	}

	s.Logger(ctx).Debug("k8s base CRD set up", zap.Any("crd", crd), zap.Any("definition", definition))

	return nil
}
