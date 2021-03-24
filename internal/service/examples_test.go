package service

import (
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	batchv1 "k8s.io/api/batch/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// example k8s structures to use for reference

var _exampleK8sTypeMeta = metav1.TypeMeta{
	Kind:       "",
	APIVersion: "",
}
var _exampleK8sObjectMeta = metav1.ObjectMeta{
	Name:                       "",
	GenerateName:               "",
	Namespace:                  "",
	SelfLink:                   "",
	UID:                        "",
	ResourceVersion:            "",
	Generation:                 0,
	CreationTimestamp:          metav1.Time{},
	DeletionTimestamp:          nil,
	DeletionGracePeriodSeconds: nil,
	Labels:                     nil,
	Annotations:                nil,
	OwnerReferences:            nil,
	Finalizers:                 nil,
	ClusterName:                "",
	ManagedFields:              nil,
}

var _exampleAutoscale = autoscaling.HorizontalPodAutoscaler{
	TypeMeta:   _exampleK8sTypeMeta,
	ObjectMeta: _exampleK8sObjectMeta,
	Spec: autoscaling.HorizontalPodAutoscalerSpec{
		ScaleTargetRef: autoscaling.CrossVersionObjectReference{
			Kind:       "",
			Name:       "",
			APIVersion: "",
		},
		MinReplicas: nil,
		MaxReplicas: 0,
		Metrics: []autoscaling.MetricSpec{
			autoscaling.MetricSpec{
				Type:              "",
				Object:            nil,
				Pods:              nil,
				Resource:          nil,
				ContainerResource: nil,
				External:          nil,
			},
		},
		Behavior: nil,
	},
	Status: autoscaling.HorizontalPodAutoscalerStatus{},
}

var _exampleK8sService = apiv1.Service{
	TypeMeta:   _exampleK8sTypeMeta,
	ObjectMeta: _exampleK8sObjectMeta,
	Spec: apiv1.ServiceSpec{
		Ports: []apiv1.ServicePort{
			apiv1.ServicePort{
				Name:        "",
				Protocol:    "",
				AppProtocol: nil,
				Port:        0,
				TargetPort:  intstr.IntOrString{},
				NodePort:    0,
			},
		},
		Selector:                      nil,
		ClusterIP:                     "",
		ClusterIPs:                    nil,
		Type:                          "",
		ExternalIPs:                   nil,
		SessionAffinity:               "",
		LoadBalancerIP:                "",
		LoadBalancerSourceRanges:      nil,
		ExternalName:                  "",
		ExternalTrafficPolicy:         "",
		HealthCheckNodePort:           0,
		PublishNotReadyAddresses:      false,
		SessionAffinityConfig:         nil,
		TopologyKeys:                  nil,
		IPFamilies:                    nil,
		IPFamilyPolicy:                nil,
		AllocateLoadBalancerNodePorts: nil,
	},
	Status: apiv1.ServiceStatus{
		LoadBalancer: apiv1.LoadBalancerStatus{},
		Conditions:   nil,
	},
}

var _exampleK8sDeployment = appsv1.Deployment{
	TypeMeta:   _exampleK8sTypeMeta,
	ObjectMeta: _exampleK8sObjectMeta,
	Spec: appsv1.DeploymentSpec{
		Replicas: nil,
		Selector: nil,
		Template: apiv1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{},
			Spec: apiv1.PodSpec{
				Volumes:        nil,
				InitContainers: nil,
				Containers: []apiv1.Container{
					apiv1.Container{
						Name:          "",
						Image:         "",
						Command:       nil,
						Args:          nil,
						WorkingDir:    "",
						Ports:         nil,
						EnvFrom:       nil,
						Env:           nil,
						Resources:     apiv1.ResourceRequirements{},
						VolumeMounts:  nil,
						VolumeDevices: nil,
						LivenessProbe: &apiv1.Probe{
							Handler:             apiv1.Handler{},
							InitialDelaySeconds: 0,
							TimeoutSeconds:      0,
							PeriodSeconds:       0,
							SuccessThreshold:    0,
							FailureThreshold:    0,
						},
						ReadinessProbe: &apiv1.Probe{
							Handler:             apiv1.Handler{},
							InitialDelaySeconds: 0,
							TimeoutSeconds:      0,
							PeriodSeconds:       0,
							SuccessThreshold:    0,
							FailureThreshold:    0,
						},
						StartupProbe: &apiv1.Probe{
							Handler:             apiv1.Handler{},
							InitialDelaySeconds: 0,
							TimeoutSeconds:      0,
							PeriodSeconds:       0,
							SuccessThreshold:    0,
							FailureThreshold:    0,
						},
						Lifecycle:                nil,
						TerminationMessagePath:   "",
						TerminationMessagePolicy: "",
						ImagePullPolicy:          "",
						SecurityContext:          nil,
						Stdin:                    false,
						StdinOnce:                false,
						TTY:                      false,
					},
				},
				EphemeralContainers:           nil,
				RestartPolicy:                 "",
				TerminationGracePeriodSeconds: nil,
				ActiveDeadlineSeconds:         nil,
				DNSPolicy:                     "",
				NodeSelector:                  nil,
				ServiceAccountName:            "",
				DeprecatedServiceAccount:      "",
				AutomountServiceAccountToken:  nil,
				NodeName:                      "",
				HostNetwork:                   false,
				HostPID:                       false,
				HostIPC:                       false,
				ShareProcessNamespace:         nil,
				SecurityContext:               nil,
				ImagePullSecrets:              nil,
				Hostname:                      "",
				Subdomain:                     "",
				Affinity:                      nil,
				SchedulerName:                 "",
				Tolerations: []apiv1.Toleration{
					apiv1.Toleration{
						Key:               "os",
						Operator:          "Equal",
						Value:             "windows",
						Effect:            "NoSchedule",
						TolerationSeconds: nil,
					},
				},
				HostAliases:               nil,
				PriorityClassName:         "",
				Priority:                  nil,
				DNSConfig:                 nil,
				ReadinessGates:            nil,
				RuntimeClassName:          nil,
				EnableServiceLinks:        nil,
				PreemptionPolicy:          nil,
				Overhead:                  nil,
				TopologySpreadConstraints: nil,
				SetHostnameAsFQDN:         nil,
			},
		},
		Strategy:                appsv1.DeploymentStrategy{},
		MinReadySeconds:         0,
		RevisionHistoryLimit:    nil,
		Paused:                  false,
		ProgressDeadlineSeconds: nil,
	},
	Status: appsv1.DeploymentStatus{
		ObservedGeneration:  0,
		Replicas:            0,
		UpdatedReplicas:     0,
		ReadyReplicas:       0,
		AvailableReplicas:   0,
		UnavailableReplicas: 0,
		Conditions:          nil,
		CollisionCount:      nil,
	},
}

