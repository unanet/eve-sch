module gitlab.unanet.io/devops/eve-sch

go 1.15

// replace gitlab.unanet.io/devops/eve => ../eve

require (
	github.com/aws/aws-sdk-go v1.36.9
	github.com/dghubble/sling v1.3.0
	github.com/docker/docker v1.13.2-0.20170601211448-f5ec1e2936dc
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.4.0
	gitlab.unanet.io/devops/eve v0.6.1-0.20201230144650-4765a4ea343f // indirect
	gitlab.unanet.io/devops/go v0.6.0
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.16.0
	google.golang.org/appengine v1.6.6 // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v0.18.2
)
