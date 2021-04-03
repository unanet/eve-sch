package service

import (
	"context"
	"encoding/json"
	goerrors "errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/util/retry"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
	apiv1 "k8s.io/api/core/v1"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/go/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func groupSchemaResourceVersion(crdDef eve.DefinitionResult) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: crdDef.Class, Version: crdDef.Version, Resource: crdDef.Resource()}
}

func (s *Scheduler) deployServiceCRD(ctx context.Context, deployment eve.DeploymentSpec, plan *eve.NSDeploymentPlan, definitions eve.DefinitionResults) error {
	mainCRDs := definitions.CRDs("main")
	// This means the definitions aren't defined in Eve (api/DB)
	if len(mainCRDs) < 2 {
		return goerrors.New("missing required Resource Definitions")
	}
	count := 0
	for _, crd := range mainCRDs {
		definition := &unstructured.Unstructured{Object: crd.Data}
		err := s.baseDefinition(ctx, definition, crd, plan, deployment)
		if err != nil {
			return errors.Wrap(err, "an error occurred trying to setup the k8s main base crd")
		}

		if strings.ToLower(crd.Kind) == "service" {
			if err := s.setServiceDefinitions(definition, deployment); err != nil {
				return errors.Wrap(err, "an error occurred trying to setup the k8s main service crd")
			}
			if err := s.saveServiceCRD(ctx, plan, deployment, definition, crd); err != nil {
				return errors.Wrap(err, "an error occurred trying to save the k8s main service crd")
			}
			count++
			continue
		}

		if strings.ToLower(crd.Kind) == "deployment" {
			err := s.setDeploymentDefinitions(definition, plan, deployment)
			if err != nil {
				return errors.Wrap(err, "an error occurred trying to setup the k8s main deployment crd")
			}
			if err := s.saveDeploymentCRD(ctx, plan, deployment, definition, crd); err != nil {
				return errors.Wrap(err, "an error occurred trying to save the k8s main deployment crd")
			}
			count++
			continue
		}

		if err := s.saveGenericCRD(ctx, definition, crd, plan, deployment); err != nil {
			return errors.Wrap(err, "failed to apply the main k8s CRD")
		}
		count++
	}

	// TODO: clean this up (no hard code)
	if count < 2 {
		s.Logger(ctx).Error("invalid deploy service count CRDs")
	}

	return nil
}

func (s *Scheduler) setDeploymentDefinitions(
	definition *unstructured.Unstructured,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) error {

	if err := unstructured.SetNestedField(definition.Object, eveDeployment.GetName(), definitionSpecKeyMap["matchLabelsApp"]...); err != nil {
		return errors.Wrap(err, "failed to set selectorApp on k8s CRD")
	}

	if err := defaultReplicas(definition, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to override replicas")
	}

	if err := defaultTerminationGracePeriod(definition); err != nil {
		return errors.Wrap(err, "failed to override terminationGracePeriod")
	}

	if config.GetConfig().EnableNodeGroup {
		if err := defaultNodeSelector(definition); err != nil {
			return errors.Wrap(err, "failed to override node selector")
		}
	}

	if err := defaultSecurityContext(definition, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to override security context")
	}

	if err := defaultServiceAccountName(definition, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to override service account name")
	}

	if err := overrideImagePullSecrets(definition); err != nil {
		return errors.Wrap(err, "failed to override image pull secrets")
	}

	if err := overrideContainers(definition, eveDeployment, plan); err != nil {
		return errors.Wrap(err, "failed to override containers")
	}

	return nil
}

func (s *Scheduler) setServiceDefinitions(definition *unstructured.Unstructured, eveDeployment eve.DeploymentSpec) error {

	if err := defaultServicePort(definition, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to set default service port")
	}

	if err := unstructured.SetNestedField(definition.Object, eveDeployment.GetName(), definitionSpecKeyMap["selectorApp"]...); err != nil {
		return errors.Wrap(err, "failed to set selectorApp on k8s CRD")
	}

	if err := defaultStickySessions(definition, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to set the default sticky sessions")
	}

	return nil
}

func (s *Scheduler) setJobDefinitions(
	definition *unstructured.Unstructured,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) error {

	if err := unstructured.SetNestedField(definition.Object, string(apiv1.RestartPolicyNever), definitionSpecKeyMap["restartPolicy"]...); err != nil {
		return errors.Wrap(err, "failed to override restartPolicy")
	}

	if config.GetConfig().EnableNodeGroup {
		if err := defaultNodeSelector(definition); err != nil {
			return errors.Wrap(err, "failed to override node selector")
		}
	}

	if err := defaultSecurityContext(definition, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to override security context")
	}

	if err := defaultServiceAccountName(definition, eveDeployment); err != nil {
		return errors.Wrap(err, "failed to override service account name")
	}

	if err := overrideImagePullSecrets(definition); err != nil {
		return errors.Wrap(err, "failed to override image pull secrets")
	}

	if err := overrideContainers(definition, eveDeployment, plan); err != nil {
		return errors.Wrap(err, "failed to override containers")
	}

	return nil
}

