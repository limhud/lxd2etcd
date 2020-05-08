/*
Package config implements a thread safe configuration yaml file parser.
*/
package config

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"sync"
	"time"

	"github.com/juju/loggo"
	"github.com/palantir/stacktrace"
	"gopkg.in/yaml.v2"
)

// TODO: add validation

var (
	lock              sync.Mutex
	configFilePath    string
	immutableLogLevel bool
	// Configuration is the global Config instance storing the current configuration
	Configuration Config
)

// --- Containers section
type ContainerData struct {
	NodeIP           string `yaml:"node_ip"`
	DefaultInterface string `yaml:"default_interface"`
}

type ContainersConfig map[string]ContainerData

func (containers *ContainersConfig) Get(containerName string) *ContainerData {
	var (
		data ContainerData
		ok   bool
	)
	data, ok = (*containers)[containerName]
	if !ok {
		return &ContainerData{}
	}
	return &data
}

func (containers *ContainersConfig) Equal(comparedWith *ContainersConfig) error {
	var (
		key           string
		value         ContainerData
		comparedValue ContainerData
		ok            bool
	)
	if comparedWith == nil {
		return stacktrace.NewError("cannot compare with <nil>")
	}
	for key, value = range *containers {
		comparedValue, ok = (*comparedWith)[key]
		if !ok {
			return stacktrace.NewError("missing key <%s>", key)
		}
		if value != comparedValue {
			return stacktrace.NewError("mismatch value for key <%s>: <%#v> != <%#v>", key, value, comparedValue)
		}
	}
	for key = range *comparedWith {
		_, ok = (*containers)[key]
		if !ok {
			return stacktrace.NewError("extraneous key <%s>", key)
		}
	}
	return nil
}

func (containers *ContainersConfig) Copy() *ContainersConfig {
	var (
		copyCfg ContainersConfig
		key     string
		value   ContainerData
	)
	copyCfg = make(map[string]ContainerData)
	for key, value = range *containers {
		copyCfg[key] = value
	}
	return &copyCfg
}

// --- LxdConfig section

// LxdConfig represents the unix socket configuration
type LxdConfig struct {
	Socket      string        `yaml:"socket"`
	WaitForDHCP time.Duration `yaml:"wait_for_dhcp"`
}

// Equal tests if content is the same
func (lxd *LxdConfig) Equal(comparedWith *LxdConfig) error {
	if comparedWith == nil {
		return stacktrace.NewError("cannot compare with <nil>")
	}
	if lxd.Socket != comparedWith.Socket {
		return stacktrace.NewError("Socket value <%s> is different: <%s>", lxd.Socket, comparedWith.Socket)
	}
	if lxd.WaitForDHCP != comparedWith.WaitForDHCP {
		return stacktrace.NewError("WaitForDHCP value <%s> is different: <%s>", lxd.WaitForDHCP, comparedWith.WaitForDHCP)
	}
	return nil
}

// Copy returns a copy of the object
func (lxd *LxdConfig) Copy() *LxdConfig {
	return &LxdConfig{
		Socket:      lxd.Socket,
		WaitForDHCP: lxd.WaitForDHCP,
	}
}

// --- EtcdConfig section

// EtcdConfig stores different parameters used for administrating the SFTP accounts
type EtcdConfig struct {
	Endpoints   []string      `yaml:"endpoints"`
	DialTimeout time.Duration `yaml:"dial_timeout"`
	Username    string        `yaml:"username"`
	Password    string        `yaml:"password"`
}

// Equal tests if content is the same
func (etcd *EtcdConfig) Equal(comparedWith *EtcdConfig) error {
	if comparedWith == nil {
		return stacktrace.NewError("cannot compare with <nil>")
	}
	if !reflect.DeepEqual(etcd.Endpoints, comparedWith.Endpoints) {
		return stacktrace.NewError("Endpoints value <%s> is different: <%s>", etcd.Endpoints, comparedWith.Endpoints)
	}
	if etcd.DialTimeout != comparedWith.DialTimeout {
		return stacktrace.NewError("DialTimeout value <%s> is different: <%s>", etcd.DialTimeout, comparedWith.DialTimeout)
	}
	if etcd.Username != comparedWith.Username {
		return stacktrace.NewError("Username value <%s> is different: <%s>", etcd.Username, comparedWith.Username)
	}
	if etcd.Password != comparedWith.Password {
		return stacktrace.NewError("Password value <%s> is different: <%s>", etcd.Password, comparedWith.Password)
	}
	return nil
}

