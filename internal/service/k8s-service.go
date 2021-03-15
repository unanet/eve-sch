package service

import (
	"context"

	"github.com/pkg/errors"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	apiv1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

var serviceMetaData = metav1.TypeMeta{Kind: "Service", APIVersion: "apps/v1"}

func hydrateK8sService(ctx context.Context, plan *eve.NSDeploymentPlan, service *eve.DeployService) *apiv1.Service {
	svc := &apiv1.Service{
		TypeMeta: serviceMetaData,
		ObjectMeta: metav1.ObjectMeta{
			Name:        service.ServiceName,
			Namespace:   plan.Namespace.Name,
			Labels:      serviceLabels(service),
			Annotations: serviceAnnotations(service),
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Port: int32(service.ServicePort),
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(service.ServicePort),
					},
				},
			},
			Selector: map[string]string{
				"app": service.ServiceName,
			},
		},
	}

	if service.StickySessions {
		svc.Spec.SessionAffinity = apiv1.ServiceAffinityClientIP
	}

	return svc
}

func (s *Scheduler) setupK8sService(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	plan *eve.NSDeploymentPlan,
	service *eve.DeployService,
) error {
	if service.ServicePort > 0 {
		k8sSvc := hydrateK8sService(ctx, plan, service)
		existingSvc, err := k8s.CoreV1().Services(plan.Namespace.Name).Get(ctx, service.ServiceName, metav1.GetOptions{})
		if err != nil {
			if k8sErrors.IsNotFound(err) {
				_, err := k8s.CoreV1().Services(plan.Namespace.Name).Create(ctx, k8sSvc, metav1.CreateOptions{})
				if err != nil {
					return errors.Wrap(err, "an error occurred trying to create the k8s service")
				}
				return nil
			} else {
				return errors.Wrap(err, "an error occurred trying to get the k8s service")
			}
		}

		//_, err = k8s.CoreV1().Services(plan.Namespace.Name).Patch(ctx, existingService.Name, types.ApplyPatchType, []byte(""), metav1.PatchOptions{})
		k8sSvc.ObjectMeta.ResourceVersion = existingSvc.ObjectMeta.ResourceVersion
		k8sSvc.Spec.ClusterIP = existingSvc.Spec.ClusterIP

		// update the existing Service
		if _, err = k8s.CoreV1().Services(plan.Namespace.Name).Update(ctx, k8sSvc, metav1.UpdateOptions{}); err != nil {
			return errors.Wrap(err, "an error occurred trying to update the k8s service")
		}

	}
	return nil
}
