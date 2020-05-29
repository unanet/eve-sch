package config

import (
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"

	"gitlab.unanet.io/devops/eve/pkg/log"

	"gitlab.unanet.io/devops/eve-sch/internal/vault"
)

var (
	config *Config
	mutex  = sync.Mutex{}
)

type VaultConfig = vault.Config

type Config struct {
	VaultConfig
	ApiQUrl                string        `split_words:"true" required:"true"`
	SchQUrl                string        `split_words:"true" required:"true"`
	SchQWaitTimeSecond     int64         `split_words:"true" default:"20"`
	SchQVisibilityTimeout  int64         `split_words:"true" default:"3600"`
	SchQMaxNumberOfMessage int64         `split_words:"true" default:"5"`
	SchQWorkerTimeout      time.Duration `split_words:"true" default:"500s"`
	FnTriggerTimeout       time.Duration `split_words:"true" default:"120s"`
	K8sDeployTimeoutSec    int64         `split_words:"true" default:"120"`
	S3Bucket               string        `split_words:"true" required:"true"`
	AWSRegion              string        `split_words:"true" required:"true"`
	MetricsPort            int           `split_words:"true" default:"3001"`
	ClusterName            string        `split_words:"true" required:"true"`
}

func GetConfig() Config {
	mutex.Lock()
	defer mutex.Unlock()
	if config != nil {
		return *config
	}
	c := Config{}
	err := envconfig.Process("EVE", &c)
	if err != nil {
		log.Logger.Panic("Unable to Load Config", zap.Error(err))
	}
	config = &c
	return *config
}
