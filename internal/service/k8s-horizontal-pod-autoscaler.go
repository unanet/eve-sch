package service

import (
	"context"
	"encoding/json"

	apiv1 "k8s.io/api/core/v1"

	"github.com/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	apimachinerymetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type replicas struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type utilization struct {
	CPU    int `json:"cpu"`
	Memory int `json:"memory"`
}

type AutoScaleSettings struct {
	Enabled     bool        `json:"enabled"`
	Utilization utilization `json:"utilization"`
	Replicas    replicas    `json:"replicas"`
}

type PodResource struct {
	Limit   apiv1.ResourceList `json:"limit"`
	Request apiv1.ResourceList `json:"request"`
}

func (pr *PodResource) IsDefault() bool {
	return pr.Limit.Cpu().IsZero() &&
		pr.Limit.Memory().IsZero() &&
		pr.Request.Memory().IsZero() &&
		pr.Request.Cpu().IsZero()
}

func (as *AutoScaleSettings) IsDefault() bool {
	return as.Enabled == false &&
		as.Utilization.CPU == 0 &&
		as.Utilization.Memory == 0 &&
		as.Replicas.Min == 0 &&
		as.Replicas.Max == 0
}

const (
	minCPUMilli = 10          // (10m) 10 millicores
	maxCPUMilli = 10000       // (10000m) 10000 millicores (10 CPU Cores)
	minMemVal   = 1048576     // (1Mi) 1 Megabyte
	maxMemVal   = 10485760000 // (10000Mi) 10,000 Megabyte (10 GB of RAM)
	minMemUtil  = 1           // 1% Average Utilization
	maxMemUtil  = 500         // 500% Average Utilization
	minReplicas = 1           // 1 Pod Replica min // TODO: this might change when we can autoscale to zero?
	maxReplicas = 1000        // 1000 Pod Replica Max
)

var (
	invalidAutoScaler = errors.New("invalid k8s autoscaler settings")
)

// Invalid checks is the supplied JSON config meets the min/max constraints
func (as *AutoScaleSettings) Invalid() bool {
	return as.Utilization.Memory < minMemUtil ||
		as.Utilization.Memory > maxMemUtil ||
		as.Replicas.Min < minReplicas ||
		as.Replicas.Max > maxReplicas
}

// Invalid checks is the supplied JSON config meets the min/max constraints
func (pr *PodResource) Invalid() bool {
	return pr.Limit.Cpu().MilliValue() < minCPUMilli ||
		pr.Limit.Cpu().MilliValue() > maxCPUMilli ||
		pr.Limit.Memory().Value() < minMemVal ||
		pr.Limit.Memory().Value() > maxMemVal ||
		pr.Request.Cpu().MilliValue() < minCPUMilli ||
		pr.Request.Cpu().MilliValue() > maxCPUMilli ||
		pr.Limit.Memory().Value() < minMemVal ||
		pr.Limit.Memory().Value() > maxMemVal
}

func (as *AutoScaleSettings) TargetRef(serviceName string) autoscaling.CrossVersionObjectReference {
	return autoscaling.CrossVersionObjectReference{
		Kind:       "Deployment",
		Name:       serviceName,
		APIVersion: "apps/v1",
	}
}

func (as *AutoScaleSettings) UtilizationMetricSpecs() []autoscaling.MetricSpec {
	var result []autoscaling.MetricSpec

	if as.Utilization.Memory > 0 {
		result = append(result, autoscaling.MetricSpec{
			Type: autoscaling.ResourceMetricSourceType,
			Resource: &autoscaling.ResourceMetricSource{
				Name: "memory",
				Target: autoscaling.MetricTarget{
					Type:               autoscaling.UtilizationMetricType,
					AverageUtilization: int32Ptr(as.Utilization.Memory),
				},
			},
		})
	}

	if as.Utilization.CPU > 0 {
		result = append(result, autoscaling.MetricSpec{
			Type: autoscaling.ResourceMetricSourceType,
			Resource: &autoscaling.ResourceMetricSource{
				Name: "cpu",
				Target: autoscaling.MetricTarget{
					Type:               autoscaling.UtilizationMetricType,
					AverageUtilization: int32Ptr(as.Utilization.CPU),
				},
			},
		})
	}

	return result
}

var (
	hpaMetaData = metav1.TypeMeta{
		Kind:       "HorizontalPodAutoscaler",
		APIVersion: "autoscaling/v2beta2",
	}
)

//if input == nil || len(input) < 2 {
//s.Logger(ctx).Warn("invalid pod resource input", zap.ByteString("pod_resource", input))
//return nil, nil
//}
//if len(input) == 2 {
//if string(input[0]) == "{" && string(input[1]) == "}" {
//s.Logger(ctx).Debug("{} pod resource default", zap.ByteString("pod_resource", input))
//} else {
//s.Logger(ctx).Error("invalid pod resource input", zap.ByteString("pod_resource", input))
//}
//return nil, nil
//}

