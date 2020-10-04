package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/eve/pkg/log"
	"go.uber.org/zap"
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

type K8sServiceOption func(*apiv1.Service)

func SvcNameOpt(serviceName string) K8sServiceOption {
	return func(s *apiv1.Service) {
		s.ObjectMeta.Name = serviceName
		if s.Spec.Selector == nil {
			s.Spec.Selector = map[string]string{}
		}
		s.Spec.Selector["app"] = serviceName
	}
}

func SvcNamespaceOpt(ns string) K8sServiceOption {
	return func(s *apiv1.Service) {
		s.ObjectMeta.Namespace = ns
	}
}

func SvcPortOpt(port int) K8sServiceOption {
	return func(s *apiv1.Service) {
		if s.Spec.Ports == nil {
			s.Spec.Ports = []apiv1.ServicePort{
				{
					TargetPort: intstr.IntOrString{},
				},
			}
		}
		s.Spec.Ports[0].Port = int32(port)
		s.Spec.Ports[0].TargetPort.Type = intstr.Int
		s.Spec.Ports[0].TargetPort.IntVal = int32(port)
	}
}

func SvcStickySessionsOpt(stickySessions bool) K8sServiceOption {
	return func(s *apiv1.Service) {
		if stickySessions {
			s.Spec.SessionAffinity = apiv1.ServiceAffinityClientIP
		}
	}
}

func NewK8sService(opts ...K8sServiceOption) *apiv1.Service {
	s := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					TargetPort: intstr.IntOrString{},
				},
			},
			Selector: map[string]string{},
		},
	}

	for _, opt := range opts {
		opt(s)
	}
	return s
}

type K8sDeployOption func(*appsv1.Deployment)

// helper used to initialize the nested container structs
// dont want null refs/panics...
func initDeploymentContainers() []apiv1.Container {
	return []apiv1.Container{
		{
			Ports:          []apiv1.ContainerPort{},
			LivenessProbe:  &apiv1.Probe{},
			ReadinessProbe: &apiv1.Probe{},
			Env:            []apiv1.EnvVar{},
		},
	}
}

func DeploymentContainerImageOpt(image string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.Spec.Containers == nil {
			d.Spec.Template.Spec.Containers = initDeploymentContainers()
		}
		d.Spec.Template.Spec.Containers[0].Image = image
	}
}

func DeploymentContainerNameOpt(artifact string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.Spec.Containers == nil {
			d.Spec.Template.Spec.Containers = initDeploymentContainers()
		}
		d.Spec.Template.Spec.Containers[0].Name = artifact
	}
}

func DeploymentContainerImagePullPolicyOpt(policy apiv1.PullPolicy) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.Spec.Containers == nil {
			d.Spec.Template.Spec.Containers = initDeploymentContainers()
		}
		d.Spec.Template.Spec.Containers[0].ImagePullPolicy = policy
	}
}

func DeploymentReplicasOpt(count int) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.Spec.Replicas = int32Ptr(count)
	}
}

func DeploymentRunAsOpt(runAs int) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.Spec.SecurityContext == nil {
			d.Spec.Template.Spec.SecurityContext = &apiv1.PodSecurityContext{}
		}
		d.Spec.Template.Spec.SecurityContext.RunAsGroup = int64Ptr(int64(runAs))
		d.Spec.Template.Spec.SecurityContext.RunAsUser = int64Ptr(int64(runAs))
	}
}

func DeploymentFSGroupOpt(fsGroup int) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.Spec.SecurityContext == nil {
			d.Spec.Template.Spec.SecurityContext = &apiv1.PodSecurityContext{}
		}
		d.Spec.Template.Spec.SecurityContext.FSGroup = int64Ptr(int64(fsGroup))
	}
}

func DeploymentServiceAccountOpt(sa string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.ServiceAccountName = sa
	}
}

