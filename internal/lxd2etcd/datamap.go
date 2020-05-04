package lxd2etcd

import (
	"encoding/json"

	"github.com/lxc/lxd/shared/api"
	"github.com/palantir/stacktrace"
)

var DataMap = map[string]*LxdEventHandler{
	"operation": &LxdEventHandler{
		Types: []string{"operation"},
		Handler: func(refreshChan chan struct{}, event api.Event) error {
			var (
				err       error
				operation *api.Operation
			)
			operation = &api.Operation{}
			err = json.Unmarshal(event.Metadata, &operation)
			if err != nil {
				return stacktrace.Propagate(err, "fail to unmarshal <%s> into operation", string(event.Metadata))
			}
			if operation.StatusCode != api.Success {
				return nil
			}
			switch operation.Description {
			case "Starting container":
				fallthrough
			case "Stopping container":
				refreshChan <- struct{}{}
			}
			return nil
		},
	},
}
