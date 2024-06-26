package session

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/internal/common"
	"github.com/foliagecp/ui-app-lib/internal/egress"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

func RegisterFunctions(runtime *statefun.Runtime) {
	statefun.NewFunctionType(runtime, inStatefun.INGRESS, Ingress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_ROUTER, SessionRouter, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_START, StartSession, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_CLOSE, CloseSession, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_UPDATE_ACTIVITY, UpdateSessionActivity, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_START_CONTROLLER, StartController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_CLEAR_CONTROLLER, ClearController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.EGRESS, Egress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))

	runtime.RegisterOnAfterStartFunction(InitSchema, false)
}

func InitSchema(runtime *statefun.Runtime) error {
	c, err := db.NewCMDBSyncClientFromRequestFunction(runtime.Request)
	if err != nil {
		return err
	}

	if err := c.TypeCreate(
		common.SetHubPreffix(runtime.Domain, inStatefun.SESSION_TYPE),
		easyjson.NewJSONObject(),
	); err != nil {
		return err
	}

	if err := c.TypesLinkCreate(
		common.SetHubPreffix(runtime.Domain, crud.BUILT_IN_TYPE_GROUP),
		common.SetHubPreffix(runtime.Domain, inStatefun.SESSION_TYPE),
		inStatefun.SESSION_TYPE,
		[]string{},
	); err != nil {
		return err
	}

	if err := c.ObjectCreate(
		common.SetHubPreffix(runtime.Domain, inStatefun.SESSIONS_ENTYPOINT),
		common.SetHubPreffix(runtime.Domain, crud.BUILT_IN_TYPE_GROUP),
		easyjson.NewJSONObject(),
	); err != nil {
		return err
	}

	return nil
}

/*
Payload:

	{
		command: "START_SESSION" | "CLOSE_SESSION" | "CLEAR_CONTROLLER",
		controllers: {
			controller_name {
				body: {},
				uuids: []
			}
		}
	}
*/
func Ingress(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	id := ctx.Self.ID
	payload := ctx.Payload
	sessionID := ctx.Domain.CreateObjectIDWithHubDomain(generate.SessionID(id).String(), false)

	slog.Info("Receive msg", "from", id, "session_id", sessionID)

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	if err := ctx.Signal(sf.AutoSignalSelect, inStatefun.SESSION_ROUTER, sessionID, payload, nil); err != nil {
		slog.Warn(err.Error())
	}
}

/*
	{
		client_id: "id",
		command: "START_SESSION" | "CLOSE_SESSION" | "CLEAR_CONTROLLER",
		controllers: {
			controller_name {
				body: {},
				uuids: []
			}
		}
	}
*/

var routes = map[Command]string{
	START_SESSION:    inStatefun.SESSION_START,
	CLOSE_SESSION:    inStatefun.SESSION_CLOSE,
	START_CONTROLLER: inStatefun.SESSION_START_CONTROLLER,
	CLEAR_CONTROLLER: inStatefun.SESSION_CLEAR_CONTROLLER,
}

func SessionRouter(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID
	payload := ctx.Payload
	logger := slog.With("session_id", sessionID)

	var command Command

	if payload.PathExists("command") {
		command = Command(payload.GetByPath("command").AsStringDefault(""))
	} else if len(payload.ObjectKeys()) > 0 {
		payload.RemoveByPath("client_id")
		payload.RemoveByPath("command")

		command = START_CONTROLLER
	}

	next, ok := routes[command]
	if !ok {
		logger.Warn("Command not found", "command", command)
		return
	}

	logger.Info("Forward to next route", "next", next)

	ctx.Signal(sf.AutoSignalSelect, next, sessionID, payload, nil)
	ctx.Signal(sf.AutoSignalSelect, inStatefun.SESSION_UPDATE_ACTIVITY, sessionID, nil, nil)
}

func StartSession(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID
	payload := ctx.Payload
	params := common.GetRemoteContext(ctx)

	if params.IsNonEmptyObject() {
		response := easyjson.NewJSONObject()
		response.SetByPath("command", easyjson.NewJSON(START_SESSION))
		response.SetByPath("status", easyjson.NewJSON("ok"))
		response.SetByPath("message", easyjson.NewJSON("already started"))

		egress.SendToSessionEgress(ctx, sessionID, easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr())

		return
	}

	now := time.Now().Unix()

	body := easyjson.NewJSONObject()
	body.SetByPath("created_at", easyjson.NewJSON(now))
	body.SetByPath("updated_at", easyjson.NewJSON(now))
	body.SetByPath("client_id", payload.GetByPath("client_id"))

	cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)

	if err := cmdb.ObjectCreate(sessionID, inStatefun.SESSION_TYPE, body); err != nil {
		return
	}

	if err := cmdb.ObjectsLinkCreate(
		inStatefun.SESSIONS_ENTYPOINT,
		sessionID,
		sessionID,
		[]string{},
	); err != nil {
		return
	}

	response := easyjson.NewJSONObject()
	response.SetByPath("command", easyjson.NewJSON(START_SESSION))
	response.SetByPath("status", easyjson.NewJSON("ok"))

	egress.SendToSessionEgress(ctx, sessionID, easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr())
}

