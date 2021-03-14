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

//func (s *Scheduler) parsePodTemplateSpec(ctx context.Context, input []byte, defType eve.DefinitionType) (*apiv1.PodTemplateSpec, error) {
//	if len(input) <= 5 {
//		return nil, nil
//	}
//	var podTemplateSpec apiv1.PodTemplateSpec
//	if err := json.Unmarshal(input, &podTemplateSpec); err != nil {
//		s.Logger(ctx).Warn("failed to unmarshal the pod template spec", zap.Error(err))
//		return nil, err
//	}
//
//}

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
			// This app hasn't been deployed yet so we need to deploy it (create the k8s deployment)
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

func (s *Scheduler) deploymentDefinition(ctx context.Context, definition []byte) (*appsv1.Deployment, error) {
	var def eve.DefinitionSpec
	//var def = make(map[string]interface{})
	if err := json.Unmarshal(definition, &def); err != nil {
		s.Logger(ctx).Error("failed to unmarshal the definition specs", zap.Error(err))
		return nil, err
	}

	deployment := &appsv1.Deployment{}

	if depDef, ok := def["appsv1.Deployment"]; ok {
		b, err := json.Marshal(depDef)
		if err != nil {
			s.Logger(ctx).Error("failed to marshal the deployment config to bytes", zap.Error(err))
			return nil, err
		}
		var deploymentConfig = &appsv1.Deployment{}
		if err := json.Unmarshal(b, &deploymentConfig); err != nil {
			s.Logger(ctx).Error("failed to unmarshal the deployment config", zap.Error(err))
			return nil, err
		}
		deployment = deploymentConfig
	}

	return deployment, nil
}

func defDeploymentLabelSelector(svcName string) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app": svcName,
		},
	}
}

// TODO: Clean this up once we migrate replicas from Service/Job to definitions
func (s *Scheduler) defaultReplicaValue(ctx context.Context, serviceValue *int32, definitionValue *int32) *int32 {
	const fallBackReplicaValue = 2

	if definitionValue == nil && serviceValue == nil {
		s.Logger(ctx).Warn("replica values nil using fallback of 2", zap.Int32("replica", fallBackReplicaValue))
		return int32Ptr(fallBackReplicaValue)
	}

	if definitionValue != nil && serviceValue != nil {
		if definitionValue != serviceValue {
			s.Logger(ctx).Warn("duplicate replica values dont match using definitionValue", zap.Int32p("replica", definitionValue))
		} else {
			s.Logger(ctx).Warn("duplicate replica values match using definitionValue", zap.Int32p("replica", definitionValue))
		}
		return definitionValue
	}

	if definitionValue != nil {
		s.Logger(ctx).Info("using definition replica value", zap.Int32p("replica", definitionValue))
		return definitionValue
	}

	if serviceValue != nil {
		s.Logger(ctx).Info("using service replica value", zap.Int32p("replica", serviceValue))
		return serviceValue
	}

	// this path should never hit
	// logging as error
	s.Logger(ctx).Error("unknown replica value using fallback of 2", zap.Int32("replica", fallBackReplicaValue))
	return int32Ptr(fallBackReplicaValue)
}

// TODO: remove RunAs from Service and use definition config
func (s *Scheduler) defaultSecurityContex(ctx context.Context, runAs int, definitionValue *apiv1.PodSecurityContext) *apiv1.PodSecurityContext {
	if definitionValue != nil {
		s.Logger(ctx).Info("using definition security context value", zap.Any("security_context", definitionValue))
		return definitionValue
	}

	s.Logger(ctx).Warn("using service security context value", zap.Int("run_as", runAs))
	return &apiv1.PodSecurityContext{
		RunAsUser:  int64Ptr(int64(runAs)),
		RunAsGroup: int64Ptr(int64(runAs)),
		FSGroup:    int64Ptr(65534),
	}
}

// TODO: remove ServiceAccount and use Definition Config or default to const in code
func (s *Scheduler) defaultServiceAccountName(ctx context.Context, serviceValue string, definitionValue string) string {
	if len(definitionValue) > 0 {
		s.Logger(ctx).Info("using definition service account value", zap.String("service_account", definitionValue))
		return definitionValue
	}
	s.Logger(ctx).Warn("using service value for service account name", zap.String("service_account", serviceValue))
	return serviceValue
}

// TODO: Use Def Spec with sane default
func (s *Scheduler) defaultTermGracePeriod(ctx context.Context, definitionValue *int64) *int64 {
	if definitionValue == nil {
		s.Logger(ctx).Debug("termination grace period not set using default 300 seconds")
		return int64Ptr(300)
	}
	return definitionValue
}

func (s *Scheduler) defaultNodeSelector(ctx context.Context, definitionValue map[string]string) map[string]string {
	if config.GetConfig().EnableNodeGroup == false {
		s.Logger(ctx).Warn("skipping node selector EVE_ENABLE_NODE_GROUP false")
		return nil
	}

	if definitionValue != nil {
		s.Logger(ctx).Info("using definition node selector")
		return definitionValue
	}

	s.Logger(ctx).Info("using default node-group:shared selector")
	return map[string]string{"node-group": "shared"}
}

