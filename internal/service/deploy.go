package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/unanet/eve-sch/internal/config"
	"github.com/unanet/eve/pkg/eve"
)

// Public CONST
const (
	DockerRepoFormat = "plainsight.jfrog.io/%s"
	TimeoutExitCode  = 124
)

func intContains(s []int, e int) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func expandSuccessExitCodes(successExitCodes string) ([]int, error) {
	var r []int
	var last int
	for _, part := range strings.Split(successExitCodes, ",") {
		if i := strings.Index(part[1:], "-"); i == -1 {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("success_exit_code parse error, parts are not a valid int: %s", err.Error())
			}
			if len(r) > 0 {
				if last == n {

					return nil, fmt.Errorf("success_exit_code parse error, duplicate value: %d", n)
				} else if last > n {
					return nil, fmt.Errorf("success_exit_code parse error, values not ordered: %d", n)
				}
			}
			r = append(r, n)
			last = n
		} else {
			n1, err := strconv.Atoi(part[:i+1])
			if err != nil {
				return nil, fmt.Errorf("success_exit_code parse error, parts are not a valid int: %s", err.Error())
			}
			n2, err := strconv.Atoi(part[i+2:])
			if err != nil {
				return nil, fmt.Errorf("success_exit_code parse error, parts are not a valid int: %s", err.Error())
			}
			if n2 < n1+2 {
				return nil, fmt.Errorf("success_exit_code parse error, invalid range: %s", part)
			}
			if len(r) > 0 {
				if last == n1 {
					return nil, fmt.Errorf("success_exit_code parse error, duplicate value: %d", n1)
				} else if last > n1 {
					return nil, fmt.Errorf("success_exit_code parse error, values not ordered: %d", n1)
				}
			}
			for i = n1; i <= n2; i++ {
				r = append(r, i)
			}
			last = n2
		}
	}

	return r, nil
}

// deploy is the entry point for Eve Jobs and Eve Services
func (s *Scheduler) deploy(ctx context.Context, deployment eve.DeploymentSpec, plan *eve.NSDeploymentPlan) {
	failNLog := s.failAndLogFn(ctx, deployment.GetName(), deployment.GetArtifact(), plan)
	logFn := s.logMessageFn(deployment.GetName(), deployment.GetArtifact(), plan)

	if err := s.deployCRDs(ctx, deployment, plan); err != nil {
		failNLog(err, "failed to deploy resource definitions")
		return
	}

	// We wait/watch for 1 successful pod to come up
	if err := s.watchPods(ctx, deployment, plan.Namespace.Name); err != nil {
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
