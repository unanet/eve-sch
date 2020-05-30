package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
)

const (
	DockerRepoFormat = "unanet-%s.jfrog.io"
)

func int32Ptr(i int) *int32 {
	i32 := int32(i)
	return &i32
}

func int64Ptr(i int64) *int64 { return &i }

func getDockerImageName(artifact *eve.DeployArtifact) string {
	repo := fmt.Sprintf(DockerRepoFormat, artifact.ArtifactoryFeed)
	return fmt.Sprintf("%s/%s:%s", repo, artifact.ArtifactoryPath, artifact.EvalImageTag())
}

func getK8sClient() (*kubernetes.Clientset, error) {
	c, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrap(err)
	}

	client, err := kubernetes.NewForConfig(c)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return client, nil
}

func getK8sService(serviceName, namespace string, servicePort int) *apiv1.Service {
	return &apiv1.Service{
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
}

func getK8sDeployment(
	instanceCount, runAs int,
	serviceAccountName,
	serviceName,
	artifactName,
	artifactVersion,
	namespace,
	containerImage,
	nuance string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(instanceCount),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": serviceName,
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     serviceName,
						"version": artifactVersion,
						"nuance":  nuance,
					},
				},
				Spec: apiv1.PodSpec{
					SecurityContext: &apiv1.PodSecurityContext{
						RunAsUser:  int64Ptr(int64(runAs)),
						RunAsGroup: int64Ptr(int64(runAs)),
						FSGroup:    int64Ptr(65534),
					},
					ServiceAccountName: serviceAccountName,
					Containers: []apiv1.Container{
						{
							Name:            artifactName,
							ImagePullPolicy: apiv1.PullAlways,
							Image:           containerImage,
							Ports:           []apiv1.ContainerPort{},
						},
					},
					ImagePullSecrets: []apiv1.LocalObjectReference{
						{
							Name: "docker-cfg",
						},
					},
				},
			},
		},
	}
}

func setupEnvironment(metadata map[string]interface{}, deployment *appsv1.Deployment) {
	var containerEnvVars []apiv1.EnvVar

	for k, v := range metadata {
		value, ok := v.(string)
		if !ok {
			continue
		}
		containerEnvVars = append(containerEnvVars, apiv1.EnvVar{
			Name:  strings.ToUpper(k),
			Value: value,
		})
	}

	deployment.Spec.Template.Spec.Containers[0].Env = containerEnvVars
}

func setupPorts(servicePort, metricsPort int, deployment *appsv1.Deployment) {
	if servicePort != 0 {
		deployment.Spec.Template.Spec.Containers[0].Ports = append(deployment.Spec.Template.Spec.Containers[0].Ports, apiv1.ContainerPort{
			Name:          "http",
			ContainerPort: int32(servicePort),
			Protocol:      apiv1.ProtocolTCP,
		})
	}

	if metricsPort != 0 {
		deployment.Spec.Template.Spec.Containers[0].Ports = append(deployment.Spec.Template.Spec.Containers[0].Ports, apiv1.ContainerPort{
			Name:          "metrics",
			ContainerPort: int32(metricsPort),
			Protocol:      apiv1.ProtocolTCP,
		})
	}
}

func setupMetrics(port int, deployment *appsv1.Deployment) {
	if port == 0 {
		return
	}

	annotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   strconv.Itoa(port),
	}

	deployment.Spec.Template.ObjectMeta.Annotations = annotations
}

func (s *Scheduler) deployDockerService(ctx context.Context, service *eve.DeployService, plan *eve.NSDeploymentPlan) {
	fail := s.failAndLogFn(ctx, service.DeployArtifact, plan)
	k8s, err := getK8sClient()
	if err != nil {
		fail(err, "an error occurred trying to get the k8s client")
		return
	}
	var instanceCount = 2
	timeNuance := strconv.Itoa(int(time.Now().Unix()))
	imageName := getDockerImageName(service.DeployArtifact)
	deployment := getK8sDeployment(
		instanceCount, service.RunAs,
		service.ServiceAccount,
		service.ServiceName,
		service.ArtifactName,
		service.AvailableVersion,
		plan.Namespace.Name,
		imageName,
		timeNuance)
	setupEnvironment(service.Metadata, deployment)
	setupMetrics(service.MetricsPort, deployment)
	setupPorts(service.ServicePort, service.MetricsPort, deployment)

	if service.ServicePort > 0 {
		_, err := k8s.CoreV1().Services(plan.Namespace.Name).Get(ctx, service.ServiceName, metav1.GetOptions{})
		if err != nil {
			if k8sErrors.IsNotFound(err) {
				_, err := k8s.CoreV1().Services(plan.Namespace.Name).Create(ctx,
					getK8sService(service.ServiceName, plan.Namespace.Name, service.ServicePort), metav1.CreateOptions{})
				if err != nil {
					fail(err, "an error occurred trying to create the service")
					return
				}
			} else {
				fail(err, "an error occurred trying to check for the service")
				return
			}
		}
	}

	_, err = k8s.AppsV1().Deployments(plan.Namespace.Name).Get(ctx, service.ServiceName, metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			// This app hasn't been deployed yet so we need to deploy it
			_, err := k8s.AppsV1().Deployments(plan.Namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
			if err != nil {
				fail(err, "an error occurred trying to create the deployment")
				return
			}
		} else {
			// an error occurred trying to see if the app is already deployed
			fail(err, "an error occurred trying to check for the deployment")
			return
		}
	} else {
		// we were able to retrieve the app which mean we need to run update instead of create
		_, err := k8s.AppsV1().Deployments(plan.Namespace.Name).Update(ctx, deployment, metav1.UpdateOptions{})
		if err != nil {
			fail(err, "an error occurred trying to update the deployment")
			return
		}
	}

	labelSelector := fmt.Sprintf("app=%s,version=%s,nuance=%s", service.ServiceName, service.AvailableVersion, timeNuance)
	pods := k8s.CoreV1().Pods(plan.Namespace.Name)
	watch, err := pods.Watch(ctx, metav1.ListOptions{
		TypeMeta:       metav1.TypeMeta{},
		LabelSelector:  labelSelector,
		TimeoutSeconds: int64Ptr(config.GetConfig().K8sDeployTimeoutSec),
	})
	if err != nil {
		fail(err, "an error occurred trying to watch the pods, deployment may have succeeded")
		return
	}
	started := make(map[string]bool)

	if strings.HasPrefix(service.ServiceName, "eve-sch") {
		instanceCount = 1
	}

	for event := range watch.ResultChan() {
		p, ok := event.Object.(*apiv1.Pod)
		if !ok {
			continue
		}
		for _, x := range p.Status.ContainerStatuses {
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
		pods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			fail(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ServiceName)
			return
		}

		if len(pods.Items) != instanceCount {
			fail(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ServiceName)
			return
		}

		var startedCount int
		for _, x := range pods.Items {
			if x.Status.ContainerStatuses[0].State.Running != nil {
				startedCount += 1
			}
		}

		if startedCount != instanceCount {
			fail(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ServiceName)
			return
		}
	}

	service.Result = eve.DeployArtifactResultSuccess
}

func (s *Scheduler) runDockerMigrationJob(ctx context.Context, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan) {

}
