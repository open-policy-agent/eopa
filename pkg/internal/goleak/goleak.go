package goleak

import (
	"go.uber.org/goleak"
)

var ignoreFuncs = []string{
	"internal/poll.runtime_pollWait",
	"net/http.(*persistConn).writeLoop",
	"github.com/golang/glog.(*loggingT).flushDaemon",
	"github.com/golang/glog.(*fileSink).flushDaemon",
	"github.com/patrickmn/go-cache.(*janitor).Run",
	"github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1",
}

var Defaults = initOpts()

func initOpts() []goleak.Option {
	options := make([]goleak.Option, 0, len(ignoreFuncs)+1)
	for _, ignoreFunc := range ignoreFuncs {
		options = append(options, goleak.IgnoreTopFunction(ignoreFunc))
	}
	options = append(options, goleak.IgnoreCurrent())
	return options
}
