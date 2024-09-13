package adapter

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/foliagecp/ui-app-lib/adapter/decorators"
	"github.com/foliagecp/ui-app-lib/internal/common"
	"github.com/foliagecp/ui-app-lib/internal/egress"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

var checkUpdates = system.GetEnvMustProceed("UI_APP_LIB_CHECK_UPDATES", true)

const (
	_CONTROLLER_DECLARATION = "declaration"
	_CONTROLLER_RESULT      = "result"
)

func RegisterFunctions(runtime *statefun.Runtime) {
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_START, StartController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_CLEAR, ClearController, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_OBJECT_UPDATE, UpdateControllerObject, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_OBJECT_TRIGGER, ControllerObjectTrigger, *statefun.NewFunctionTypeConfig())
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_CONSTRUCT, ControllerConstruct, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1).SetAllowedRequestProviders(sfplugins.AutoRequestSelect))

	decorators.Register(runtime)

	runtime.RegisterOnAfterStartFunction(InitSchema, false)
}

func InitSchema(runtime *statefun.Runtime) error {
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
func StartController(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	self := ctx.Self
	caller := ctx.Caller
	payload := ctx.Payload

	sessionId := payload.GetByPath("session_id").AsStringDefault("")

	body := ctx.GetObjectContext()
	body.SetByPath(_CONTROLLER_DECLARATION, payload.GetByPath(_CONTROLLER_DECLARATION))
	body.SetByPath("name", payload.GetByPath("name"))
	body.SetByPath("plugin", payload.GetByPath("plugin"))

	cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)

	if err := cmdb.ObjectCreate(self.ID, inStatefun.CONTROLLER_TYPE, *body); err != nil {
		cmdb.ObjectUpdate(self.ID, *body, true)
	}

	if err := cmdb.ObjectsLinkCreate(self.ID, caller.ID, caller.ID, []string{}); err != nil {
		if !common.ErrorAlreadyExists(err) {
			slog.Warn("failed to create objects link between controller and session", "err", err.Error())
			return
		}
	}

	if err := cmdb.ObjectsLinkCreate(caller.ID, self.ID, self.ID, []string{}); err != nil {
		if !common.ErrorAlreadyExists(err) {
			slog.Warn("failed to create objects link between session and controller", "err", err.Error())
			return
		}
	}

	uuids, _ := payload.GetByPath("uuids").AsArrayString()
	typesTriggersCreated := map[string]struct{}{}
	for _, objectUUID := range uuids {
		controllerObjectID := generate.UUID(self.ID + objectUUID).String()
		controllerObjectBody := easyjson.NewJSONObject()
		controllerObjectBody.SetByPath("object_id", easyjson.NewJSON(objectUUID))
		controllerObjectBody.SetByPath("parent", easyjson.NewJSON(self.ID))

		if err := cmdb.ObjectCreate(controllerObjectID, inStatefun.CONTROLLER_OBJECT_TYPE, controllerObjectBody); err != nil {
			if !common.ErrorAlreadyExists(err) {
				slog.Warn("failed to create controller object", "err", err.Error())
				continue
			}
		}

		objectType, err := common.ObjectType(cmdb, objectUUID)
		if err != nil {
			if !common.ErrorAlreadyExists(err) {
				slog.Warn("failed to find uuid type", "err", err.Error())
				continue
			}
		}

		if err := cmdb.TypesLinkCreate(inStatefun.CONTROLLER_OBJECT_TYPE, objectType, inStatefun.CONTROLLER_SUBJECT_TYPE, []string{}); err != nil {
			if !common.ErrorAlreadyExists(err) {
				slog.Warn("failed to create types link between controller object and uuid", "err", err.Error())
				continue
			}
		}

		if err := cmdb.ObjectsLinkCreate(controllerObjectID, objectUUID, "uiapplib_"+objectUUID, []string{}); err != nil {
			if !common.ErrorAlreadyExists(err) {
				slog.Warn("failed to create objects link between controller object and uuid", "err", err.Error())
				continue
			}
		}

		if err := cmdb.ObjectsLinkCreate(self.ID, controllerObjectID, controllerObjectID, []string{}); err != nil {
			if !common.ErrorAlreadyExists(err) {
				slog.Warn("failed to create objects link between controller and controller object", "err", err.Error())
				continue
			}
		}

		if _, ok := typesTriggersCreated[objectType]; !ok {
			cmdb.TriggerObjectSet(objectType, db.UpdateTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER)
			cmdb.TriggerObjectSet(objectType, db.DeleteTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER)

			if typeData, err := cmdb.TypeRead(objectType); err == nil {
				linksIn := typeData.GetByPath("links.in")
				for i := 0; i < linksIn.ArraySize(); i++ {
					linkData := typeData.GetByPath("links.in").ArrayElement(i)
					if linkData.GetByPath("name").AsStringDefault("") == objectType { // link from other type
						fromType := linkData.GetByPath("from").AsStringDefault("")
						cmdb.TriggerLinkSet(fromType, objectType, db.CreateTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER)
						cmdb.TriggerLinkSet(fromType, objectType, db.DeleteTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER)
					}
				}
			}

			typesTriggersCreated[objectType] = struct{}{}
		}

		// send to update сontroller object
		payload := easyjson.NewJSONObjectWithKeyValue("force_update_session_id", easyjson.NewJSON(sessionId))
		ctx.Signal(sfplugins.AutoSignalSelect, inStatefun.CONTROLLER_OBJECT_UPDATE, controllerObjectID, &payload, nil)
	}
}

