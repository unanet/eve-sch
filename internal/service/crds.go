package service

import (
	"context"
	"encoding/json"
	goerrors "errors"
	"strings"
	"time"

	"github.com/unanet/eve/pkg/eve"
	"github.com/unanet/go/pkg/errors"
	"go.uber.org/zap"
	apiv1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
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
			if deployment.GetServicePort() > 0 {
				svcPorts, found, err := unstructured.NestedSlice(definition.Object, "spec", "ports")
				if err != nil || !found || svcPorts == nil || len(svcPorts) == 0 {
					portEntry := map[string]interface{}{"port": int64(deployment.GetServicePort())}
					if err := unstructured.SetNestedSlice(definition.Object, []interface{}{portEntry}, "spec", "ports"); err != nil {
						return errors.Wrap(err, "failed to set spec.selector.matchLabels.app on k8s CRD")
					}
					s.Logger(ctx).Warn("set default service port", zap.String("name", deployment.GetName()))
				}
			}

			if err := unstructured.SetNestedField(definition.Object, deployment.GetName(), "spec", "selector", "app"); err != nil {
				return errors.Wrap(err, "failed to set selectorApp on k8s CRD")
			}

			if err := s.saveServiceCRD(ctx, plan, deployment, definition, crd); err != nil {
				return errors.Wrap(err, "an error occurred trying to save the k8s main service crd")
			}
			count++
			continue
		}

		if strings.ToLower(crd.Kind) == "deployment" {
			err := s.setDeploymentDefinitions(ctx, definition, plan, deployment)
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
	ctx context.Context,
	definition *unstructured.Unstructured,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) error {

	if err := unstructured.SetNestedField(definition.Object, eveDeployment.GetName(), "spec", "selector", "matchLabels", "app"); err != nil {
		return errors.Wrap(err, "failed to set selectorApp on k8s CRD")
	}

	if err := s.overrideImagePullSecrets(ctx, definition); err != nil {
		return errors.Wrap(err, "failed to override image pull secrets")
	}

	if err := overrideContainers(definition, eveDeployment, plan); err != nil {
		return errors.Wrap(err, "failed to override containers")
	}

	return nil
}

