// Copyright 2023 NJWS Inc.

package uilib

import (
	"github.com/foliagecp/sdk/statefun"
)

const (
	_SESSION_TYPE            = "ui_session"
	_CONTROLLER_TYPE         = "ui_controller"
	_SUBSCRIBER_TYPE         = "ui_subscriber"
	_SESSIONS_ENTYPOINT      = "ui_sessions_entrypoint"
	_CONTROLLER_SUBJECT_TYPE = "ui_controller_subject"
)

type statefunHandler struct {
	cfg *config
}

func RegisterAllFunctionTypes(runtime *statefun.Runtime, opts ...UIOpt) {
	h := &statefunHandler{
		cfg: defaultConfig,
	}

	for _, opt := range opts {
		opt(h)
	}

	statefun.NewFunctionType(runtime, h.cfg.IngressTopic, h.initClient, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, clientEgressFunction, h.clientEgress, *statefun.NewFunctionTypeConfig())

	statefun.NewFunctionType(runtime, sessionInitFunction, h.initSession, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, sessionDeleteFunction, h.deleteSession, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, sessionUnsubFunction, h.unsubSession, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, sessionCommandFunction, h.sessionCommand, *statefun.NewFunctionTypeConfig())

	statefun.NewFunctionType(runtime, clientControllersSetFunction, h.setSessionController, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, controllerSetupFunction, h.setupController, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, controllerUnsubFunction, h.unsubController, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, controllerConstructCreate, h.createControllerConstruct, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, controllerUpdateFunction, h.updateController, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, controllerTriggerFunction, h.controllerTrigger, *statefun.NewFunctionTypeConfig())
}
