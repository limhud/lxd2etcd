#!/bin/bash                                                                    
systemctl stop lxd2etcd || true
systemctl disable lxd2etcd || true