func (s *Scheduler) deployJobCRD(ctx context.Context, deployment eve.DeploymentSpec, plan *eve.NSDeploymentPlan, definitions eve.DefinitionResults) error {
	mainCRDs := definitions.CRDs("main")
	// This means the definitions aren't defined in Eve (api/DB)
	if len(mainCRDs) < 1 {
		return goerrors.New("missing required Resource Definitions")
	}

	// Apply the Main/Required Job Resource Definition
	for _, crd := range mainCRDs {
		definition := &unstructured.Unstructured{Object: crd.Data}
		if err := s.baseDefinition(ctx, definition, crd, plan, deployment); err != nil {
			return errors.Wrap(err, "failed to apply base definition main CRD")
		}
		if crd.Kind == "Job" {
			if err := s.setJobDefinitions(definition, plan, deployment); err != nil {
				return errors.Wrap(err, "failed to set job definition main CRD")
			}
		}
		if err := s.saveJobCRD(ctx, plan, deployment, definition, crd); err != nil {
			return errors.Wrap(err, "failed to deploy pre CRD")
		}
	}
	return nil
}

func (s *Scheduler) deployHorizontalPodAutoscalerCRD(ctx context.Context, deployment eve.DeploymentSpec, plan *eve.NSDeploymentPlan, crd eve.DefinitionResult) error {

	definition := &unstructured.Unstructured{Object: crd.Data}
	if err := s.baseDefinition(ctx, definition, crd, plan, deployment); err != nil {
		return errors.Wrap(err, "failed to apply base definition pre HPA CRD")
	}

	// TODO: Need a cleaner way to associate Resources
	// this relates the HPA to a deployment
	// having some kind of Parent/Child relationship between CRDs would help here
	scaleRef := map[string]interface{}{"kind": "Deployment", "name": deployment.GetName(), "apiVersion": "apps/v1"}

	if err := unstructured.SetNestedField(definition.Object, scaleRef, "spec", "scaleTargetRef"); err != nil {
		return errors.Wrap(err, "failed to update scaleTargetRef")
	}

	return s.saveGenericCRD(ctx, definition, crd, plan, deployment)

}

func (s *Scheduler) deployCRDs(ctx context.Context, deployment eve.DeploymentSpec, plan *eve.NSDeploymentPlan) error {

	// Parse the Resource Definitions in the Deployment Payload
	var resourceDefinitions = make(eve.DefinitionResults, 0)
	if err := json.Unmarshal(deployment.GetDefinitions(), &resourceDefinitions); err != nil {
		return errors.Wrap(err, "failed to parse resource definitions")
	}

	// Apply the PRE Resource Definitions (ex: PVCs)
	// optional (if not supplied we do not create sane defaults)
	for _, crd := range resourceDefinitions.CRDs("pre") {
		if strings.ToLower(crd.Kind) == "persistentvolumeclaim" {
			s.Logger(ctx).Debug("deploying autoscale crd definition", zap.Any("crd", crd))
			definition := &unstructured.Unstructured{Object: crd.Data}
			if err := s.baseDefinition(ctx, definition, crd, plan, deployment); err != nil {
				return errors.Wrap(err, "failed to apply base definition pre CRD")
			}
			_, _, err := s.resolveExistingCRD(ctx, definition, crd, plan, deployment)
			if err != nil {
				return errors.Wrap(err, "failed resolve PVC CRD")
			}
			continue
		}

		definition := &unstructured.Unstructured{Object: crd.Data}
		if err := s.baseDefinition(ctx, definition, crd, plan, deployment); err != nil {
			return errors.Wrap(err, "failed to apply base definition pre CRD")
		}
		if err := s.saveGenericCRD(ctx, definition, crd, plan, deployment); err != nil {
			return errors.Wrap(err, "failed to save generic pre CRD")
		}
	}

	// Apply the Main/Required Resource Definitions (ex: Job/Service/Deployment)
	// required: if not supplied we use sane defaults and create
	switch deployment.(type) {
	case *eve.DeployJob:
		if err := s.deployJobCRD(ctx, deployment, plan, resourceDefinitions); err != nil {
			return errors.Wrap(err, "failed to deploy job CRD")
		}
	case *eve.DeployService:
		if err := s.deployServiceCRD(ctx, deployment, plan, resourceDefinitions); err != nil {
			return errors.Wrap(err, "failed to deploy service CRDs")
		}
	}

	// Apply the POST Resource Definitions (ex: HPA)
	// we need to capture the HPA (for now, to make sure we are backwards compatible)
	hpaCount := 0
	for _, crd := range resourceDefinitions.CRDs("post") {
		if strings.ToLower(crd.Kind) == "horizontalpodautoscaler" {
			s.Logger(ctx).Debug("deploying autoscale crd definition", zap.Any("crd", crd))
			if err := s.deployHorizontalPodAutoscalerCRD(ctx, deployment, plan, crd); err != nil {
				return errors.Wrap(err, "failed to deploy HPA CRD")
			}
			hpaCount++
			continue
		}

		definition := &unstructured.Unstructured{Object: crd.Data}
		if err := s.baseDefinition(ctx, definition, crd, plan, deployment); err != nil {
			return errors.Wrap(err, "failed to apply base definition post CRD")
		}
		if err := s.saveGenericCRD(ctx, definition, crd, plan, deployment); err != nil {
			return errors.Wrap(err, "failed to deploy post CRD")
		}
	}

	s.Logger(ctx).Debug("post crd definitions deployed", zap.Any("hpa_count", hpaCount))
	return nil
}

