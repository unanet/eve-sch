package service

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/go/pkg/errors"
	"gitlab.unanet.io/devops/go/pkg/log"
	"go.uber.org/zap"
	"k8s.io/client-go/dynamic"
)

const (
	CommandUpdateDeployment string = "api-update-deployment"
	CommandCallbackMessage  string = "api-callback-message"
)

type QueueWorker interface {
	Start(queue.Handler)
	Stop()
	DeleteMessage(ctx context.Context, m *queue.M) error
	// Message sends a message to a different queue given a url, not this one
	Message(ctx context.Context, qUrl string, m *queue.M) error
}

type Scheduler struct {
	worker           QueueWorker
	downloader       eve.CloudDownloader
	uploader         eve.CloudUploader
	sigChannel       chan os.Signal
	done             chan bool
	apiQUrl          string
	k8sDynamicClient dynamic.Interface
	k8sClient        *kubernetes.Clientset
}

func NewScheduler(
	worker QueueWorker,
	downloader eve.CloudDownloader,
	uploader eve.CloudUploader,
	apiQUrl string,
	k8sDynamic dynamic.Interface,
	k8sClient *kubernetes.Clientset,
) *Scheduler {
	return &Scheduler{
		worker:           worker,
		downloader:       downloader,
		uploader:         uploader,
		done:             make(chan bool),
		sigChannel:       make(chan os.Signal, 1024),
		apiQUrl:          apiQUrl,
		k8sDynamicClient: k8sDynamic,
		k8sClient:        k8sClient,
	}
}

func (s *Scheduler) Stop() {
	s.worker.Stop()
	close(s.done)
}

func (s *Scheduler) Logger(ctx context.Context) *zap.Logger {
	return queue.GetLogger(ctx)
}

func (s *Scheduler) Start() {
	go s.start()
}

func (s *Scheduler) start() {
	signal.Notify(s.sigChannel, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	s.worker.Start(queue.HandlerFunc(s.handleMessage))
	<-s.done
	log.Logger.Info("Service Shutdown")
}

func (s *Scheduler) handleMessage(ctx context.Context, m *queue.M) error {
	switch m.Command {
	case queue.CommandDeployNamespace, queue.CommandRestartNamespace:
		return s.deployNamespace(ctx, m)
	default:
		return errors.Wrapf("unrecognized command: %s", m.Command)
	}
}

func (s *Scheduler) deployNamespace(ctx context.Context, m *queue.M) error {
	plan, err := eve.UnMarshalNSDeploymentFromS3LocationBody(ctx, s.downloader, m.Body)
	if err != nil {
		return errors.Wrap(err)
	}

	for _, x := range plan.Services {
		x.Metadata, err = parseServiceMetadata(x.Metadata, x, plan)
		if err != nil {
			plan.Message("could not parse metadata, service: %s, error: %s", x.ArtifactName, err)
		}
		x.Definition, err = parseServiceDefinition(x.Definition, x, plan)
		if err != nil {
			plan.Message("could not parse definition, service: %s, error: %s", x.ArtifactName, err)
		}

		if x.ArtifactoryFeedType == eve.ArtifactoryFeedTypeDocker {
			s.deploy(ctx, x, plan)
		}
	}

	for _, x := range plan.Jobs {
		x.Metadata, err = parseJobMetadata(x.Metadata, x, plan)
		if err != nil {
			plan.Message("could not parse metadata, job: %s, error: %s", x.ArtifactName, err)
		}

		x.Definition, err = parseJobDefinition(x.Definition, x, plan)
		if err != nil {
			plan.Message("could not parse definition, job: %s, error: %s", x.ArtifactName, err)
		}

		if x.ArtifactoryFeedType == eve.ArtifactoryFeedTypeDocker {
			s.deploy(ctx, x, plan)
		}
	}

	err = s.worker.DeleteMessage(ctx, m)
	if err != nil {
		return errors.Wrap(err)
	}

	if plan.Failed() {
		plan.Status = eve.DeploymentPlanStatusErrors
	} else {
		plan.Status = eve.DeploymentPlanStatusComplete
	}

	mBody, err := eve.MarshalNSDeploymentPlanToS3LocationBody(ctx, s.uploader, plan)
	if err != nil {
		return errors.Wrap(err)
	}

	err = s.worker.Message(ctx, s.apiQUrl, &queue.M{
		ID:      m.ID,
		GroupID: CommandUpdateDeployment,
		Body:    mBody,
		Command: CommandUpdateDeployment,
	})
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

func (s *Scheduler) logMessageFn(optName string, service *eve.DeployArtifact, plan *eve.NSDeploymentPlan) func(format string, a ...interface{}) {
	return func(format string, a ...interface{}) {
		if len(optName) == 0 {
			format = format + " [artifact:%s]"
			a = append(a, service.ArtifactName)
		} else {
			format = format + " (%s)[artifact:%s]"
			a = append(a, optName, service.ArtifactName)
		}

		plan.Message(format, a...)
	}
}

func (s *Scheduler) failAndLogFn(ctx context.Context, optName string, service *eve.DeployArtifact, plan *eve.NSDeploymentPlan) func(err error, format string, a ...interface{}) {
	logFn := s.logMessageFn(optName, service, plan)
	return func(err error, format string, a ...interface{}) {
		logFn(format, a...)
		service.Result = eve.DeployArtifactResultFailed

		if err != nil {
			s.Logger(ctx).Error(fmt.Sprintf(format, a...), zap.String("artifact", service.ArtifactName), zap.String("deploy_name", optName), zap.Error(err))
		} else {
			s.Logger(ctx).Warn(fmt.Sprintf(format, a...), zap.String("artifact", service.ArtifactName), zap.String("deploy_name", optName))
		}
	}
}
