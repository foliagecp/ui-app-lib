package adapter

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/adapter/decorators"
	"github.com/foliagecp/ui-app-lib/internal/common"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

const (
	_CONTROLLER_DECLARATION = "declaration"
	_CONTROLLER_RESULT      = "result"
)

func RegisterFunctions(runtime *statefun.Runtime) {
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_START, startController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_CLEAR, clearController, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_OBJECT_UPDATE, updateControllerObject, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_OBJECT_TRIGGER, controllerObjectTrigger, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_CONSTRUCT, controllerConstruct, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1).SetAllowedRequestProviders(sfplugins.AutoRequestSelect))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_UPDATE, updateController, *statefun.NewFunctionTypeConfig())

	decorators.Register(runtime)

	runtime.RegisterOnAfterStartFunction(initSchema, false)
}

func initSchema(runtime *statefun.Runtime) error {
	cmdb, err := db.NewCMDBSyncClientFromRequestFunction(runtime.Request)
	if err != nil {
		return err
	}

	if err := cmdb.TypeCreate(
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_TYPE),
		easyjson.NewJSONObject(),
	); err != nil {
		return err
	}

	if err := cmdb.TypeCreate(
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_OBJECT_TYPE),
		easyjson.NewJSONObject(),
	); err != nil {
		return err
	}

	if err := cmdb.TypesLinkCreate(
		common.SetHubPreffix(runtime.Domain, inStatefun.SESSION_TYPE),
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_TYPE),
		inStatefun.CONTROLLER_TYPE,
		[]string{},
	); err != nil {
		return err
	}

	if err := cmdb.TypesLinkCreate(
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_TYPE),
		common.SetHubPreffix(runtime.Domain, inStatefun.SESSION_TYPE),
		inStatefun.SUBSCRIBER_TYPE,
		[]string{},
	); err != nil {
		return err
	}

	if err := cmdb.TypesLinkCreate(
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_TYPE),
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_OBJECT_TYPE),
		inStatefun.CONTROLLER_OBJECT_TYPE,
		[]string{},
	); err != nil {
		return err
	}

	return nil
}

/*
	payload: {
		declaration:{},
		uuids: []string,
		name: string,
	}

	controller_id: {
		name: string,
		declaration: {...},
	},
*/
func startController(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	self := ctx.Self
	caller := ctx.Caller
	payload := ctx.Payload

	body := ctx.GetObjectContext()
	body.SetByPath(_CONTROLLER_DECLARATION, payload.GetByPath(_CONTROLLER_DECLARATION))
	body.SetByPath("name", payload.GetByPath("name"))

	cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)

	if err := cmdb.ObjectCreate(self.ID, inStatefun.CONTROLLER_TYPE, *body); err != nil {
		cmdb.ObjectUpdate(self.ID, *body, true)
	}

	if err := cmdb.ObjectsLinkCreate(self.ID, caller.ID, caller.ID, []string{}); err != nil {
		slog.Warn("failed to create objects link between controller and session", "err", err.Error())
	}

	if err := cmdb.ObjectsLinkCreate(caller.ID, self.ID, self.ID, []string{}); err != nil {
		slog.Warn("failed to create objects link between session and controller", "err", err.Error())
	}

	uuids, _ := payload.GetByPath("uuids").AsArrayString()
	for _, objectUUID := range uuids {
		controllerObjectID := generate.UUID(self.ID + objectUUID).String()
		controllerObjectBody := easyjson.NewJSONObject()
		controllerObjectBody.SetByPath("object_id", easyjson.NewJSON(objectUUID))
		controllerObjectBody.SetByPath("parent", easyjson.NewJSON(self.ID))

		if err := cmdb.ObjectCreate(controllerObjectID, inStatefun.CONTROLLER_OBJECT_TYPE, controllerObjectBody); err != nil {
			slog.Warn("failed to create controller object", "err", err.Error())
		}

		objectType, err := common.ObjectType(cmdb, objectUUID)
		if err != nil {
			slog.Warn("failed to find uuid type", "err", err.Error())
			continue
		}

		if err := cmdb.TypesLinkCreate(inStatefun.CONTROLLER_OBJECT_TYPE, objectType, inStatefun.CONTROLLER_SUBJECT_TYPE, []string{}); err != nil {
			slog.Warn("failed to create types link between controller object and uuid", "err", err.Error())
		}

		if err := cmdb.ObjectsLinkCreate(controllerObjectID, objectUUID, objectUUID, []string{}); err != nil {
			slog.Warn("failed to create objects link between controller object and uuid", "err", err.Error())
		}

		if err := cmdb.ObjectsLinkCreate(self.ID, controllerObjectID, controllerObjectID, []string{}); err != nil {
			slog.Warn("failed to create objects link between controller and controller object", "err", err.Error())
		}

		cmdb.TriggerObjectSet(objectType, db.UpdateTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER)

		// send to update сontroller object
		ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.CONTROLLER_OBJECT_UPDATE, controllerObjectID, nil, nil)
	}
}

