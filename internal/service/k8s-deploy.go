package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const (
	DockerRepoFormat           = "unanet-%s.jfrog.io"
	dockerCreds                = "docker-cfg"
	fsGroup                    = 65534
	terminationGracePeriodSecs = 300
)

func int32Ptr(i int) *int32 {
	i32 := int32(i)
	return &i32
}

func int64Ptr(i int64) *int64 { return &i }

type k8sServiceOption func(*apiv1.Service)

func svcNameOpt(serviceName string) k8sServiceOption {
	return func(s *apiv1.Service) {
		s.ObjectMeta.Name = serviceName
		if s.Spec.Selector == nil {
			s.Spec.Selector = map[string]string{
				"app": serviceName,
			}
		} else {
			s.Spec.Selector["app"] = serviceName
		}
	}
}

func svcNamespaceOpt(ns string) k8sServiceOption {
	return func(s *apiv1.Service) {
		s.ObjectMeta.Namespace = ns
	}
}

func svcPortOpt(port int) k8sServiceOption {
	return func(s *apiv1.Service) {
		svcPort := apiv1.ServicePort{
			Port: int32(port),
			TargetPort: intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: int32(port),
			},
		}
		if s.Spec.Ports == nil {
			s.Spec.Ports = []apiv1.ServicePort{svcPort}
		} else {
			s.Spec.Ports = append(s.Spec.Ports, svcPort)
		}
	}
}

func svcStickySessionsOpt(stickySessions bool) k8sServiceOption {
	return func(s *apiv1.Service) {
		if stickySessions {
			s.Spec.SessionAffinity = apiv1.ServiceAffinityClientIP
		}
	}
}

func newK8sService(opts ...k8sServiceOption) *apiv1.Service {
	s := &apiv1.Service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type K8sDeployOption func(*appsv1.Deployment)

func deploymentContainerOpt(
	appPort, metricsPort int,
	name, image string,
	readinessProbe, livelinessProbe *apiv1.Probe,
	metadata map[string]interface{}) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		containerPorts := []apiv1.ContainerPort{}
		if appPort > 0 {
			containerPorts = append(containerPorts, containerPortHelper("http", appPort, apiv1.ProtocolTCP))
		}

		if metricsPort > 0 {
			containerPorts = append(containerPorts, containerPortHelper("metrics", metricsPort, apiv1.ProtocolTCP))
			if d.Spec.Template.ObjectMeta.Annotations == nil {
				d.Spec.Template.ObjectMeta.Annotations = map[string]string{
					"prometheus.io/scrape": "true",
					"prometheus.io/port":   strconv.Itoa(metricsPort),
				}
			} else {
				d.Spec.Template.ObjectMeta.Annotations["prometheus.io/scrape"] = "true"
				d.Spec.Template.ObjectMeta.Annotations["prometheus.io/port"] = strconv.Itoa(metricsPort)
			}
		}

		var containerEnvVars []apiv1.EnvVar

		for k, v := range metadata {
			value, ok := v.(string)
			if !ok {
				continue
			}
			containerEnvVars = append(containerEnvVars, apiv1.EnvVar{
				Name:  k,
				Value: value,
			})
		}

		container := apiv1.Container{
			Name:            name,
			Image:           image,
			ImagePullPolicy: apiv1.PullAlways,
			Ports:           containerPorts,
			LivenessProbe:   livelinessProbe,
			ReadinessProbe:  readinessProbe,
			Env:             containerEnvVars,
		}

		if d.Spec.Template.Spec.Containers == nil {
			d.Spec.Template.Spec.Containers = []apiv1.Container{container}
		} else {
			d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, container)
		}

	}
}

func deploymentReplicasOpt(count int) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.Spec.Replicas = int32Ptr(count)
	}
}

func deploymentSecurityContextOpt(runAs int) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.Spec.SecurityContext == nil {
			d.Spec.Template.Spec.SecurityContext = &apiv1.PodSecurityContext{
				RunAsGroup: int64Ptr(int64(runAs)),
				RunAsUser:  int64Ptr(int64(runAs)),
				FSGroup:    int64Ptr(int64(fsGroup)),
			}
		} else {
			d.Spec.Template.Spec.SecurityContext.RunAsGroup = int64Ptr(int64(runAs))
			d.Spec.Template.Spec.SecurityContext.RunAsUser = int64Ptr(int64(runAs))
			d.Spec.Template.Spec.SecurityContext.FSGroup = int64Ptr(int64(fsGroup))

		}
	}
}

