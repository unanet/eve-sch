package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
)

// Public CONST
const (
	DockerRepoFormat = "unanet-%s.jfrog.io"
	TimeoutExitCode  = -999999
)

// Private CONST
const (
	defaultNodeGroupLabel = "shared"
)

var (
	deploymentMetaData = metav1.TypeMeta{
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}

	imagePullSecrets = []apiv1.LocalObjectReference{{Name: "docker-cfg"}}
)

func int32Ptr(i int) *int32 {
	i32 := int32(i)
	return &i32
}

func int64Ptr(i int64) *int64 { return &i }

func containerEnvVars(deploymentID uuid.UUID, artifact *eve.DeployArtifact) []apiv1.EnvVar {
	c := config.GetConfig()
	artifact.Metadata["EVE_CALLBACK_URL"] = fmt.Sprintf("http://eve-sch-v1.%s:%d/callback?id=%s", c.Namespace, c.Port, deploymentID.String())
	artifact.Metadata["EVE_IMAGE_NAME"] = getDockerImageName(artifact)

	var containerEnvVars []apiv1.EnvVar
	for k, v := range artifact.Metadata {
		value, ok := v.(string)
		if !ok {
			continue
		}
		containerEnvVars = append(containerEnvVars, apiv1.EnvVar{
			Name:  k,
			Value: value,
		})
	}

	return containerEnvVars
}

func serviceAnnotations(service *eve.DeployService) map[string]string {
	result := make(map[string]string)

	for k, v := range service.Annotations {
		if vv, ok := v.(string); ok {
			result[k] = vv
		}
	}

	return result
}

func deploymentAnnotations(service *eve.DeployService) map[string]string {
	result := make(map[string]string)

	if service.MetricsPort != 0 {
		result["prometheus.io/scrape"] = "true"
		result["prometheus.io/port"] = strconv.Itoa(service.MetricsPort)
	}

	return result
}

// { "limit": { "cpu": "1000m", "memory": "3000Mi" }, "request": { "cpu": "250m", "memory": "2000Mi" } }
func (s *Scheduler) parsePodResource(ctx context.Context, input []byte) (*PodResource, error) {
	if input == nil || len(input) < 2 || (len(input) == 2 && (string(input[0]) != "{" || string(input[1]) != "}")) {
		s.Logger(ctx).Warn("invalid pod resource input", zap.ByteString("pod_resource", input))
		return nil, nil
	}
	var podResource PodResource
	if err := json.Unmarshal(input, &podResource); err != nil {
		s.Logger(ctx).Error("failed to unmarshal the autoscale settings", zap.ByteString("pod_resource", input), zap.Error(err))
		return nil, err
	}

	if podResource.IsDefault() {
		return nil, nil
	}

	// We have this here as a safety guard
	// it protects us from setting values too high or too low
	// when this occurs, we will log an error and return nil, so that the pod resources don't get set
	if podResource.Invalid() {
		s.Logger(ctx).Error("invalid pod resource values", zap.ByteString("pod_resource", input))
		return nil, nil
	}

	return &podResource, nil
}

func (s *Scheduler) parseProbe(ctx context.Context, input []byte) (*apiv1.Probe, error) {
	if len(input) <= 5 {
		return nil, nil
	}
	var probe apiv1.Probe
	if err := json.Unmarshal(input, &probe); err != nil {
		s.Logger(ctx).Warn("failed to unmarshal the probe", zap.Error(err))
		return nil, err
	}
	if probe.Handler.Exec == nil && probe.Handler.HTTPGet == nil && probe.Handler.TCPSocket == nil {
		s.Logger(ctx).Warn("invalid readiness probe, the handler was not set")
		return nil, fmt.Errorf("invalid probe")
	}
	return &probe, nil
}