func (s *Scheduler) setJobDefinitions(
	ctx context.Context,
	definition *unstructured.Unstructured,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) error {

	if err := unstructured.SetNestedField(definition.Object, string(apiv1.RestartPolicyNever), "spec", "template", "spec", "restartPolicy"); err != nil {
		return errors.Wrap(err, "failed to override restartPolicy")
	}

	if err := s.overrideImagePullSecrets(ctx, definition); err != nil {
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
			if err := s.setJobDefinitions(ctx, definition, plan, deployment); err != nil {
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

	existingCRD, err := s.k8sDynamicClient.Resource(groupSchemaResourceVersion(crd)).Namespace(plan.Namespace.Name).Get(ctx, eveDeployment.GetName(), metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			newCRD, createErr := s.createCRD(ctx, groupSchemaResourceVersion(crd), plan.Namespace.Name, definition)
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
	eveCRD eve.DefinitionResult) error {

	crd, newlyCreated, err := s.resolveExistingCRD(ctx, definition, eveCRD, plan, eveDeployment)
	if err != nil {
		return errors.Wrap(err, "failed to resolve the existing CRD")
	}
	if newlyCreated {
		// The CRD didn't exists, so it was created, time to return
		return nil
	}

	if plan.Type == eve.DeploymentPlanTypeRestart {
		if err := unstructured.SetNestedField(crd.Object, eveDeployment.GetNuance(), "spec", "template", "metadata", "labels", "nuance"); err != nil {
			return errors.Wrap(err, "failed to set nuance for k8s restart CRD")
		}
	}

	return s.updateCRD(ctx, eveCRD, plan, definition)
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

		return s.updateCRD(ctx, crdDef, plan, definition)

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

	s.Logger(ctx).Info("saving job crd", zap.String("name", eveDeployment.GetName()))

	existingJobCRD, newlyCreated, err := s.resolveExistingCRD(ctx, definition, crdDef, plan, eveDeployment)
	if err != nil {
		return errors.Wrap(err, "failed to resolve the existing CRD")
	}
	if newlyCreated {
		s.Logger(ctx).Info("newly created job", zap.String("name", eveDeployment.GetName()))
		// The CRD didn't exists, so it was created, time to return
		return nil
	}

	s.Logger(ctx).Info("job already exists; delete it...", zap.String("name", eveDeployment.GetName()))

	if err := s.deleteCRD(ctx, plan.Namespace.Name, existingJobCRD, crdDef); err != nil {
		return errors.Wrap(err, "failed to delete k8s CRD")
	}

	// TODO: Yuck! Remove this sleep and watch the deleted job pod(s)
	// we need to wait until the job is removed before we can create it again
	// however, in k8s 1.21 there is the Job TTL (which I am kind of holding off for...:)
	s.Logger(ctx).Info("job deleted...sleeping...", zap.String("name", eveDeployment.GetName()))

	time.Sleep(15 * time.Second)

	s.Logger(ctx).Info("creating new job", zap.String("name", eveDeployment.GetName()))

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if _, err := s.k8sDynamicClient.Resource(groupSchemaResourceVersion(crdDef)).Namespace(plan.Namespace.Name).Create(ctx, definition, metav1.CreateOptions{}); err != nil {
			if k8sErrors.IsAlreadyExists(err) || k8sErrors.IsConflict(err) {
				s.Logger(ctx).Warn("failed job creation", zap.String("name", eveDeployment.GetName()), zap.Error(err))
				return nil
			}
			return errors.Wrap(err, "failed to create the k8s job")
		}
		return nil
	})
	if retryErr != nil {
		return errors.Wrap(retryErr, "failed to create k8s CRD after retry")
	}

	s.Logger(ctx).Info("job successfully deleted and created", zap.String("name", eveDeployment.GetName()))
	return nil
}

func (s *Scheduler) saveGenericCRD(
	ctx context.Context,
	definition *unstructured.Unstructured,
	eveCRD eve.DefinitionResult,
	plan *eve.NSDeploymentPlan,
	eveDeployment eve.DeploymentSpec,
) error {

	_, newlyCreated, err := s.resolveExistingCRD(ctx, definition, eveCRD, plan, eveDeployment)
	if err != nil {
		return errors.Wrap(err, "failed to resolve the existing CRD")
	}
	if newlyCreated {
		// The CRD didn't exists, so it was created, time to return
		return nil
	}

	// CRD Already exists, so let's update it with the new definition
	return s.updateCRD(ctx, eveCRD, plan, definition)

}

/*
	CRUD Ops for CRDs
*/
func (s *Scheduler) updateCRD(ctx context.Context, crdDef eve.DefinitionResult, plan *eve.NSDeploymentPlan, definition *unstructured.Unstructured) error {

	gsrv := groupSchemaResourceVersion(crdDef)

	gsrv.Version = strings.TrimPrefix(gsrv.Version, "/")

	s.Logger(ctx).Debug("updating CRD",
		zap.Any("Version", gsrv.Version),
		zap.Any("Resource", gsrv.Resource),
		zap.Any("Group", gsrv.Group),
		zap.Any("GroupResource", gsrv.GroupResource()),
		zap.Any("GroupVersion", gsrv.GroupVersion()),
	)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, updateErr := s.k8sDynamicClient.Resource(gsrv).Namespace(plan.Namespace.Name).Update(ctx, definition, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return errors.Wrap(retryErr, "failed to update k8s CRD after retry")
	}
	return nil
}

func (s *Scheduler) createCRD(ctx context.Context, groupSchemaVersion schema.GroupVersionResource, nameSpace string, definition *unstructured.Unstructured) (*unstructured.Unstructured, error) {

	s.Logger(ctx).Debug("creating new k8s crds with definition",
		zap.String("namespace", nameSpace),
		zap.Any("definition", definition),
		zap.Any("groupSchemaVersion", groupSchemaVersion),
	)

	var newCRD *unstructured.Unstructured
	var err, retryErr error
	retryErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if newCRD, err = s.k8sDynamicClient.Resource(groupSchemaVersion).Namespace(nameSpace).Create(ctx, definition, metav1.CreateOptions{}); err != nil {
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
	s.Logger(ctx).Info("deleting k8s crds with definition",
		zap.String("namespace", nameSpace),
		zap.Any("existing_crd", existingCRD),
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