func (s *Scheduler) defaultContainers(ctx context.Context, service *eve.DeployService, plan *eve.NSDeploymentPlan, definitionContainers []apiv1.Container) []apiv1.Container {
	defaultContainer := apiv1.Container{
		Name:            service.ArtifactName,
		ImagePullPolicy: apiv1.PullAlways,
		Image:           getDockerImageName(service.DeployArtifact),
		Ports:           getServiceContainerPorts(service),
		Env:             containerEnvVars(plan.DeploymentID, service.DeployArtifact),
	}

	if definitionContainers == nil || len(definitionContainers) == 0 {
		return []apiv1.Container{defaultContainer}
	}

	// TODO: Support more than 1 container?
	definitionContainers[0].Name = defaultContainer.Name
	definitionContainers[0].ImagePullPolicy = defaultContainer.ImagePullPolicy
	definitionContainers[0].Image = defaultContainer.Image
	definitionContainers[0].Ports = defaultContainer.Ports
	definitionContainers[0].Env = defaultContainer.Env

	return definitionContainers
}

// TODO: Migrate Probe to Def data
func (s *Scheduler) defaultProbe(ctx context.Context, serviceValue []byte, definitionValue *apiv1.Probe) *apiv1.Probe {
	if definitionValue != nil {
		return definitionValue
	}

	probe, err := s.parseProbe(ctx, serviceValue)
	if err != nil {
		s.Logger(ctx).Error("failed to parse the probe")
		return nil
	}
	return probe
}

// TODO: Migrate Pod Resource to Def Data
func (s *Scheduler) defaultContainerResources(ctx context.Context, serviceValue []byte, definitionValue apiv1.ResourceRequirements) apiv1.ResourceRequirements {
	if definitionValue.Limits != nil && definitionValue.Requests != nil {
		s.Logger(ctx).Info("using pod resource definition value", zap.Any("value", definitionValue))
		return definitionValue
	}

	podResource, err := s.parsePodResource(ctx, serviceValue)
	if err != nil {
		s.Logger(ctx).Error("failed to parse the pod resource value", zap.Error(err))
		return apiv1.ResourceRequirements{}
	}

	if podResource != nil {
		s.Logger(ctx).Warn("using service value for pod resources", zap.Any("value", podResource))
		return apiv1.ResourceRequirements{
			Limits:   podResource.Limit,
			Requests: podResource.Request,
		}
	}

	s.Logger(ctx).Warn("unknown pod resource value")
	return apiv1.ResourceRequirements{}
}

func (s *Scheduler) hydrateK8sDeployment(ctx context.Context, plan *eve.NSDeploymentPlan, service *eve.DeployService, nuance string) (*appsv1.Deployment, error) {

	newDeployment, err := s.deploymentDefinition(ctx, service.Definition)
	if err != nil {
		s.Logger(ctx).Error("failed to generate deployment definition", zap.Error(err))
		return nil, err
	}

	newDeployment.TypeMeta = deploymentMetaData
	newDeployment.ObjectMeta.Name = service.ServiceName
	newDeployment.ObjectMeta.Namespace = plan.Namespace.Name
	newDeployment.Spec.Selector = defDeploymentLabelSelector(service.ServiceName)
	newDeployment.Spec.Replicas = s.defaultReplicaValue(ctx, int32Ptr(service.Count), newDeployment.Spec.Replicas)
	newDeployment.Spec.Template.ObjectMeta.Labels = mergeMapStringString(deploymentLabels(service, nuance), newDeployment.Spec.Template.ObjectMeta.Labels)
	newDeployment.Spec.Template.ObjectMeta.Annotations = mergeMapStringString(deploymentAnnotations(service), newDeployment.Spec.Template.ObjectMeta.Annotations)
	newDeployment.Spec.Template.Spec.ImagePullSecrets = imagePullSecrets
	newDeployment.Spec.Template.Spec.SecurityContext = s.defaultSecurityContex(ctx, service.RunAs, newDeployment.Spec.Template.Spec.SecurityContext)
	newDeployment.Spec.Template.Spec.ServiceAccountName = s.defaultServiceAccountName(ctx, service.ServiceAccount, newDeployment.Spec.Template.Spec.ServiceAccountName)
	newDeployment.Spec.Template.Spec.TerminationGracePeriodSeconds = s.defaultTermGracePeriod(ctx, newDeployment.Spec.Template.Spec.TerminationGracePeriodSeconds)
	newDeployment.Spec.Template.Spec.NodeSelector = s.defaultNodeSelector(ctx, newDeployment.Spec.Template.Spec.NodeSelector)
	newDeployment.Spec.Template.Spec.Containers = s.defaultContainers(ctx, service, plan, newDeployment.Spec.Template.Spec.Containers)
	newDeployment.Spec.Template.Spec.Containers[0].ReadinessProbe = s.defaultProbe(ctx, service.ReadinessProbe, newDeployment.Spec.Template.Spec.Containers[0].ReadinessProbe)
	newDeployment.Spec.Template.Spec.Containers[0].LivenessProbe = s.defaultProbe(ctx, service.LivelinessProbe, newDeployment.Spec.Template.Spec.Containers[0].LivenessProbe)
	newDeployment.Spec.Template.Spec.Containers[0].Resources = s.defaultContainerResources(ctx, service.PodResource, newDeployment.Spec.Template.Spec.Containers[0].Resources)

	s.Logger(ctx).Info("k8s deployment spec", zap.Any("deployment", newDeployment))

	return newDeployment, nil
}

// standard is always applied
// def comes from the API definition Spec
func mergeMapStringString(standard map[string]string, definition map[string]string) map[string]string {
	var merged = make(map[string]string)
	if definition != nil {
		for k, v := range definition {
			merged[k] = v
		}
	}

	for k, v := range standard {
		merged[k] = v
	}

	return merged
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