func (s *Scheduler) setupK8sDeployment(ctx context.Context, k8s *kubernetes.Clientset, plan *eve.NSDeploymentPlan, service *eve.DeployService, timeNuance string) error {

	newDeployment, err := s.hydrateK8sDeployment(ctx, plan, service, timeNuance)
	if err != nil {
		return errors.Wrap(err, "failed to hydrate the k8s deployment object")
	}

	existingDeployment, err := k8s.AppsV1().Deployments(plan.Namespace.Name).Get(ctx, service.ServiceName, metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			// This app hasn't been deployed yet so we need to deploy it
			if _, err = k8s.AppsV1().Deployments(plan.Namespace.Name).Create(ctx, newDeployment, metav1.CreateOptions{}); err != nil {
				return errors.Wrap(err, "an error occurred trying to create the deployment")
			}
			return nil
		}
		// an error occurred trying to see if the app is already deployed
		return errors.Wrap(err, "an error occurred trying to check for the deployment")
	}

	// Restart existing Deployment
	if plan.Type == eve.DeploymentPlanTypeRestart {
		if existingDeployment.Spec.Template.ObjectMeta.Labels == nil {
			existingDeployment.Spec.Template.ObjectMeta.Labels = make(map[string]string)
		}
		existingDeployment.Spec.Template.ObjectMeta.Labels["nuance"] = timeNuance
		if _, err = k8s.AppsV1().Deployments(plan.Namespace.Name).Update(ctx, existingDeployment, metav1.UpdateOptions{TypeMeta: deploymentMetaData}); err != nil {
			return errors.Wrap(err, "an error occurred trying to restart the existing deployment")
		}
	} else {
		// we were able to retrieve the app which mean we need to run update instead of create
		if _, err = k8s.AppsV1().Deployments(plan.Namespace.Name).Update(ctx, newDeployment, metav1.UpdateOptions{TypeMeta: deploymentMetaData}); err != nil {
			return errors.Wrap(err, "an error occurred trying to update the deployment")
		}
	}

	return nil
}

func deploymentLabelSelector(service *eve.DeployService, timeNuance string) string {
	return fmt.Sprintf("app=%s,version=%s,nuance=%s", service.ServiceName, service.AvailableVersion, timeNuance)
}

func deploymentLabels(service *eve.DeployService, timeNuance string) map[string]string {
	baseLabels := map[string]string{
		"app":     service.ServiceName,
		"version": service.AvailableVersion,
		"nuance":  timeNuance,
	}

	// If the service has a metrics port we will set up scrape label here
	if service.MetricsPort > 0 {
		baseLabels["metrics"] = "enabled"
	}

	return baseLabels
}

func serviceLabels(service *eve.DeployService) map[string]string {
	base := make(map[string]string)

	for k, v := range service.Labels {
		if vv, ok := v.(string); ok {
			base[k] = vv
		}
	}

	return base
}

func (s *Scheduler) hydrateK8sDeployment(ctx context.Context, plan *eve.NSDeploymentPlan, service *eve.DeployService, nuance string) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		TypeMeta: deploymentMetaData,
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.ServiceName,
			Namespace: plan.Namespace.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(service.Count),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": service.ServiceName,
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      deploymentLabels(service, nuance),
					Annotations: deploymentAnnotations(service),
				},
				Spec: apiv1.PodSpec{
					SecurityContext: &apiv1.PodSecurityContext{
						RunAsUser:  int64Ptr(int64(service.RunAs)),
						RunAsGroup: int64Ptr(int64(service.RunAs)),
						FSGroup:    int64Ptr(65534),
					},
					ServiceAccountName: service.ServiceAccount,
					Containers: []apiv1.Container{
						{
							Name:            service.ArtifactName,
							ImagePullPolicy: apiv1.PullAlways,
							Image:           getDockerImageName(service.DeployArtifact),
							Ports:           getServiceContainerPorts(service),
							Env:             containerEnvVars(plan.DeploymentID, service.DeployArtifact),
						},
					},
					TerminationGracePeriodSeconds: int64Ptr(300),
					ImagePullSecrets:              imagePullSecrets,
				},
			},
		},
	}

	// Setup the Probes
	readinessProbe, err := s.parseProbe(ctx, service.ReadinessProbe)
	if err != nil {
		return nil, err
	}
	if readinessProbe != nil {
		deployment.Spec.Template.Spec.Containers[0].ReadinessProbe = readinessProbe
	}

	livelinessProbe, err := s.parseProbe(ctx, service.LivelinessProbe)
	if err != nil {
		return nil, err
	}
	if livelinessProbe != nil {
		deployment.Spec.Template.Spec.Containers[0].LivenessProbe = livelinessProbe
	}

	// Setup the pod resource constraints
	podResource, err := s.parsePodResource(ctx, service.PodResource)
	if err != nil {
		return nil, err
	}
	if podResource != nil {
		deployment.Spec.Template.Spec.Containers[0].Resources = apiv1.ResourceRequirements{
			Requests: podResource.Request,
			Limits:   podResource.Limit,
		}
	}

	// Hiding this behind a feature flag for now
	// until all node group labels are added to all machines in all clusters
	if config.GetConfig().EnableNodeGroup {
		if len(service.NodeGroup) <= 0 {
			s.Logger(ctx).Warn("node group empty using default 'shared' value")
			service.NodeGroup = defaultNodeGroupLabel
		}

		deployment.Spec.Template.Spec.NodeSelector = map[string]string{
			"node-group": service.NodeGroup,
		}
	} else {
		s.Logger(ctx).Info("skipping node group selector: EVE_ENABLE_NODE_GROUP false", zap.Any("deployment_name", deployment.ObjectMeta.Name))
	}

	return deployment, nil
}

