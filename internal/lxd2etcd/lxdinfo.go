package lxd2etcd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/limhud/lxd2etcd/internal/config"

	"github.com/juju/loggo"
	"github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/palantir/stacktrace"
	"go.etcd.io/etcd/clientv3"
)

type NetworkInfo struct {
	MAC string `json:"mac"`
}

type NetDev struct {
	Network string   `json:"network"`
	Port    string   `json:"port"`
	MAC     string   `json:"mac"`
	IPv4    []string `json:"ipv4"`
	IPv6    []string `json:"ipv6"`
}

type ContainerInfo struct {
	NetDevs map[string]*NetDev `json:"netdevs"`
}

type LxdInfo struct {
	Networks   map[string]*NetworkInfo   `json:"networks"`
	Containers map[string]*ContainerInfo `json:"containers"`
}

func (lxdInfo *LxdInfo) Populate(instanceServer lxd.InstanceServer) error {
	var (
		err             error
		networks        []api.Network
		network         api.Network
		networkInfo     *NetworkInfo
		networkState    *api.NetworkState
		containers      []api.ContainerFull
		container       api.ContainerFull
		containerInfo   *ContainerInfo
		netname         string
		net             api.ContainerStateNetwork
		netdev          *NetDev
		instanceAddress api.ContainerStateNetworkAddress
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
		loggo.GetLogger("").Tracef("processing network: <%#v>", network)
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
		loggo.GetLogger("").Tracef("processing container: <%#v>", container)
		containerInfo = &ContainerInfo{}
		containerInfo.NetDevs = make(map[string]*NetDev)
		for netname, net = range container.State.Network {
			loggo.GetLogger("").Tracef("processing container network <%s>: <%#v>", netname, net)
			netdev = &NetDev{}
			netdev.Network = container.ExpandedDevices[netname]["network"]
			netdev.Port = net.HostName
			netdev.MAC = net.Hwaddr
			netdev.IPv4 = []string{}
			netdev.IPv6 = []string{}
			for _, instanceAddress = range net.Addresses {
				loggo.GetLogger("").Tracef("processing net device address: <%#v>", instanceAddress)
				if instanceAddress.Family == "inet" {
					netdev.IPv4 = append(netdev.IPv4, fmt.Sprintf("%s/%s", instanceAddress.Address, instanceAddress.Netmask))
				} else {
					netdev.IPv6 = append(netdev.IPv6, fmt.Sprintf("%s/%s", instanceAddress.Address, instanceAddress.Netmask))
				}
			}
			containerInfo.NetDevs[netname] = netdev
		}
		lxdInfo.Containers[container.Name] = containerInfo
	}
	return nil
}

func (lxdInfo *LxdInfo) Persist(ctx context.Context, etcdClient *clientv3.Client) error {
	var (
		err     error
		key     string
		binJson []byte
		value   string
	)
	// Persist network infos
	key = fmt.Sprintf("/lxd/%s/networks", config.GetHostname())
	binJson, err = json.Marshal(lxdInfo.Networks)
	if err != nil {
		return stacktrace.Propagate(err, "fail to serialize <%#v>", lxdInfo.Networks)
	}
	value = string(binJson)
	_, err = etcdClient.Put(ctx, key, value)
	if err != nil {
		return stacktrace.Propagate(err, "fail to put key <%s> in etcd", key)
	}
	// Persist container infos
	key = fmt.Sprintf("/lxd/%s/containers", config.GetHostname())
	binJson, err = json.Marshal(lxdInfo.Containers)
	if err != nil {
		return stacktrace.Propagate(err, "fail to serialize <%#v>", lxdInfo.Containers)
	}
	value = string(binJson)
	_, err = etcdClient.Put(ctx, key, value)
	if err != nil {
		return stacktrace.Propagate(err, "fail to put key <%s> in etcd", key)
	}
	return nil
}

func (lxdInfo *LxdInfo) PrettyString() string {
	var (
		err error
		b   []byte
		out bytes.Buffer
	)
	b, err = json.Marshal(lxdInfo)
	if err != nil {
		loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to compute pretty string for <%#v>", lxdInfo).Error())
		return fmt.Sprintf("fail to compute pretty string for <%#v>", lxdInfo)
	}
	json.Indent(&out, b, "", "  ")
	return out.String()
}
