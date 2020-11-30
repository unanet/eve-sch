package service

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	"k8s.io/client-go/kubernetes"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	batchv1 "k8s.io/api/batch/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	jobMetaData = metav1.TypeMeta{
		Kind:       "Job",
		APIVersion: "batch/v1",
	}
)

// TODO: Remove this once migrations are removed and we are full on "Job"
//  this is still being used bu the k8s-migrations.go setup
func setupJobEnvironment(metadata map[string]interface{}, job *batchv1.Job) {
	var containerEnvVars []apiv1.EnvVar

	for k, v := range metadata {
		value, ok := v.(string)
		if !ok {
			continue
		}
		containerEnvVars = append(containerEnvVars, apiv1.EnvVar{
			Name:  k,
			Value: value,
		})
	}

	job.Spec.Template.Spec.Containers[0].Env = containerEnvVars
}

func jobLabelSelector(job *eve.DeployJob) string {
	return fmt.Sprintf("job=%s", job.JobName)
}

func jobMatchLabels(job *eve.DeployJob) map[string]string {
	return map[string]string{
		"job":     job.JobName,
		"version": job.AvailableVersion,
	}
}

func (s *Scheduler) hydrateK8sJob(ctx context.Context, plan *eve.NSDeploymentPlan, job *eve.DeployJob) (*batchv1.Job, error) {
	return &batchv1.Job{
		TypeMeta: jobMetaData,
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.JobName,
			Namespace: plan.Namespace.Name,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: int32Ptr(0),
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: jobMatchLabels(job),
				},
				Spec: apiv1.PodSpec{
					RestartPolicy: apiv1.RestartPolicyNever,
					SecurityContext: &apiv1.PodSecurityContext{
						RunAsUser:  int64Ptr(int64(job.RunAs)),
						RunAsGroup: int64Ptr(int64(job.RunAs)),
						FSGroup:    int64Ptr(65534),
					},
					ServiceAccountName: job.ServiceAccount,
					Containers: []apiv1.Container{
						{
							Name:            job.ArtifactName,
							ImagePullPolicy: apiv1.PullAlways,
							Image:           getDockerImageName(job.DeployArtifact),
							Env:             containerEnvVars(job.Metadata),
						},
					},
					ImagePullSecrets: imagePullSecrets,
				},
			},
		},
	}, nil
}

func (s *Scheduler) setupK8sJob(ctx context.Context, k8s *kubernetes.Clientset, plan *eve.NSDeploymentPlan, job *eve.DeployJob) error {
	newJob, err := s.hydrateK8sJob(ctx, plan, job)
	if err != nil {
		return errors.Wrap(err, "failed to hydrate the k8s job object")
	}

	// Delete the Job if it exists (swallow the error)
	_ = k8s.BatchV1().Jobs(plan.Namespace.Name).Delete(ctx, job.JobName, metav1.DeleteOptions{})

	existingPods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{
		TypeMeta:      metav1.TypeMeta{},
		LabelSelector: jobLabelSelector(job),
	})
	if err == nil {
		for _, x := range existingPods.Items {
			_ = k8s.CoreV1().Pods(plan.Namespace.Name).Delete(ctx, x.Name, metav1.DeleteOptions{})
		}
	}

	if _, err = k8s.BatchV1().Jobs(plan.Namespace.Name).Create(ctx, newJob, metav1.CreateOptions{}); err != nil {
		return errors.Wrap(err, "an error occurred trying to create the job")
	}
	return nil
}

func (s *Scheduler) watchJobPods(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	plan *eve.NSDeploymentPlan,
	job *eve.DeployJob,
) error {
	pods := k8s.CoreV1().Pods(plan.Namespace.Name)
	watch, err := pods.Watch(ctx, metav1.ListOptions{
		TypeMeta:       metav1.TypeMeta{},
		LabelSelector:  jobLabelSelector(job),
		TimeoutSeconds: int64Ptr(config.GetConfig().K8sDeployTimeoutSec),
	})
	if err != nil {
		return errors.Wrap(err, "an error occurred trying to watch the pods, deployment may have succeeded")
	}
	started := make(map[string]bool)

	for event := range watch.ResultChan() {
		p, ok := event.Object.(*apiv1.Pod)
		if !ok {
			continue
		}
		for _, x := range p.Status.ContainerStatuses {
			if x.LastTerminationState.Terminated != nil {
				job.ExitCode = int(x.LastTerminationState.Terminated.ExitCode)
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
		}
	}
	return nil
}

func (s *Scheduler) runDockerJob(ctx context.Context, job *eve.DeployJob, plan *eve.NSDeploymentPlan) {
	fail := s.failAndLogFn(ctx, job.JobName, job.DeployArtifact, plan)
	logFn := s.logMessageFn(job.JobName, job.DeployArtifact, plan)
	k8s, err := getK8sClient()
	if err != nil {
		fail(err, "an error occurred trying to get the k8s client")
		return
	}

	// Setup the K8s Job
	if err := s.setupK8sJob(ctx, k8s, plan, job); err != nil {
		fail(err, "an error occurred setting up the k8s job")
		return
	}

	// Let's watch the pods for results
	if err := s.watchJobPods(ctx, k8s, plan, job); err != nil {
		fail(err, "an error occurred while watching k8s job pods")
		return
	}

	if job.ExitCode != 0 {
		logFn("pod failed to start and returned a non zero exit code: %d", job.ExitCode)
		validExitCodes, err := expandSuccessExitCodes(job.SuccessExitCodes)
		if err != nil {
			fail(err, "an error occurred parsing valid exit codes for the service")
			return
		}

		if !intContains(validExitCodes, job.ExitCode) {
			job.Result = eve.DeployArtifactResultFailed
		}
	}

	// if we've set it to a failure above somewhere, we don't want to now state it's succeeded.
	if job.Result == eve.DeployArtifactResultNoop {
		job.Result = eve.DeployArtifactResultSuccess
	}
}
