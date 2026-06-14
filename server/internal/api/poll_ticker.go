// Package api — small ticker helper isolated to allow test overrides.
package api

import "time"

type pollTicker struct {
	*time.Ticker
}

func newPollTicker(intervalSec int) *pollTicker {
	return &pollTicker{Ticker: time.NewTicker(time.Duration(intervalSec) * time.Second)}
}
