// Copyright 2023 NJWS Inc.

package uiconnector

import "time"

var defaultConfig = &config{
	Verbose:                  false,
	IngressTopic:             "ui.ingress",
	EgressTopic:              "ui.egress",
	SessionLifeTime:          time.Hour * 24,
	SessionInactivityTimeout: time.Minute * 15,
	SessionUpdateTimeout:     time.Second * 10,
}

type config struct {
	Verbose                  bool
	IngressTopic             string
	EgressTopic              string
	SessionLifeTime          time.Duration
	SessionInactivityTimeout time.Duration
	SessionUpdateTimeout     time.Duration
}

type UIOpt func(h *statefunHandler)

func WithIngressTopic(topic string) UIOpt {
	return func(h *statefunHandler) { h.cfg.IngressTopic = topic }
}

func WithEgressTopic(topic string) UIOpt {
	return func(h *statefunHandler) { h.cfg.EgressTopic = topic }
}

func WithSessionLifeTime(t time.Duration) UIOpt {
	return func(h *statefunHandler) { h.cfg.SessionLifeTime = t }
}

func WithSessionInactivityTimeout(t time.Duration) UIOpt {
	return func(h *statefunHandler) { h.cfg.SessionInactivityTimeout = t }
}

func WithSessionUpdateTimeout(t time.Duration) UIOpt {
	return func(h *statefunHandler) { h.cfg.SessionUpdateTimeout = t }
}

func WithVerbose() UIOpt {
	return func(h *statefunHandler) { h.cfg.Verbose = true }
}
