package service

import (
	"context"
	"fmt"
	"time"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	batchv1 "k8s.io/api/batch/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
)

func (s *Scheduler) runDockerMigrationJob(ctx context.Context, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan) {
	fail := s.failAndLogFn(ctx, migration.DatabaseName, migration.DeployArtifact, plan)

	k8s, err := getK8sClient()
	if err != nil {
		fail(err, "an error occurred trying to get the k8s client")
		return
	}
	jobName := fmt.Sprintf("%s-migration", migration.DatabaseName)
	labelSelector := fmt.Sprintf("job=%s", jobName)
	imageName := getDockerImageName(migration.DeployArtifact)
	job := getK8sMigrationJob(
		jobName,
		plan.Namespace.Name,
		migration.ServiceAccount,
		migration.ArtifactName,
		imageName,
		migration.AvailableVersion,
		migration.RunAs)
	setupJobEnvironment(migration.Metadata, job)

	_ = k8s.BatchV1().Jobs(plan.Namespace.Name).Delete(ctx, jobName, metav1.DeleteOptions{})

	existingPods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{
		TypeMeta:      metav1.TypeMeta{},
		LabelSelector: labelSelector,
	})

	if err == nil {
		for _, x := range existingPods.Items {
			_ = k8s.CoreV1().Pods(plan.Namespace.Name).Delete(ctx, x.Name, metav1.DeleteOptions{})
		}
	}

	for i := 1; i < 60; i++ {
		time.Sleep(1 * time.Second)
		existingPods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{
			TypeMeta:      metav1.TypeMeta{},
			LabelSelector: fmt.Sprintf("job=%s", jobName),
		})
		if err != nil {
			fail(err, "an error occurred trying to wait for old migration jobs to be removed")
			return
		}
		if len(existingPods.Items) == 0 {
			break
		}
	}

	_, err = k8s.BatchV1().Jobs(plan.Namespace.Name).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		fail(err, "an error occurred trying to create the migration job")
		return
	}

	watchPods := k8s.CoreV1().Pods(plan.Namespace.Name)
	watch, err := watchPods.Watch(ctx, metav1.ListOptions{
		TypeMeta:       metav1.TypeMeta{},
		LabelSelector:  labelSelector,
		TimeoutSeconds: int64Ptr(config.GetConfig().K8sDeployTimeoutSec),
	})
	if err != nil {
		fail(err, "an error occurred trying to watch the pod, migration may have succeeded")
		return
	}

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
				migration.Result = eve.DeployArtifactResultFailed
				plan.Message("migration failed, exit code: %d, database: %s", x.State.Terminated.ExitCode, migration.DatabaseName)
				return
			}
		}
	}

	// make sure we don't get a false positive and actually check
	pods, err := k8s.CoreV1().Pods(plan.Namespace.Name).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		fail(nil, "an error occurred while trying to migrate: %s, timed out waiting for migration to finish.", migration.DatabaseName)
		return
	}

	for _, x := range pods.Items {
		if x.Status.ContainerStatuses[0].State.Terminated == nil {
			fail(nil, "an error occurred while trying to migrate: %s, timed out waiting for migration to finish.", migration.DatabaseName)
			return
		}

		if x.Status.ContainerStatuses[0].State.Terminated.ExitCode != 0 {
			fail(nil, "an error occurred while trying to migrate: %s, exit code: %d", migration.DatabaseName, x.Status.ContainerStatuses[0].State.Terminated.ExitCode)
			return
		}
	}

	migration.Result = eve.DeployArtifactResultSuccess
}

func getK8sMigrationJob(
	jobName,
	namespace,
	serviceAccountName,
	artifactName,
	containerImage,
	artifactVersion string, runAs int) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: int32Ptr(0),
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"job": jobName, "version": artifactVersion},
				},
				Spec: apiv1.PodSpec{
					RestartPolicy: apiv1.RestartPolicyNever,
					SecurityContext: &apiv1.PodSecurityContext{
						RunAsUser:  int64Ptr(int64(runAs)),
						RunAsGroup: int64Ptr(int64(runAs)),
						FSGroup:    int64Ptr(65534),
					},
					ServiceAccountName: serviceAccountName,
					Containers: []apiv1.Container{
						{
							Name:            artifactName,
							ImagePullPolicy: apiv1.PullAlways,
							Image:           containerImage,
						},
					},
					ImagePullSecrets: imagePullSecrets,
				},
			},
		},
	}
}
