package chat

import "github.com/sixath/framework/memory"

var defaultFailureSink memory.FailureSignalSink = memory.MultiFailureSink{
	memory.LoggingFailureSink{},
	&memory.RingFailureSink{N: 64},
}

// DefaultFailureSignalSink returns Logging+Ring sink for turnBus bridges.
func DefaultFailureSignalSink() memory.FailureSignalSink {
	return defaultFailureSink
}
