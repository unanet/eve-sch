package service

import (
	"context"
	"fmt"
	"time"

	"gitlab.unanet.io/devops/eve/pkg/log"
	"go.uber.org/zap"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"

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
		return errors.Wrap(err, "failed to hydrate the k8s job")
	}

	_, err = k8s.BatchV1().Jobs(plan.Namespace.Name).Get(ctx, job.JobName, metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			// This job hasn't been deployed yet so we need to deploy it (create)
			if _, err = k8s.BatchV1().Jobs(plan.Namespace.Name).Create(ctx, newJob, metav1.CreateOptions{}); err != nil {
				return errors.Wrap(err, "an error occurred trying to create the job")
			}
			return nil
		}
		// an error occurred trying to see if the job is already deployed
		return errors.Wrap(err, "an error occurred trying to check for the deployment")
	}

	// OK A Job already exists. Let's cleanup the existing pods, l
	// let's watch the existing pods, to make sure they aren't still running (WIP)
	// Let's watch the pods for results
	//pods := k8s.CoreV1().Pods(plan.Namespace.Name)
	//if pods != nil {
	//
	//}
	//watch, err := pods.Watch(ctx, metav1.ListOptions{
	//	TypeMeta:       metav1.TypeMeta{},
	//	LabelSelector:  jobLabelSelector(job),
	//	TimeoutSeconds: int64Ptr(config.GetConfig().K8sDeployTimeoutSec),
	//})
	//if err != nil {
	//	return errors.Wrap(err, "an error occurred trying to watch the pods, deployment may have succeeded")
	//}

	existingPods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{
		TypeMeta:      metav1.TypeMeta{},
		LabelSelector: jobLabelSelector(job),
	})
	if err == nil && existingPods != nil && len(existingPods.Items) > 0 {
		iterations := 0
	loop:
		iterations++
		runningCount := 0
		for _, x := range existingPods.Items {
			log.Logger.Info("TROY", zap.Any("existing pod status", x.Status))
			if x.Status.Phase == apiv1.PodRunning {
				log.Logger.Info("TROY", zap.Any("runningCount", runningCount))
				runningCount++
			}
			//_ = k8s.CoreV1().Pods(plan.Namespace.Name).Delete(ctx, x.Name, metav1.DeleteOptions{})
		}
		time.Sleep(2 * time.Second)
		if iterations < 30 && runningCount > 0 {
			goto loop
		}
	}
	log.Logger.Info("TROY NO Existing pod")

	//// TODO: We need to wrap this in a common retry/backoff pattern
	//for i := 1; i < 60; i++ {
	//	time.Sleep(1 * time.Second)
	//	existingPods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{
	//		TypeMeta:      metav1.TypeMeta{},
	//		LabelSelector: jobLabelSelector(job),
	//	})
	//	if err != nil {
	//		return errors.Wrap(err, "an error occurred trying to wait for old migration jobs to be removed")
	//	}
	//	// All existing pods are gone...let's break
	//	if len(existingPods.Items) == 0 {
	//		break
	//	}
	//
	//	if i == 59 {
	//		return errors.Wrap(err, "waited 60 seconds for job pods to be removed but some still exist")
	//	}
	//}
	//
	//var dp = metav1.DeletePropagationOrphan
	//
	//k8s.BatchV1().Jobs(plan.Namespace.Name).Delete(ctx, existingJob.Name, metav1.DeleteOptions{
	//	TypeMeta:           jobMetaData,
	//	GracePeriodSeconds: nil,
	//	Preconditions:      nil,
	//	OrphanDependents:   nil,
	//	PropagationPolicy:  &dp,
	//	DryRun:             nil,
	//})

	//log.Logger.Info("TROY", zap.Any("existingJob selector", existingJob.Spec.Selector))
	//log.Logger.Info("TROY", zap.Any("existingJob ObjectMeta.Labels", existingJob.ObjectMeta.Labels))
	//log.Logger.Info("TROY", zap.Any("existingJob Template.ObjectMeta.Labels", existingJob.Spec.Template.ObjectMeta.Labels))
	//
	//newJob.Spec.Selector = existingJob.Spec.Selector
	////newJob.ObjectMeta.Labels = existingJob.ObjectMeta.Labels
	////newJob.Spec.Template.ObjectMeta.Labels = existingJob.Spec.Template.ObjectMeta.Labels
	//
	//newJob.Spec.Template.ObjectMeta.Labels = make(map[string]string)
	//newJob.Spec.Template.ObjectMeta.Labels["controller-uid"] = existingJob.Spec.Template.ObjectMeta.Labels["controller-uid"]
	//newJob.Spec.Template.ObjectMeta.Labels["job-name"] = existingJob.Spec.Template.ObjectMeta.Labels["job-name"]
	//
	//log.Logger.Info("TROY", zap.Any("newJob selector", newJob.Spec.Selector))
	//log.Logger.Info("TROY", zap.Any("newJob ObjectMeta.Labels", newJob.ObjectMeta.Labels))
	//log.Logger.Info("TROY", zap.Any("newJob Template.ObjectMeta.Labels", newJob.Spec.Template.ObjectMeta.Labels))
	//
	//if _, err = k8s.BatchV1().Jobs(plan.Namespace.Name).Update(ctx, newJob, metav1.UpdateOptions{TypeMeta: jobMetaData}); err != nil {
	//	return errors.Wrap(err, "an error occurred trying to update the existing job")
	//}
	//log.Logger.Info("TROY", zap.Any("success update", true))
	return nil
}

func (s *Scheduler) watchJobPods(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	plan *eve.NSDeploymentPlan,
	job *eve.DeployJob,
) error {
	pods := k8s.CoreV1().Pods(plan.Namespace.Name)
	if pods == nil {
		return nil
	}
	watch, err := pods.Watch(ctx, metav1.ListOptions{
		TypeMeta:       metav1.TypeMeta{},
		LabelSelector:  jobLabelSelector(job),
		TimeoutSeconds: int64Ptr(config.GetConfig().K8sDeployTimeoutSec),
	})
	if err != nil {
		return errors.Wrap(err, "an error occurred trying to watch the pods, deployment may have succeeded")
	}

	//started := make(map[string]bool)

	//log.Logger.Info("TROY newJob watch pods")
	//for event := range watch.ResultChan() {
	//	log.Logger.Info("TROY newJob watch pods", zap.Any("event", event))
	//	p, ok := event.Object.(*apiv1.Pod)
	//	if !ok {
	//		continue
	//	}
	//	log.Logger.Info("TROY POD Event", zap.Any("container status", p))
	//	for _, x := range p.Status.ContainerStatuses {
	//		log.Logger.Info("TROY Container Status", zap.Any("container status", x))
	//		if x.LastTerminationState.Terminated != nil {
	//			job.ExitCode = int(x.LastTerminationState.Terminated.ExitCode)
	//			watch.Stop()
	//			return nil
	//		}
	//
	//		if !x.Ready {
	//			continue
	//		}
	//		started[p.Name] = true
	//	}
	//
	//	log.Logger.Info("TROY started pods count", zap.Any("started", len(started)))
	//	if len(started) >= 1 {
	//		watch.Stop()
	//	}
	//}
	//log.Logger.Info("TROY Done watching pods")
	//return nil

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
				job.Result = eve.DeployArtifactResultFailed
				plan.Message("job failed, exit code: %d, job: %s", x.State.Terminated.ExitCode, job.JobName)
				return fmt.Errorf("job failed with exit code: %v", x.State.Terminated.ExitCode)
			}
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
