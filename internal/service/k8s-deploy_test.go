// +build local

package service

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}

	return os.Getenv("USERPROFILE") // windows
}

func GetK8sClient(t *testing.T) *kubernetes.Clientset {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	require.NoError(t, err)

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	return clientset
}

func TestScheduler_deployNamespace(t *testing.T) {
	k8s := GetK8sClient(t)
	ctx := context.TODO()
	imageName := getDockerImageName(&eve.DeployArtifact{
		ArtifactID:          1,
		ArtifactName:        "infocus-web",
		AvailableVersion:    "2020.2.0.132",
		ArtifactoryFeed:     "docker-int",
		ArtifactoryPath:     "clearview/infocus-web",
		ArtifactoryFeedType: "docker",
	})
	fmt.Println(imageName)
	deployment := getK8sDeployment(2, "infocus-web", "2020.2.0.132", "cvs-curr-int", imageName, "")
	_, err := k8s.AppsV1().Deployments("cvs-curr-int").Get(ctx, "infocus-web", metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			result, err := k8s.AppsV1().Deployments("cvs-curr-int").Create(ctx, deployment, metav1.CreateOptions{})
			require.NoError(t, err)
			fmt.Println(result)
		}
	}

}
