package lxd2etcd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lxc/lxd/shared/api"
	"github.com/palantir/stacktrace"
)

// LxdEventHandler is run for each received event from LXD.
// It triggers a config refresh according to the event.
var LxdEventHandler = func(refreshChan chan struct{}, event api.Event) error {
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
}

// LxdEventToString returns a human readable representation of an LXD api event.
func LxdEventToString(event api.Event) string {
	var (
		builder strings.Builder
	)
	builder.WriteString(fmt.Sprintf("Type:%s, ", event.Type))
	builder.WriteString(fmt.Sprintf("Timestamp:%s, ", event.Timestamp.Format("2006-01-02 15:04:05")))
	builder.WriteString("Metadata:")
	builder.Write(event.Metadata)
	return builder.String()
}