// Copy returns a copy of the object
func (etcd *EtcdConfig) Copy() *EtcdConfig {
	return &EtcdConfig{
		Endpoints:   etcd.Endpoints,
		DialTimeout: etcd.DialTimeout,
		Username:    etcd.Username,
		Password:    etcd.Password,
	}
}

// --- Global Config section

// Config file structure definition
type Config struct {
	Debug      bool             `yaml:"debug"`
	Hostname   string           `yaml:"hostname"`
	Lxd        LxdConfig        `yaml:"lxd"`
	Etcd       EtcdConfig       `yaml:"etcd"`
	Containers ContainersConfig `yaml:"containers"`
}

// String returns a string representing a config struct.
func (c *Config) String() string {
	return fmt.Sprintf("%#v", c)
}

// Equal tests if content is the same
func (c *Config) Equal(comparedWith *Config) error {
	var (
		err error
	)
	if comparedWith == nil {
		return stacktrace.NewError("cannot compare with <%s>", comparedWith)
	}
	if c.Debug != comparedWith.Debug {
		return stacktrace.NewError("debug value <%t> is different: <%t>", c.Debug, comparedWith.Debug)
	}
	if c.Hostname != comparedWith.Hostname {
		return stacktrace.NewError("hostname value <%s> is different: <%s>", c.Hostname, comparedWith.Hostname)
	}
	err = c.Lxd.Equal(&comparedWith.Lxd)
	if err != nil {
		return stacktrace.Propagate(err, "lxd section is different")
	}
	err = c.Etcd.Equal(&comparedWith.Etcd)
	if err != nil {
		return stacktrace.Propagate(err, "etcd section is different")
	}
	err = c.Containers.Equal(&comparedWith.Containers)
	if err != nil {
		return stacktrace.Propagate(err, "containers section is different")
	}
	return nil
}

// SetConfigFile set the path to the config file to read.
func SetConfigFile(path string) {
	configFilePath = path
}

// ReadInConfig triggers the reading of the config from the file.
func ReadInConfig() error {
	var (
		err  error
		data []byte
	)

	lock.Lock()
	defer lock.Unlock()

	//read file and unmarshal yaml
	data, err = ioutil.ReadFile(configFilePath)
	if err != nil {
		return stacktrace.Propagate(err, "fail to read <%s>", configFilePath)
	}

	loggo.GetLogger("").Debugf("config file <%s> read successfully", configFilePath)
	err = yaml.Unmarshal(data, &Configuration)
	if err != nil {
		return stacktrace.Propagate(err, "parsing error in <%s>", configFilePath)
	}
	loggo.GetLogger("").Debugf("config file <%s> parsed successfully", configFilePath)

	if !immutableLogLevel {
		if Configuration.Debug {
			loggo.GetLogger("").SetLogLevel(loggo.DEBUG)
		} else {
			loggo.GetLogger("").SetLogLevel(loggo.INFO)
		}
	}

	loggo.GetLogger("").Debugf("config struct: <%#v>", Configuration)

	return err
}

// String returns a string representing the config object.
func String() (string, error) {
	var (
		err  error
		data []byte
	)

	lock.Lock()
	defer lock.Unlock()

	data, err = yaml.Marshal(Configuration)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("config file <%s>:\n%s", configFilePath, data), err
}

// GetLxd returns the lxd config section
func GetLxd() *LxdConfig {
	lock.Lock()
	defer lock.Unlock()

	return Configuration.Lxd.Copy()
}

// GetEtcd returns the lxd config section
func GetEtcd() *EtcdConfig {
	lock.Lock()
	defer lock.Unlock()

	return Configuration.Etcd.Copy()
}

// GetContainers returns the containers config section
func GetContainers() *ContainersConfig {
	lock.Lock()
	defer lock.Unlock()

	return Configuration.Containers.Copy()
}

// GetHostname returns hostname config field.
func GetHostname() string {
	lock.Lock()
	defer lock.Unlock()

	return Configuration.Hostname
}

// GetDebug returns true if debug is activated in the config file, false otherwise.
func GetDebug() bool {
	lock.Lock()
	defer lock.Unlock()

	return Configuration.Debug
}

// SetLogLevelImmutable sets a flag to deactivate log level modification by configuration
func SetLogLevelImmutable() {
	lock.Lock()
	defer lock.Unlock()

	immutableLogLevel = true
}
