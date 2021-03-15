package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"gitlab.unanet.io/devops/eve-sch/internal/api"
	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve-sch/internal/service"
	"gitlab.unanet.io/devops/eve-sch/internal/service/callback"
	"gitlab.unanet.io/devops/eve-sch/internal/vault"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/eve/pkg/s3"
	"gitlab.unanet.io/devops/go/pkg/log"
	"go.uber.org/zap"
)

func main() {
	c := config.GetConfig()
	awsSession, err := session.NewSession(&aws.Config{
		Region: aws.String(c.AWSRegion),
	})
	if err != nil {
		log.Logger.Panic("failed to create aws session", zap.Error(err))
	}

	vaultClient, err := vault.NewClient()
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

	worker := queue.NewWorker("eve-sch", schQueue, c.SchQWorkerTimeout)
	scheduler := service.NewScheduler(worker, s3Downloader, s3Uploader, c.ApiQUrl, vaultClient)
	scheduler.Start()

	callbackManager := callback.NewManager(worker, c.ApiQUrl)
	controllers, err := api.InitializeControllers(callbackManager)
	if err != nil {
		log.Logger.Panic("Unable to Initialize the Controllers")
	}

	apiServer, err := api.NewApi(controllers, c)
	if err != nil {
		log.Logger.Panic("Failed to Create Api App", zap.Error(err))
	}

	apiServer.Start(scheduler.Stop)
}