// resolveExistingCRD attempts to query for an existing CRD (to update)
// or it creates the new CRD (basically GET or CREATE and return results)
// we need to know if it was an existing CRD (something to update)
// or if it is a newly created CRD
func (s *Scheduler) resolveExistingCRD(
	ctx context.Context,
	definition *unstructured.Unstructured,
	crd eve.DefinitionResult,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) (*unstructured.Unstructured, bool, error) {

	groupSchemaVersion := groupSchemaResourceVersion(crd)

	existingCRD, err := s.k8sDynamicClient.Resource(groupSchemaVersion).Namespace(plan.Namespace.Name).Get(ctx, eveDeployment.GetName(), metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			newCRD, createErr := s.createCRD(ctx, groupSchemaVersion, plan.Namespace.Name, definition)
			if createErr != nil {
				return nil, false, errors.Wrap(createErr, "an error occurred trying to create the new k8s Service CRD")
			}
			return newCRD, true, nil
		}
		return nil, false, errors.Wrap(err, "an error occurred trying to read the k8s Service CRD")
	}
	return existingCRD, false, nil
}

func (s *Scheduler) saveDeploymentCRD(
	ctx context.Context,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
	definition *unstructured.Unstructured,
	crdDef eve.DefinitionResult) error {

	crd, newlyCreated, err := s.resolveExistingCRD(ctx, definition, crdDef, plan, eveDeployment)
	if err != nil {
		return errors.Wrap(err, "failed to resolve the existing CRD")
	}
	if newlyCreated {
		// The CRD didn't exists, so it was created, time to return
		return nil
	}

	if plan.Type == eve.DeploymentPlanTypeRestart {
		if err := unstructured.SetNestedField(crd.Object, eveDeployment.GetNuance(), definitionSpecKeyMap["labelsNuance"]...); err != nil {
			return errors.Wrap(err, "failed to set nuance for k8s restart CRD")
		}
	}

	return s.updateCRD(ctx, groupSchemaResourceVersion(crdDef), plan, definition)
}

func (s *Scheduler) saveServiceCRD(
	ctx context.Context,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
	definition *unstructured.Unstructured,
	crdDef eve.DefinitionResult) error {

	if eveDeployment.GetServicePort() > 0 {

		crd, newlyCreated, err := s.resolveExistingCRD(ctx, definition, crdDef, plan, eveDeployment)
		if err != nil {
			return errors.Wrap(err, "failed to resolve the existing CRD")
		}
		if newlyCreated {
			// The CRD didn't exists, so it was created, time to return
			return nil
		}

		definition.SetResourceVersion(crd.GetResourceVersion())

		clusterIP, found, err := unstructured.NestedString(crd.Object, "spec", "clusterIP")
		if err != nil || !found {
			return errors.Wrap(err, "failed to find the existing service CRD clusterIP")
		}

		if err := unstructured.SetNestedField(definition.Object, clusterIP, "spec", "clusterIP"); err != nil {
			return errors.Wrap(err, "failed to set clusterIP on k8s service CRD")
		}

		return s.updateCRD(ctx, groupSchemaResourceVersion(crdDef), plan, definition)

	}
	s.Logger(ctx).Warn("service CRD saved without Service Port")
	return nil
}

