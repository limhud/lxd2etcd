package lxd2etcd

import (
	"context"
	"time"

	"github.com/limhud/lxd2etcd/internal/config"

	"github.com/juju/loggo"
	"github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/palantir/stacktrace"
)

// Service represents a service struct.
type Service struct {
	initialized    bool
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
	service.initialized = false
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
	loggo.GetLogger("").Tracef("initializing service")
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

func initServiceWithRetries(ctx context.Context, service *Service) {
	var (
		err               error
		wait              time.Duration
		errChan           chan error
		inChan            chan *Service
		outChan           chan *Service
		intializedService *Service
		timer             *time.Timer
	)
	service.initialized = false
	wait = 0
	errChan = make(chan error)
	inChan = make(chan *Service)
	outChan = make(chan *Service)
	loggo.GetLogger("").Tracef("starting to initialize service with retries")
	for {
		go func() {
			var (
				err                 error
				serviceToInitialize *Service
			)
			select {
			case <-ctx.Done():
				return
			case serviceToInitialize = <-inChan:
				err = serviceToInitialize.init()
				if err != nil {
					loggo.GetLogger("").Tracef("intialization error")
					errChan <- stacktrace.Propagate(err, "fail to initialize lxd client")
					return
				}
				outChan <- serviceToInitialize
			}
		}()
		inChan <- service
		select {
		case <-ctx.Done():
			loggo.GetLogger("").Tracef("initialization canceled")
			return
		case intializedService = <-outChan:
			service.instanceServer = intializedService.instanceServer
			service.eventListener = intializedService.eventListener
			service.initialized = true
			return
		case err = <-errChan:
			loggo.GetLogger("").Errorf(err.Error())
		}
		timer = time.NewTimer(wait * time.Second)
		select {
		case <-ctx.Done():
			loggo.GetLogger("").Tracef("initialization canceled")
			return
		case <-timer.C:
			loggo.GetLogger("").Tracef("trying again to initialize service")
			if wait < 60 {
				wait = wait + 10
			}
		}
	}
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
func (service *Service) Start(ctx context.Context) error {
	var (
		err     error
		lxdInfo *LxdInfo
	)
ServiceLoop:
	for {
		initServiceWithRetries(ctx, service)
		if service.initialized {
			go func() {
				service.refreshChan <- struct{}{}
			}()
		}
	RefreshLoop:
		for {
			select {
			case <-ctx.Done():
				loggo.GetLogger("").Infof("stopping service...")
				if service.initialized {
					service.eventListener.Disconnect()
				}
				break ServiceLoop
			case <-service.refreshChan:
				loggo.GetLogger("").Infof("refresh triggered")
				// TODO: trigger refresh for info in etcd
				lxdInfo = &LxdInfo{}
				err = lxdInfo.Populate(service.instanceServer)
				if err != nil {
					loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to obtain lxd infos").Error())
				}
				loggo.GetLogger("").Debugf("retrieved lxd info:\n%s", lxdInfo.PrettyString())
			case err = <-service.errorChan:
				service.eventListener.Disconnect()
				loggo.GetLogger("").Errorf(err.Error())
				break RefreshLoop
			}
		}
	}
	loggo.GetLogger("").Infof("service has been stopped...")
	return err
}
