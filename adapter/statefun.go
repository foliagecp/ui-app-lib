package adapter

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/foliagecp/ui-app-lib/internal/common"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	internalStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	_CONTROLLER_DECLARATION = "declaration"
	_CONTROLLER_RESULT      = "result"
)

func RegisterFunctions(runtime *statefun.Runtime) {
	statefun.NewFunctionType(runtime, internalStatefun.CONTROLLER_SETUP, setupController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, internalStatefun.CONTROLLER_UNSUB, unsubController, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, internalStatefun.CONTROLLER_CONSTRUCT, controllerConstruct, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, internalStatefun.CONTROLLER_RESULT_COMPARE, controllerResultCompare, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, internalStatefun.CONTROLLER_UPDATE, updateController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, internalStatefun.CONTROLLER_TRIGGER, controllerTrigger, *statefun.NewFunctionTypeConfig())
}

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
func setupController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
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

		if err := common.CreateObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := common.CreateObjectsLink(ctxProcessor, caller.ID, self.ID); err != nil {
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
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, internalStatefun.CONTROLLER_UPDATE, self.ID, &updatePayload, nil); err != nil {
			log.Warn(err.Error())
		}
	} else {
		subscribers := common.GetChildrenUUIDSByLinkType(ctxProcessor, self.ID, internalStatefun.SUBSCRIBER_TYPE)

		for _, v := range subscribers {
			if v == caller.ID {
				return
			}
		}

		if err := common.CreateObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := common.CreateObjectsLink(ctxProcessor, caller.ID, self.ID); err != nil {
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

			if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, internalStatefun.PREPARE_EGRESS, caller.ID, &reply, nil); err != nil {
				log.Warn(err.Error())
			}
		}
	}
}

func unsubController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	caller := ctxProcessor.Caller
	self := ctxProcessor.Self

	defer common.ReplyOk(ctxProcessor)

	start := time.Now()

	defer func() {
		if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_controller_unsub_time", "", []string{"id"}); err == nil {
			gaugeVec.With(prometheus.Labels{"id": self.ID}).Set(float64(time.Since(start).Microseconds()))
		}
	}()

	if err := common.DeleteObjectsLink(ctxProcessor, self.ID, caller.ID); err != nil {
		slog.Warn(err.Error())
		return
	}

	if err := common.DeleteObjectsLink(ctxProcessor, caller.ID, self.ID); err != nil {
		slog.Warn(err.Error())
		return
	}
}

func controllerTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectUUID := ctxProcessor.Self.ID
	pattern := common.InLinkKeyPattern(objectUUID, ">")

	for _, v := range ctxProcessor.GlobalCache.GetKeysByPattern(pattern) {
		s := strings.Split(v, ".")
		if len(s) == 0 {
			continue
		}

		lt := s[len(s)-1]

		if lt != internalStatefun.CONTROLLER_SUBJECT_TYPE {
			continue
		}

		controllerID := s[len(s)-2]

		updatePayload := easyjson.NewJSONObject()
		err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal,
			internalStatefun.CONTROLLER_UPDATE, controllerID, &updatePayload, nil)
		if err != nil {
			slog.Warn(err.Error())
		}
	}
}

func updateController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	log := slog.With("controller_id", self.ID)

	object := ctxProcessor.GetObjectContext()

	controllerName, _ := object.GetByPath("name").AsString()
	controllerUUID, _ := object.GetByPath("object_id").AsString()
	resultRef, _ := object.GetByPath("result_ref").AsString()
	declaration := object.GetByPath(_CONTROLLER_DECLARATION)

	result, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, internalStatefun.CONTROLLER_CONSTRUCT, controllerUUID, &declaration, nil)
	if err != nil {
		log.Warn("Controller construct failed", "error", err)
		result = easyjson.NewJSONObject().GetPtr()
	}

	newControllerResult := result.GetByPath("payload.result")

	result, err = ctxProcessor.Request(sfplugins.GolangLocalRequest, internalStatefun.CONTROLLER_RESULT_COMPARE, resultRef, &newControllerResult, nil)
	if err := common.CheckRequestError(result, err); err != nil {
		return
	}

	path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, newControllerResult)

	subscribers := common.GetChildrenUUIDSByLinkType(ctxProcessor, self.ID, internalStatefun.SUBSCRIBER_TYPE)

	for _, subID := range subscribers {
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, internalStatefun.PREPARE_EGRESS, subID, &updateReply, nil); err != nil {
			slog.Warn(err.Error())
		}
	}
}

/*
@property:<json path>

@function:<function.name.id>:[[arg1 value],[arg2 value],...[argN value]] - ideal

@function:getChildren(linkType) - now
`
*/
func controllerConstruct(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
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

	common.Reply(ctxProcessor, "ok", construct)
}

func controllerResultCompare(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	currentResult := ctxProcessor.GetObjectContext()
	newResult := ctxProcessor.Payload

	if newResult.Equals(*currentResult) {
		common.ReplyError(ctxProcessor, errors.New("same result"))
		return
	}

	ctxProcessor.SetObjectContext(newResult)

	common.ReplyOk(ctxProcessor)
}

func initController(ctx *sfplugins.StatefunContextProcessor, id string, payload *easyjson.JSON) error {
	objectUUID := payload.GetByPath("object_id").AsStringDefault("")
	controllerResultID := generate.UUID(id + "result").String()

	controllerBody := easyjson.NewJSONObject()
	controllerBody.SetByPath(_CONTROLLER_DECLARATION, payload.GetByPath(_CONTROLLER_DECLARATION))
	controllerBody.SetByPath("name", payload.GetByPath("name"))
	controllerBody.SetByPath("object_id", easyjson.NewJSON(objectUUID))
	controllerBody.SetByPath("result_ref", easyjson.NewJSON(controllerResultID))

	if err := common.CreateObject(ctx, id, internalStatefun.CONTROLLER_TYPE, controllerBody); err != nil {
		return err
	}

	if err := common.CreateObject(ctx, controllerResultID, internalStatefun.CONTROLLER_RESULT_TYPE, easyjson.NewJSONObject()); err != nil {
		return err
	}

	if err := common.CreateObjectsLink(ctx, id, controllerResultID); err != nil {
		return err
	}

	objectTypes := common.GetChildrenUUIDSByLinkType(ctx, objectUUID, "__type")
	if len(objectTypes) == 0 {
		return fmt.Errorf("target object miss type")
	}

	objectType := objectTypes[0]

	if err := common.CreateTypesLink(ctx, internalStatefun.CONTROLLER_TYPE, objectType, internalStatefun.CONTROLLER_SUBJECT_TYPE); err != nil {
		return err
	}

	if err := common.CreateObjectsLink(ctx, id, objectUUID); err != nil {
		return err
	}

	triggerPayload := easyjson.NewJSONObject()
	triggerPayload.SetByPath("body.triggers.update", easyjson.JSONFromArray([]string{internalStatefun.CONTROLLER_TRIGGER}))
	_, err := ctx.Request(sfplugins.AutoSelect, "functions.cmdb.api.type.update", objectType, &triggerPayload, nil)
	if err != nil {
		return err
	}

	return nil
}