func (s *Scheduler) saveJobCRD(
	ctx context.Context,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
	definition *unstructured.Unstructured,
	crdDef eve.DefinitionResult) error {

	existingJobCRD, newlyCreated, err := s.resolveExistingCRD(ctx, definition, crdDef, plan, eveDeployment)
	if err != nil {
		return errors.Wrap(err, "failed to resolve the existing CRD")
	}
	if newlyCreated {
		// The CRD didn't exists, so it was created, time to return
		return nil
	}

	if err := s.deleteCRD(ctx, plan.Namespace.Name, existingJobCRD, crdDef); err != nil {
		return errors.Wrap(err, "failed to delete k8s CRD")
	}

	s.Logger(ctx).Info("info job deleted", zap.String("name", eveDeployment.GetName()))

	time.Sleep(5 * time.Second)

	s.Logger(ctx).Info("info creating new", zap.String("name", eveDeployment.GetName()))

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if _, err := s.k8sDynamicClient.Resource(groupSchemaResourceVersion(crdDef)).Namespace(plan.Namespace.Name).Create(ctx, definition, metav1.CreateOptions{}); err != nil {
			if k8sErrors.IsAlreadyExists(err) || k8sErrors.IsConflict(err) {
				return nil
			}
			return errors.Wrap(err, "failed to create the k8s job")
		}
		return nil
	})
	if retryErr != nil {
		return errors.Wrap(retryErr, "failed to create k8s CRD after retry")
	}
	return nil
}

func (s *Scheduler) saveGenericCRD(
	ctx context.Context,
	definition *unstructured.Unstructured,
	crd eve.DefinitionResult,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) error {

	_, newlyCreated, err := s.resolveExistingCRD(ctx, definition, crd, plan, eveDeployment)
	if err != nil {
		return errors.Wrap(err, "failed to resolve the existing CRD")
	}
	if newlyCreated {
		// The CRD didn't exists, so it was created, time to return
		return nil
	}

	// CRD Already exists, so let's update it with the new definition
	return s.updateCRD(ctx, groupSchemaResourceVersion(crd), plan, definition)

}

/*
	CRUD Ops for CRDs
*/
func (s *Scheduler) updateCRD(ctx context.Context, crdResult schema.GroupVersionResource, plan *eve.NSDeploymentPlan, definition *unstructured.Unstructured) error {

	crdResult.Version = strings.TrimPrefix(crdResult.Version, "/")

	s.Logger(ctx).Debug("updating CRD",
		zap.Any("crd.Version", crdResult.Version),
		zap.Any("crd.Resource", crdResult.Resource),
		zap.Any("crd.Group", crdResult.Group),
		zap.Any("crd.GroupResource", crdResult.GroupResource()),
		zap.Any("crd.GroupVersion", crdResult.GroupVersion()),
	)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, updateErr := s.k8sDynamicClient.Resource(crdResult).Namespace(plan.Namespace.Name).Update(ctx, definition, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return errors.Wrap(retryErr, "failed to update k8s CRD after retry")
	}
	return nil
}

func (s *Scheduler) createCRD(ctx context.Context, crdResult schema.GroupVersionResource, nameSpace string, definition *unstructured.Unstructured) (*unstructured.Unstructured, error) {

	s.Logger(ctx).Debug("creating new k8s crds with definition",
		zap.String("namespace", nameSpace),
		zap.Any("definition", definition),
		zap.Any("crd", crdResult),
	)

	var newCRD *unstructured.Unstructured
	var err, retryErr error
	retryErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if newCRD, err = s.k8sDynamicClient.Resource(crdResult).Namespace(nameSpace).Create(ctx, definition, metav1.CreateOptions{}); err != nil {
			s.Logger(ctx).Error("failed creating new k8s crds", zap.Error(err))
			return err
		}
		return nil
	})
	if retryErr != nil {
		return nil, errors.Wrap(retryErr, "failed to create k8s CRD")
	}
	return newCRD, nil
}

func (s *Scheduler) deleteCRD(ctx context.Context, nameSpace string, existingCRD *unstructured.Unstructured, crdDef eve.DefinitionResult) error {
	s.Logger(ctx).Debug("deleting  k8s crds with definition",
		zap.String("namespace", nameSpace),
		zap.Any("definition", existingCRD),
		zap.Any("crd", crdDef),
	)
	dp := metav1.DeletePropagationForeground

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deleteErr := s.k8sDynamicClient.Resource(groupSchemaResourceVersion(crdDef)).Namespace(nameSpace).Delete(ctx, existingCRD.GetName(), metav1.DeleteOptions{
			TypeMeta:           metav1.TypeMeta{Kind: crdDef.Kind, APIVersion: crdDef.APIVersion()},
			GracePeriodSeconds: int64Ptr(0),
			PropagationPolicy:  &dp,
		})
		if deleteErr != nil {
			if k8sErrors.IsNotFound(deleteErr) {
				return nil
			}
			return errors.Wrap(deleteErr, "failed to delete k8s CRD")
		}
		return nil
	})
	if retryErr != nil {
		return errors.Wrap(retryErr, "failed to delete k8s CRD after retry")
	}
	return nil
}
