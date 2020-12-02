package service

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gitlab.unanet.io/devops/eve/pkg/log"
	"go.uber.org/zap"
	apiv1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/require"
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

	job, err := k8s.BatchV1().Jobs("una-dev-current").Get(ctx, "hello-world", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, job)
}

func TestThis(t *testing.T) {
	log.Logger.Info("TROY")
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

	log.Logger.Info("TROY Event", zap.Any("container status", ""))
	for event := range watch.ResultChan() {
		p, ok := event.Object.(*apiv1.Pod)
		if !ok {
			continue
		}
		log.Logger.Info("TROY POD Event", zap.Any("container status", p))
		for _, x := range p.Status.ContainerStatuses {
			log.Logger.Info("TROY Container Status", zap.Any("container status", x))
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
