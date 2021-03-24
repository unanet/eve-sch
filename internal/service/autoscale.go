package service

import (
	"encoding/json"

	autoscaling "k8s.io/api/autoscaling/v2beta2"
)

// TODO: remove after migration from eve service to definition
type AutoScaleSettings map[string]interface{}

func parseAutoScale(input []byte) (AutoScaleSettings, error) {
	if input == nil || len(input) < 2 {
		return nil, nil
	}
	if len(input) == 2 {
		return nil, nil
	}
	var autoscale = make(AutoScaleSettings)
	if err := json.Unmarshal(input, &autoscale); err != nil {
		return nil, err
	}
	return autoscale, nil
}

func parseResource(input []byte) map[string]interface{} {
	if input == nil || len(input) < 2 || (len(input) == 2 && (string(input[0]) != "{" || string(input[1]) != "}")) {
		return nil
	}
	var podResource = make(map[string]interface{})
	if err := json.Unmarshal(input, &podResource); err != nil {
		return nil
	}

	return podResource
}

func (as AutoScaleSettings) UtilizationMetricSpecs() []interface{} {
	result := make([]interface{}, 0)

	def := func(name string, val interface{}) map[string]interface{} {
		return map[string]interface{}{
			"type": string(autoscaling.ResourceMetricSourceType),
			"resource": map[string]interface{}{
				"name": name,
				"target": map[string]interface{}{
					"type":               string(autoscaling.UtilizationMetricType),
					"averageUtilization": val,
				},
			},
		}
	}

	if uSpec, ok := as["utilization"].(map[string]interface{}); ok {
		if mem, ok := uSpec["memory"]; ok {
			result = append(result, def("memory", mem))
		}
		if cpu, ok := uSpec["cpu"]; ok {
			result = append(result, def("cpu", cpu))
		}
	}

	return result
}
