package service

import (
	"context"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve/pkg/eve"
)

// Public CONST
const (
	DockerRepoFormat = "unanet-%s.jfrog.io"
	TimeoutExitCode  = -999999
)

// deploy is the entry point for Eve Jobs and Eve Services
func (s *Scheduler) deploy(ctx context.Context, deployment eve.DeploymentSpec, plan *eve.NSDeploymentPlan) {
	failNLog := s.failAndLogFn(ctx, deployment.GetName(), deployment.GetArtifact(), plan)
	logFn := s.logMessageFn(deployment.GetName(), deployment.GetArtifact(), plan)

	if err := s.deployCRDs(ctx, deployment, plan); err != nil {
		failNLog(err, "failed to deploy resource definitions")
		return
	}

	// We wait/watch for 1 successful pod to come up
	if err := s.watchPods(ctx, deployment, plan); err != nil {
		failNLog(err, "an error occurred while watching k8s deployment pods")
		return
	}

	if deployment.GetExitCode() != 0 {
		if deployment.GetExitCode() == TimeoutExitCode {
			logFn("timeout of %d seconds exceeded while waiting for the deployment to start", config.GetConfig().K8sDeployTimeoutSec)
		} else {
			logFn("pod failed to start and returned a non zero exit code: %d", deployment.GetExitCode())
		}
		validExitCodes, err := expandSuccessExitCodes(deployment.GetSuccessCodes())
		if err != nil {
			failNLog(err, "an error occurred parsing valid exit codes for the deployment")
			return
		}

		if !intContains(validExitCodes, deployment.GetExitCode()) {
			deployment.SetDeployResult(eve.DeployArtifactResultFailed)
		}
	}

	// if we've set it to a failure above somewhere, we don't want to now state it's succeeded.
	if deployment.GetDeployResult() == eve.DeployArtifactResultNoop {
		deployment.SetDeployResult(eve.DeployArtifactResultSuccess)
	}
}
