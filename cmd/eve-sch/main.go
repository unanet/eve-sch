package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/unanet/eve-sch/internal/api"
	"github.com/unanet/eve-sch/internal/config"
	"github.com/unanet/eve-sch/internal/service"
	"github.com/unanet/eve-sch/internal/service/callback"
	"github.com/unanet/eve/pkg/queue"
	"github.com/unanet/eve/pkg/s3"
	"github.com/unanet/go/pkg/errors"
	"github.com/unanet/go/pkg/log"
	"go.uber.org/zap"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	c := config.GetConfig()
	awsSession, err := session.NewSession(&aws.Config{Region: aws.String(c.AWSRegion)})
	if err != nil {
		log.Logger.Panic("failed to create aws session", zap.Error(err))
	}

	k8sDynamicClient, err := getDynamicK8sClient()
	if err != nil {
		log.Logger.Panic("failed to create the k8s client")
	}

	// TODO: remove this once someone figures out how to use the dynamic client when watching pods
	k8sClient, err := getK8sClient()
	if err != nil {
		log.Logger.Panic("failed to create the k8s client")
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
	worker := queue.NewWorker("eve-sch", schQueue, c.SchQWorkerTimeout)
	scheduler := service.NewScheduler(worker, s3Downloader, s3Uploader, c.ApiQUrl, k8sDynamicClient, k8sClient)
	scheduler.Start()
	callbackManager := callback.NewManager(worker, c.ApiQUrl)

	controllers, err := api.InitializeControllers(callbackManager)
	if err != nil {
		log.Logger.Panic("Unable to Initialize the Controllers")
	}

	api.NewApi(controllers, c).Start(scheduler.Stop)
}

func getDynamicK8sClient() (dynamic.Interface, error) {

	c, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrap(err)
	}

	client, err := dynamic.NewForConfig(c)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return client, nil
}

func getK8sClient() (*kubernetes.Clientset, error) {
	c, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrap(err)
	}

	client, err := kubernetes.NewForConfig(c)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return client, nil
}
