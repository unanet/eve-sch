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
	secret, err := k8s.CoreV1().Secrets("una-int-prev-1").Get(ctx, "docker-cfg", metav1.GetOptions{})
	require.NoError(t, err)
	fmt.Println(secret)

	_, err = k8s.CoreV1().Namespaces().Get(ctx, "una-int-prev-5", metav1.GetOptions{})
	if k8sErrors.IsNotFound(err) {
		fmt.Println("blah")
	} else {
		fmt.Println("##################################")
	}

}
