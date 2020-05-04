package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/limhud/lxd2etcd/internal/config"
	"github.com/limhud/lxd2etcd/internal/lxd2etcd"

	"github.com/cosiner/flag"
	"github.com/juju/loggo"
	"github.com/juju/loggo/loggocolor"
	"github.com/palantir/stacktrace"
)

type command struct {
	Help    bool   `names:"-h, --help" usage:"display this help and exit"`
	Version bool   `names:"-v, --version" usage:"display version information and exit"`
	Debug   bool   `names:"-d, --debug" usage:"debug"`
	Trace   bool   `names:"-t, --trace" usage:"trace"`
	NoColor bool   `names:"--no-color" usage:"no output color"`
	Config  string `names:"-c, --config" usage:"config file" default:"/etc/lxd2etcd.yml"`
}

func (*command) Metadata() map[string]flag.Flag {
	return map[string]flag.Flag{
		"": {
			Usage: "LXD2ETCD importer",
		},
	}
}

var (
	version   string
	buildDate string
	buildHash string

	cmd command
)

func main() {
	var (
		err          error
		flagSet      *flag.FlagSet
		configString string
		service      *lxd2etcd.Service
		sigChan      chan os.Signal
	)

	// Configure logger
	loggo.GetLogger("").SetLogLevel(loggo.INFO)

	// Parse command line
	flagSet = flag.NewFlagSet(flag.Flag{}).ErrHandling(0)
	err = flagSet.StructFlags(&cmd)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}

	if err = flagSet.Parse(os.Args...); err != nil {
		fmt.Printf("Error: %s\n", err)
		flagSet.Help(false)
		os.Exit(1)
	}

	if !cmd.NoColor {
		_, err = loggo.ReplaceDefaultWriter(loggocolor.NewWriter(os.Stderr))
		if err != nil {
			loggo.GetLogger("").Errorf("fail to setup colorized writer")
		}
	}

	if cmd.Debug {
		loggo.GetLogger("").SetLogLevel(loggo.DEBUG)
	}

	if cmd.Trace {
		loggo.GetLogger("").SetLogLevel(loggo.TRACE)
	}

	if cmd.Version {
		printVersion()
		os.Exit(0)
	}

	loggo.GetLogger("").Debugf("%+v\n", cmd)

	// Load service configuration
	config.SetConfigFile(cmd.Config)
	if err = config.ReadInConfig(); err != nil {
		loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "configuration error").Error())
		os.Exit(1)
	}

	configString, err = config.String()
	if err != nil {
		loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "configuration error").Error())
		os.Exit(1)
	}
	loggo.GetLogger("").Debugf(configString)

	// Create listener instance
	service, err = lxd2etcd.NewService()
	if err != nil {
		loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "service initialization error").Error())
		os.Exit(1)
	}

	// Handle service signals
	sigChan = make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR2, syscall.SIGHUP, syscall.SIGUSR1)

	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				err = service.Shutdown()
				if err != nil {
					loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to shutdown server").Error())
				}

			case syscall.SIGUSR2:
				service.ToggleDebug()
			}
		}
	}()

	// Start service and wait
	loggo.GetLogger("").Debugf("starting service...")
	err = service.Start()
	if err != nil {
		loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "service error").Error())
		err = service.Shutdown()
		if err != nil {
			loggo.GetLogger("").Errorf(err.Error())
		}
		os.Exit(1)
	}

	err = service.Shutdown()
	if err != nil {
		loggo.GetLogger("").Errorf(err.Error())
	}

	os.Exit(0)
}

func printVersion() {
	fmt.Printf("Version:     %s\n", version)
	fmt.Printf("Build date:  %s\n", buildDate)
	fmt.Printf("Build hash:  %s\n", buildHash)
	fmt.Printf("Compiler:    %s (%s)\n", runtime.Version(), runtime.Compiler)
}
