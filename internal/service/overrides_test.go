package service

import (
	"testing"

	"github.com/unanet/eve/pkg/eve"
)

func Test_getDockerImageName(t *testing.T) {
	type args struct {
		artifact *eve.DeployArtifact
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "happy",
			args: args{
				artifact: &eve.DeployArtifact{
					ArtifactID:          100,
					ArtifactName:        "something-cool",
					RequestedVersion:    "0.1.0",
					DeployedVersion:     "0.1.0.2",
					AvailableVersion:    "0.1.0.3",
					ImageTag:            "$version",
					Metadata:            nil,
					ArtifactoryFeed:     "docker-int",
					ArtifactoryPath:     "ops/eve-bot",
					ArtifactoryFeedType: "docker",
					Result:              "",
					ExitCode:            0,
					Deploy:              false,
				},
			},
			want: "unanet-docker-int.jfrog.io/ops/eve-bot:0.1.0.3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getDockerImageName(tt.args.artifact); got != tt.want {
				t.Errorf("getDockerImageName() = %v, want %v", got, tt.want)
			}
		})
	}
}