// { "enabled": true, "utilization": { "cpu": 80, "memory": 100 }, "replicas": { "min": 2, "max": 10 } }
func (s *Scheduler) parseAutoScale(ctx context.Context, input []byte) (*AutoScaleSettings, error) {
	if input == nil || len(input) < 2 {
		s.Logger(ctx).Warn("invalid pod autoscale input", zap.ByteString("pod_autoscale", input))
		return nil, nil
	}
	if len(input) == 2 {
		if string(input[0]) == "{" && string(input[1]) == "}" {
			s.Logger(ctx).Debug("{} pod autoscale default", zap.ByteString("pod_autoscale", input))
		} else {
			s.Logger(ctx).Error("invalid pod autoscale input", zap.ByteString("pod_autoscale", input))
		}
		return nil, nil
	}
	var autoscale AutoScaleSettings
	if err := json.Unmarshal(input, &autoscale); err != nil {
		s.Logger(ctx).Error("failed to unmarshal the autoscale settings", zap.ByteString("autoscale", input), zap.Error(err))
		return nil, err
	}
	return &autoscale, nil
}

func (s *Scheduler) setupK8sAutoscaler(ctx context.Context, k8s *kubernetes.Clientset, plan *eve.NSDeploymentPlan, service *eve.DeployService) error {
	var removeAutoScaler bool
	autoscale, err := s.parseAutoScale(ctx, service.Autoscaling)
	if err != nil {
		return err
	}

	// Validate the incoming Autoscale settings
	switch {
	case autoscale == nil: // ``
		s.Logger(ctx).Debug("autoscale config is nil")
		removeAutoScaler = true
	case autoscale.IsDefault() == true: // `{}`
		s.Logger(ctx).Debug("autoscale not set with default")
		removeAutoScaler = true
	case autoscale.Invalid(): // `{ "enabled": true, "limit": { "cpu": "10001m", "memory": "10001Mi" }}`
		return invalidAutoScaler
	case autoscale.Enabled == false: // `{"enabled":false}
		s.Logger(ctx).Debug("autoscale disabled deleting existing autoscaler if exist")
		removeAutoScaler = true
	}

	var k8sAutoScaler *autoscaling.HorizontalPodAutoscaler

	// We can remove an existing autoscaler by setting enabled: false
	// or by leaving default json {}
	// OR by completely removing the autoscaling value field in the DB
	// The "normal/standard" way of doing this should just be `{"enabled": false}`
	if removeAutoScaler == false {
		k8sAutoScaler = &autoscaling.HorizontalPodAutoscaler{
			TypeMeta:   hpaMetaData,
			ObjectMeta: metav1.ObjectMeta{Name: service.ServiceName, Namespace: plan.Namespace.Name},
			Spec: autoscaling.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscale.TargetRef(service.ServiceName),
				MinReplicas:    int32Ptr(autoscale.Replicas.Min),
				MaxReplicas:    int32(autoscale.Replicas.Max),
				Metrics:        autoscale.UtilizationMetricSpecs(),
			},
		}
	}

	_, err = k8s.AutoscalingV2beta2().HorizontalPodAutoscalers(plan.Namespace.Name).Get(ctx, service.ServiceName, apimachinerymetav1.GetOptions{TypeMeta: hpaMetaData})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			if removeAutoScaler == false {
				if _, err := k8s.AutoscalingV2beta2().HorizontalPodAutoscalers(plan.Namespace.Name).Create(ctx, k8sAutoScaler, apimachinerymetav1.CreateOptions{}); err != nil {
					// an error occurred trying to see if the app is already deployed
					return errors.Wrap(err, "an error occurred trying to create the autoscaler")
				}
			} else {
				// nothing to remove (the autoscaler does not exist yet so we can't remove it)
				return nil
			}
		} else {
			// an error occurred trying to see if the app is already deployed
			return errors.Wrap(err, "an error occurred trying to get the autoscaler")
		}
	}

	if removeAutoScaler {
		if err = k8s.AutoscalingV2beta2().HorizontalPodAutoscalers(plan.Namespace.Name).Delete(ctx, service.ServiceName, apimachinerymetav1.DeleteOptions{
			TypeMeta: hpaMetaData,
		}); err != nil {
			// an error occurred trying to delete the existing autoscaler
			return errors.Wrap(err, "an error occurred trying to delete the existing autoscaler")
		}
		// Deletion successful lets bail out
		return nil
	}

	if _, err = k8s.AutoscalingV2beta2().HorizontalPodAutoscalers(plan.Namespace.Name).Update(ctx, k8sAutoScaler, apimachinerymetav1.UpdateOptions{
		TypeMeta: hpaMetaData,
	}); err != nil {
		// an error occurred trying to see if the app is already deployed
		return errors.Wrap(err, "an error occurred trying to update the autoscaler")
	}
	// Update successful lets return to caller
	return nil
}
