

package uilib

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

const triggerCreateFunction = "functions.trigger.create"

func (h *statefunHandler) createTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	caller := ctxProcessor.Caller
	payload := ctxProcessor.Payload

	subscriber := payload.GetByPath("subscriber").AsStringDefault(caller.ID)
	dest, _ := payload.GetByPath("destination").AsString()

	object := ctxProcessor.GetObjectContext()
	triggerBodyIsEmpty := !object.GetByPath("body").IsNonEmptyObject()

	if triggerBodyIsEmpty {
		if err := createLink(ctxProcessor, dest, self.ID, "trigger", easyjson.NewJSONObject().GetPtr(), self.ID); err != nil {
			slog.Warn("Cannot create link", "error", err)
		}
	}

	if err := createLink(ctxProcessor, subscriber, self.ID, "trigger", easyjson.NewJSONObject().GetPtr(), self.ID); err != nil {
		slog.Warn("Cannot create link", "error", err)
	}

	if err := createLink(ctxProcessor, self.ID, subscriber, "subscriber", easyjson.NewJSONObject().GetPtr(), subscriber); err != nil {
		slog.Warn("Cannot create link", "error", err)
	}

	replyOk(ctxProcessor)
}

const triggerUpdateFunction = "functions.trigger.update"

func (h *statefunHandler) updateTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	subs := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "subscriber")
	for _, v := range subs {
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, triggerSubscriberUpdateFunction, v, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
			slog.Warn(err.Error())
		}
	}
}

const triggerSubscriberUpdateFunction = "functions.trigger.subscriber.update"

func (h *statefunHandler) updateTriggerSubscriber(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, triggerSubscriberUpdateFunction)
	if err != nil {
		return
	}

	object := ctxProcessor.GetObjectContext()

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, triggerSubscriberUpdateFunction); err != nil {
			slog.Warn("Key mutex unlock", "caller", triggerSubscriberUpdateFunction, "error", err)
		}
	}()

	split := strings.Split(self.ID, "_")

	controllerName := split[0]
	controllerUUID := split[len(split)-1]

	body := object.GetByPath("body") // declaration

	result, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerConstructCreate, controllerUUID, &body, nil)
	if err != nil {
		slog.Warn("Controller creation construct failed", "error", err)
	}

	newControllerConstruct := result.GetByPath("payload.result")
	oldControllerConstruct := object.GetByPath("construct")

	if oldControllerConstruct.IsNonEmptyObject() && newControllerConstruct.Equals(oldControllerConstruct) {
		return
	}

	object.SetByPath("construct", newControllerConstruct)
	ctxProcessor.SetObjectContext(object)

	path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, newControllerConstruct)

	subscribers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "subscriber")

	for _, subID := range subscribers {
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, subID, &updateReply, nil); err != nil {
			slog.Warn(err.Error())
		}
	}
}
