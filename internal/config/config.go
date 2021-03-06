package config

import (
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/unanet/go/pkg/log"
	"go.uber.org/zap"
)

var (
	config *Config
	mutex  = sync.Mutex{}
)

type Config struct {
	ApiQUrl                string        `split_words:"true" required:"true"`
	Namespace              string        `split_words:"true" required:"true"`
	SchQUrl                string        `split_words:"true" required:"true"`
	BaseArtifactHost       string        `split_words:"true" required:"true"`
	SchQWaitTimeSecond     int64         `split_words:"true" default:"20"`
	SchQVisibilityTimeout  int64         `split_words:"true" default:"3600"`
	SchQMaxNumberOfMessage int64         `split_words:"true" default:"5"`
	SchQWorkerTimeout      time.Duration `split_words:"true" default:"7200s"`
	K8sDeployTimeoutSec    int64         `split_words:"true" default:"300"`
	K8sJobTimeoutSec       int64         `split_words:"true" default:"3600"`
	S3Bucket               string        `split_words:"true" required:"true"`
	AWSRegion              string        `split_words:"true" required:"true"`
	MetricsPort            int           `split_words:"true" default:"3001"`
	Port                   int           `split_words:"true" default:"8080"`
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
