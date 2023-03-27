package lxd2etcd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/juju/loggo"
	"github.com/lxc/lxd/shared/api"
	"github.com/palantir/stacktrace"
)

// HandleLxdEvent is run for each received event from LXD.
// It triggers a config refresh according to the event.
func HandleLxdEvent(refreshChan chan struct{}, event api.Event) error {
	var (
		err     error
		lcEvent *api.EventLifecycle
	)
	lcEvent = &api.EventLifecycle{}
	err = json.Unmarshal(event.Metadata, lcEvent)
	if err != nil {
		return stacktrace.Propagate(err, "fail to unmarshal <%s> into EventLifecycle", string(event.Metadata))
	}
	if strings.HasPrefix(lcEvent.Action, "instance-") || strings.HasPrefix(lcEvent.Action, "network-") {
		loggo.GetLogger("").Tracef("triggering refresh for action <%s>", lcEvent.Action)
		refreshChan <- struct{}{}
	}
	return nil
}

// LxdEventToString returns a human readable representation of an LXD api event.
func LxdEventToString(event api.Event) string {
	var (
		builder strings.Builder
	)
	builder.WriteString(fmt.Sprintf("Project:%s, ", event.Project))
	builder.WriteString(fmt.Sprintf("Location:%s, ", event.Location))
	builder.WriteString(fmt.Sprintf("Type:%s, ", event.Type))
	builder.WriteString(fmt.Sprintf("Timestamp:%s, ", event.Timestamp.Format("2006-01-02 15:04:05")))
	builder.WriteString("Metadata:")
	builder.Write(event.Metadata)
	return builder.String()
}
