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
	"github.com/foliagecp/ui-app-lib/internal/common"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

const (
	_CONTROLLER_DECLARATION = "declaration"
	_CONTROLLER_RESULT      = "result"
)

func RegisterFunctions(runtime *statefun.Runtime) {
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_SETUP, setupController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_UNSUB, unsubController, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_CONSTRUCT, controllerConstruct, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1).SetAllowedRequestProviders(sfplugins.AutoRequestSelect))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_RESULT_COMPARE, controllerResultCompare, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1).SetAllowedRequestProviders(sfplugins.AutoRequestSelect))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_UPDATE, updateController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_TRIGGER, controllerTrigger, *statefun.NewFunctionTypeConfig())

	runtime.RegisterOnAfterStartFunction(initSchema, false)
}

func initSchema(runtime *statefun.Runtime) error {
	if err := common.CreateType(runtime, inStatefun.CONTROLLER_TYPE, easyjson.NewJSONObject()); err != nil {
		return err
	}

	if err := common.CreateType(runtime, inStatefun.CONTROLLER_RESULT_TYPE, easyjson.NewJSONObject()); err != nil {
		return err
	}

	if err := common.CreateTypesLink(runtime, runtime.Domain.CreateObjectIDWithHubDomain(inStatefun.SESSION_TYPE, false), inStatefun.CONTROLLER_TYPE, inStatefun.CONTROLLER_TYPE); err != nil {
		return err
	}

	if err := common.CreateTypesLink(runtime, inStatefun.CONTROLLER_TYPE, runtime.Domain.CreateObjectIDWithHubDomain(inStatefun.SESSION_TYPE, false), inStatefun.SUBSCRIBER_TYPE); err != nil {
		return err
	}

	if err := common.CreateTypesLink(runtime, inStatefun.CONTROLLER_TYPE, inStatefun.CONTROLLER_RESULT_TYPE, inStatefun.CONTROLLER_RESULT_TYPE); err != nil {
		return err
	}

	return nil
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
func setupController(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	self := ctx.Self
	caller := ctx.Caller
	payload := ctx.Payload
	sCtx := common.NewStatefunContext(ctx)

	log := slog.With("controller_id", self.ID)

	object := ctx.GetObjectContext()
	declarationIsEmpty := !object.GetByPath(_CONTROLLER_DECLARATION).IsNonEmptyObject()

	if declarationIsEmpty {
		start := time.Now()

		if err := initController(sCtx, self.ID, payload); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := common.CreateObjectsLink(sCtx, self.ID, caller.ID, caller.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := common.CreateObjectsLink(sCtx, caller.ID, self.ID, self.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		if time.Since(start).Milliseconds() > 500 {
			log.Warn("create controller", "dt", time.Since(start))
		}

		updatePayload := easyjson.NewJSONObject()
		if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.CONTROLLER_UPDATE, self.ID, &updatePayload, nil); err != nil {
			log.Warn(err.Error())
		}
	} else {
		subscribers := common.GetChildrenUUIDSByLinkType(ctx, self.ID, inStatefun.SUBSCRIBER_TYPE)

		for _, v := range subscribers {
			if v == caller.ID {
				return
			}
		}

		if err := common.CreateObjectsLink(sCtx, self.ID, caller.ID, caller.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := common.CreateObjectsLink(sCtx, caller.ID, self.ID, caller.ID); err != nil {
			log.Warn(err.Error())
			return
		}

		controllerName, _ := object.GetByPath("name").AsString()
		controllerUUID, _ := object.GetByPath("object_id").AsString()
		resultRef, _ := object.GetByPath("result_ref").AsString()

		if result, _ := ctx.Domain.Cache().GetValueAsJSON(resultRef); result.IsNonEmptyObject() {
			path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

			reply := easyjson.NewJSONObject()
			reply.SetByPath(path, *result)

			if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.PREPARE_EGRESS, caller.ID, &reply, nil); err != nil {
				log.Warn(err.Error())
			}
		}
	}
}

func unsubController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	caller := ctxProcessor.Caller
	self := ctxProcessor.Self
	sCtx := common.NewStatefunContext(ctxProcessor)

	defer common.ReplyOk(ctxProcessor)

	if err := common.DeleteObjectsLink(sCtx, self.ID, caller.ID); err != nil {
		slog.Warn(err.Error())
		return
	}

	if err := common.DeleteObjectsLink(sCtx, caller.ID, self.ID); err != nil {
		slog.Warn(err.Error())
		return
	}
}

func controllerTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectUUID := ctxProcessor.Self.ID
	pattern := common.InLinkKeyPattern(objectUUID, ">")

	for _, v := range ctxProcessor.Domain.Cache().GetKeysByPattern(pattern) {
		s := strings.Split(v, ".")
		if len(s) == 0 {
			continue
		}

		controllerID := s[len(s)-2]

		updatePayload := easyjson.NewJSONObject()
		err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal,
			inStatefun.CONTROLLER_UPDATE, controllerID, &updatePayload, nil)
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

	result, err := ctxProcessor.Request(sfplugins.AutoRequestSelect, inStatefun.CONTROLLER_CONSTRUCT, controllerUUID, &declaration, nil)
	if err != nil {
		log.Warn("Controller construct failed", "error", err)
		result = easyjson.NewJSONObject().GetPtr()
	}

	newControllerResult := result.GetByPath("result")

	result, err = ctxProcessor.Request(sfplugins.AutoRequestSelect, inStatefun.CONTROLLER_RESULT_COMPARE, resultRef, &newControllerResult, nil)
	if err := common.CheckRequestError(result, err); err != nil {
		log.Warn(err.Error())
		return
	}

	path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, newControllerResult)

	subscribers := common.GetChildrenUUIDSByLinkType(ctxProcessor, self.ID, inStatefun.SUBSCRIBER_TYPE)

	for _, subID := range subscribers {
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.PREPARE_EGRESS, subID, &updateReply, nil); err != nil {
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

	decorators := parseDecorators(id, payload)
	construct := easyjson.NewJSONObject()

	for key, cd := range decorators {
		result := cd.Invoke(ctxProcessor)
		construct.SetByPath(key, result)
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

func initController(ctx *common.StatefunContext, id string, payload *easyjson.JSON) error {
	objectUUID := payload.GetByPath("object_id").AsStringDefault("")
	controllerResultID := ctx.Domain.CreateObjectIDWithThisDomain(generate.UUID(id+"result").String(), false)

	controllerBody := easyjson.NewJSONObject()
	controllerBody.SetByPath(_CONTROLLER_DECLARATION, payload.GetByPath(_CONTROLLER_DECLARATION))
	controllerBody.SetByPath("name", payload.GetByPath("name"))
	controllerBody.SetByPath("object_id", easyjson.NewJSON(objectUUID))
	controllerBody.SetByPath("result_ref", easyjson.NewJSON(controllerResultID))

	if err := common.CreateObject(ctx, id, inStatefun.CONTROLLER_TYPE, controllerBody); err != nil {
		return fmt.Errorf("create controller: %w", err)
	}

	if err := common.CreateObject(ctx, controllerResultID, inStatefun.CONTROLLER_RESULT_TYPE, easyjson.NewJSONObject()); err != nil {
		return fmt.Errorf("create controller result %s: %w", controllerResultID, err)
	}

	if err := common.CreateObjectsLink(ctx, id, controllerResultID, controllerResultID); err != nil {
		return fmt.Errorf("create link between controller and result: %w", err)
	}

	objectType, err := common.FindObjectType(ctx, objectUUID)
	if err != nil {
		return fmt.Errorf("find object type: %w", err)
	}

	if err := common.CreateTypesLink(ctx, inStatefun.CONTROLLER_TYPE, objectType, inStatefun.CONTROLLER_SUBJECT_TYPE); err != nil {
		return fmt.Errorf("create types link: %w", err)
	}

	if err := common.CreateObjectsLink(ctx, id, objectUUID, objectUUID); err != nil {
		return fmt.Errorf("create types link: %w", err)
	}

	triggerPayload := easyjson.NewJSONObject()
	triggerPayload.SetByPath("body.triggers.update", easyjson.JSONFromArray([]string{inStatefun.CONTROLLER_TRIGGER}))
	_, err = ctx.Request(sfplugins.AutoRequestSelect, "functions.cmdb.api.type.update", objectType, &triggerPayload, nil)
	if err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}

	return nil
}
