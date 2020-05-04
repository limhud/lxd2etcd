package lxd2etcd

import (
	"github.com/juju/loggo"
	"github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/palantir/stacktrace"
)

type NetworkInfo struct {
	MAC string
}

type ContainerInfo struct {
	Network string
	Port    string
	IP      string
	MAC     string
}

type LxdInfo struct {
	Networks   map[string]*NetworkInfo
	Containers map[string]*ContainerInfo
}

func (lxdInfo *LxdInfo) Populate(instanceServer lxd.InstanceServer) error {
	var (
		err           error
		networks      []api.Network
		network       api.Network
		networkInfo   *NetworkInfo
		networkState  *api.NetworkState
		containers    []api.ContainerFull
		container     api.ContainerFull
		containerInfo *ContainerInfo
	)
	lxdInfo.Networks = make(map[string]*NetworkInfo)
	lxdInfo.Containers = make(map[string]*ContainerInfo)
	// network infos
	loggo.GetLogger("").Debugf("retrieve network infos")
	networks, err = instanceServer.GetNetworks()
	if err != nil {
		return stacktrace.Propagate(err, "fail to retrieve networks")
	}
	for _, network = range networks {
		networkInfo = &NetworkInfo{}
		networkState, err = instanceServer.GetNetworkState(network.Name)
		if err != nil {
			return stacktrace.Propagate(err, "fail to retrieve state of network <%s>", network.Name)
		}
		networkInfo.MAC = networkState.Hwaddr
		lxdInfo.Networks[network.Name] = networkInfo
	}
	// container infos
	loggo.GetLogger("").Debugf("retrieve container infos")
	containers, err = instanceServer.GetContainersFull()
	if err != nil {
		return stacktrace.Propagate(err, "fail to retrieve containers")
	}
	for _, container = range containers {
		containerInfo = &ContainerInfo{}
		lxdInfo.Containers[container.Name] = containerInfo
	}
	return nil
}