func deploymentServiceAccountOpt(sa string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.ServiceAccountName = sa
	}
}

func deploymentLabelsOpt(name, version, nuance string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.ObjectMeta.Labels == nil {
			d.Spec.Template.ObjectMeta.Labels = map[string]string{
				"app":     name,
				"version": version,
				"nuance":  nuance,
			}
		} else {
			d.Spec.Template.ObjectMeta.Labels["app"] = name
			d.Spec.Template.ObjectMeta.Labels["version"] = version
			d.Spec.Template.ObjectMeta.Labels["nuance"] = nuance
		}
	}
}

func deploymentSelectorLabelsOpt(name string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Selector.MatchLabels == nil {
			d.Spec.Selector.MatchLabels = map[string]string{
				"app": name,
			}
		} else {
			d.Spec.Selector.MatchLabels["app"] = name
		}
	}
}

func deploymentNamespaceOpt(ns string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.ObjectMeta.Namespace = ns
	}
}

func deploymentImagePullSecretsOpt() K8sDeployOption {
	return func(d *appsv1.Deployment) {
		dockerCreds := apiv1.LocalObjectReference{Name: dockerCreds}
		if d.Spec.Template.Spec.ImagePullSecrets == nil {
			d.Spec.Template.Spec.ImagePullSecrets = []apiv1.LocalObjectReference{dockerCreds}
		} else {
			d.Spec.Template.Spec.ImagePullSecrets = append(d.Spec.Template.Spec.ImagePullSecrets, dockerCreds)
		}

	}
}

func deploymentTerminationGracePeriodOpt() K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.TerminationGracePeriodSeconds = int64Ptr(int64(terminationGracePeriodSecs))
	}
}

func containerPortHelper(name string, port int, protocol apiv1.Protocol) apiv1.ContainerPort {
	return apiv1.ContainerPort{Name: name, ContainerPort: int32(port), Protocol: protocol}
}

func (s *Scheduler) probeHelper(ctx context.Context, probeBytes []byte) *apiv1.Probe {
	if len(probeBytes) < 5 {
		return nil
	}
	var probe apiv1.Probe
	err := json.Unmarshal(probeBytes, &probe)
	if err != nil {
		s.Logger(ctx).Error("failed to unmarshal the probe", zap.Error(err))
		return nil
	}
	if probe.Handler.Exec == nil && probe.Handler.HTTPGet == nil && probe.Handler.TCPSocket == nil {
		s.Logger(ctx).Error("invalid probe, the handler was not set")
		return nil
	}
	return &probe
}

