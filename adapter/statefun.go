package adapter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	"github.com/foliagecp/sdk/statefun/logger"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/foliagecp/ui-app-lib/adapter/decorators"
	"github.com/foliagecp/ui-app-lib/internal/common"
	"github.com/foliagecp/ui-app-lib/internal/egress"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

var (
	checkUpdates                                              = system.GetEnvMustProceed("UI_APP_LIB_CHECK_UPDATES", true)
	controllerObjectOnTriggerWindowUpdaterTasks               = map[string]*easyjson.JSON{}
	controllerObjectOnTriggerWindowUpdaterWindowTimeout       = 2 * time.Second
	controllerObjectOnTriggerWindowUpdaterWindowStartNs int64 = 0
	controllerObjectOnTriggerWindowUpdaterMutex         sync.Mutex
)

const (
	_CONTROLLER_DECLARATION = "declaration"
	_CONTROLLER_RESULT      = "result"
)

func controllerObjectOnTriggerWindowUpdater(runtime *statefun.Runtime) {
	for {
		time.Sleep(2 * time.Second)

		controllerObjectOnTriggerWindowUpdaterMutex.Lock()
		if controllerObjectOnTriggerWindowUpdaterWindowStartNs > 0 {
			if controllerObjectOnTriggerWindowUpdaterWindowStartNs+int64(controllerObjectOnTriggerWindowUpdaterWindowTimeout) < system.GetCurrentTimeNs() {
				for objectUUI, updatePayload := range controllerObjectOnTriggerWindowUpdaterTasks {
					err := runtime.Signal(sfplugins.AutoSignalSelect, inStatefun.CONTROLLER_OBJECT_UPDATE, objectUUI, updatePayload, nil)
					if err != nil {
						logger.GetLogger().Warn(context.TODO(), err.Error())
					}
				}
				clear(controllerObjectOnTriggerWindowUpdaterTasks)
				controllerObjectOnTriggerWindowUpdaterWindowStartNs = 0
			}
		}
		controllerObjectOnTriggerWindowUpdaterMutex.Unlock()
	}
}

func RegisterFunctions(runtime *statefun.Runtime) {
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_START, StartController, *statefun.NewFunctionTypeConfig().SetIdChannelSize(100))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_CLEAR, ClearController, *statefun.NewFunctionTypeConfig().SetIdChannelSize(100))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_OBJECT_UPDATE, UpdateControllerObject, *statefun.NewFunctionTypeConfig().SetIdChannelSize(100))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_OBJECT_TRIGGER, ControllerObjectTrigger, *statefun.NewFunctionTypeConfig().SetIdChannelSize(100))
	statefun.NewFunctionType(runtime, inStatefun.CONTROLLER_CONSTRUCT, ControllerConstruct, *statefun.NewFunctionTypeConfig().SetAllowedRequestProviders(sfplugins.AutoRequestSelect).SetIdChannelSize(100))

	decorators.Register(runtime)

	runtime.RegisterOnAfterStartFunction(InitSchema, false)

	go controllerObjectOnTriggerWindowUpdater(runtime)
}

