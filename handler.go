package muxmetrics

import "github.com/stvp/roll"

type RollbarHandler struct {
	handler http.Handler
	client  roll.Client
}
