// +build local

package service

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	// "github.com/unanet/eve-sch/internal/vault"
	"github.com/unanet/eve/pkg/eve"
	"github.com/unanet/eve/pkg/queue"
	"github.com/unanet/eve/pkg/s3"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	mockNuance          = "0000000000001"
	mockQueueWorker     = newMockWorkerQueue()
	mockCloudDownloader = newMockCloudDownloader()
	mockCloudUploader   = newMockCloudUploader()
	mockSecretClient    = newMockSecretsClient()
	mockDeploymentPlan  = newMockDeploymentPlan()
	mockDeployService   = newMockDeployService()
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
	cfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	require.NoError(t, err)

	// create the clientset
	clientset, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	return clientset
}

func TestScheduler_deployNamespace(t *testing.T) {
	k8s := GetK8sClient(t)
	ctx := context.TODO()

	job, err := k8s.BatchV1().Jobs("una-dev-current").Get(ctx, "hello-world", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, job)
}

func TestThis(t *testing.T) {
	k8s := GetK8sClient(t)
	ctx := context.TODO()
	pods := k8s.CoreV1().Pods("una-dev-current")
	watch, err := pods.Watch(ctx, metav1.ListOptions{
		TypeMeta:       metav1.TypeMeta{},
		LabelSelector:  fmt.Sprintf("job=%s", "hello-world"),
		TimeoutSeconds: int64Ptr(10),
	})
	require.NoError(t, err)
	require.NotNil(t, watch)

	started := make(map[string]bool)

	for event := range watch.ResultChan() {
		p, ok := event.Object.(*apiv1.Pod)
		if !ok {
			continue
		}
		for _, x := range p.Status.ContainerStatuses {
			if x.LastTerminationState.Terminated != nil {
				watch.Stop()
			}

			if !x.Ready {
				continue
			}
			started[p.Name] = true
		}

		if len(started) >= 1 {
			watch.Stop()
		}
	}
}

type MockQueueWorker struct {
}

func (mwq *MockQueueWorker) DeleteMessage(ctx context.Context, m *queue.M) error {
	return nil
}

func (mwq *MockQueueWorker) Message(ctx context.Context, qUrl string, m *queue.M) error {
	return nil
}

func (mwq *MockQueueWorker) Start(queue.Handler) {
	return
}

func (mwq *MockQueueWorker) Stop() {
	return
}

func newMockWorkerQueue() *MockQueueWorker {
	return &MockQueueWorker{}
}

func newMockDeployService() *eve.DeployService {

	return &eve.DeployService{
		DeployArtifact: &eve.DeployArtifact{
			ArtifactID:          1,
			ArtifactName:        "test-artifact",
			RequestedVersion:    "0.1",
			DeployedVersion:     "0.1.0.50",
			AvailableVersion:    "0.1.0.100",
			ServiceAccount:      "test-svc-account",
			ImageTag:            "",
			Metadata:            make(eve.MetadataField),
			ArtifactoryFeed:     "artifact-feed",
			ArtifactoryPath:     "artifact-path",
			ArtifactoryFeedType: "docker",
			Result:              "",
			ExitCode:            0,
			RunAs:               1001,
			Deploy:              true,
		},
		ServiceID:        20,
		ServicePort:      3000,
		MetricsPort:      3001,
		ServiceName:      "some-silly-service",
		Count:            2,
		Definition:       nil,
		SuccessExitCodes: "0",
	}
}

func mockDeployServices() eve.DeployServices {
	return make([]*eve.DeployService, 0)
}

func newMockDeploymentPlan() *eve.NSDeploymentPlan {

	return &eve.NSDeploymentPlan{
		DeploymentID: uuid.NewV4(),
		Namespace: &eve.NamespaceRequest{
			ID:          1,
			Alias:       "ns-alias",
			Name:        "full-ns-name",
			ClusterID:   2,
			ClusterName: "test-cluster",
			Version:     "0.1",
		},
		EnvironmentName:   "test-env",
		EnvironmentAlias:  "test",
		Services:          mockDeployServices(),
		Jobs:              nil,
		Messages:          nil,
		SchQueueUrl:       "",
		CallbackURL:       "",
		Status:            "",
		MetadataOverrides: nil,
		Type:              "application",
	}
}

type MockSecretClient struct {
}

func (msc *MockSecretClient) GetKVSecretString(ctx context.Context, path string, key string) (string, error) {
	return "supersecretstuffhere", nil
}

// func (msc *MockSecretClient) GetKVSecrets(ctx context.Context, path string) (vault.Secrets, error) {
// 	result := make(map[string]string)
// 	result["fakekey"] = "fakevalue"
// 	return result, nil
// }

func newMockSecretsClient() *MockSecretClient {
	return &MockSecretClient{}
}

type MockCloudUploader struct {
}

type MockCloudDownloader struct {
}

func (mcd *MockCloudDownloader) Download(ctx context.Context, location *s3.Location) ([]byte, error) {
	return []byte("hello"), nil
}

func newMockCloudDownloader() *MockCloudDownloader {
	return &MockCloudDownloader{}
}

func (mcd *MockCloudUploader) Upload(ctx context.Context, key string, body []byte) (*s3.Location, error) {
	return &s3.Location{
		Bucket: "fake-bucket",
		Key:    "key-key",
		Url:    "https://fakeurl.com",
	}, nil
}

func newMockCloudUploader() *MockCloudUploader {
	return &MockCloudUploader{}
}