func InitSchema(ctx context.Context, runtime *statefun.Runtime) error {
	cmdb, err := db.NewCMDBSyncClientFromRequestFunction(runtime.Request)
	if err != nil {
		return err
	}

	if err := cmdb.TypeUpdate(
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_TYPE),
		easyjson.NewJSONObject(),
		false,
		true,
	); err != nil {
		return err
	}

	if err := cmdb.TypeUpdate(
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_OBJECT_TYPE),
		easyjson.NewJSONObject(),
		false,
		true,
	); err != nil {
		return err
	}

	if err := cmdb.TypesLinkUpdate(
		common.SetHubPreffix(runtime.Domain, inStatefun.SESSION_TYPE),
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_TYPE),
		[]string{},
		easyjson.NewJSONObject(),
		false,
		inStatefun.CONTROLLER_TYPE,
	); err != nil {
		return err
	}

	if err := cmdb.TypesLinkUpdate(
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_TYPE),
		common.SetHubPreffix(runtime.Domain, inStatefun.SESSION_TYPE),
		[]string{},
		easyjson.NewJSONObject(),
		false,
		inStatefun.SUBSCRIBER_TYPE,
	); err != nil {
		return err
	}

	if err := cmdb.TypesLinkUpdate(
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_TYPE),
		common.SetHubPreffix(runtime.Domain, inStatefun.CONTROLLER_OBJECT_TYPE),
		[]string{},
		easyjson.NewJSONObject(),
		false,
		inStatefun.CONTROLLER_OBJECT_TYPE,
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

	inited := body.IsNonEmptyObject()

	uuids, _ := payload.GetByPath("uuids").AsArrayString()
	if len(uuids) == 0 {
		return
	}

	body.SetByPath(_CONTROLLER_DECLARATION, payload.GetByPath(_CONTROLLER_DECLARATION))
	body.SetByPath("name", payload.GetByPath("name"))
	body.SetByPath("plugin", payload.GetByPath("plugin"))
	body.SetByPath("is_shadow_object_in_domain", easyjson.NewJSON(payload.GetByPath("is_shadow_object_in_domain").AsStringDefault("")))

	cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)

	if !inited {
		system.MsgOnErrorReturn(cmdb.ObjectUpdate(self.ID, *body, true, inStatefun.CONTROLLER_TYPE))
	}

	if err := cmdb.ObjectsLinkCreate(self.ID, caller.ID, caller.ID, []string{}); err != nil {
		if !common.ErrorAlreadyExists(err) {
			logger.GetLogger().Warnf(context.TODO(), "failed to create objects link between controller and session, err=%v", err.Error())
			return
		}
	}

	if err := cmdb.ObjectsLinkCreate(caller.ID, self.ID, self.ID, []string{}); err != nil {
		if !common.ErrorAlreadyExists(err) {
			logger.GetLogger().Warnf(context.TODO(), "failed to create objects link between session and controller, err=%v", err.Error())
			return
		}
	}

	if !inited {
		// Prepare type data ---------------------------------
		objectUUID := ctx.Domain.GetValidObjectId(uuids[0])
		objectType, err := crud.FindObjectType(ctx, objectUUID)
		if err != nil {
			if !common.ErrorAlreadyExists(err) {
				logger.GetLogger().Warnf(context.TODO(), "failed to find uuid type, err=%v", err.Error())
				return
			}
		}

		if err := cmdb.TypesLinkCreate(inStatefun.CONTROLLER_OBJECT_TYPE, objectType, inStatefun.CONTROLLER_SUBJECT_TYPE, []string{}); err != nil {
			if !common.ErrorAlreadyExists(err) {
				logger.GetLogger().Warn(context.TODO(), "failed to create types link between controller object and uuid", err.Error())
				return
			}
		}

		system.MsgOnErrorReturn(cmdb.TriggerObjectSet(objectType, db.UpdateTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER))
		system.MsgOnErrorReturn(cmdb.TriggerObjectSet(objectType, db.DeleteTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER))

		if typeData, err := cmdb.TypeRead(objectType); err == nil {
			linksIn := typeData.GetByPath("links.in")
			for i := 0; i < linksIn.ArraySize(); i++ {
				linkData := typeData.GetByPath("links.in").ArrayElement(i)
				if linkData.GetByPath("name").AsStringDefault("") == objectType { // link from other type
					fromId := linkData.GetByPath("from").AsStringDefault("")
					if len(fromId) > 0 && ctx.Domain.GetObjectIDWithoutDomain(fromId) != crud.BUILT_IN_TYPES {
						system.MsgOnErrorReturn(cmdb.TriggerLinkSet(fromId, objectType, db.CreateTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER))
						system.MsgOnErrorReturn(cmdb.TriggerLinkSet(fromId, objectType, db.DeleteTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER))
					}
				}
			}
		}
		// ----------------------------------------------------
	}

	for _, oUUID := range uuids {
		objectUUID := ctx.Domain.GetValidObjectId(oUUID)

		controllerObjectID := generate.UUID(self.ID + objectUUID).String()
		controllerObjectBody := easyjson.NewJSONObject()
		controllerObjectBody.SetByPath("object_id", easyjson.NewJSON(objectUUID))
		controllerObjectBody.SetByPath("parent", easyjson.NewJSON(self.ID))

		// send to update сontroller object
		payload := easyjson.NewJSONObjectWithKeyValue("force_update_session_id", easyjson.NewJSON(sessionId))
		payload.SetByPath("controllerObjectBody", controllerObjectBody)
		//ctx.Request(sfplugins.AutoRequestSelect, inStatefun.CONTROLLER_OBJECT_UPDATE, controllerObjectID, &payload, nil) // Sync call for Golang direct call if possible (speedup?)
		system.MsgOnErrorReturn(ctx.Signal(sfplugins.AutoSignalSelect, inStatefun.CONTROLLER_OBJECT_UPDATE, controllerObjectID, &payload, nil))
	}

	if inited {
		ctx.SetObjectContext(body)
	}
}

