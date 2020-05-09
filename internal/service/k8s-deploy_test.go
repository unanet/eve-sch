// +build local

package service_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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
	ctx := context.TODO()
	client := GetK8sClient(t)
	labelSelector := fmt.Sprintf("app=%s,version=%s", "infocus-web", "2020.2.0.132")
	pods, err := client.CoreV1().Pods("cvs-curr-int").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	require.NoError(t, err)

	for _, x := range pods.Items {
		if x.Status.ContainerStatuses[0].State.Running != nil {

		}

	}

}
