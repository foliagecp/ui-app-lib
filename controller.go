// Copyright 2023 NJWS Inc.

package uilib

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/foliagecp/easyjson"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	_CONTROLLER_DECLARATION = "declaration"
	_CONTROLLER_RESULT      = "result"
)

// decorators
const (
	_PROPERTY = "@property"
	_FUNCTION = "@function"
)

const controllerSetupFunction = "functions.ui.app.controller.setup"

/*
	payload: {
		body:{},
	}

	controller_id: {
		name: string,
		object_id: string,
		result_ref: uuid,
		declaration: {...},
		subscribers: as links
	},
*/
func (h *statefunHandler) setupController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	caller := ctxProcessor.Caller
	payload := ctxProcessor.Payload

	log := slog.With("controller_id", self.ID)

	object := ctxProcessor.GetObjectContext()
	declarationIsEmpty := !object.GetByPath(_CONTROLLER_DECLARATION).IsNonEmptyObject()

	if declarationIsEmpty {
		start := time.Now()

		if err := initController(ctxProcessor, self.ID, payload); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := createObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := createObjectsLink(ctxProcessor, caller.ID, self.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		if time.Since(start).Milliseconds() > 500 {
			log.Warn("create controller", "dt", time.Since(start))
		}

		// if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_controller_creation_time", "", []string{"id"}); err == nil {
		// 	gaugeVec.With(prometheus.Labels{"id": self.ID}).Set(float64(time.Since(start).Microseconds()))
		// }

		updatePayload := easyjson.NewJSONObject()
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, controllerUpdateFunction, self.ID, &updatePayload, nil); err != nil {
			log.Warn(err.Error())
		}
	} else {
		subscribers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, _SUBSCRIBER_TYPE)

		for _, v := range subscribers {
			if v == caller.ID {
				return
			}
		}

		if err := createObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := createObjectsLink(ctxProcessor, caller.ID, self.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		controllerName, _ := object.GetByPath("name").AsString()
		controllerUUID, _ := object.GetByPath("object_id").AsString()
		resultRef, _ := object.GetByPath("result_ref").AsString()

		if result, _ := ctxProcessor.GlobalCache.GetValueAsJSON(resultRef); result.IsNonEmptyObject() {
			path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

			reply := easyjson.NewJSONObject()
			reply.SetByPath(path, *result)

			if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, caller.ID, &reply, nil); err != nil {
				log.Warn(err.Error())
			}
		}
	}
}

const controllerUnsubFunction = "functions.ui.app.controller.unsub"

func (h *statefunHandler) unsubController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	caller := ctxProcessor.Caller
	self := ctxProcessor.Self

	defer replyOk(ctxProcessor)

	start := time.Now()

	defer func() {
		if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_controller_unsub_time", "", []string{"id"}); err == nil {
			gaugeVec.With(prometheus.Labels{"id": self.ID}).Set(float64(time.Since(start).Microseconds()))
		}
	}()

	if err := deleteObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
		slog.Warn(err.Error())
		return
	}

	if err := deleteObjectsLink(ctxProcessor, caller.ID, self.ID); err != nil {
		slog.Warn(err.Error())
		return
	}
}

const controllerTriggerFunction = "functions.ui.app.controller.trigger"

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
		err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal,
			controllerUpdateFunction, controllerID, &updatePayload, nil)
		if err != nil {
			slog.Warn(err.Error())
		}
	}
}

const controllerUpdateFunction = "functions.ui.app.controller.update"

func (h *statefunHandler) updateController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	log := slog.With("controller_id", self.ID)

	object := ctxProcessor.GetObjectContext()

	controllerName, _ := object.GetByPath("name").AsString()
	controllerUUID, _ := object.GetByPath("object_id").AsString()
	resultRef, _ := object.GetByPath("result_ref").AsString()
	declaration := object.GetByPath(_CONTROLLER_DECLARATION)

	result, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerConstruct, controllerUUID, &declaration, nil)
	if err != nil {
		log.Warn("Controller construct failed", "error", err)
		result = easyjson.NewJSONObject().GetPtr()
	}

	newControllerResult := result.GetByPath("payload.result")

	result, err = ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerResultCompare, resultRef, &newControllerResult, nil)
	if err := checkRequestError(result, err); err != nil {
		return
	}

	path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, newControllerResult)

	subscribers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, _SUBSCRIBER_TYPE)

	for _, subID := range subscribers {
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, subID, &updateReply, nil); err != nil {
			slog.Warn(err.Error())
		}
	}
}

const controllerConstruct = "functions.ui.app.controller.construct"

/*
@property:<json path>

@function:<function.name.id>:[[arg1 value],[arg2 value],...[argN value]] - ideal

@function:getChildren(linkType) - now
`
*/
func (h *statefunHandler) controllerConstruct(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	id := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload

	start := time.Now()

	decorators := parseDecorators(id, payload)
	construct := easyjson.NewJSONObject()

	for key, cd := range decorators {
		result := cd.Invoke(ctxProcessor)
		construct.SetByPath(key, result)
	}

	if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_controller_construct", "", []string{"id"}); err == nil {
		gaugeVec.With(prometheus.Labels{
			"id": id,
		}).Set(float64(time.Since(start).Microseconds()))
	}

	reply(ctxProcessor, "ok", construct)
}

const controllerResultCompare = "functions.ui.app.controller.result.compare"

func (h *statefunHandler) controllerResultCompare(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	currentResult := ctxProcessor.GetObjectContext()
	newResult := ctxProcessor.Payload

	if newResult.Equals(*currentResult) {
		replyError(ctxProcessor, errors.New("same result"))
		return
	}

	ctxProcessor.SetObjectContext(newResult)

	replyOk(ctxProcessor)
}

func initController(ctx *sfplugins.StatefunContextProcessor, id string, payload *easyjson.JSON) error {
	objectUUID := payload.GetByPath("object_id").AsStringDefault("")
	controllerResultID := generateUUID(id + "result").String()

	controllerBody := easyjson.NewJSONObject()
	controllerBody.SetByPath(_CONTROLLER_DECLARATION, payload.GetByPath(_CONTROLLER_DECLARATION))
	controllerBody.SetByPath("name", payload.GetByPath("name"))
	controllerBody.SetByPath("object_id", easyjson.NewJSON(objectUUID))
	controllerBody.SetByPath("result_ref", easyjson.NewJSON(controllerResultID))

	if err := createObject(ctx, id, _CONTROLLER_TYPE, controllerBody); err != nil {
		return err
	}

	if err := createObject(ctx, controllerResultID, _CONTROLLER_RESULT_TYPE, easyjson.NewJSONObject()); err != nil {
		return err
	}

	if err := createObjectsLink(ctx, id, controllerResultID); err != nil {
		return err
	}

	objectTypes := getChildrenUUIDSByLinkType(ctx, objectUUID, "__type")
	if len(objectTypes) == 0 {
		return fmt.Errorf("target object miss type")
	}

	objectType := objectTypes[0]

	if err := createTypesLink(ctx, _CONTROLLER_TYPE, objectType, _CONTROLLER_SUBJECT_TYPE); err != nil {
		return err
	}

	if err := createObjectsLink(ctx, id, objectUUID); err != nil {
		return err
	}

	triggerPayload := easyjson.NewJSONObject()
	triggerPayload.SetByPath("body.triggers.update", easyjson.JSONFromArray([]string{controllerTriggerFunction}))
	_, err := ctx.Request(sfplugins.GolangLocalRequest, "functions.cmdb.api.type.update", objectType, &triggerPayload, nil)
	if err != nil {
		return err
	}

	return nil
}
