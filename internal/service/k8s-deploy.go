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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
)

const (
	DockerRepoFormat = "unanet-%s.jfrog.io"
)

func int32Ptr(i int32) *int32 { return &i }

func int64Ptr(i int64) *int64 { return &i }

func getDockerImageName(artifact *eve.DeployArtifact) string {
	repo := fmt.Sprintf(DockerRepoFormat, artifact.ArtifactoryFeed)
	return fmt.Sprintf("%s/%s:%s", repo, artifact.ArtifactoryPath, artifact.AvailableVersion)
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

func getK8sDeployment(instanceCount int32, artifactName, artifactVersion, namespace, containerImage string) *appsv1.Deployment {
	timeNuance := strconv.Itoa(int(time.Now().Unix()))
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      artifactName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(instanceCount),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":     artifactName,
					"version": artifactVersion,
					"nuance":  timeNuance,
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     artifactName,
						"version": artifactVersion,
						"nuance":  timeNuance,
					},
				},
				Spec: apiv1.PodSpec{
					ServiceAccountName: "unanet",
					Containers: []apiv1.Container{
						{
							Name:            artifactName,
							ImagePullPolicy: apiv1.PullAlways,
							Image:           containerImage,
							Ports: []apiv1.ContainerPort{
								{
									Name:          "http",
									Protocol:      apiv1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
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

func setupVaultInjection(paths []string, deployment *appsv1.Deployment) {
	if len(paths) == 0 {
		return
	}

	annotations := map[string]string{
		"vault.hashicorp.com/agent-inject":            "true",
		"vault.hashicorp.com/agent-pre-populate-only": "true",
		"vault.hashicorp.com/role":                    "eve-sch",
	}

	for _, x := range paths {
		annotationPath := strings.ReplaceAll(x, "/", "-")
		annotations[fmt.Sprintf("vault.hashicorp.com/agent-inject-secret-%s", annotationPath)] = fmt.Sprintf("kv/data/%s", x)
		annotations[fmt.Sprintf("vault.hashicorp.com/agent-inject-template-%s", annotationPath)] = fmt.Sprintf(`
{{- with secret "kv/data/%s" -}}
{{ range $k, $v := .Data.data }}
export {{ $k | toUpper }}={{ $v }}
{{ end }}
{{- end }}
`, x)
	}
	// inject vault variables
	deployment.Spec.Template.ObjectMeta.Annotations = annotations
}

func (s *Scheduler) deployDockerService(ctx context.Context, service *eve.DeployService, plan *eve.NSDeploymentPlan, vaultPaths []string) {
	fail := s.failAndLogFn(ctx, service.DeployArtifact, plan)
	k8s, err := getK8sClient()
	if err != nil {
		fail(err, "an error occurred trying to get the k8s client")
		return
	}
	var instanceCount int32 = 2
	imageName := getDockerImageName(service.DeployArtifact)
	deployment := getK8sDeployment(instanceCount, service.ArtifactName, service.AvailableVersion, plan.Namespace.Name, imageName)
	setupEnvironment(service.Metadata, deployment)
	setupVaultInjection(vaultPaths, deployment)

	_, err = k8s.AppsV1().Deployments(plan.Namespace.Name).Get(ctx, service.ArtifactName, metav1.GetOptions{})
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

	labelSelector := fmt.Sprintf("app=%s,version=%s", service.ArtifactName, service.AvailableVersion)
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

		if len(started) == int(instanceCount) {
			watch.Stop()
		}
	}

	if len(started) != int(instanceCount) {
		// make sure we don't get a false positive and actually check
		pods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			fail(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ArtifactName)
			return
		}

		if len(pods.Items) != int(instanceCount) {
			fail(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ArtifactName)
			return
		}

		var startedCount int
		for _, x := range pods.Items {
			if x.Status.ContainerStatuses[0].State.Running != nil {
				startedCount += 1
			}
		}

		if startedCount != int(instanceCount) {
			fail(nil, "an error occurred while trying to deploy: %s, timed out waiting for app to start.", service.ArtifactName)
			return
		}
	}

	service.Result = eve.DeployArtifactResultSuccess
}

func (s *Scheduler) runDockerMigrationJob(ctx context.Context, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan, vaultPaths []string) {

}
