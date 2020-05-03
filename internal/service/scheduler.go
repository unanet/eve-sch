package service

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"


	"gitlab.unanet.io/devops/eve/pkg/log"
	"gitlab.unanet.io/devops/eve/pkg/metrics"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/eve/pkg/s3"
	"go.uber.org/zap"
)

const (
	CommandDeployNamespace string = "sch-deploy-namespace"
)

type QueueWorker interface {
	Start(queue.Handler)
	Stop()
	DeleteMessage(ctx context.Context, m *queue.M) error
	// Message sends a message to a different queue given a url, not this one
	Message(ctx context.Context, qUrl string, m *queue.M) error
}

type CloudDownloader interface {
	Download(ctx context.Context, location *s3.Location) ([]byte, error)
}

type CloudUploader interface {
	UploadText(ctx context.Context, key string, body string) (*s3.Location, error)
}

type Scheduler struct {
	worker     QueueWorker
	downloader CloudDownloader
	uploader   CloudUploader
	sigChannel chan os.Signal
	mServer     *http.Server
	done        chan bool
}

func NewScheduler(worker QueueWorker, downloader CloudDownloader, uploader CloudUploader) *Scheduler {
	return &Scheduler{
		worker: worker,
		downloader: downloader,
		uploader: uploader,
		done:       make(chan bool),
		sigChannel: make(chan os.Signal, 1024),
	}
}

func (s *Scheduler) Logger(ctx context.Context) *zap.Logger {
	return queue.GetLogger(ctx)
}

func (s *Scheduler) Start() {
	s.mServer = metrics.StartMetricsServer(GetConfig().MetricsPort)

	signal.Notify(s.sigChannel, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	go s.sigHandler()
	s.worker.Start(queue.HandlerFunc(s.handleMessage))
	<-s.done
	log.Logger.Info("Service Shutdown")
}

func (s *Scheduler) sigHandler() {
	for {
		sig := <-s.sigChannel
		switch sig {
		case syscall.SIGHUP:
			log.Logger.Warn("SIGHUP hit, Nothing supports this currently")
		case os.Interrupt, syscall.SIGTERM, syscall.SIGINT:
			log.Logger.Info("Caught Shutdown Signal", zap.String("signal", sig.String()))
			s.gracefulShutdown()
		}
	}
}

func (s *Scheduler) gracefulShutdown() {
	// Pause the Context for `ShutdownTimeoutSecs` config value
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(120)*time.Second)
	defer cancel()

	// Turn off keepalive
	s.mServer.SetKeepAlivesEnabled(false)

	if err := s.mServer.Shutdown(ctx); err != nil {
		panic("HTTP Metrics Server Failed Graceful Shutdown")
	}
	if err := log.Logger.Sync(); err != nil {
		// not much to do here
	}
	s.worker.Stop()
	close(s.done)
}

