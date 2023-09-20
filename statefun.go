

package uilib

import (
	"github.com/foliagecp/sdk/statefun"
)

const (
	clientIngressFunction = "ui.ingress"
	clientEgressFunction  = "ui.pre.egress"

	sessionInitFunction          = "functions.client.session.init"
	sessionCreateFunction        = "functions.client.session.create"
	sessionDeleteFunction        = "functions.client.session.delete"
	sessionUnsubFunction         = "functions.client.session.unsub"
	sessionCommandFunction       = "functions.client.session.command"
	sessionAutoControlFunction   = "functions.client.session.auto.control"
	clientControllersSetFunction = "functions.client.controllers.set"

	controllerSetupFunction   = "functions.controller.setup"
	controllerUnsubFunction   = "functions.controller.unsub"
	controllerConstructCreate = "functions.controller.construct.create"

	triggerCreateFunction           = "functions.trigger.create"
	triggerSubscriberUpdateFunction = "functions.trigger.subscriber.update"
	triggerUpdateFunction           = "functions.trigger.update"
)

type statefunHandler struct {
	runtime *statefun.Runtime
	cfg     *config
}

func RegisterAllFunctionTypes(runtime *statefun.Runtime, opts ...UIOpt) {
	h := &statefunHandler{
		runtime: runtime,
		cfg:     defaultConfig,
	}

	for _, opt := range opts {
		opt(h)
	}

	statefun.NewFunctionType(runtime, h.cfg.IngressTopic, h.clientInit, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, clientEgressFunction, h.clientEgress, *statefun.NewFunctionTypeConfig())

	statefun.NewFunctionType(runtime, sessionInitFunction, h.sessionInit, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, sessionCreateFunction, h.createSessionFunction, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, sessionDeleteFunction, h.deleteSessionFunction, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, sessionUnsubFunction, h.unsubSessionFunction, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, sessionAutoControlFunction, h.clientSessionAutoControl, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, sessionCommandFunction, h.sessionCommand, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, clientControllersSetFunction, h.clientControllersSet, *statefun.NewFunctionTypeConfig())

	statefun.NewFunctionType(runtime, controllerSetupFunction, h.setupControllerFunction, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, controllerUnsubFunction, h.unsubControllerFunction, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, controllerConstructCreate, h.createControllerConstruct, *statefun.NewFunctionTypeConfig())

	statefun.NewFunctionType(runtime, triggerCreateFunction, h.createTriggerFunction, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, triggerSubscriberUpdateFunction, h.updateTriggerSubscriber, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, triggerUpdateFunction, h.updateTrigger, *statefun.NewFunctionTypeConfig())
}