func UpdateSessionActivity(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	params := common.GetRemoteContext(ctx)
	if !params.IsNonEmptyObject() {
		return
	}

	now := time.Now().Unix()
	params.SetByPath("updated_at", easyjson.NewJSON(now))

	common.SetRemoteContext(ctx, params)
}

func CloseSession(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID

	/*dbc, err := db.NewDBSyncClientFromRequestFunction(ctx.Request)
	if err != nil {
		slog.Error(err.Error())
		return
	}*/
	cmdb, err := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)
	if err != nil {
		slog.Error(err.Error())
		return
	}

	/*// Find all ui controller objects of this session and delete them -----------------------------
	ids, err := dbc.Query.JPGQLCtraQuery(ctx.Self.ID, fmt.Sprintf(".*[type('%s')].*[type('%s')]", inStatefun.CONTROLLER_TYPE, inStatefun.CONTROLLER_OBJECT_TYPE))
	if err != nil {
		slog.Error(err.Error())
		return
	}

	for _, controllerObjectId := range ids {
		cmdb.ObjectDelete(controllerObjectId)
	}
	// --------------------------------------------------------------------------------------------*/
	/*// Find all controllers of this session and delete them ---------------------------------------
	ids, err := dbc.Query.JPGQLCtraQuery(ctx.Self.ID, fmt.Sprintf(".*[type('%s')]", inStatefun.CONTROLLER_TYPE))
	if err != nil {
		slog.Error(err.Error())
		return
	}

	for _, controllerId := range ids {
		cmdb.ObjectDelete(controllerId)
	}
	// --------------------------------------------------------------------------------------------*/

	cmdb.ObjectDelete(ctx.Self.ID)

	response := easyjson.NewJSONObject()
	response.SetByPath("command", easyjson.NewJSON(CLOSE_SESSION))
	response.SetByPath("status", easyjson.NewJSON("ok"))

	egress.SendToSessionEgress(ctx, sessionID, easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr())
}

func StartController(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID

	for _, plugin := range ctx.Payload.ObjectKeys() {
		var controllers map[string]Controller
		if err := json.Unmarshal(ctx.Payload.GetByPath(plugin).ToBytes(), &controllers); err != nil {
			slog.Error(err.Error())
			return
		}

		for name, controller := range controllers {
			if len(controller.UUIDs) == 0 {
				continue
			}

			body := easyjson.NewJSON(controller.Body)

			payload := easyjson.NewJSONObject()
			payload.SetByPath("plugin", easyjson.NewJSON(plugin))
			payload.SetByPath("declaration", body)
			payload.SetByPath("uuids", easyjson.JSONFromArray(controller.UUIDs))
			payload.SetByPath("session_id", easyjson.NewJSON(sessionID))
			payload.SetByPath("name", easyjson.NewJSON(name))

			controllerID := generate.UUID(plugin + name + body.ToString())
			controllerIDWithDomain := ctx.Domain.CreateObjectIDWithDomain(
				ctx.Domain.GetDomainFromObjectID(controller.UUIDs[0]),
				controllerID.String(),
				false,
			)

			err := ctx.Signal(sf.AutoSignalSelect, inStatefun.CONTROLLER_START, controllerIDWithDomain, &payload, nil)
			if err != nil {
				slog.Error(err.Error())
				return
			}
		}
	}

	response := easyjson.NewJSONObject()
	response.SetByPath("command", easyjson.NewJSON(START_CONTROLLER))
	response.SetByPath("status", easyjson.NewJSON("ok"))

	egress.SendToSessionEgress(ctx, sessionID, easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr())
}

// find all controller objects
// delete links
// clear declaration
func ClearController(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID

	slog.Error(errors.ErrUnsupported.Error())

	response := easyjson.NewJSONObject()
	response.SetByPath("command", easyjson.NewJSON(CLEAR_CONTROLLER))
	response.SetByPath("status", easyjson.NewJSON("ok"))

	egress.SendToSessionEgress(ctx, sessionID, easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr())
}

func Egress(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	if err := ctx.Egress(sf.NatsCoreEgress, ctx.Payload, egress.ClientIDFromEgressID(ctx.Self.ID)); err != nil {
		slog.Warn(err.Error())
	}
}
