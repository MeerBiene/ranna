package sandbox

import (
	"errors"
	"path"
	"sync"
	"time"

	"github.com/sarulabs/di/v2"
	"github.com/sirupsen/logrus"
	"github.com/zekroTJA/ranna/internal/config"
	"github.com/zekroTJA/ranna/internal/file"
	"github.com/zekroTJA/ranna/internal/namespace"
	"github.com/zekroTJA/ranna/internal/spec"
	"github.com/zekroTJA/ranna/internal/static"
	"github.com/zekroTJA/ranna/internal/util"
	"github.com/zekroTJA/ranna/pkg/models"
	"github.com/zekroTJA/ranna/pkg/timeout"
)

var (
	errUnsupportredLanguage = errors.New("unsupported language spec")
	errTimedOut             = errors.New("code execution timed out")
)

type Manager interface {
	RunInSandbox(req *models.ExecutionRequest) (res *models.ExecutionResponse, err error)
	PrepareEnvironments() []error
	TryCleanup() []error
}

type managerImpl struct {
	sandbox Provider
	spec    spec.Provider
	file    file.Provider
	cfg     config.Provider
	ns      namespace.Provider

	streamBufferCap  int
	runningSandboxes *sync.Map
	isCleanup        bool
}

type sandboxWrapper struct {
	sbx     Sandbox
	hostDir string
}

type SystemError struct {
	error
}

func IsSystemError(err error) (ok bool) {
	_, ok = err.(SystemError)
	return
}

func NewManager(ctn di.Container) (m *managerImpl, err error) {
	m = &managerImpl{}

	m.sandbox = ctn.Get(static.DiSandboxProvider).(Provider)
	m.spec = ctn.Get(static.DiSpecProvider).(spec.Provider)
	m.file = ctn.Get(static.DiFileProvider).(file.Provider)
	m.cfg = ctn.Get(static.DiConfigProvider).(config.Provider)
	m.ns = ctn.Get(static.DiNamespaceProvider).(namespace.Provider)

	m.runningSandboxes = &sync.Map{}
	sbc, err := util.ParseMemoryStr(m.cfg.Config().Sandbox.StreamBufferCap)
	if err != nil {
		return
	}
	m.streamBufferCap = int(sbc)

	return
}

func (m *managerImpl) PrepareEnvironments() (errs []error) {
	errs = []error{}

	for _, spec := range m.spec.Spec() {
		if spec.Image == "" {
			continue
		}
		if err := m.sandbox.Prepare(*spec); err != nil {
			logrus.WithField("image", spec.Image).WithError(err).Error("failed preparing env")
			errs = append(errs, err)
		}
	}

	return
}

func (m *managerImpl) RunInSandbox(req *models.ExecutionRequest) (res *models.ExecutionResponse, err error) {
	defer func() {
		if err != nil && IsSystemError(err) {
			logrus.
				WithError(err).
				WithFields(logrus.Fields{
					"spec": req.Language,
				}).
				Error("sandbox run failed")
		}
	}()

	spc, ok := m.spec.Spec().Get(req.Language)
	if !ok {
		err = errUnsupportredLanguage
		return
	}

	runSpc := RunSpec{Spec: spc}

	if runSpc.Subdir, err = m.ns.Get(); err != nil {
		err = SystemError{err}
		return
	}

	runSpc.HostDir = m.cfg.Config().HostRootDir
	runSpc.Arguments = req.Arguments
	runSpc.Environment = req.Environment

	if runSpc.Cmd == "" {
		runSpc.Cmd = spc.FileName
	}

	hostDir := runSpc.GetAssambledHostDir()
	if err = m.file.CreateDirectory(hostDir); err != nil {
		err = SystemError{err}
		return
	}

	fileDir := path.Join(hostDir, spc.FileName)
	if err = m.file.CreateFileWithContent(fileDir, req.Code); err != nil {
		err = SystemError{err}
		return
	}

	sbx, err := m.sandbox.CreateSandbox(runSpc)
	if err != nil {
		err = SystemError{err}
		return
	}
	logrus.WithFields(logrus.Fields{
		"id":   sbx.ID(),
		"spec": req.Language,
	}).Info("created sandbox")

	wrapper := &sandboxWrapper{sbx, hostDir}
	m.runningSandboxes.Store(sbx.ID(), wrapper)

	res = new(models.ExecutionResponse)
	timedOut := timeout.RunBlockingWithTimeout(func() {
		res.StdOut, res.StdErr, err = sbx.Run(m.streamBufferCap)
	}, time.Duration(m.cfg.Config().Sandbox.TimeoutSeconds)*time.Second)

	if err != nil {
		err = SystemError{err}
		return
	}
	if m.isCleanup {
		return
	}
	if err = m.killAndCleanUp(wrapper); err != nil {
		return
	}
	if timedOut {
		err = errTimedOut
	}
	logrus.WithFields(logrus.Fields{
		"id":   sbx.ID(),
		"spec": req.Language,
	}).Info("sandbox cleaned up")

	return
}

func (m *managerImpl) TryCleanup() (errs []error) {
	m.isCleanup = true
	errs = []error{}

	m.runningSandboxes.Range(func(key, value interface{}) bool {
		w, ok := value.(*sandboxWrapper)
		if ok {
			logrus.WithField("id", w.sbx.ID()).Info("killing and cleaning up running container")
			if err := m.killAndCleanUp(w); err != nil {
				errs = append(errs, err)
			}
		}
		return true
	})

	return
}

func (m *managerImpl) killAndCleanUp(w *sandboxWrapper) (err error) {
	defer func() {
		if err != nil {
			logrus.
				WithError(err).
				WithField("id", w.sbx.ID()).
				Error("failed cleaning up container")
		}
	}()

	logrus.WithField("id", w.sbx.ID()).Debug("calling killAndCleanUp")

	ok, err := w.sbx.IsRunning()
	if err != nil {
		return
	}
	if ok {
		err = w.sbx.Kill()
	}
	if err = w.sbx.Delete(); err != nil {
		return
	}
	if err = m.file.DeleteDirectory(w.hostDir); err != nil {
		return
	}
	m.runningSandboxes.Delete(w.sbx.ID())
	return
}