var _exampleK8sJob = batchv1.Job{
	TypeMeta:   _exampleK8sTypeMeta,
	ObjectMeta: _exampleK8sObjectMeta,
	Spec: batchv1.JobSpec{
		Parallelism:             nil,
		Completions:             nil,
		ActiveDeadlineSeconds:   nil,
		BackoffLimit:            nil,
		Selector:                nil,
		ManualSelector:          nil,
		Template:                apiv1.PodTemplateSpec{},
		TTLSecondsAfterFinished: nil,
	},
	Status: batchv1.JobStatus{
		Conditions:     nil,
		StartTime:      nil,
		CompletionTime: nil,
		Active:         0,
		Succeeded:      0,
		Failed:         0,
	},
}

var _exampleK8sPVC = apiv1.PersistentVolumeClaim{
	TypeMeta:   _exampleK8sTypeMeta,
	ObjectMeta: _exampleK8sObjectMeta,
	Spec: apiv1.PersistentVolumeClaimSpec{
		AccessModes: []apiv1.PersistentVolumeAccessMode{},
		Selector:    nil,
		Resources: apiv1.ResourceRequirements{
			Limits:   apiv1.ResourceList{},
			Requests: apiv1.ResourceList{},
		},
		VolumeName:       "",
		StorageClassName: nil,
		VolumeMode:       nil,
		DataSource:       nil,
	},
	Status: apiv1.PersistentVolumeClaimStatus{},
}
