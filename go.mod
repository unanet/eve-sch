module gitlab.unanet.io/devops/eve-sch

go 1.16

//replace gitlab.unanet.io/devops/eve => ../eve

require (
	github.com/aws/aws-sdk-go v1.37.25
	github.com/go-chi/chi v4.1.2+incompatible
	github.com/go-chi/render v1.0.1
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1
	github.com/imdario/mergo v0.3.11 // indirect
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/pkg/errors v0.9.1
	github.com/satori/go.uuid v1.2.0
	github.com/stretchr/testify v1.6.1
	gitlab.unanet.io/devops/eve v0.16.4
	gitlab.unanet.io/devops/go v1.4.0
	go.uber.org/zap v1.16.0
	k8s.io/api v0.20.4
	k8s.io/apimachinery v0.20.4
	k8s.io/client-go v0.20.4
	k8s.io/klog/v2 v2.6.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.0 // indirect
)
