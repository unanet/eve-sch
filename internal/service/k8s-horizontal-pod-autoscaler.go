package service

import (
	"context"

	"github.com/pkg/errors"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	apimachinerymetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TODO: Should these be configurable in the DB???
const (
	minPodReplicas = 2
	maxPodReplicas = 15
)

var (
	hpaMetaData = metav1.TypeMeta{
		Kind:       "HorizontalPodAutoscaler",
		APIVersion: "autoscaling/v2beta2",
	}
)

func hydrateK8sPodAutoScaling(serviceName, namespace string) *autoscaling.HorizontalPodAutoscaler {
	return &autoscaling.HorizontalPodAutoscaler{
		TypeMeta: hpaMetaData,
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: autoscaling.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscaling.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       serviceName,
				APIVersion: "apps/v1",
			},
			MinReplicas: int32Ptr(minPodReplicas),
			MaxReplicas: maxPodReplicas,
			Metrics: []autoscaling.MetricSpec{
				{
					Type: autoscaling.ResourceMetricSourceType,
					Resource: &autoscaling.ResourceMetricSource{
						Name: "cpu",
						Target: autoscaling.MetricTarget{
							Type:               autoscaling.UtilizationMetricType,
							AverageUtilization: int32Ptr(75),
						},
					},
				},
				{
					Type: autoscaling.ResourceMetricSourceType,
					Resource: &autoscaling.ResourceMetricSource{
						Name: "memory",
						Target: autoscaling.MetricTarget{
							Type:               autoscaling.UtilizationMetricType,
							AverageUtilization: int32Ptr(75),
						},
					},
				},
			},
		},
	}
}

func (s *Scheduler) setupK8sAutoscaler(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	plan *eve.NSDeploymentPlan,
	service *eve.DeployService,
) error {
	// We only setup AutoScaling when the Resource Requests were supplied
	// the autoscaler uses the requested resources to determine when to scale
	// so if they aren't being used, we won't setup the autoscaler
	if len(service.ResourceRequests) == 0 {
		return nil
	}

	k8sAutoScaler := hydrateK8sPodAutoScaling(service.ServiceName, plan.Namespace.Name)

	_, err := k8s.AutoscalingV2beta2().HorizontalPodAutoscalers(plan.Namespace.Name).Get(ctx, service.ServiceName, apimachinerymetav1.GetOptions{
		TypeMeta: hpaMetaData,
	})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			if _, err := k8s.AutoscalingV2beta2().HorizontalPodAutoscalers(plan.Namespace.Name).Create(ctx, k8sAutoScaler, apimachinerymetav1.CreateOptions{}); err != nil {
				// an error occurred trying to see if the app is already deployed
				return errors.Wrap(err, "an error occurred trying to create the autoscaler")
			}
		} else {
			// an error occurred trying to see if the app is already deployed
			return errors.Wrap(err, "an error occurred trying to get the autoscaler")
		}
	}

	if _, err = k8s.AutoscalingV2beta2().HorizontalPodAutoscalers(plan.Namespace.Name).Update(ctx, k8sAutoScaler, apimachinerymetav1.UpdateOptions{
		TypeMeta: hpaMetaData,
	}); err != nil {
		// an error occurred trying to see if the app is already deployed
		return errors.Wrap(err, "an error occurred trying to update the autoscaler")
	}
	return nil
}
