// +build local

package service

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gitlab.unanet.io/devops/eve-sch/internal/config"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"gitlab.unanet.io/devops/eve-sch/internal/fn"
	"gitlab.unanet.io/devops/eve-sch/internal/vault"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/eve/pkg/s3"
	appsv1 "k8s.io/api/apps/v1"
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
	mockFnTrigger       = newMockFnTrigger()
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

func TestScheduler_hydrateK8sDeployment(t *testing.T) {

	type fields struct {
		worker     QueueWorker
		downloader eve.CloudDownloader
		uploader   eve.CloudUploader
		sigChannel chan os.Signal
		done       chan bool
		apiQUrl    string
		vault      SecretsClient
		fnTrigger  FunctionTrigger
	}
	type args struct {
		ctx     context.Context
		plan    *eve.NSDeploymentPlan
		service *eve.DeployService
		nuance  string
	}

	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *appsv1.Deployment
		wantErr bool
	}{
		{
			name: "happy",
			fields: fields{
				worker:     mockQueueWorker,
				downloader: mockCloudDownloader,
				uploader:   mockCloudUploader,
				sigChannel: make(chan os.Signal, 1024),
				done:       make(chan bool),
				apiQUrl:    "https://idontknow.com/some-queue",
				vault:      mockSecretClient,
				fnTrigger:  mockFnTrigger,
			},
			args: args{
				ctx:     context.Background(),
				plan:    mockDeploymentPlan,
				service: mockDeployService,
				nuance:  mockNuance,
			},
			want: expectedDeployment(mockDeploymentPlan, mockDeployService),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Scheduler{
				worker:     tt.fields.worker,
				downloader: tt.fields.downloader,
				uploader:   tt.fields.uploader,
				sigChannel: tt.fields.sigChannel,
				done:       tt.fields.done,
				apiQUrl:    tt.fields.apiQUrl,
				vault:      tt.fields.vault,
				fnTrigger:  tt.fields.fnTrigger,
			}
			got, err := s.hydrateK8sDeployment(tt.args.ctx, tt.args.plan, tt.args.service, tt.args.nuance)
			if err != nil && tt.wantErr == false {
				t.Errorf("didn't want an error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil && tt.wantErr {
				t.Errorf("wanted an error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got == nil {
				t.Error("nil k8s deployments")
				return
			}

			if got.Namespace != tt.want.Namespace {
				t.Error("invalid deployment namespace")
			}
			if got.ClusterName != tt.want.ClusterName {
				t.Error("invalid deployment cluster name")
			}

			if reflect.DeepEqual(got.Spec.Template.Spec.SecurityContext, tt.want.Spec.Template.Spec.SecurityContext) == false {
				t.Error("invalid deployment Spec SecurityContext")
			}
			if reflect.DeepEqual(got.Spec.Template.Spec.Containers[0].ReadinessProbe, tt.want.Spec.Template.Spec.Containers[0].ReadinessProbe) == false {
				t.Error("invalid deployment Spec ReadinessProbe")
			}
			if reflect.DeepEqual(got.Spec.Template.Spec.Containers[0].LivenessProbe, tt.want.Spec.Template.Spec.Containers[0].LivenessProbe) == false {
				t.Error("invalid deployment Spec LivenessProbe")
			}
			if reflect.DeepEqual(got.Spec.Template.Spec.Containers[0].Resources, tt.want.Spec.Template.Spec.Containers[0].Resources) == false {
				t.Error("invalid deployment Spec Resources")
			}
			if reflect.DeepEqual(got.Spec.Template.Spec.NodeSelector, tt.want.Spec.Template.Spec.NodeSelector) == false {
				t.Error("invalid deployment Spec NodeSelector")
			}
			if reflect.DeepEqual(got.Spec.Selector, tt.want.Spec.Selector) == false {
				t.Error("invalid deployment Spec Selector")
			}

			if reflect.DeepEqual(got.Spec.Template, tt.want.Spec.Template) {
				t.Log("Deployments Spec Template Equal")
			} else {
				t.Error("invalid deployment Spec Template")
			}

			if reflect.DeepEqual(got.Spec, tt.want.Spec) {
				t.Log("Deployments Specs Equal")
			} else {
				t.Error("invalid deployment Spec")
			}
			if reflect.DeepEqual(got.ObjectMeta, tt.want.ObjectMeta) {
				t.Log("Deployments ObjectMeta Equal")
			} else {
				t.Error("invalid Deployments ObjectMeta")
			}

			if reflect.DeepEqual(got, tt.want) {
				t.Log("Deployments Equal")
			} else {
				t.Error("invalid Deployments")
			}

			deploymentDef, _ := json.Marshal(got)
			fmt.Println(string(deploymentDef))

		})
	}
}

