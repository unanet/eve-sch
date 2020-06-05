package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitlab.unanet.io/devops/eve/pkg/errors"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/eve/pkg/log"
	"gitlab.unanet.io/devops/eve/pkg/metrics"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve-sch/internal/fn"
	"gitlab.unanet.io/devops/eve-sch/internal/vault"
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

type SecretsClient interface {
	GetKVSecretString(ctx context.Context, path string, key string) (string, error)
	GetKVSecrets(ctx context.Context, path string) (vault.Secrets, error)
}

type FunctionTrigger interface {
	Post(ctx context.Context, url string, code string, body interface{}) (*fn.Response, error)
}

type Scheduler struct {
	worker     QueueWorker
	downloader eve.CloudDownloader
	uploader   eve.CloudUploader
	sigChannel chan os.Signal
	mServer    *http.Server
	done       chan bool
	apiQUrl    string
	vault      SecretsClient
	fnTrigger  FunctionTrigger
}

func NewScheduler(worker QueueWorker, downloader eve.CloudDownloader, uploader eve.CloudUploader, apiQUrl string, vault SecretsClient, fnTrigger FunctionTrigger) *Scheduler {
	return &Scheduler{
		worker:     worker,
		downloader: downloader,
		uploader:   uploader,
		done:       make(chan bool),
		sigChannel: make(chan os.Signal, 1024),
		apiQUrl:    apiQUrl,
		vault:      vault,
		fnTrigger:  fnTrigger,
	}
}

func (s *Scheduler) Logger(ctx context.Context) *zap.Logger {
	return queue.GetLogger(ctx)
}

func (s *Scheduler) Start() {
	s.mServer = metrics.StartMetricsServer(config.GetConfig().MetricsPort)

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

func (s *Scheduler) failAndLogFn(ctx context.Context, optName string, service *eve.DeployArtifact, plan *eve.NSDeploymentPlan) func(err error, format string, a ...interface{}) {
	return func(err error, format string, a ...interface{}) {
		if len(optName) == 0 {
			format = format + " [artifact:%s]"
			a = append(a, service.ArtifactName)
		} else {
			format = format + " (%s)[artifact:%s]"
			a = append(a, optName, service.ArtifactName)
		}

		plan.Message(format, a...)
		service.Result = eve.DeployArtifactResultFailed
		if err == nil {
			s.Logger(ctx).Error(fmt.Sprintf(format, a...), zap.String("artifact", service.ArtifactName), zap.String("deploy_name", optName))
		} else {
			s.Logger(ctx).Error(fmt.Sprintf(format, a...), zap.String("artifact", service.ArtifactName), zap.String("deploy_name", optName), zap.Error(err))
		}
	}
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

func getDockerImageName(artifact *eve.DeployArtifact) string {
	repo := fmt.Sprintf(DockerRepoFormat, artifact.ArtifactoryFeed)
	return fmt.Sprintf("%s/%s:%s", repo, artifact.ArtifactoryPath, artifact.EvalImageTag())
}
