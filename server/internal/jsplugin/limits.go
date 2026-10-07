package jsplugin

import "time"

const (
	probePoolResident = 2
	probePoolMax      = 8
	probeHookTimeout  = 200 * time.Millisecond
	// poolWaitTimeout bounds how long a call queues for a runtime when the pool is at max.
	poolWaitTimeout = time.Second

	protocolPoolResident = 4
	protocolPoolMax      = 16
	// protocolHookTimeout is shorter than the probe timeout: protocol hooks run on the request path.
	protocolHookTimeout = 50 * time.Millisecond
	// MaxProtocolResponseBody is the largest upstream body passed to parseResponse.
	MaxProtocolResponseBody = 4 << 20
	// MaxProbeSteps is the number of probe hook calls, which allows at most five HTTP requests.
	MaxProbeSteps = 6
	// MaxProbeRequestBody is the encoded size of one probe request body.
	MaxProbeRequestBody  = 64 << 10
	maxProbeResponseBody = 1 << 20
	// MaxAllocBytes is the per-call allocation budget passed to moejs.
	MaxAllocBytes int64 = 16 << 20
	// MaxResultBytes is the hook result bound passed to moejs.
	MaxResultBytes        int64 = 1 << 20
	maxRequestHeaders           = 32
	maxRequestHeaderBytes       = 8 << 10
	maxPluginLogs               = 20
	maxPluginLogBytes           = 1024
	hostAPIVersion              = 1
	hookAPIVersion              = 1
)

// HeapLimitAvailable reports whether NewRuntime is configured with both an
// allocation budget and a result-size bound. A zero value means that limit is off.
func HeapLimitAvailable() bool {
	return MaxAllocBytes > 0 && MaxResultBytes > 0
}
