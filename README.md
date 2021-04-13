# eve-sch

[![pipeline status](https://gitlab.unanet.io/devops/eve-sch/badges/master/pipeline.svg)](https://gitlab.unanet.io/devops/eve-sch/-/commits/master) [![Bugs](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=bugs)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Code Smells](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=code_smells)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Coverage](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=coverage)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Lines of Code](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=ncloc)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Maintainability Rating](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=sqale_rating)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Quality Gate Status](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=alert_status)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Reliability Rating](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=reliability_rating)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Security Rating](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=security_rating)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Technical Debt](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=sqale_index)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Vulnerabilities](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=vulnerabilities)](https://sonarqube.unanet.io/dashboard?id=eve-sch)

Eve-sch is a component of the Eve CI/CD ChatOps Pipeline. 
It is the component that "knows" about Kubernetes, and interfaces with the K8s Clusters. 
Every cluster is bootstrapped/provisioned with an eve-sch component. 
There is also a 1-to-1 between scheduler, cluster, and an AWS Message Queue (SQS).
When a cluster is provisioned, it is setup with an eve scheduler, a queue, and matching entry in the eve-db `cluster` table.
The eve scheduler is responsible for listening to the queue, and applying those details to the cluster, and then reporting back the results. 

## Definitions

The scheduler reads messages from a queue, downloads the JSON Payload from an S3 bucket, and applies the `Kubernetes Resource Definitions` that are serialized byte arrays on the DeploymentPlan object.
Those `definitions` are declared in the `defintions` table, but typcially as "micro" fragments of the Kubernetes Resources.
Meaning, we declare small portions of a definition and then dynamically stitch them together in the `definitions_service_map` and the `definitions_job_map`, based on different "tiers", like artifact, environment, namespace, cluster and service.
The two Top Level entities that the scheduler handles/manages are Eve Services, and Eve Jobs. 

We call these `Eve Services` and `Eve Jobs`, primarily because the "service" nomenclature can tend to be overused. In Eve it means one thing, and in k8s it means another.
In Eve, the notion of a Service represents an application service (like an HTTP REST Service), but also where it lives (namespace) Basically something that will listen for incoming requests (REST, GRPC, RPC, HTTP, etc.).
Although similar, in Kubernetes, the term Service is an actual Kubernetes Resource that is used to expose an application. You can read more about k8s services [here](https://kubernetes.io/docs/concepts/services-networking/service/).
In Eve, every "service" represents a minimum of two types of Kubernetes Resources; a [Kubernetes Service](https://kubernetes.io/docs/concepts/services-networking/service/) and a [Kubernetes Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/).
In Eve, every "job" represents a single [Kubernetes Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/). 


## Definition Types

Definitions are broken out by "type". When we dynamically roll up the definitions called out in the `definitions_service_map` and the `definitions_job_map` tables, we aggregate them up to the Definition Type. 

### Deployment

[Docs](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)

[API Spec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#deployment-v1-apps)

Example: 

```json
{
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "readinessProbe": {
              "httpGet": {
                "path": "/ping",
                "port": 8080
              },
              "periodSeconds": 10,
              "initialDelaySeconds": 30
            }
          }
        ]
      }
    }
  }
}
```

### Job

[Docs](https://kubernetes.io/docs/concepts/workloads/controllers/job/)

[API Spec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#job-v1-batch)

Example:
```json
{
  "spec": {
    "template": {
      "labels": {
        "job": "{{ .Job.JobName }}",
        "nuance": "{{ .Job.Nuance }}",
        "version": "{{ .Job.AvailableVersion }}"
      }
    }
  },
  "metadata": {
    "labels": {
      "app.kubernetes.io/name": "{{ .Job.JobName }}",
      "app.kubernetes.io/version": "{{ .Job.AvailableVersion }}",
      "app.kubernetes.io/instance": "{{ .Job.JobName }}-{{ .Job.Nuance }}",
      "app.kubernetes.io/managed-by": "eve"
    }
  }
}
```


### Horizontal Pod Autoscaler (HPA)

[Docs](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)

[API Spec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#horizontalpodautoscaler-v2beta2-autoscaling)

Example:
```json
{
  "spec": {
    "metrics": [
      {
        "type": "Resource",
        "resource": {
          "name": "cpu",
          "target": {
            "type": "Utilization",
            "averageUtilization": 85
          }
        }
      },
      {
        "type": "Resource",
        "resource": {
          "name": "memory",
          "target": {
            "type": "Utilization",
            "averageUtilization": 110
          }
        }
      }
    ],
    "maxReplicas": 4,
    "minReplicas": 2
  }
}
```

### Service

[Docs](https://kubernetes.io/docs/concepts/services-networking/service/)

[API Spec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#service-v1-core)

Example:
```json
{
  "metadata": {
    "labels": {
      "proxy": "unanet"
    }
  }
}
```


### Persistent Volume Claim (PVC)

[Docs](https://kubernetes.io/docs/concepts/storage/dynamic-provisioning/)

[API Spec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#persistentvolumeclaim-v1-core)

Example:
```json
{
  "spec": {
    "resources": {
      "requests": {
        "storage": "2Gi"
      }
    },
    "accessModes": [
      "ReadWriteOnce"
    ],
    "storageClassName": "gp2"
  }
}
```