// fetch declaration from controller
// send to construct
// compare result
// if it's different send update to controller
func UpdateControllerObject(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
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

	if !result.IsNonEmptyObject() {
		return
	}

	newResult := result.GetByPath("result")

	forceUpdateSessionId := ctx.Payload.GetByPath("force_update_session_id").AsStringDefault("")
	if len(forceUpdateSessionId) == 0 && checkUpdates {
		oldResult := body.GetByPath("result")

		if oldResult.Equals(newResult) {
			return
		}
	}

	body.SetByPath("result", newResult)
	ctx.SetObjectContext(body)

	// send update to controller subs -----------------------------------------
	controllerPlugin, _ := controllerBody.GetByPath("plugin").AsString()

	path := fmt.Sprintf("payload.plugins.%s.%s", controllerPlugin, realObjectID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, newResult)

	subscribers := getChildrenUUIDSByLinkTypeLocal(ctx, parentControllerID, inStatefun.SUBSCRIBER_TYPE)

	if len(forceUpdateSessionId) == 0 {
		slog.Info("Send update to subscribers", "subscribers", subscribers)
		for _, subID := range subscribers {
			if err := egress.SendToSessionEgress(ctx, subID, &updateReply); err != nil {
				slog.Warn(err.Error())
			}
		}
	} else {
		slog.Info("Send update to force update requested session only", "subscribers", subscribers)
		if err := egress.SendToSessionEgress(ctx, forceUpdateSessionId, &updateReply); err != nil {
			slog.Warn(err.Error())
		}
	}
	// ------------------------------------------------------------------------
}

func ControllerObjectTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectUUID := ctxProcessor.Self.ID
	fmt.Printf("           ControllerObjectTrigger on object %s\n         data: %s\n", objectUUID, ctxProcessor.Payload.ToString())

	cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctxProcessor.Request)
	if objData, err := cmdb.ObjectRead(objectUUID); err == nil {
		linksIn := objData.GetByPath("links.in")
		for i := 0; i < linksIn.ArraySize(); i++ {
			linkData := objData.GetByPath("links.in").ArrayElement(i)
			fromId := linkData.GetByPath("from").AsStringDefault("")
			linkName := linkData.GetByPath("name").AsStringDefault("")
			if strings.Contains(linkName, "uiapplib_") {
				if ctxProcessor.Payload.PathExists("trigger.object.delete") {
					fmt.Printf("          >> ControllerObjectTrigger NOT DELETE on object %s on controller object %s\n", objectUUID, fromId)
					cmdb.ObjectDelete(fromId)
					continue
				}
				updatePayload := easyjson.NewJSONObject()

				fmt.Printf("          >> ControllerObjectTrigger NOT DELETE on object %s on controller object %s\n", objectUUID, fromId)
				err := ctxProcessor.Signal(sfplugins.AutoSignalSelect, inStatefun.CONTROLLER_OBJECT_UPDATE, fromId, &updatePayload, nil)
				if err != nil {
					slog.Warn(err.Error())
				}
			}
		}
	}
}

func UpdateController(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	self := ctx.Self
	body := ctx.GetObjectContext()
	controllerPlugin, _ := body.GetByPath("plugin").AsString()

	payload := ctx.Payload
	forceUpdateSessionId := ctx.Payload.GetByPath("force_update_session_id").AsStringDefault("")
	update := payload.GetByPath("result")
	realObjectID, _ := payload.GetByPath("object_id").AsString()

	path := fmt.Sprintf("payload.plugins.%s.%s", controllerPlugin, realObjectID)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, update)

	subscribers := getChildrenUUIDSByLinkTypeLocal(ctx, self.ID, inStatefun.SUBSCRIBER_TYPE)

	if len(forceUpdateSessionId) == 0 {
		slog.Info("Send update to subscribers", "subscribers", subscribers)
		for _, subID := range subscribers {
			if err := egress.SendToSessionEgress(ctx, subID, &updateReply); err != nil {
				slog.Warn(err.Error())
			}
		}
	} else {
		slog.Info("Send update to force update requested session only", "subscribers", subscribers)
		if err := egress.SendToSessionEgress(ctx, forceUpdateSessionId, &updateReply); err != nil {
			slog.Warn(err.Error())
		}
	}
}

/*
@property:<json path>

@function:<function.name.id>:[[arg1 value],[arg2 value],...[argN value]] - ideal

@function:getChildren(linkType) - now
*/
func ControllerConstruct(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
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

func ClearController(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	return
}
