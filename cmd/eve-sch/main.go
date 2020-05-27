package main

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"gitlab.unanet.io/devops/eve/pkg/log"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/eve/pkg/s3"
	"go.uber.org/zap"

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

	vault, err := vault.NewClient()
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
