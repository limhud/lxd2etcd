package lxd2etcd

import (
	"github.com/limhud/lxd2etcd/internal/config"

	"github.com/juju/loggo"
	"github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/palantir/stacktrace"
)

// Service represents a service struct.
type Service struct {
	stopChan       chan struct{}
	instanceServer lxd.InstanceServer
	eventListener  *lxd.EventListener
	errorChan      chan error
	refreshChan    chan struct{}
}

// NewService returns a new service instance.
func NewService() (*Service, error) {
	var (
		err     error
		service *Service
	)
	service = &Service{}
	err = service.init()
	if err != nil {
		return nil, err
	}
	return service, nil
}

func (service *Service) init() error {
	var (
		err          error
		eventName    string
		eventHandler *LxdEventHandler
	)
	service.stopChan = make(chan struct{})
	service.errorChan = make(chan error)
	service.refreshChan = make(chan struct{})
	// initialize lxd listener
	service.instanceServer, err = lxd.ConnectLXDUnix(config.GetLxd().Socket, nil)
	if err != nil {
		return stacktrace.Propagate(err, "fail to initialize lxd client")
	}
	loggo.GetLogger("").Debugf("lxd client initialized")
	service.eventListener, err = service.instanceServer.GetEvents()
	if err != nil {
		return stacktrace.Propagate(err, "fail to initialize lxd event listener")
	}
	// initialize listener handlers
	for eventName, eventHandler = range DataMap {
		_, err = service.eventListener.AddHandler(eventHandler.Types, func(event api.Event) {
			var (
				err error
			)
			loggo.GetLogger("").Debugf("event <%s>/<%s>: <%s>", eventName, eventHandler.Types, LxdEventToString(event))
			err = eventHandler.Handler(service.refreshChan, event)
			if err != nil {
				service.errorChan <- err
			}
		})
		if err != nil {
			return stacktrace.Propagate(err, "fail to add event handler for event <%s>", eventName)
		}
	}
	return nil
}

// ToggleDebug toggles log levele between DEBUG and INFO.
func (service *Service) ToggleDebug() {
	if loggo.GetLogger("").LogLevel() == loggo.INFO {
		loggo.GetLogger("").Infof("setting log level to Debug")
		loggo.GetLogger("").SetLogLevel(loggo.DEBUG)
	} else if loggo.GetLogger("").LogLevel() == loggo.DEBUG {
		loggo.GetLogger("").Infof("setting log level to Trace")
		loggo.GetLogger("").SetLogLevel(loggo.TRACE)
	} else {
		loggo.GetLogger("").Infof("setting log level to Info")
		loggo.GetLogger("").SetLogLevel(loggo.INFO)
	}
}

// Start the service.
func (service *Service) Start() error {
	var (
		err error
	)
ServiceLoop:
	for {
		select {
		case <-service.stopChan:
			break ServiceLoop
		case <-service.refreshChan:
			loggo.GetLogger("").Infof("refresh triggered")
			// TODO: trigger refresh for info in etcd
		case err = <-service.errorChan:
			break ServiceLoop
		}
	}
	service.eventListener.Disconnect()
	loggo.GetLogger("").Infof("service has been stopped...")
	return err
}

// Shutdown stops the service.
func (service *Service) Shutdown() error {
	loggo.GetLogger("").Infof("received shutdown signal, stopping service...")
	close(service.stopChan)
	return nil
}
