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
		service *Service
	)
	service = &Service{}
	service.stopChan = make(chan struct{})
	service.errorChan = make(chan error)
	service.refreshChan = make(chan struct{})
	return service, nil
}

func (service *Service) init() error {
	var (
		err          error
		eventName    string
		eventHandler *LxdEventHandler
	)
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
	for eventName, eventHandler = range LxdEventHandlers {
		_, err = service.eventListener.AddHandler(eventHandler.Types, func(event api.Event) {
			var (
				err error
			)
			loggo.GetLogger("").Tracef("event <%s>/<%s>: <%s>", eventName, eventHandler.Types, LxdEventToString(event))
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
		err     error
		lxdInfo *LxdInfo
	)
	go func() {
		service.refreshChan <- struct{}{}
	}()
ServiceLoop:
	for {
		err = service.init()
		if err != nil {
			break ServiceLoop
		}
	RefreshLoop:
		for {
			select {
			case <-service.stopChan:
				loggo.GetLogger("").Infof("stopping service...")
				service.eventListener.Disconnect()
				break ServiceLoop
			case <-service.refreshChan:
				loggo.GetLogger("").Infof("refresh triggered")
				// TODO: trigger refresh for info in etcd
				lxdInfo = &LxdInfo{}
				err = lxdInfo.Populate(service.instanceServer)
				if err != nil {
					loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to obtain lxd infos").Error())
				}
				loggo.GetLogger("").Debugf("retrieved lxd info: <%#v>", lxdInfo)
			case err = <-service.errorChan:
				service.eventListener.Disconnect()
				break RefreshLoop
			}
		}
		// TODO: exponential backoff
	}
	loggo.GetLogger("").Infof("service has been stopped...")
	return err
}

// Shutdown stops the service.
func (service *Service) Shutdown() error {
	loggo.GetLogger("").Infof("shutdown signal received")
	service.stopChan <- struct{}{}
	return nil
}
