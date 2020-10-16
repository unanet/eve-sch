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

var (
	serviceMetaData = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "apps/v1",
	}
)

func hydrateK8sService(serviceName, namespace string, servicePort int, stickySessions bool) *apiv1.Service {
	service := &apiv1.Service{
		TypeMeta: serviceMetaData,
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Port: int32(servicePort),
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(servicePort),
					},
				},
			},
			Selector: map[string]string{
				"app": serviceName,
			},
		},
	}

	if stickySessions {
		service.Spec.SessionAffinity = apiv1.ServiceAffinityClientIP
	}

	return service
}

func (s *Scheduler) setupK8sService(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	plan *eve.NSDeploymentPlan,
	service *eve.DeployService,
) error {
	if service.ServicePort > 0 {
		k8sSvc := hydrateK8sService(service.ServiceName, plan.Namespace.Name, service.ServicePort, service.StickySessions)
		_, err := k8s.CoreV1().Services(plan.Namespace.Name).Get(ctx, service.ServiceName, metav1.GetOptions{})
		if err != nil {
			if k8sErrors.IsNotFound(err) {
				_, err := k8s.CoreV1().Services(plan.Namespace.Name).Create(ctx, k8sSvc, metav1.CreateOptions{})
				if err != nil {
					return errors.Wrap(err, "an error occurred trying to create the service")
				}
			} else {
				return errors.Wrap(err, "an error occurred trying to check for the service")
			}
		} else {
			_, err := k8s.CoreV1().Services(plan.Namespace.Name).Update(ctx, k8sSvc, metav1.UpdateOptions{
				TypeMeta: serviceMetaData,
			})
			if err != nil {
				return errors.Wrap(err, "an error occurred trying to update the service")
			}
		}
	}
	return nil
}
