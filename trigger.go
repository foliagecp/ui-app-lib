

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

func (h *statefunHandler) createTrigger(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	self := contextProcessor.Self
	caller := contextProcessor.Caller
	payload := contextProcessor.Payload

	subscriber := payload.GetByPath("subscriber").AsStringDefault(caller.ID)
	dest, _ := payload.GetByPath("destination").AsString()

	object := contextProcessor.GetObjectContext()
	triggerBodyIsEmpty := !object.GetByPath("body").IsNonEmptyObject()

	if triggerBodyIsEmpty {
		// object.SetByPath("body", easyjson.NewJSONObject())
		// contextProcessor.SetObjectContext(object)

		if err := createLink(contextProcessor, dest, self.ID, "trigger", easyjson.NewJSONObject().GetPtr(), self.ID); err != nil {
			slog.Warn("Cannot create link", "error", err)
		}
	}

	if err := createLink(contextProcessor, subscriber, self.ID, "trigger", easyjson.NewJSONObject().GetPtr(), self.ID); err != nil {
		slog.Warn("Cannot create link", "error", err)
	}

	if err := createLink(contextProcessor, self.ID, subscriber, "subscriber", easyjson.NewJSONObject().GetPtr(), subscriber); err != nil {
		slog.Warn("Cannot create link", "error", err)
	}

	queryID := common.GetQueryID(contextProcessor)

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", easyjson.NewJSON(""))

	common.ReplyQueryID(queryID, &result, contextProcessor)
}

func (h *statefunHandler) updateTrigger(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	self := contextProcessor.Self
	subs := getChildrenUUIDSByLinkType(contextProcessor, self.ID, "subscriber")
	for _, v := range subs {
		contextProcessor.Call(triggerSubscriberUpdateFunction, v, easyjson.NewJSONObject().GetPtr(), nil)
	}
}

func (h *statefunHandler) updateTriggerSubscriber(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	const op = "updateTriggerSubscriber"

	self := contextProcessor.Self

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, op)
	if err != nil {
		return
	}

	object := contextProcessor.GetObjectContext()

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, op); err != nil {
			slog.Warn("Key mutex unlock", "caller", op, "error", err)
		}
	}()

	split := strings.Split(self.ID, "_")

	controllerName := split[0]
	controllerUUID := split[len(split)-1]

	body := object.GetByPath("body") // declaration

	result, err := contextProcessor.GolangCallSync(controllerConstructCreate, controllerUUID, &body, nil)
	if err != nil {
		slog.Warn("Controller creation construct failed", "error", err)
	}

	newControllerConstruct := result.GetByPath("result")
	oldControllerConstruct := object.GetByPath("construct")

	if oldControllerConstruct.IsNonEmptyObject() && newControllerConstruct.Equals(oldControllerConstruct) {
		return
	}

	object.SetByPath("construct", newControllerConstruct)
	contextProcessor.SetObjectContext(object)

	path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, newControllerConstruct)

	subscribers := getChildrenUUIDSByLinkType(contextProcessor, self.ID, "subscriber")

	for _, subID := range subscribers {
		contextProcessor.Call(clientEgressFunction, subID, &updateReply, nil)
	}
}
