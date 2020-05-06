package service

import (
	"context"
	"fmt"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gitlab.unanet.io/devops/eve-sch/internal/vault"
)

const (
	DockerRepoFormat = "unanet-%s.jfrog.io"
)

func int32Ptr(i int32) *int32 { return &i }

func getDockerImagePath(artifact eve.DeployArtifact) string {
	repo := fmt.Sprintf(DockerRepoFormat, artifact.ArtifactoryFeed)
	return fmt.Sprintf("%s/%s/%s:%s", repo, artifact.ArtifactoryPath, artifact.ArtifactName, artifact.AvailableVersion)
}

func getKubernetesClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrap(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return clientset, nil
}

func (s *Scheduler) deployDockerService(ctx context.Context, secrets vault.Secrets, service *eve.DeployService, plan *eve.NSDeploymentPlan) {
	client, err := getKubernetesClient()
	if err != nil {
		s.Logger(ctx).Error("there was a problem getting the kubernetes client", zap.Error(err))
		return
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "demo-deployment",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "demo",
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "demo",
					},
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:  "web",
							Image: "nginx:1.12",
							Ports: []apiv1.ContainerPort{
								{
									Name:          "http",
									Protocol:      apiv1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	deploymentsClient := client.AppsV1().Deployments(apiv1.NamespaceDefault)
	_, err = deploymentsClient.Create(context.TODO(), deployment, metav1.CreateOptions{})
	if err != nil {
		s.Logger(ctx).Error("error running deploy", zap.Error(err))
	}

}

func (s *Scheduler) runDockerMigrationJob(ctx context.Context, secrets vault.Secrets, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan) {

}