//var nodeSelectorDefinition = []byte("{\"spec\": {\"nodeSelector\": {\"node-group\": \"shared\"}}}")

func expectedDeployment(plan *eve.NSDeploymentPlan, service *eve.DeployService) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		TypeMeta: deploymentMetaData,
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.ServiceName,
			Namespace: plan.Namespace.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(service.Count),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": service.ServiceName,
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      deploymentLabels(service, mockNuance),
					Annotations: deploymentAnnotations(service),
				},
				Spec: apiv1.PodSpec{
					SecurityContext: &apiv1.PodSecurityContext{
						RunAsUser:  int64Ptr(int64(service.RunAs)),
						RunAsGroup: int64Ptr(int64(service.RunAs)),
						FSGroup:    int64Ptr(65534),
					},
					ServiceAccountName: service.ServiceAccount,
					Containers: []apiv1.Container{
						{
							Name:            service.ArtifactName,
							ImagePullPolicy: apiv1.PullAlways,
							Image:           getDockerImageName(service.DeployArtifact),
							Ports:           getServiceContainerPorts(service),
							Env:             containerEnvVars(plan.DeploymentID, service.DeployArtifact),
						},
					},
					TerminationGracePeriodSeconds: int64Ptr(300),
					ImagePullSecrets:              imagePullSecrets,
				},
			},
		},
	}

	var readinessProbe *apiv1.Probe
	_ = json.Unmarshal(service.ReadinessProbe, &readinessProbe)
	deployment.Spec.Template.Spec.Containers[0].ReadinessProbe = readinessProbe

	var livelinessProbe *apiv1.Probe
	_ = json.Unmarshal(service.LivelinessProbe, &livelinessProbe)
	deployment.Spec.Template.Spec.Containers[0].LivenessProbe = livelinessProbe

	var podResource PodResource
	_ = json.Unmarshal(service.PodResource, &podResource)
	deployment.Spec.Template.Spec.Containers[0].Resources = apiv1.ResourceRequirements{
		Requests: podResource.Request,
		Limits:   podResource.Limit,
	}

	if config.GetConfig().EnableNodeGroup {
		if deployment.Spec.Template.Spec.NodeSelector == nil {
			deployment.Spec.Template.Spec.NodeSelector = map[string]string{
				"node-group": "shared",
			}
		} else {
			deployment.Spec.Template.Spec.NodeSelector["node-group"] = "shared"
		}
	}

	return deployment
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
			Labels:              make(eve.MetadataField),
			Annotations:         make(eve.MetadataField),
			Metadata:            make(eve.MetadataField),
			DefinitionSpec:      make(eve.MetadataField),
			ArtifactoryFeed:     "artifact-feed",
			ArtifactoryPath:     "artifact-path",
			ArtifactFnPtr:       "",
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
		StickySessions:   false,
		Count:            2,
		Definition:       nil,
		LivelinessProbe:  []byte("{\"httpGet\": {\"path\": \"/liveliness\", \"port\": 3000}, \"periodSeconds\": 30, \"initialDelaySeconds\": 10}"),
		ReadinessProbe:   []byte("{\"httpGet\": {\"path\": \"/readiness\", \"port\": 3000}, \"periodSeconds\": 5, \"initialDelaySeconds\": 3}"),
		Autoscaling:      []byte("{\"enabled\": true, \"replicas\": {\"max\": 4, \"min\": 2}, \"utilization\": {\"cpu\": 85, \"memory\": 110}}"),
		PodResource:      []byte("{\"limit\": {\"cpu\": \"1000m\", \"memory\": \"250M\"}, \"request\": {\"cpu\": \"200m\", \"memory\": \"50M\"}}"),
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

type MockFnTrigger struct {
}

func (mft *MockFnTrigger) Post(ctx context.Context, url string, code string, body interface{}) (*fn.Response, error) {
	return &fn.Response{
		Result:   "something totally fake",
		Messages: nil,
	}, nil
}

func newMockFnTrigger() *MockFnTrigger {
	return &MockFnTrigger{}
}

type MockSecretClient struct {
}

func (msc *MockSecretClient) GetKVSecretString(ctx context.Context, path string, key string) (string, error) {
	return "supersecretstuffhere", nil
}

func (msc *MockSecretClient) GetKVSecrets(ctx context.Context, path string) (vault.Secrets, error) {
	result := make(map[string]string)
	result["fakekey"] = "fakevalue"
	return result, nil
}

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
