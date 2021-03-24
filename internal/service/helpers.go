package service

import (
	"fmt"
	"strconv"
	"strings"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	uuid "github.com/satori/go.uuid"
	"gitlab.unanet.io/devops/eve-sch/internal/config"
	"gitlab.unanet.io/devops/eve/pkg/eve"
)

func int64Ptr(i int64) *int64 { return &i }

func mergeM(standard map[string]interface{}, definition map[string]interface{}) map[string]interface{} {
	var merged = make(map[string]interface{})
	if definition != nil {
		for k, v := range definition {
			merged[k] = v
		}
	}

	for k, v := range standard {
		merged[k] = v
	}

	return merged
}

func defaultContainerEnvVars(deploymentID uuid.UUID, artifact *eve.DeployArtifact) []interface{} {
	c := config.GetConfig()
	artifact.Metadata["EVE_CALLBACK_URL"] = fmt.Sprintf("http://eve-sch-v1.%s:%d/callback?id=%s", c.Namespace, c.Port, deploymentID.String())
	artifact.Metadata["EVE_IMAGE_NAME"] = getDockerImageName(artifact)

	var containerEnvVars = make([]interface{}, 0)
	for k, v := range artifact.Metadata {
		value, ok := v.(string)
		if !ok {
			continue
		}

		containerEnvVars = append(containerEnvVars, map[string]interface{}{
			"name":  k,
			"value": value,
		})
	}

	return containerEnvVars
}

func deploymentLabelSelector(eveDeployment eve.DeploymentSpec) string {
	return fmt.Sprintf("app=%s,version=%s,nuance=%s", eveDeployment.GetName(), eveDeployment.GetArtifact().AvailableVersion, eveDeployment.GetNuance())
}

func jobLabelSelector(eveDeployment eve.DeploymentSpec) string {
	return fmt.Sprintf("job=%s", eveDeployment.GetName())
}

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

func getDockerImageName(artifact *eve.DeployArtifact) string {
	return fmt.Sprintf("%s/%s:%s", fmt.Sprintf(DockerRepoFormat, artifact.ArtifactoryFeed), artifact.ArtifactoryPath, artifact.EvalImageTag())
}

func getDeploymentContainerPorts(eveDeployment eve.DeploymentSpec) []interface{} {
	var result = make([]interface{}, 0)
	// Setup the Service Port
	if eveDeployment.GetServicePort() != 0 {
		result = append(result, map[string]interface{}{
			"name":          "http",
			"containerPort": int64(eveDeployment.GetServicePort()),
			"protocol":      string(apiv1.ProtocolTCP),
		})
	}
	// Setup the Metrics Port
	if eveDeployment.GetMetricsPort() != 0 {
		result = append(result, map[string]interface{}{
			"name":          "metrics",
			"containerPort": int64(eveDeployment.GetMetricsPort()),
			"protocol":      string(apiv1.ProtocolTCP),
		})
	}
	return result
}

func groupSchemaResourceVersion(crdDef eve.DefinitionResult) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: crdDef.Class, Version: crdDef.Version, Resource: crdDef.Resource()}
}