// newK8sDeployment initializes an empty k8s Deployment and then applies the given K8sDeployOption options
func newK8sDeployment(opts ...K8sDeployOption) *appsv1.Deployment {
	d := &appsv1.Deployment{}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// upsertDeployment will either create the initial K8s Deployment or Update and existing deployment
func (s *Scheduler) upsertDeployment(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	service *eve.DeployService,
	deployment *appsv1.Deployment,
	plan *eve.NSDeploymentPlan,
	failNLog func(err error, format string, a ...interface{}),
) {

	_, err := k8s.AppsV1().Deployments(plan.Namespace.Name).Get(ctx, service.ServiceName, metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			// This app hasn't been deployed yet so we need to deploy it
			_, err := k8s.AppsV1().Deployments(plan.Namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
			if err != nil {
				failNLog(err, "an error occurred trying to create the deployment")
				return
			}
		} else {
			// an error occurred trying to see if the app is already deployed
			failNLog(err, "an error occurred trying to check for the deployment")
			return
		}
	} else {
		// we were able to retrieve the app which mean we need to run update instead of create
		_, err := k8s.AppsV1().Deployments(plan.Namespace.Name).Update(ctx, deployment, metav1.UpdateOptions{})
		if err != nil {
			failNLog(err, "an error occurred trying to update the deployment")
			return
		}
	}
}

// ensureK8sServiceExists Ensures that the K8s Service exists, if not, then we create it
func (s *Scheduler) ensureServiceExists(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	service *eve.DeployService,
	plan *eve.NSDeploymentPlan,
	failNLog func(err error, format string, a ...interface{}),
) {

	if service.ServicePort > 0 {
		_, err := k8s.CoreV1().Services(plan.Namespace.Name).Get(ctx, service.ServiceName, metav1.GetOptions{})
		if err != nil {
			if k8sErrors.IsNotFound(err) {
				_, err := k8s.CoreV1().Services(plan.Namespace.Name).Create(
					ctx,
					newK8sService(
						svcNameOpt(service.ServiceName),
						svcNamespaceOpt(plan.Namespace.Name),
						svcPortOpt(service.ServicePort),
						svcStickySessionsOpt(service.StickySessions),
					),
					metav1.CreateOptions{},
				)
				if err != nil {
					failNLog(err, "an error occurred trying to create the service")
					return
				}
			} else {
				failNLog(err, "an error occurred trying to check for the service")
				return
			}
		}
	}
}

func (s *Scheduler) watchPodStatus(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	service *eve.DeployService,
	d *appsv1.Deployment,
	plan *eve.NSDeploymentPlan,
	failNLog func(err error, format string, a ...interface{}),
) {

	var labelSelector = fmt.Sprintf("app=%s,version=%s,nuance=%s", service.ServiceName, service.AvailableVersion, d.Spec.Template.ObjectMeta.Labels["nuance"])

	pods := k8s.CoreV1().Pods(plan.Namespace.Name)
	podWatcher, err := pods.Watch(ctx, metav1.ListOptions{
		TypeMeta:       metav1.TypeMeta{},
		LabelSelector:  labelSelector,
		TimeoutSeconds: int64Ptr(config.GetConfig().K8sDeployTimeoutSec),
	})
	if err != nil {
		failNLog(err, "an error occurred trying to watch the pods, deployment may have succeeded")
		return
	}
	started := make(map[string]bool)

	var instanceCount = service.Count
	if strings.HasPrefix(service.ServiceName, "eve-sch") {
		instanceCount = 1
	}

	for event := range podWatcher.ResultChan() {
		p, ok := event.Object.(*apiv1.Pod)
		if !ok {
			continue
		}
		for _, x := range p.Status.ContainerStatuses {
			if x.LastTerminationState.Terminated != nil {
				failNLog(nil, "pod failed to start and returned a non zero exit code: %v", x.LastTerminationState.Terminated.ExitCode)
				continue
			}
			if !*x.Started {
				continue
			}
			started[p.Name] = true
		}

		if len(started) == instanceCount {
			podWatcher.Stop()
		}
	}

	if len(started) != instanceCount {
		// make sure we don't get a false positive and actually check
		pods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			failNLog(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ServiceName)
			return
		}

		if len(pods.Items) != instanceCount {
			failNLog(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ServiceName)
			return
		}

		var startedCount int
		for _, x := range pods.Items {
			if x.Status.ContainerStatuses[0].State.Running != nil {
				startedCount += 1
			}
		}

		if startedCount != instanceCount {
			failNLog(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ServiceName)
			return
		}
	}
}

func (s *Scheduler) deployDockerService(ctx context.Context, service *eve.DeployService, plan *eve.NSDeploymentPlan) {
	s.Logger(ctx).Debug("deploying docker service", zap.String("artifact_name", service.ArtifactName))
	// Generate the K8s Deployment Resource
	nuance := strconv.Itoa(int(time.Now().Unix()))
	dockerImage := getDockerImageName(service.DeployArtifact)

	deployment := newK8sDeployment(
		deploymentContainerOpt(
			service.ServicePort,
			service.MetricsPort,
			service.ArtifactName,
			dockerImage,
			s.probeHelper(ctx, service.ReadinessProbe),
			s.probeHelper(ctx, service.LivelinessProbe),
			service.Metadata,
		),
		deploymentSelectorLabelsOpt(service.ServiceName),
		deploymentLabelsOpt(service.ServiceName, service.AvailableVersion, nuance),
		deploymentSecurityContextOpt(service.RunAs),
		deploymentReplicasOpt(service.Count),
		deploymentServiceAccountOpt(service.ServiceAccount),
		deploymentNamespaceOpt(plan.Namespace.Name),
		deploymentImagePullSecretsOpt(),
		deploymentTerminationGracePeriodOpt(),
	)

	// establish the failure handler function
	failNLog := s.failAndLogFn(ctx, service.ServiceName, service.DeployArtifact, plan)

	// get the k8s API Client
	k8s, err := getK8sClient()
	if err != nil {
		failNLog(err, "an error occurred trying to get the k8s client")
		return
	}

	// ensure that the k8s service exists
	s.ensureServiceExists(ctx, k8s, service, plan, failNLog)

	// create or update the deployment
	s.upsertDeployment(ctx, k8s, service, deployment, plan, failNLog)

	// watch pod status changes
	s.watchPodStatus(ctx, k8s, service, deployment, plan, failNLog)

	// Return the successful result
	service.Result = eve.DeployArtifactResultSuccess
}
