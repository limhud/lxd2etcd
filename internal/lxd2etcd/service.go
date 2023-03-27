package lxd2etcd

import (
	"context"
	"time"

	"github.com/limhud/lxd2etcd/internal/config"

	"github.com/juju/loggo"
	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/palantir/stacktrace"
	"go.etcd.io/etcd/clientv3"
)

// Service represents a service struct.
type Service struct {
	initialized       bool
	lxdInstanceServer lxd.InstanceServer
	lxdEventListener  *lxd.EventListener
	etcdClient        *clientv3.Client
	errorChan         chan error
	refreshChan       chan struct{}
}

// NewService returns a new service instance.
func NewService() (*Service, error) {
	service := &Service{}
	service.initialized = false
	service.errorChan = make(chan error)
	service.refreshChan = make(chan struct{}, 1000)
	return service, nil
}

func initServiceWithRetries(ctx context.Context, service *Service) {
	var (
		err      error
		wait     time.Duration
		errChan  chan error
		inChan   chan *Service
		doneChan chan struct{}
		timer    *time.Timer
	)
	service.initialized = false
	wait = 0
	errChan = make(chan error)
	inChan = make(chan *Service)
	doneChan = make(chan struct{})
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
				err = serviceToInitialize.init(ctx)
				if err != nil {
					loggo.GetLogger("").Tracef("intialization error")
					errChan <- stacktrace.Propagate(err, "fail to initialize service")
					return
				}
				doneChan <- struct{}{}
			}
		}()
		inChan <- service
		select {
		case <-ctx.Done():
			loggo.GetLogger("").Tracef("initialization canceled")
			return
		case <-doneChan:
			service.initialized = true
			loggo.GetLogger("").Debugf("service initialized")
			loggo.GetLogger("").Tracef("service: <%#v>", service)
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

func (service *Service) init(ctx context.Context) error {
	var (
		err        error
		eventName  string
		etcdConfig clientv3.Config
	)
	loggo.GetLogger("").Tracef("initializing service")
	// initialize lxd listener
	service.lxdInstanceServer, err = lxd.ConnectLXDUnix(config.GetLxd().Socket, nil)
	if err != nil {
		return stacktrace.Propagate(err, "fail to initialize lxd client")
	}
	loggo.GetLogger("").Debugf("lxd client initialized")
	service.lxdEventListener, err = service.lxdInstanceServer.GetEventsAllProjects()
	if err != nil {
		return stacktrace.Propagate(err, "fail to initialize lxd event listener")
	}
	// initialize lxd listener handler
	_, err = service.lxdEventListener.AddHandler([]string{"lifecycle"}, func(event api.Event) {
		var (
			err error
		)
		loggo.GetLogger("").Tracef("event <%s>: <%s>", eventName, LxdEventToString(event))
		err = HandleLxdEvent(service.refreshChan, event)
		if err != nil {
			service.errorChan <- err
		}
	})
	if err != nil {
		return stacktrace.Propagate(err, "fail to add event handler for <lifecycle> event type")
	}
	loggo.GetLogger("").Debugf("lxd event handlers installed")
	// initialize etcd client
	etcdConfig = clientv3.Config{
		Endpoints:   config.GetEtcd().Endpoints,
		DialTimeout: config.GetEtcd().DialTimeout,
		Username:    config.GetEtcd().Username,
		Password:    config.GetEtcd().Password,
		Context:     ctx,
	}
	loggo.GetLogger("").Debugf("etcd config: <%#v>", etcdConfig)
	service.etcdClient, err = clientv3.New(etcdConfig)
	if err != nil {
		return stacktrace.Propagate(err, "fail to initialize etcd client with config <%#v>", etcdConfig)
	}
	loggo.GetLogger("").Debugf("etcd client initialized")
	return nil
}

func (service *Service) disconnect() {
	var (
		err error
	)
	// disconnect from lxd
	if service.lxdEventListener != nil && service.lxdEventListener.IsActive() {
		service.lxdEventListener.Disconnect()
	}
	// disconnect from etcd
	if service.etcdClient != nil && service.etcdClient.ActiveConnection() != nil {
		err = service.etcdClient.Close()
		if err != nil {
			loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to close etcd client").Error())
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
		err                   error
		lxdInfo               *LxdInfo
		processingTriggerChan chan struct{}
		ticker                *time.Ticker
		emptyChanTimer        *time.Timer
		waitForDHCPTimer      *time.Timer
	)
	processingTriggerChan = make(chan struct{}, 1)
	ticker = time.NewTicker(config.GetLxd().PeriodicRefresh)
	defer ticker.Stop()
	emptyChanTimer = time.NewTimer(time.Second)
	defer emptyChanTimer.Stop()
	waitForDHCPTimer = time.AfterFunc(config.GetLxd().WaitForDHCP, func() {
		select {
		case processingTriggerChan <- struct{}{}:
			loggo.GetLogger("").Infof("refresh triggered by automatic wait for dhcp")
		default: // chan is already full, no need to trigger refresh
			loggo.GetLogger("").Tracef("automatic wait for dhcp cancelled because chan is already full")
		}
	})
	waitForDHCPTimer.Stop()
	defer waitForDHCPTimer.Stop()
ServiceLoop:
	for {
		initServiceWithRetries(ctx, service)
		// trigger initial refresh
		processingTriggerChan <- struct{}{}
	RefreshLoop:
		for {
			loggo.GetLogger("").Tracef("waiting for refresh")
			select {
			case <-ctx.Done():
				loggo.GetLogger("").Infof("stopping service...")
				break ServiceLoop
			case <-ticker.C:
				select {
				case processingTriggerChan <- struct{}{}:
					loggo.GetLogger("").Infof("refresh triggered by ticker")
				default:
					loggo.GetLogger("").Tracef("ticker refresh cancelled because chan is already full")
					// chan is already full, do not block.
				}
			case <-service.refreshChan:
				// empty refreshChan
				if !emptyChanTimer.Stop() {
					<-emptyChanTimer.C
				}
				emptyChanTimer.Reset(time.Second) // start timer limiting the time for flushing events
			EmptyChanLoop:
				for {
					select {
					case <-service.refreshChan:
					case <-emptyChanTimer.C: // too much time flushing events
						loggo.GetLogger("").Tracef("too much time flushing events, stopping here.")
						break EmptyChanLoop
					default: // no more event to flush
						break EmptyChanLoop
					}
				}
				// non blocking send
				select {
				case processingTriggerChan <- struct{}{}:
				default:
					// chan is already full meaning a refresh is already pending, so we do not need to append a new refresh.
				}
				loggo.GetLogger("").Infof("refresh triggered by event")
				waitForDHCPTimer.Reset(config.GetLxd().WaitForDHCP)
			case <-processingTriggerChan:
				if !service.initialized {
					loggo.GetLogger("").Warningf("cancelling triggered processing before service initialization")
					continue
				}
				loggo.GetLogger("").Tracef("processing refresh")
				lxdInfo = &LxdInfo{}
				err = lxdInfo.Populate(service.lxdInstanceServer)
				if err != nil {
					loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to obtain lxd infos").Error())
					service.disconnect()
					break RefreshLoop
				}
				loggo.GetLogger("").Debugf("retrieved lxd info:\n%s", lxdInfo.PrettyString())
				err = lxdInfo.Persist(ctx, service.etcdClient)
				if err != nil {
					loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to persist data to etcd").Error())
					service.disconnect()
					break RefreshLoop
				}
				loggo.GetLogger("").Infof("etcd updated")
			case err = <-service.errorChan:
				service.disconnect()
				loggo.GetLogger("").Errorf(err.Error())
				break RefreshLoop
			}
		}
	}
	service.disconnect()
	loggo.GetLogger("").Infof("service has been stopped...")
	return err
}
