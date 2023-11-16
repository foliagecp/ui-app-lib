// Copyright 2023 NJWS Inc.

package uilib

import (
	"fmt"
	"log/slog"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

const (
	_PROPERTY = "@property"
	_FUNCTION = "@function"
)

const controllerSetupFunction = "functions.client.controller.setup"

/*
	payload: {
		body:{},
	}

	controller_id: {
		name: string,
		object_id: string,
		body: {...},
		subscribers: as links
		construct: {}
	},
*/
func (h *statefunHandler) setupController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	caller := ctxProcessor.Caller
	payload := ctxProcessor.Payload

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, controllerSetupFunction)
	if err != nil {
		return
	}

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, controllerSetupFunction); err != nil {
			slog.Warn("Key mutex unlock", "caller", controllerSetupFunction, "error", err)
		}
	}()

	object := ctxProcessor.GetObjectContext()
	bodyIsEmpty := !object.GetByPath("body").IsNonEmptyObject()

	if bodyIsEmpty {
		controllerBody := easyjson.NewJSONObject()
		controllerBody.SetByPath("body", payload.GetByPath("body"))
		controllerBody.SetByPath("name", payload.GetByPath("name"))
		controllerBody.SetByPath("object_id", payload.GetByPath("object_id"))
		controllerBody.SetByPath("construct", easyjson.NewJSONObject())

		tx, err := beginTransaction(ctxProcessor, "full")
		if err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.createObject(ctxProcessor, self.ID, "controller", &controllerBody); err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.createObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.commit(ctxProcessor); err != nil {
			slog.Warn(err.Error())
			return
		}

		updatePayload := easyjson.NewJSONObject()
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, controllerSubscriberUpdateFunction, self.ID, &updatePayload, nil); err != nil {
			slog.Warn(err.Error())
		}
	} else {
		subscribers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "subscriber")

		for _, v := range subscribers {
			if v == caller.ID {
				return
			}
		}

		tx, err := beginTransaction(ctxProcessor, "full")
		if err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.createObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.commit(ctxProcessor); err != nil {
			slog.Warn(err.Error())
			return
		}

		controllerName, _ := object.GetByPath("name").AsString()
		controllerUUID, _ := object.GetByPath("object_id").AsString()

		if construct := object.GetByPath("construct"); construct.IsNonEmptyObject() {
			path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

			reply := easyjson.NewJSONObject()
			reply.SetByPath(path, construct)

			if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, caller.ID, &reply, nil); err != nil {
				slog.Warn(err.Error())
			}
		}
	}

	replyOk(ctxProcessor)
}

const controllerUnsubFunction = "functions.client.controller.unsub"

func (h *statefunHandler) unsubController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	caller := ctxProcessor.Caller
	self := ctxProcessor.Self

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, controllerUnsubFunction)
	if err != nil {
		return
	}

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, controllerUnsubFunction); err != nil {
			slog.Warn("Key mutex unlock", "caller", controllerUnsubFunction, "error", err)
		}
	}()

	link := easyjson.NewJSONObject()
	link.SetByPath("descendant_uuid", easyjson.NewJSON(caller.ID))
	link.SetByPath("link_type", easyjson.NewJSON("subscriber"))

	if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, "functions.graph.ll.api.link.delete", self.ID, &link, nil); err != nil {
		slog.Warn("Cannot delete link", "error", err)
		return
	}

	subs := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "subscriber")
	if len(subs) == 0 {
		deleteObjectPayload := easyjson.NewJSONObject()
		_, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, "functions.graph.ll.api.object.delete", self.ID, &deleteObjectPayload, nil)
		if err != nil {
			slog.Warn("Cannot delete object", "error", err)
		}
	}

	replyOk(ctxProcessor)
}

const controllerSubscriberUpdateFunction = "functions.client.controller.subscriber.update"

func (h *statefunHandler) updateControllerSubscriber(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, controllerSubscriberUpdateFunction)
	if err != nil {
		return
	}

	object := ctxProcessor.GetObjectContext()

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, controllerSubscriberUpdateFunction); err != nil {
			slog.Warn("Key mutex unlock", "caller", controllerSubscriberUpdateFunction, "error", err)
		}
	}()

	controllerName, _ := object.GetByPath("name").AsString()
	controllerUUID, _ := object.GetByPath("object_id").AsString()
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

const controllerConstructCreate = "functions.client.controller.construct.create"

/*
@property:<json path>

@function:<function name>:[[arg1 value],[arg2 value],...[argN value]] - ideal

@function:getChildren(linkType) - now
*/
func (h *statefunHandler) createControllerConstruct(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectID := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload

	decorators := parseDecorators(objectID, payload)
	decoratorsReply := easyjson.NewJSONObject()

	for key, cd := range decorators {
		result := cd.Invoke(ctxProcessor)
		decoratorsReply.SetByPath(key, result)
	}

	reply(ctxProcessor, "ok", decoratorsReply)
}
