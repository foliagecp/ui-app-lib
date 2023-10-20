

package uilib

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/common"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

func (h *statefunHandler) createTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	caller := ctxProcessor.Caller
	payload := ctxProcessor.Payload

	subscriber := payload.GetByPath("subscriber").AsStringDefault(caller.ID)
	dest, _ := payload.GetByPath("destination").AsString()

	object := ctxProcessor.GetObjectContext()
	triggerBodyIsEmpty := !object.GetByPath("body").IsNonEmptyObject()

	if triggerBodyIsEmpty {
		// object.SetByPath("body", easyjson.NewJSONObject())
		// ctxProcessor.SetObjectContext(object)

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

	queryID := common.GetQueryID(ctxProcessor)

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", easyjson.NewJSON(""))

	common.ReplyQueryID(queryID, &result, ctxProcessor)
}

func (h *statefunHandler) updateTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	subs := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "subscriber")
	for _, v := range subs {
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, triggerSubscriberUpdateFunction, v, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
			slog.Warn(err.Error())
		}
	}
}

func (h *statefunHandler) updateTriggerSubscriber(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	const op = "updateTriggerSubscriber"

	self := ctxProcessor.Self

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, op)
	if err != nil {
		return
	}

	object := ctxProcessor.GetObjectContext()

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, op); err != nil {
			slog.Warn("Key mutex unlock", "caller", op, "error", err)
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

	newControllerConstruct := result.GetByPath("result")
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
