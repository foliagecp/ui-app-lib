

package uilib

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/foliagecp/easyjson"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/prometheus/client_golang/prometheus"
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

		if err := createObject(ctxProcessor, self.ID, _CONTROLLER_TYPE, &controllerBody); err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
			return
		}

		if err := createObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
			return
		}

		objectTypes := ctxProcessor.GlobalCache.GetKeysByPattern(outLinkKeyPattern(objectUUID, ">", "__type"))
		objectType := objectTypes[0]

		if err := createTypesLink(ctxProcessor, _CONTROLLER_TYPE, objectType, _CONTROLLER_SUBJECT_TYPE); err != nil {
			slog.Warn(err.Error())
			replyError(ctxProcessor, err)
			return
		}

		if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_controller_creation_time", "", []string{"id"}); err == nil {
			gaugeVec.With(prometheus.Labels{"id": self.ID}).Set(float64(time.Since(start).Microseconds()))
		}

		if time.Since(start).Milliseconds() > 500 {
			slog.Warn("create controller", "ctrl_id", self.ID, "dt", time.Since(start))
		}

		triggerPayload := easyjson.NewJSONObject()
		triggerPayload.SetByPath("body.triggers.update", easyjson.JSONFromArray([]string{controllerTriggerFunction}))
		ctxProcessor.Request(sfplugins.GolangLocalRequest, "functions.cmdb.api.object.update", objectUUID, &triggerPayload, nil)

		updatePayload := easyjson.NewJSONObject()
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, controllerUpdateFunction, self.ID, &updatePayload, nil); err != nil {
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

		if err := createObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
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

	start := time.Now()

	// ctxProcessor.ObjectMutexLock(false)
	// defer ctxProcessor.ObjectMutexUnlock()

	if err := deleteObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
		slog.Warn(err.Error())
		return
	}

	if err := deleteObjectsLink(ctxProcessor, caller.ID, self.ID); err != nil {
		slog.Warn(err.Error())
		return
	}

	if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_controller_unsub_time", "", []string{"id"}); err == nil {
		gaugeVec.With(prometheus.Labels{"id": self.ID}).Set(float64(time.Since(start).Microseconds()))
	}
	// TODO: delete controller if there is no subs
}

const controllerTriggerFunction = "functions.client.controller.trigger"

func (h *statefunHandler) controllerTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectUUID := ctxProcessor.Self.ID
	pattern := inLinkKeyPattern(objectUUID, ">")

	for _, v := range ctxProcessor.GlobalCache.GetKeysByPattern(pattern) {
		s := strings.Split(v, ".")
		if len(s) == 0 {
			continue
		}

		lt := s[len(s)-1]

		if lt != _CONTROLLER_SUBJECT_TYPE {
			continue
		}

		controllerID := s[len(s)-2]

		updatePayload := easyjson.NewJSONObject()
		ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, controllerUpdateFunction, controllerID, &updatePayload, nil)
	}
}

const controllerUpdateFunction = "functions.client.controller.update"

func (h *statefunHandler) updateController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self

	// ctxProcessor.ObjectMutexLock(false)
	// defer ctxProcessor.ObjectMutexUnlock()

	start := time.Now()

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

	if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_controller_update_subscribers_time", "", []string{"id", "count"}); err == nil {
		gaugeVec.With(prometheus.Labels{
			"id":    self.ID,
			"count": strconv.Itoa(len(subscribers)),
		}).Set(float64(time.Since(start).Microseconds()))
	}
}

const controllerConstructCreate = "functions.client.controller.construct.create"

/*
@property:<json path>

@function:<function.name.id>:[[arg1 value],[arg2 value],...[argN value]] - ideal

@function:getChildren(linkType) - now
*/
func (h *statefunHandler) createControllerConstruct(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectID := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload

	start := time.Now()

	decorators := parseDecorators(objectID, payload)
	construct := easyjson.NewJSONObject()

	for key, cd := range decorators {
		result := cd.Invoke(ctxProcessor)
		construct.SetByPath(key, result)
	}

	if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_controller_create_construct", "", []string{"id"}); err == nil {
		gaugeVec.With(prometheus.Labels{
			"id": objectID,
		}).Set(float64(time.Since(start).Microseconds()))
	}

	reply(ctxProcessor, "ok", construct)
}
