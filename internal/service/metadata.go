package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"

	"github.com/docker/docker/utils/templates"
	uuid "github.com/satori/go.uuid"
	"gitlab.unanet.io/devops/eve/pkg/eve"

	"gitlab.unanet.io/devops/eve-sch/internal/config"
)

type TemplateServiceData struct {
	Plan    *eve.NSDeploymentPlan
	Service *eve.DeployService
}

type TemplateMigrationData struct {
	Plan      *eve.NSDeploymentPlan
	Migration *eve.DeployMigration
}

type TemplateJobData struct {
	Plan *eve.NSDeploymentPlan
	Job  *eve.DeployJob
}

func replace(input, from, to string) string {
	return strings.Replace(input, from, to, -1)
}

func ParseServiceMetadata(metadata map[string]interface{}, service *eve.DeployService, plan *eve.NSDeploymentPlan) (map[string]interface{}, error) {
	metadataJson, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	temp := template.New("metadata")
	temp.Funcs(template.FuncMap{
		"replace": replace,
	})
	temp, err = temp.Parse(string(metadataJson))
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	err = temp.Execute(&b, TemplateServiceData{
		Plan:    plan,
		Service: service,
	})
	if err != nil {
		return nil, err
	}

	var returnMap map[string]interface{}
	err = json.Unmarshal(b.Bytes(), &returnMap)
	if err != nil {
		return nil, err
	}

	return overrideMetaData(returnMap, plan.DeploymentID, plan.MetadataOverrides), nil
}

func ParseMigrationMetadata(metadata map[string]interface{}, migration *eve.DeployMigration, plan *eve.NSDeploymentPlan) (map[string]interface{}, error) {
	metadataJson, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	temp, err := templates.Parse(string(metadataJson))
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	err = temp.Execute(&b, TemplateMigrationData{
		Plan:      plan,
		Migration: migration,
	})
	if err != nil {
		return nil, err
	}

	var returnMap map[string]interface{}
	err = json.Unmarshal(b.Bytes(), &returnMap)
	if err != nil {
		return nil, err
	}

	return overrideMetaData(returnMap, plan.DeploymentID, plan.MetadataOverrides), nil
}

func overrideMetaData(m map[string]interface{}, id uuid.UUID, overrides eve.MetadataField) map[string]interface{} {
	if overrides != nil && len(overrides) > 0 {
		for k, v := range overrides {
			m[k] = v
		}
	}
	c := config.GetConfig()
	m["EVE_CALLBACK_URL"] = fmt.Sprintf("http://eve-sch-v1.%s:%d/callback?id=%s", c.Namespace, c.Port, id.String())
	return m
}

func ParseJobMetadata(metadata map[string]interface{}, job *eve.DeployJob, plan *eve.NSDeploymentPlan) (map[string]interface{}, error) {
	metadataJson, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	temp, err := templates.Parse(string(metadataJson))
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	err = temp.Execute(&b, TemplateJobData{
		Plan: plan,
		Job:  job,
	})
	if err != nil {
		return nil, err
	}

	var returnMap map[string]interface{}
	err = json.Unmarshal(b.Bytes(), &returnMap)
	if err != nil {
		return nil, err
	}

	return overrideMetaData(returnMap, plan.DeploymentID, plan.MetadataOverrides), nil
}
