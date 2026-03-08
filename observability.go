package arc

import "time"

// Observer receives request lifecycle events for logging/metrics/tracing.
type Observer interface {
	OnRequestStart(*RequestContext)
	OnRequestEnd(*RequestContext, int, error, time.Duration)
}

// ObserverFunc allows plugging simple callbacks.
type ObserverFunc struct {
	Start func(*RequestContext)
	End   func(*RequestContext, int, error, time.Duration)
}

func (o ObserverFunc) OnRequestStart(rc *RequestContext) {
	if o.Start != nil {
		o.Start(rc)
	}
}

func (o ObserverFunc) OnRequestEnd(rc *RequestContext, status int, err error, d time.Duration) {
	if o.End != nil {
		o.End(rc, status, err, d)
	}
}
