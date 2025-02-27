package docker

import (
	dockerclient "github.com/fsouza/go-dockerclient"
	"github.com/zekroTJA/ranna/pkg/cappedbuffer"
)

type DockerSandbox struct {
	client    *dockerclient.Client
	container *dockerclient.Container
}

func (s *DockerSandbox) ID() string {
	return s.container.ID
}

func (s *DockerSandbox) Run(bufferCap int) (stdout, stderr string, err error) {
	buffStdout := cappedbuffer.New([]byte{}, bufferCap)
	buffStderr := cappedbuffer.New([]byte{}, bufferCap)
	waiter, err := s.client.AttachToContainerNonBlocking(dockerclient.AttachToContainerOptions{
		Container:    s.container.ID,
		Stdout:       true,
		Stderr:       true,
		Stream:       true,
		OutputStream: buffStdout,
		ErrorStream:  buffStderr,
	})
	if err != nil {
		return
	}

	err = s.client.StartContainer(s.container.ID, nil)
	if err != nil {
		return
	}

	waiter.Wait()
	stdout = buffStdout.String()
	stderr = buffStderr.String()
	return
}

func (s *DockerSandbox) IsRunning() (ok bool, err error) {
	ctn, err := s.client.InspectContainer(s.container.ID)
	if err != nil {
		return
	}

	ok = ctn.State.Running
	return
}

func (s *DockerSandbox) Kill() error {
	return s.client.KillContainer(dockerclient.KillContainerOptions{
		ID: s.container.ID,
	})
}

func (s *DockerSandbox) Delete() error {
	return s.client.RemoveContainer(dockerclient.RemoveContainerOptions{
		ID: s.container.ID,
	})
}
