package service

import (
	"bytes"
	"encoding/json"

	"github.com/docker/docker/utils/templates"
	"gitlab.unanet.io/devops/eve/pkg/eve"
)

const (
	vaultInjectionPath = "inject_vault_paths"
)

type TemplateServiceData struct {
	Plan    *eve.NSDeploymentPlan
	Service *eve.DeployService
}

type TemplateMigrationData struct {
	Plan      *eve.NSDeploymentPlan
	Migration *eve.DeployMigration
}

func ParseServiceMetadata(metadata map[string]interface{}, service *eve.DeployService, plan *eve.NSDeploymentPlan) (map[string]interface{}, []string, error) {
	metadataJson, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, err
	}

	temp, err := templates.Parse(string(metadataJson))
	if err != nil {
		return nil, nil, err
	}

	var b bytes.Buffer
	temp.Execute(&b, TemplateServiceData{
		Plan:    plan,
		Service: service,
	})

	var returnMap map[string]interface{}
	err = json.Unmarshal(b.Bytes(), &returnMap)
	if err != nil {
		return nil, nil, err
	}

	return parseVaultInjectionPaths(returnMap)
}

func ParseMigrationMetadata(metadata map[string]interface{}, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan) (map[string]interface{}, []string, error) {
	metadataJson, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, err
	}

	temp, err := templates.Parse(string(metadataJson))
	if err != nil {
		return nil, nil, err
	}

	var b bytes.Buffer
	temp.Execute(&b, TemplateMigrationData{
		Plan:      plan,
		Migration: migration,
	})

	var returnMap map[string]interface{}
	err = json.Unmarshal(b.Bytes(), &returnMap)
	if err != nil {
		return nil, nil, err
	}

	return parseVaultInjectionPaths(returnMap)
}

func parseVaultInjectionPaths(metadata map[string]interface{}) (map[string]interface{}, []string, error) {
	if vip, ok := metadata[vaultInjectionPath]; ok {
		delete(metadata, vaultInjectionPath)
		if vips, ok := vip.([]string); ok {
			return metadata, vips, nil
		}

		if vips, ok := vip.(string); ok {
			return metadata, []string{vips}, nil
		}
	}

	return metadata, []string{}, nil
}