// fetch declaration from controller
// send to construct
// compare result
// if it's different send update to controller
func updateControllerObject(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	controllerObjectID := ctx.Self.ID
	slog.Info("Update controller object", "id", controllerObjectID)

	body := ctx.GetObjectContext()
	parentControllerID, ok := body.GetByPath("parent").AsString()
	if !ok {
		slog.Warn("empty controller id")
		return
	}

	controllerBody, err := ctx.Domain.Cache().GetValueAsJSON(parentControllerID)
	if err != nil {
		slog.Error(err.Error())
		return
	}

	controllerDeclaration := controllerBody.GetByPath(_CONTROLLER_DECLARATION)

	realObjectID := body.GetByPath("object_id").AsStringDefault("")

	result, err := ctx.Request(sfplugins.AutoRequestSelect, inStatefun.CONTROLLER_CONSTRUCT, realObjectID, &controllerDeclaration, nil)
	if err != nil {
		result = easyjson.NewJSONObject().GetPtr()
	}

	newResult := result.GetByPath("result")

	body.SetByPath("result", newResult)
	ctx.SetObjectContext(body)

	update := easyjson.NewJSONObject()
	update.SetByPath("result", newResult)
	update.SetByPath("object_id", easyjson.NewJSON(realObjectID))

	slog.Info("Send update upstream to controller", "id", parentControllerID)
	// send update to controller subs
	ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.CONTROLLER_UPDATE, parentControllerID, &update, nil)
}

func controllerObjectTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectUUID := ctxProcessor.Self.ID
	pattern := common.InLinkKeyPattern(objectUUID, ">")

	for _, v := range ctxProcessor.Domain.Cache().GetKeysByPattern(pattern) {
		s := strings.Split(v, ".")
		if len(s) == 0 {
			continue
		}

		controllerObjectID := s[len(s)-2]

		updatePayload := easyjson.NewJSONObject()
		err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal,
			inStatefun.CONTROLLER_OBJECT_UPDATE, controllerObjectID, &updatePayload, nil)
		if err != nil {
			slog.Warn(err.Error())
		}
	}
}

func updateController(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	self := ctx.Self
	body := ctx.GetObjectContext()
	controllerName, _ := body.GetByPath("name").AsString()

	payload := ctx.Payload
	update := payload.GetByPath("result")
	realObjectID, _ := payload.GetByPath("object_id").AsString()

	path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, realObjectID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, update)

	subscribers := getChildrenUUIDSByLinkType(ctx, self.ID, inStatefun.SUBSCRIBER_TYPE)

	slog.Info("Send update to subscribers", "subscribers", subscribers)
	for _, subID := range subscribers {
		if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.PREPARE_EGRESS, subID, &updateReply, nil); err != nil {
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
func controllerConstruct(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	id := ctx.Self.ID
	payload := ctx.Payload

	decorators := parseDecorators(id, payload)
	construct := easyjson.NewJSONObject()

	for key, d := range decorators {
		result := d.Decorate(ctx)
		construct.SetByPath(key, result)
	}

	common.Reply(ctx, "ok", construct)
}

func clearController(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	return
}