// fetch declaration from controller
// send to construct
// compare result
// if it's different send update to controller
/*func UpdateControllerObject(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	controllerObjectID := ctx.Self.ID
	slog.Info("Update controller object", "id", controllerObjectID)

	var body *easyjson.JSON
	var parentControllerID string
	var realObjectID string

	// -----------------------------------------
	if controllerObjectBody := ctx.Payload.GetByPath("controllerObjectBody"); controllerObjectBody.IsNonEmptyObject() {
		body = ctx.GetObjectContext()
		parentUUID := body.GetByPath("parent").AsStringDefault("")
		objectUUID := body.GetByPath("object_id").AsStringDefault("")

		if len(parentUUID) == 0 || len(objectUUID) == 0 {
			parentUUID = controllerObjectBody.GetByPath("parent").AsStringDefault("")
			objectUUID = controllerObjectBody.GetByPath("object_id").AsStringDefault("")
			cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)

			if err := cmdb.ObjectCreate(controllerObjectID, inStatefun.CONTROLLER_OBJECT_TYPE, controllerObjectBody); err != nil {
				if !common.ErrorAlreadyExists(err) {
					slog.Warn("failed to create controller object", "err", err.Error())
					return
				}
			}

			if err := cmdb.ObjectsLinkCreate(controllerObjectID, objectUUID, "uiapplib_"+objectUUID, []string{}); err != nil {
				if !common.ErrorAlreadyExists(err) {
					slog.Warn("failed to create objects link between controller object and uuid", "err", err.Error())
					return
				}
			}

			if err := cmdb.ObjectsLinkCreate(parentUUID, controllerObjectID, controllerObjectID, []string{}); err != nil {
				if !common.ErrorAlreadyExists(err) {
					slog.Warn("failed to create objects link between controller and controller object", "err", err.Error())
					return
				}
			}
			body = &controllerObjectBody
		}
		parentControllerID = parentUUID
		realObjectID = objectUUID
	} else {
		body = ctx.GetObjectContext()
		parentUUID, ok := body.GetByPath("parent").AsString()
		if !ok {
			slog.Warn("empty controller id")
			return
		}
		parentControllerID = parentUUID
		realObjectID = body.GetByPath("object_id").AsStringDefault("")
	}
	// -----------------------------------------

	controllerBody, err := ctx.Domain.Cache().GetValueJSON(parentControllerID)
	if err != nil {
		slog.Error(err.Error())
		return
	}

	controllerDeclaration := controllerBody.GetByPath(_CONTROLLER_DECLARATION)

	result, err := ControllerConstruct(ctx, realObjectID, &controllerDeclaration)
	if err != nil {
		result = easyjson.NewJSONObject().GetPtr()
	}

	if !result.IsNonEmptyObject() {
		return
	}

	newResult := *result

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

	isShadowObjectInDomain := controllerBody.GetByPath("is_shadow_object_in_domain").AsStringDefault("")
	replyObjectId := realObjectID
	if len(isShadowObjectInDomain) > 0 {
		replyObjectId = ctx.Domain.CreateCustomShadowId(isShadowObjectInDomain, ctx.Domain.Name(), ctx.Domain.GetObjectIDWithoutDomain(realObjectID))
	}
	path := fmt.Sprintf("payload.plugins.%s.%s", controllerPlugin, replyObjectId)

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
}*/

