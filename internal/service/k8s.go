package service

import (
	"context"
	"fmt"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"go.uber.org/zap"
	k8sErros "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gitlab.unanet.io/devops/eve-sch/internal/vault"
)

const (
	DockerRepoFormat = "unanet-%s.jfrog.io"
)

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

	// get pods in all the namespaces by omitting namespace
	// Or specify namespace to get pods in particular namespace
	pods, err := client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))

	// Examples for error handling:
	// - Use helper functions e.g. errors.IsNotFound()
	// - And/or cast to StatusError and use its properties like e.g. ErrStatus.Message
	_, err = client.CoreV1().Pods("default").Get(context.TODO(), "example-xxxxx", metav1.GetOptions{})
	if k8sErros.IsNotFound(err) {
		fmt.Printf("Pod example-xxxxx not found in default namespace\n")
	} else if statusError, isStatus := err.(*k8sErros.StatusError); isStatus {
		fmt.Printf("Error getting pod %v\n", statusError.ErrStatus.Message)
	} else if err != nil {
		panic(err.Error())
	} else {
		fmt.Printf("Found example-xxxxx pod in default namespace\n")
	}

}

func (s *Scheduler) runDockerMigrationJob(ctx context.Context, secrets vault.Secrets, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan) {

}
