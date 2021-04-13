package service

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	"gitlab.unanet.io/devops/eve-sch/internal/config"

	"k8s.io/apimachinery/pkg/watch"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func deploymentLabelSelector(eveDeployment eve.DeploymentSpec) string {
	return fmt.Sprintf("app=%s,version=%s,nuance=%s", eveDeployment.GetName(), eveDeployment.GetArtifact().AvailableVersion, eveDeployment.GetNuance())
}

func jobLabelSelector(eveDeployment eve.DeploymentSpec) string {
	return fmt.Sprintf("job=%s", eveDeployment.GetName())
}

func (s *Scheduler) getWatcher(ctx context.Context, eveDeployment eve.DeploymentSpec, nameSpace string) (watch.Interface, error) {
	var labelSelector string
	var timeout int64

	cfg := config.GetConfig()

	switch eveDeployment.(type) {
	case *eve.DeployService:
		labelSelector = deploymentLabelSelector(eveDeployment)
		timeout = cfg.K8sDeployTimeoutSec
	case *eve.DeployJob:
		labelSelector = jobLabelSelector(eveDeployment)
		timeout = cfg.K8sJobTimeoutSec
	default:
		return nil, fmt.Errorf("invalid DeploymentSpec type")
	}

	return s.k8sClient.CoreV1().Pods(nameSpace).Watch(ctx, metav1.ListOptions{
		TypeMeta:       metav1.TypeMeta{},
		LabelSelector:  labelSelector,
		TimeoutSeconds: int64Ptr(timeout),
	})

}

func (s *Scheduler) watchPods(
	ctx context.Context,
	eveDeployment eve.DeploymentSpec,
	namespace string,
) error {

	w, err := s.getWatcher(ctx, eveDeployment, namespace)
	if err != nil {
		return errors.Wrap(err, "error trying to get watcher")
	}

	switch eveDeployment.(type) {
	case *eve.DeployService:
		return s.watchServicePods(w, eveDeployment)
	case *eve.DeployJob:
		return s.watchJobPods(w, eveDeployment)
	}

	return fmt.Errorf("invalid pod watch")
}

func (s *Scheduler) watchServicePods(watch watch.Interface, eveDeployment eve.DeploymentSpec) error {
	started := make(map[string]bool)
	for event := range watch.ResultChan() {
		p, ok := event.Object.(*apiv1.Pod)
		if !ok {
			continue
		}
		for _, x := range p.Status.ContainerStatuses {
			if x.LastTerminationState.Terminated != nil {
				eveDeployment.SetExitCode(int(x.LastTerminationState.Terminated.ExitCode))
				watch.Stop()
				return nil
			}

			if !x.Ready {
				continue
			}
			started[p.Name] = true
		}

		if len(started) >= 1 {
			watch.Stop()
			return nil
		}
	}
	eveDeployment.SetExitCode(TimeoutExitCode)
	return nil
}

func (s *Scheduler) watchJobPods(watch watch.Interface, eveDeployment eve.DeploymentSpec) error {
	for event := range watch.ResultChan() {
		p, ok := event.Object.(*apiv1.Pod)
		if !ok {
			continue
		}
		for _, x := range p.Status.ContainerStatuses {
			if x.State.Terminated == nil {
				continue
			}
			watch.Stop()
			if x.State.Terminated.ExitCode != 0 {
				eveDeployment.SetExitCode(int(x.State.Terminated.ExitCode))
			}
			return nil
		}
	}
	eveDeployment.SetExitCode(TimeoutExitCode)
	return nil
}