// fetch declaration from controller
// send to construct
// compare result
// if it's different send update to controller
func UpdateControllerObject(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	controllerObjectID := ctx.Self.ID
	logger.GetLogger().Infof(context.TODO(), "Update controller object, id=%s", controllerObjectID)

	var body *easyjson.JSON
	var parentControllerID string
	var realObjectID string

	body = ctx.GetObjectContext()
	defer ctx.SetObjectContext(body)

	// -----------------------------------------
	if controllerObjectBody := ctx.Payload.GetByPath("controllerObjectBody"); controllerObjectBody.IsNonEmptyObject() {
		/*linkCacheStr := parentUUID + "+"
		body.GetByPath("link_cache")*/

		parentControllerID = body.GetByPath("parent").AsStringDefault("")
		realObjectID = body.GetByPath("object_id").AsStringDefault("")

		if len(parentControllerID) == 0 || len(realObjectID) == 0 {
			parentControllerID = controllerObjectBody.GetByPath("parent").AsStringDefault("")
			realObjectID = controllerObjectBody.GetByPath("object_id").AsStringDefault("")
			cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)

			if err := cmdb.ObjectCreate(controllerObjectID, inStatefun.CONTROLLER_OBJECT_TYPE, controllerObjectBody); err != nil {
				if !common.ErrorAlreadyExists(err) {
					logger.GetLogger().Warnf(context.TODO(), "failed to create controller object, err=%s", err.Error())
					return
				}
			}

			if err := cmdb.ObjectsLinkCreate(controllerObjectID, realObjectID, "uiapplib_"+realObjectID, []string{}); err != nil {
				if !common.ErrorAlreadyExists(err) {
					logger.GetLogger().Warnf(context.TODO(), "failed to create objects link between controller object and uuid, err=%s", err.Error())
					return
				}
			}

			if err := cmdb.ObjectsLinkCreate(parentControllerID, controllerObjectID, controllerObjectID, []string{}); err != nil {
				if !common.ErrorAlreadyExists(err) {
					logger.GetLogger().Warnf(context.TODO(), "failed to create objects link between controller and controller object, err=%s", err.Error())
					return
				}
			}
			body.SetByPath("parent", easyjson.NewJSON(parentControllerID))
			body.SetByPath("object_id", easyjson.NewJSON(realObjectID))
		}
	} else {
		parentUUID, ok := body.GetByPath("parent").AsString()
		if !ok {
			logger.GetLogger().Warn(context.TODO(), "empty controller id")
			return
		}
		parentControllerID = parentUUID
		realObjectID = body.GetByPath("object_id").AsStringDefault("")
	}
	// -----------------------------------------

	controllerBody, err := ctx.Domain.Cache().GetValueJSON(parentControllerID)
	if err != nil {
		logger.GetLogger().Error(context.TODO(), err.Error())
		return
	}

	controllerDeclaration := controllerBody.GetByPath(_CONTROLLER_DECLARATION)

	forceUpdateSessionId := ctx.Payload.GetByPath("force_update_session_id").AsStringDefault("")

	//cacheMiss := false
	//db := common.MustDBClient(ctx.Request)
	//realObjectData, err := db.Graph.VertexRead(realObjectID, false)
	//realObjectDataHash := system.GetHashStr(realObjectData.GetByPath("body").ToString())
	/*if err == nil {
		if body.GetByPath("cached_real_object_body_hash").AsStringDefault("") != realObjectDataHash {
			//cacheMiss = true
		}
	}*/

	oldResult := body.GetByPath("result")
	newResult := oldResult

	if true {
		result, err := ctx.Request(sfplugins.AutoRequestSelect, inStatefun.CONTROLLER_CONSTRUCT, realObjectID, &controllerDeclaration, nil)
		if err != nil {
			result = easyjson.NewJSONObject().GetPtr()
		}
		if !result.IsNonEmptyObject() {
			return
		}
		newResult = result.GetByPath("result")

		body.SetByPath("result", newResult)
		//body.SetByPath("cached_real_object_body_hash", easyjson.NewJSON(realObjectDataHash))
	}

	if len(forceUpdateSessionId) == 0 && checkUpdates {
		if oldResult.Equals(newResult) {
			return
		}
	}

	// send update to controller subs -----------------------------------------
	controllerPlugin, _ := controllerBody.GetByPath("plugin").AsString()

	isShadowObjectInDomain := controllerBody.GetByPath("is_shadow_object_in_domain").AsStringDefault("")
	replyObjectId := realObjectID
	if len(isShadowObjectInDomain) > 0 {
		replyObjectId = ctx.Domain.CreateCustomShadowId(isShadowObjectInDomain, ctx.Domain.Name(), ctx.Domain.GetObjectIDWithoutDomain(realObjectID))
	}
	path := fmt.Sprintf("payload.plugins.%s.%s", controllerPlugin, replyObjectId)

	updateReply := easyjson.NewJSONObject()
	updateReply.SetByPath(path, newResult)

	subscribers := getChildrenUUIDSByLinkTypeLocal(ctx, parentControllerID, inStatefun.SUBSCRIBER_TYPE)

	if len(forceUpdateSessionId) == 0 {
		logger.GetLogger().Infof(context.TODO(), "Send update to subscribers=%v", subscribers)
		for _, subID := range subscribers {
			if err := egress.SendToSessionEgress(ctx, subID, &updateReply); err != nil {
				logger.GetLogger().Warn(context.TODO(), err.Error())
			}
		}
	} else {
		logger.GetLogger().Infof(context.TODO(), "Send update to force update requested session only, subscribers=%v", subscribers)
		if err := egress.SendToSessionEgress(ctx, forceUpdateSessionId, &updateReply); err != nil {
			logger.GetLogger().Warn(context.TODO(), err.Error())
		}
	}
	// ------------------------------------------------------------------------
}

