package lxd2etcd

import (
	"fmt"
	"strings"

	"github.com/lxc/lxd/shared/api"
)

type LxdEventHandler struct {
	Types   []string
	Handler func(chan struct{}, api.Event) error
}

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