func DeploymentNameOpt(name string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.ObjectMeta.Name = name
		if d.Spec.Selector.MatchLabels == nil {
			d.Spec.Selector.MatchLabels = map[string]string{}
		}
		d.Spec.Selector.MatchLabels["app"] = name

		if d.Spec.Template.ObjectMeta.Labels == nil {
			d.Spec.Template.ObjectMeta.Labels = map[string]string{}
		}
		d.Spec.Template.ObjectMeta.Labels["app"] = name
	}
}

func DeploymentVersionOpt(version string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.ObjectMeta.Labels == nil {
			d.Spec.Template.ObjectMeta.Labels = map[string]string{}
		}
		d.Spec.Template.ObjectMeta.Labels["version"] = version
	}
}

func DeploymentNamespaceOpt(ns string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.ObjectMeta.Namespace = ns
	}
}

func DeploymentNuanceOpt(nuance string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.ObjectMeta.Labels == nil {
			d.Spec.Template.ObjectMeta.Labels = map[string]string{}
		}
		d.Spec.Template.ObjectMeta.Labels["nuance"] = nuance
	}
}

func DeploymentDockerCredsOpt(creds string) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.ImagePullSecrets[0].Name = creds
	}
}

func DeploymentTerminationGracePeriodOpt(period int) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.TerminationGracePeriodSeconds = int64Ptr(int64(period))
	}
}

func containerPortHelper(name string, port int, protocol apiv1.Protocol) apiv1.ContainerPort {
	return apiv1.ContainerPort{
		Name:          name,
		ContainerPort: int32(port),
		Protocol:      protocol,
	}
}

func DeploymentMetricsPortOpt(port int) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if port != 0 {
			if d.Spec.Template.Spec.Containers == nil {
				d.Spec.Template.Spec.Containers = initDeploymentContainers()
			}
			d.Spec.Template.Spec.Containers[0].Ports = append(
				d.Spec.Template.Spec.Containers[0].Ports,
				containerPortHelper("metrics", port, apiv1.ProtocolTCP),
			)

			d.Spec.Template.ObjectMeta.Annotations = map[string]string{
				"prometheus.io/scrape": "true",
				"prometheus.io/port":   strconv.Itoa(port),
			}
		}
	}
}

func DeploymentApplicationPortOpt(port int) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if port != 0 {
			if d.Spec.Template.Spec.Containers == nil {
				d.Spec.Template.Spec.Containers = initDeploymentContainers()
			}
			d.Spec.Template.Spec.Containers[0].Ports = append(
				d.Spec.Template.Spec.Containers[0].Ports,
				containerPortHelper("http", port, apiv1.ProtocolTCP),
			)
		}
	}
}

func DeploymentEnvironmentVarsOpt(metadata map[string]interface{}) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if d.Spec.Template.Spec.Containers == nil {
			d.Spec.Template.Spec.Containers = initDeploymentContainers()
		}

		for k, v := range metadata {
			value, ok := v.(string)
			if !ok {
				continue
			}
			d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, apiv1.EnvVar{
				Name:  k,
				Value: value,
			})
		}

	}
}

func DeploymentLivelinessProbeOpt(probeBytes []byte) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if len(probeBytes) < 5 {
			return
		}
		var probe apiv1.Probe
		err := json.Unmarshal(probeBytes, &probe)
		if err != nil {
			log.Logger.Warn("failed to unmarshal the liveliness probe", zap.Error(err))
			return
		}
		if probe.Handler.Exec == nil && probe.Handler.HTTPGet == nil && probe.Handler.TCPSocket == nil {
			log.Logger.Warn("invalid liveliness probe, the handler was not set")
			return
		}
		if d.Spec.Template.Spec.Containers == nil {
			d.Spec.Template.Spec.Containers = initDeploymentContainers()
		}
		d.Spec.Template.Spec.Containers[0].LivenessProbe = &probe
	}
}