func getServiceContainerPorts(service *eve.DeployService) []apiv1.ContainerPort {
	var result []apiv1.ContainerPort
	// Setup the Service Port
	if service.ServicePort != 0 {
		result = append(result, apiv1.ContainerPort{
			Name:          "http",
			ContainerPort: int32(service.ServicePort),
			Protocol:      apiv1.ProtocolTCP,
		})
	}
	// Setup the Metrics Port
	if service.MetricsPort != 0 {
		result = append(result, apiv1.ContainerPort{
			Name:          "metrics",
			ContainerPort: int32(service.MetricsPort),
			Protocol:      apiv1.ProtocolTCP,
		})
	}
	return result
}

func (s *Scheduler) watchServicePods(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	plan *eve.NSDeploymentPlan,
	service *eve.DeployService,
	timeNuance string,
) error {
	pods := k8s.CoreV1().Pods(plan.Namespace.Name)
	watch, err := pods.Watch(ctx, metav1.ListOptions{
		TypeMeta:       metav1.TypeMeta{},
		LabelSelector:  deploymentLabelSelector(service, timeNuance),
		TimeoutSeconds: int64Ptr(config.GetConfig().K8sDeployTimeoutSec),
	})
	if err != nil {
		return errors.Wrap(err, "an error occurred trying to watch the pods, deployment may have succeeded")
	}
	started := make(map[string]bool)

	for event := range watch.ResultChan() {
		p, ok := event.Object.(*apiv1.Pod)
		if !ok {
			continue
		}
		for _, x := range p.Status.ContainerStatuses {
			if x.LastTerminationState.Terminated != nil {
				service.ExitCode = int(x.LastTerminationState.Terminated.ExitCode)
				watch.Stop()
				return nil
			}

			if !x.Ready {
				continue
			}
			started[p.Name] = true
		}

		if len(started) >= 1 {
			watch.Stop()
			return nil
		}
	}
	service.ExitCode = TimeoutExitCode
	return nil
}

func (s *Scheduler) deployDockerService(ctx context.Context, service *eve.DeployService, plan *eve.NSDeploymentPlan) {
	failNLog := s.failAndLogFn(ctx, service.ServiceName, service.DeployArtifact, plan)
	logFn := s.logMessageFn(service.ServiceName, service.DeployArtifact, plan)
	k8s, err := getK8sClient()
	if err != nil {
		failNLog(err, "an error occurred trying to get the k8s client")
		return
	}

	timeNuance := strconv.Itoa(int(time.Now().Unix()))
	// k8s Service is Required
	if err := s.setupK8sService(ctx, k8s, plan, service); err != nil {
		failNLog(err, "an error occurred setting up the k8s service")
		return
	}
	// k8s Deployment is Required
	if err := s.setupK8sDeployment(ctx, k8s, plan, service, timeNuance); err != nil {
		failNLog(err, "an error occurred setting up the k8s deployment")
		return
	}
	// We wait/watch for 1 successful pod to come up
	if err := s.watchServicePods(ctx, k8s, plan, service, timeNuance); err != nil {
		failNLog(err, "an error occurred while watching k8s deployment pods")
		return
	}
	// k8s autoscaler is optional
	if err := s.setupK8sAutoscaler(ctx, k8s, plan, service); err != nil {
		failNLog(err, "an error occurred while setting up k8s horizontal pod autoscaler")
		return
	}

	if service.ExitCode != 0 {
		if service.ExitCode == TimeoutExitCode {
			logFn("timeout of %d seconds exceeded while waiting for the service to start", config.GetConfig().K8sDeployTimeoutSec)
		} else {
			logFn("pod failed to start and returned a non zero exit code: %d", service.ExitCode)
		}
		validExitCodes, err := expandSuccessExitCodes(service.SuccessExitCodes)
		if err != nil {
			failNLog(err, "an error occurred parsing valid exit codes for the service")
			return
		}

		if !intContains(validExitCodes, service.ExitCode) {
			service.Result = eve.DeployArtifactResultFailed
		}
	}

	// if we've set it to a failure above somewhere, we don't want to now state it's succeeded.
	if service.Result == eve.DeployArtifactResultNoop {
		service.Result = eve.DeployArtifactResultSuccess
	}
}
