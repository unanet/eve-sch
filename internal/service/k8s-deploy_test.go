// +build local

package service_test

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/utils/templates"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	service2 "gitlab.unanet.io/devops/eve-sch/internal/service"
)

func int32Ptr(i int32) *int32 { return &i }

func int64Ptr(i int64) *int64 { return &i }

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
	service := service2.TemplateServiceData{
		Plan: &eve.NSDeploymentPlan{
			DeploymentID: uuid.UUID{},
			Namespace: &eve.NamespaceRequest{
				ID:          0,
				Alias:       "",
				Name:        "",
				ClusterID:   0,
				ClusterName: "",
			},
			EnvironmentName: "",
			Services:        nil,
			Migrations:      nil,
			Messages:        nil,
			SchQueueUrl:     "",
			CallbackURL:     "",
			Status:          "",
		},
		Service: &eve.DeployService{
			DeployArtifact: &eve.DeployArtifact{
				ArtifactID:          0,
				ArtifactName:        "",
				RequestedVersion:    "",
				DeployedVersion:     "",
				AvailableVersion:    "",
				Metadata:            nil,
				ArtifactoryFeed:     "",
				ArtifactoryPath:     "",
				ArtifactFnPtr:       "",
				ArtifactoryFeedType: "",
				Result:              "",
				Deploy:              false,
			},
			ServiceID: 0,
		},
	}
	json := "{\"cluster\": \"{{ Plan.Namespace.ClusterName }}\", \"namespace\": \"{{ .Plan.Namespace.Alias }}\", \"environment\": \"{{ .Plan.EnvironmentName }}\", \"artifact_name\": \"{{ .Service.ArtifactName }}\", \"artifact_path\": \"{{ .Service.ArtifactoryPath }}\", \"artifact_repo\": \"{{ .Service.ArtifactoryFeed }}\", \"artifact_version\": \"{{ .Service.AvailableVersion }}\", \"inject_vault_paths\": \"{{ .Plan.Namespace.ClusterName }}\"}"
	temp, err := templates.Parse(json)
	require.NoError(t, err)
	var b bytes.Buffer
	temp.Execute(&b, service)
	fmt.Println(b.String())
}
