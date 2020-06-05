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

	labelSelector := fmt.Sprintf("job=%s", "unaneta-migration")
	// make sure we don't get a false positive and actually check
	pods, err := k8s.CoreV1().Pods("una-int-current").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	require.NoError(t, err)
	require.True(t, len(pods.Items) > 0)
	for _, x := range pods.Items {
		require.NotNil(t, x.Status.ContainerStatuses[0].State.Terminated)
		fmt.Println(x.Status.ContainerStatuses[0].State.Terminated)
	}

}
