package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	apimachinerymetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	hpaMetaData = metav1.TypeMeta{
		Kind:       "HorizontalPodAutoscaler",
		APIVersion: "autoscaling/v2beta2",
	}
)

func (s *Scheduler) parseUtilizationLimits(ctx context.Context, input []byte) (*eve.UtilizationLimits, error) {
	if len(input) <= 5 {
		s.Logger(ctx).Debug("not setting utilization limit", zap.ByteString("utilization_limit", input))
		return nil, nil
	}
	var utilLimits eve.UtilizationLimits
	if err := json.Unmarshal(input, &utilLimits); err != nil {
		s.Logger(ctx).Warn("failed to unmarshal the utilization limit bytes", zap.ByteString("utilization_limit", input), zap.Error(err))
		return nil, err
	}
	return &utilLimits, nil
}

func (s *Scheduler) parseReplicaLimits(ctx context.Context, input []byte) (*eve.ReplicaLimits, error) {
	if len(input) <= 5 {
		s.Logger(ctx).Debug("not setting replication limit", zap.ByteString("replication_limit", input))
		return nil, nil
	}
	var replicaLimits eve.ReplicaLimits
	if err := json.Unmarshal(input, &replicaLimits); err != nil {
		s.Logger(ctx).Warn("failed to unmarshal the replication limit bytes", zap.ByteString("replication_limit", input), zap.Error(err))
		return nil, err
	}
	return &replicaLimits, nil
}

func (s *Scheduler) hydrateK8sPodAutoScaling(ctx context.Context, service *eve.DeployService, plan *eve.NSDeploymentPlan) (*autoscaling.HorizontalPodAutoscaler, error) {

	utilLimits, err := s.parseUtilizationLimits(ctx, service.UtilizationLimits)
	if err != nil {
		return nil, err
	}
	if utilLimits == nil {
		s.Logger(ctx).Debug("nil utilization limit")
		return nil, nil
	}

	replicaLimits, err := s.parseReplicaLimits(ctx, service.ReplicaLimits)
	if err != nil {
		return nil, err
	}
	if replicaLimits == nil {
		s.Logger(ctx).Debug("nil replication limit")
		return nil, nil
	}

	return &autoscaling.HorizontalPodAutoscaler{
		TypeMeta: hpaMetaData,
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.ServiceName,
			Namespace: plan.Namespace.Name,
		},
		Spec: autoscaling.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscaling.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       service.ServiceName,
				APIVersion: "apps/v1",
			},
			MinReplicas: int32Ptr(replicaLimits.Min),
			MaxReplicas: int32(replicaLimits.Max),
			Metrics: []autoscaling.MetricSpec{
				{
					Type: autoscaling.ResourceMetricSourceType,
					Resource: &autoscaling.ResourceMetricSource{
						Name: "cpu",
						Target: autoscaling.MetricTarget{
							Type:               autoscaling.UtilizationMetricType,
							AverageUtilization: int32Ptr(utilLimits.CPU),
						},
					},
				},
				{
					Type: autoscaling.ResourceMetricSourceType,
					Resource: &autoscaling.ResourceMetricSource{
						Name: "memory",
						Target: autoscaling.MetricTarget{
							Type:               autoscaling.UtilizationMetricType,
							AverageUtilization: int32Ptr(utilLimits.Memory),
						},
					},
				},
			},
		},
	}, nil
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
	// TODO: Fix this up with a helper/extension method
	if len(service.ResourceRequests) <= 5 || len(service.ResourceLimits) <= 5 || len(service.UtilizationLimits) <= 5 {
		s.Logger(ctx).Debug("not setting autoscaler",
			zap.String("service", service.ServiceName),
			zap.ByteString("resource_requests", service.ResourceRequests),
			zap.ByteString("resource_limits", service.ResourceLimits),
			zap.ByteString("utilization_limits", service.UtilizationLimits),
		)
		return nil
	}

	k8sAutoScaler, err := s.hydrateK8sPodAutoScaling(ctx, service, plan)
	if err != nil {
		return errors.Wrap(err, "an error occurred trying to hydrate the k8s autoscaler object")
	}
	if k8sAutoScaler == nil {
		return fmt.Errorf("failed to hydrate k8s pod autoscaler")
	}

	_, err = k8s.AutoscalingV2beta2().HorizontalPodAutoscalers(plan.Namespace.Name).Get(ctx, service.ServiceName, apimachinerymetav1.GetOptions{
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
