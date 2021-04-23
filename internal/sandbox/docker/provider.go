package docker

import (
	"path"
	"strings"

	dockerclient "github.com/fsouza/go-dockerclient"
	"github.com/zekroTJA/ranna/internal/sandbox"
)

type DockerSandboxProvider struct {
	client *dockerclient.Client
}

func NewDockerSandboxProvider() (dsp *DockerSandboxProvider, err error) {
	dsp = new(DockerSandboxProvider)

	dsp.client, err = dockerclient.NewClientFromEnv()
	if err != nil {
		return
	}

	return
}

func (dsp *DockerSandboxProvider) CreateSandbox(spec sandbox.Spec) (sandbox *DockerSandbox, err error) {
	repo, tag := getImage(spec.Image)

	err = dsp.client.PullImage(dockerclient.PullImageOptions{
		Repository: repo,
		Tag:        tag,
	}, dockerclient.AuthConfiguration{})
	if err != nil {
		return
	}

	workingDir := path.Join("/var/tmp/exec", spec.Subdir)
	hostDir := path.Join(spec.HostDir, spec.Subdir)
	container, err := dsp.client.CreateContainer(dockerclient.CreateContainerOptions{
		Config: &dockerclient.Config{
			Image:      repo + ":" + tag,
			WorkingDir: workingDir,
			Entrypoint: strings.Split(spec.Entrypoint, " "),
			Cmd:        strings.Split(spec.Cmd, " "),
		},
		HostConfig: &dockerclient.HostConfig{
			Binds: []string{hostDir + ":" + workingDir},
		},
	})

	sandbox = &DockerSandbox{
		client:    dsp.client,
		container: container,
	}

	return
}

func getImage(environmentDescriptor string) (repo, tag string) {
	split := strings.SplitN(environmentDescriptor, ":", 2)
	if len(split) == 1 {
		split = append(split, "latest")
	}

	return split[0], split[1]
}
