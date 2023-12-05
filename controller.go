

package uilib

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/foliagecp/easyjson"
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

	// ctxProcessor.ObjectMutexLock(false)
	// defer ctxProcessor.ObjectMutexUnlock()

	object := ctxProcessor.GetObjectContext()
	bodyIsEmpty := !object.GetByPath("body").IsNonEmptyObject()

	if bodyIsEmpty {
		objectUUID := payload.GetByPath("object_id").AsStringDefault("")

		controllerBody := easyjson.NewJSONObject()
		controllerBody.SetByPath("body", payload.GetByPath("body"))
		controllerBody.SetByPath("name", payload.GetByPath("name"))
		controllerBody.SetByPath("object_id", payload.GetByPath("object_id"))
		controllerBody.SetByPath("construct", easyjson.NewJSONObject())

		start := time.Now()

		m := map[string]beginTxType{
			_SESSION_TYPE: {
				Mode:    "only",
				Objects: map[string]struct{}{caller.ID: {}},
			},
			_CONTROLLER_TYPE: {
				Mode: "none",
			},
		}
		tx, err := beginTransactionWithTypes(ctxProcessor, pool.GetTxID(), m)
		if err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
			return
		}

		//slog.Warn("begin tx", "session_id", sessionID, "tx_id", tx.id, "time", time.Since(begin))

		if err := tx.createObject(ctxProcessor, self.ID, _CONTROLLER_TYPE, &controllerBody); err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
			return
		}

		if err := tx.createObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
			return
		}

		if err := tx.commit(ctxProcessor); err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
			return
		}

		if time.Since(start).Milliseconds() > 500 {
			slog.Warn("create controller", "ctrl_id", self.ID, "dt", time.Since(start))
		}

		go createLink(ctxProcessor, self.ID, objectUUID, "controller_sub", easyjson.NewJSONObject().GetPtr())

		updatePayload := easyjson.NewJSONObject()
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, controllerSubscriberUpdateFunction, self.ID, &updatePayload, nil); err != nil {
			slog.Warn(err.Error())
		}
	} else {
		subscribers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, _SUBSCRIBER_TYPE)

		for _, v := range subscribers {
			if v == caller.ID {
				replyOk(ctxProcessor)
				return
			}
		}

		m := map[string]beginTxType{
			_SESSION_TYPE: {
				Mode:    "only",
				Objects: map[string]struct{}{caller.ID: {}},
			},
			_CONTROLLER_TYPE: {
				Mode:    "only",
				Objects: map[string]struct{}{self.ID: {}},
			},
		}
		tx, err := beginTransactionWithTypes(ctxProcessor, pool.GetTxID(), m)
		if err != nil {
			slog.Warn(err.Error())
			return
		}

		//slog.Warn("begin tx", "session_id", sessionID, "tx_id", tx.id, "time", time.Since(begin))

		if err := tx.createObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
			return
		}

		if err := tx.commit(ctxProcessor); err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
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

	defer replyOk(ctxProcessor)

	// ctxProcessor.ObjectMutexLock(false)
	// defer ctxProcessor.ObjectMutexUnlock()
	m := map[string]beginTxType{
		_SESSION_TYPE: {
			Mode: "only",
			Objects: map[string]struct{}{
				caller.ID: {},
			},
		},
		_CONTROLLER_TYPE: {
			Mode: "only",
			Objects: map[string]struct{}{
				self.ID: {},
			},
		},
	}

	tx, err := beginTransactionWithTypes(ctxProcessor, pool.GetTxID(), m)
	if err != nil {
		slog.Warn(err.Error())
		return
	}

	if err := tx.deleteObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
		slog.Warn(err.Error())
		return
	}

	if err := tx.deleteObjectsLink(ctxProcessor, caller.ID, self.ID); err != nil {
		slog.Warn(err.Error())
		return
	}

	if err := tx.commit(ctxProcessor); err != nil {
		slog.Warn(err.Error())
		return
	}

	// TODO: delete controller if there is no subs
}

const controllerSubscriberUpdateFunction = "functions.client.controller.subscriber.update"

func (h *statefunHandler) updateControllerSubscriber(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self

	// ctxProcessor.ObjectMutexLock(false)
	// defer ctxProcessor.ObjectMutexUnlock()

	object := ctxProcessor.GetObjectContext()

	controllerName, _ := object.GetByPath("name").AsString()
	controllerUUID, _ := object.GetByPath("object_id").AsString()
	body := object.GetByPath("body") // declaration

	result, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerConstructCreate, controllerUUID, &body, nil)
	if err != nil {
		slog.Warn("Controller creation construct failed", "error", err)
		result = easyjson.NewJSONObject().GetPtr()
	}

	newControllerConstruct := result.GetByPath("payload.result")
	oldControllerConstruct := object.GetByPath("construct")

	if !newControllerConstruct.IsNonEmptyObject() {
		return
	}

	if oldControllerConstruct.IsNonEmptyObject() &&
		newControllerConstruct.IsNonEmptyObject() &&
		newControllerConstruct.Equals(oldControllerConstruct) {
		return
	}

	object.SetByPath("construct", newControllerConstruct)
	ctxProcessor.SetObjectContext(object)

	path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, newControllerConstruct)

	subscribers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, _SUBSCRIBER_TYPE)

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
	construct := easyjson.NewJSONObject()

	for key, cd := range decorators {
		result := cd.Invoke(ctxProcessor)
		construct.SetByPath(key, result)
	}

	reply(ctxProcessor, "ok", construct)
}
