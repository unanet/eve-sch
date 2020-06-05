package service

import (
	"context"
	"fmt"

	"gitlab.unanet.io/devops/eve/pkg/eve"
	batchv1 "k8s.io/api/batch/v1"

	apiv1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
)

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

func (s *Scheduler) runDockerMigrationJob(ctx context.Context, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan) {
	fail := s.failAndLogFn(ctx, migration.DeployArtifact, plan)
	k8s, err := getK8sClient()
	if err != nil {
		fail(err, "an error occurred trying to get the k8s client")
		return
	}
	imageName := getDockerImageName(migration.DeployArtifact)
	job := getK8sMigrationJob(
		migration.DatabaseName,
		plan.Namespace.Name,
		migration.ServiceAccount,
		migration.ArtifactName,
		imageName,
		migration.RequestedVersion,
		migration.RunAs)
	setupJobEnvironment(migration.Metadata, job)

	_, err = k8s.BatchV1().Jobs(plan.Namespace.Name).Get(ctx, migration.DatabaseName, metav1.GetOptions{})
	if err == nil {
		err = k8s.BatchV1().Jobs(plan.Namespace.Name).Delete(ctx, migration.DatabaseName, metav1.DeleteOptions{})
		if err != nil {
			fail(err, "an error occurred trying to delete the old migration job")
			return
		}
	} else {
		if !k8sErrors.IsNotFound(err) {
			fail(err, "an error occurred trying to check for old migration jobs")
			return
		}
	}

	_, err = k8s.BatchV1().Jobs(plan.Namespace.Name).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		fail(err, "an error occurred trying to create the migration job")
		return
	}

	labelSelector := fmt.Sprintf("job=%s,version=%s", migration.DatabaseName, migration.AvailableVersion)
	pods := k8s.CoreV1().Pods(plan.Namespace.Name)
	watch, err := pods.Watch(ctx, metav1.ListOptions{
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
				plan.Message("migration failed, exit code: %d", x.State.Terminated.ExitCode)
				return
			}
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
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"job": jobName,
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"job":     jobName,
						"version": artifactVersion,
					},
				},
				Spec: apiv1.PodSpec{
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
							Ports:           []apiv1.ContainerPort{},
						},
					},
					ImagePullSecrets: []apiv1.LocalObjectReference{
						{
							Name: "docker-cfg",
						},
					},
				},
			},
		},
	}
}
