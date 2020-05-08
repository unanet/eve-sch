package service

import (
	"context"
	"fmt"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
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

}

func (s *Scheduler) runDockerMigrationJob(ctx context.Context, secrets vault.Secrets, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan) {

}
