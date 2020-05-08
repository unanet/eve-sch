package main

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"gitlab.unanet.io/devops/eve/pkg/log"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/eve/pkg/s3"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve-sch/internal/fn"
	"gitlab.unanet.io/devops/eve-sch/internal/service"
	"gitlab.unanet.io/devops/eve-sch/internal/vault"
)

func main() {
	c := config.GetConfig()
	awsSession, err := session.NewSession(&aws.Config{
		Region: aws.String(c.AWSRegion),
	})
	if err != nil {
		log.Logger.Panic("failed to create aws session", zap.Error(err))
	}

	vault, err := vault.NewClient(c.VaultConfig)
	if err != nil {
		log.Logger.Panic("failed to create the vault secrets client")
	}

	schQueue := queue.NewQ(awsSession, queue.Config{
		MaxNumberOfMessage: c.SchQMaxNumberOfMessage,
		QueueURL:           c.SchQUrl,
		WaitTimeSecond:     c.SchQWaitTimeSecond,
		VisibilityTimeout:  c.SchQVisibilityTimeout,
	})

	s3Uploader := s3.NewUploader(awsSession, s3.Config{
		Bucket: c.S3Bucket,
	})

	s3Downloader := s3.NewDownloader(awsSession)

	fnTrigger := fn.NewTrigger(1 * time.Hour)

	worker := queue.NewWorker("eve-sch", schQueue, c.SchQWorkerTimeout)
	service.NewScheduler(worker, s3Downloader, s3Uploader, c.ApiQUrl, vault, fnTrigger).Start()
}

func kube() {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	for {
		// get pods in all the namespaces by omitting namespace
		// Or specify namespace to get pods in particular namespace
		pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))

		// Examples for error handling:
		// - Use helper functions e.g. errors.IsNotFound()
		// - And/or cast to StatusError and use its properties like e.g. ErrStatus.Message
		_, err = clientset.CoreV1().Pods("default").Get(context.TODO(), "example-xxxxx", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			fmt.Printf("Pod example-xxxxx not found in default namespace\n")
		} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
			fmt.Printf("Error getting pod %v\n", statusError.ErrStatus.Message)
		} else if err != nil {
			panic(err.Error())
		} else {
			fmt.Printf("Found example-xxxxx pod in default namespace\n")
		}

		time.Sleep(10 * time.Second)
	}
}
