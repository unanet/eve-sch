package service

import (
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"

	"gitlab.unanet.io/devops/eve/pkg/log"
)

var (
	config   *Config
	mutex    = sync.Mutex{}
)

type Config struct {
	ApiQUrl                string        `split_words:"true" required:"true"`
	SchQUrl                string        `split_words:"true" required:"true"`
	SchQWaitTimeSecond     int64         `split_words:"true" default:"20"`
	SchQVisibilityTimeout  int64         `split_words:"true" default:"3600"`
	SchQMaxNumberOfMessage int64         `split_words:"true" default:"10"`
	SchQWorkerTimeout      time.Duration `split_words:"true" default:"60s"`
	S3Bucket               string        `split_words:"true" required:"true"`
	AWSRegion              string        `split_words:"true" required:"true"`
	MetricsPort 		   int           `split_words:"true" default:"3001"`
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