func ControllerObjectTrigger(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectUUID := ctxProcessor.Self.ID

	cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctxProcessor.Request)
	if ctxProcessor.Payload.GetByPath("trigger.link.delete.type").AsStringDefault("") == inStatefun.CONTROLLER_SUBJECT_TYPE {
		system.MsgOnErrorReturn(cmdb.ObjectDelete(objectUUID))
		return
	}

	if objData, err := cmdb.ObjectRead(objectUUID); err == nil {
		linksIn := objData.GetByPath("links.in")
		for i := 0; i < linksIn.ArraySize(); i++ {
			linkData := objData.GetByPath("links.in").ArrayElement(i)
			fromId := linkData.GetByPath("from").AsStringDefault("")
			linkName := linkData.GetByPath("name").AsStringDefault("")
			if strings.Contains(linkName, "uiapplib_") {
				updatePayload := easyjson.NewJSONObject()

				// Need grouping for same controller object at some time window.
				// Otherwise object controller for object like network switch can start updating every time its port disappears
				controllerObjectOnTriggerWindowUpdaterMutex.Lock()
				if controllerObjectOnTriggerWindowUpdaterWindowStartNs == 0 {
					controllerObjectOnTriggerWindowUpdaterWindowStartNs = system.GetCurrentTimeNs()
				}
				controllerObjectOnTriggerWindowUpdaterTasks[fromId] = &updatePayload
				controllerObjectOnTriggerWindowUpdaterMutex.Unlock()
			}
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

	construct := easyjson.NewJSONObject().GetPtr()

	db := common.MustDBClient(ctx.Request)
	if data, err := db.Graph.VertexRead(id, false); err == nil {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for key, d := range decorators {
			wg.Add(1)
			go func(key string, d controllerDecorator) {
				defer wg.Done()
				result := d.Decorate(&db, &data)
				mu.Lock()
				construct.SetByPath(key, result)
				mu.Unlock()
			}(key, d)
		}
		wg.Wait()

		common.Reply(ctx, "ok", *construct)
	} else {
		common.Reply(ctx, "error", easyjson.NewJSONObject())
	}
}

/*func ControllerConstruct(ctx *sfplugins.StatefunContextProcessor, realObjectId string, controllerDeclaration *easyjson.JSON) (*easyjson.JSON, error) {
	decorators := parseDecorators(realObjectId, controllerDeclaration)

	construct := easyjson.NewJSONObject()

	db := common.MustDBClient(ctx.Request)
	if data, err := db.Graph.VertexRead(realObjectId, false); err == nil {
		for key, d := range decorators {
			result := d.Decorate(&db, &data)
			construct.SetByPath(key, result)
		}
	} else {
		return nil, fmt.Errorf("ControllerConstruct error: %s", err.Error())
	}

	return &construct, nil
}*/

func ClearController(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	//return
}