func DeploymentReadinessProbeOpt(probeBytes []byte) K8sDeployOption {
	return func(d *appsv1.Deployment) {
		if len(probeBytes) < 5 {
			return
		}
		var probe apiv1.Probe
		err := json.Unmarshal(probeBytes, &probe)
		if err != nil {
			log.Logger.Warn("failed to unmarshal the readiness probe", zap.Error(err))
			return
		}
		if probe.Handler.Exec == nil && probe.Handler.HTTPGet == nil && probe.Handler.TCPSocket == nil {
			log.Logger.Warn("invalid readiness probe, the handler was not set")
			return
		}
		if d.Spec.Template.Spec.Containers == nil {
			d.Spec.Template.Spec.Containers = initDeploymentContainers()
		}
		d.Spec.Template.Spec.Containers[0].ReadinessProbe = &probe
	}
}

// NewK8sDeployment initializes an empty k8s Deployment and then applies the given K8sDeployOption options
func NewK8sDeployment(opts ...K8sDeployOption) *appsv1.Deployment {
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
				Spec: apiv1.PodSpec{
					SecurityContext:  &apiv1.PodSecurityContext{},
					Containers:       initDeploymentContainers(),
					ImagePullSecrets: []apiv1.LocalObjectReference{},
				},
			},
		},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// upsertDeployment will either create the initial K8s Deployment or Update and existing deployment
func upsertDeployment(
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
func ensureServiceExists(
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
					NewK8sService(
						SvcNameOpt(service.ServiceName),
						SvcNamespaceOpt(plan.Namespace.Name),
						SvcPortOpt(service.ServicePort),
						SvcStickySessionsOpt(service.StickySessions),
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

func watchPodStatus(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	service *eve.DeployService,
	d *appsv1.Deployment,
	plan *eve.NSDeploymentPlan,
	failNLog func(err error, format string, a ...interface{}),
) {

	var labelSelector = fmt.Sprintf("app=%s,version=%s,nuance=%s", service.ServiceName, service.AvailableVersion, d.Spec.Template.ObjectMeta.Labels["nuance"])

	pods := k8s.CoreV1().Pods(plan.Namespace.Name)
	watch, err := pods.Watch(ctx, metav1.ListOptions{
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

	for event := range watch.ResultChan() {
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
			watch.Stop()
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
	// Generate the K8s Deployment Resource
	deployment := NewK8sDeployment(
		DeploymentReplicasOpt(service.Count),
		DeploymentRunAsOpt(service.RunAs),
		DeploymentServiceAccountOpt(service.ServiceAccount),
		DeploymentNameOpt(service.ServiceName),
		DeploymentVersionOpt(service.AvailableVersion),
		DeploymentNamespaceOpt(plan.Namespace.Name),
		DeploymentContainerImageOpt(getDockerImageName(service.DeployArtifact)),
		DeploymentContainerNameOpt(service.ArtifactName),
		DeploymentContainerImagePullPolicyOpt(apiv1.PullAlways),
		DeploymentNuanceOpt(strconv.Itoa(int(time.Now().Unix()))),
		DeploymentFSGroupOpt(fsGroup),
		DeploymentDockerCredsOpt(dockerCreds),
		DeploymentTerminationGracePeriodOpt(terminationGracePeriodSecs),
		DeploymentMetricsPortOpt(service.MetricsPort),
		DeploymentApplicationPortOpt(service.ServicePort),
		DeploymentEnvironmentVarsOpt(service.Metadata),
		DeploymentLivelinessProbeOpt(service.LivelinessProbe),
		DeploymentReadinessProbeOpt(service.ReadinessProbe),
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
	ensureServiceExists(ctx, k8s, service, plan, failNLog)

	// create or update the deployment
	upsertDeployment(ctx, k8s, service, deployment, plan, failNLog)

	// watch pod status changes
	watchPodStatus(ctx, k8s, service, deployment, plan, failNLog)

	// Return the successful result
	service.Result = eve.DeployArtifactResultSuccess
}
