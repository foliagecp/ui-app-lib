package session

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/internal/common"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

const (
	SessionLifeTime          time.Duration = time.Hour * 24
	SessionInactivityTimeout time.Duration = time.Minute * 15
	SessionUpdateTimeout     time.Duration = time.Second * 10
)

func RegisterFunctions(runtime *statefun.Runtime) {
	statefun.NewFunctionType(runtime, inStatefun.INGRESS, ingress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_ROUTER, sessionRouter, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_START, startSession, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_CLOSE, closeSession, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_START_CONTROLLER, startController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.SESSION_CLEAR_CONTROLLER, clearController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.PREPARE_EGRESS, preEgress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.EGRESS, egress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))

	runtime.RegisterOnAfterStartFunction(initSchema, false)
}

func initSchema(runtime *statefun.Runtime) error {
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
func ingress(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	id := ctx.Self.ID
	payload := ctx.Payload
	sessionID := ctx.Domain.CreateObjectIDWithHubDomain(generate.SessionID(id).String(), false)

	slog.Info("Receive msg", "from", id, "session_id", sessionID)

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	if err := ctx.Signal(sf.JetstreamGlobalSignal, inStatefun.SESSION_ROUTER, sessionID, payload, nil); err != nil {
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

func sessionRouter(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID
	payload := ctx.Payload

	in := IngressPayload{}
	if err := json.Unmarshal(payload.ToBytes(), &in); err != nil {
		return
	}

	args := strings.Split(in.Command, " ")

	if len(args) == 0 && len(in.Controllers) == 0 {
		slog.Info("invalid args")
		return
	}

	var command Command

	if len(in.Controllers) > 0 {
		command = START_CONTROLLER
	} else {
		command = Command(args[0])
	}

	next, ok := routes[command]
	if !ok {
		return
	}

	slog.Info("Go to next route", "next", next, "session", sessionID)

	ctx.Signal(sf.JetstreamGlobalSignal, next, sessionID, payload, nil)
}

func startSession(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	params := ctx.GetObjectContext()
	if params.IsNonEmptyObject() {
		return
	}

	sessionID := ctx.Self.ID
	payload := ctx.Payload
	now := time.Now().UnixNano()

	body := easyjson.NewJSONObject()
	body.SetByPath("life_time", easyjson.NewJSON(SessionLifeTime))
	body.SetByPath("inactivity_timeout", easyjson.NewJSON(SessionInactivityTimeout.String()))
	body.SetByPath("creation_time", easyjson.NewJSON(now))
	body.SetByPath("last_activity_time", easyjson.NewJSON(now))
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

	ctx.Signal(sf.JetstreamGlobalSignal,
		inStatefun.PREPARE_EGRESS,
		sessionID,
		easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr(),
		nil,
	)
}

func closeSession(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID

	slog.Error(errors.ErrUnsupported.Error())

	response := easyjson.NewJSONObject()
	response.SetByPath("command", easyjson.NewJSON(CLOSE_SESSION))
	response.SetByPath("status", easyjson.NewJSON("ok"))

	ctx.Signal(sf.JetstreamGlobalSignal,
		inStatefun.PREPARE_EGRESS,
		sessionID,
		easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr(),
		nil,
	)
}

func startController(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID

	var controllers map[string]Controller
	if err := json.Unmarshal(ctx.Payload.GetByPath("controllers").ToBytes(), &controllers); err != nil {
		slog.Error(err.Error())
		return
	}

	for name, controller := range controllers {
		if len(controller.UUIDs) == 0 {
			continue
		}

		body := easyjson.NewJSON(controller.Body)

		payload := easyjson.NewJSONObject()
		payload.SetByPath("declaration", body)
		payload.SetByPath("uuids", easyjson.JSONFromArray(controller.UUIDs))

		controllerID := generate.UUID(name)
		controllerIDWithDomain := ctx.Domain.CreateObjectIDWithDomain(
			ctx.Domain.GetDomainFromObjectID(controller.UUIDs[0]),
			controllerID.String(),
			false,
		)

		payload.SetByPath("name", easyjson.NewJSON(name))

		err := ctx.Signal(sf.JetstreamGlobalSignal, inStatefun.CONTROLLER_START, controllerIDWithDomain, &payload, nil)
		if err != nil {
			continue
		}
	}

	response := easyjson.NewJSONObject()
	response.SetByPath("command", easyjson.NewJSON(START_CONTROLLER))
	response.SetByPath("status", easyjson.NewJSON("ok"))

	ctx.Signal(sf.JetstreamGlobalSignal,
		inStatefun.PREPARE_EGRESS,
		sessionID,
		easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr(),
		nil,
	)
}

// find all controller objects
// delete links
// clear declaration
func clearController(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	sessionID := ctx.Self.ID

	slog.Error(errors.ErrUnsupported.Error())

	response := easyjson.NewJSONObject()
	response.SetByPath("command", easyjson.NewJSON(CLEAR_CONTROLLER))
	response.SetByPath("status", easyjson.NewJSON("ok"))

	ctx.Signal(sf.JetstreamGlobalSignal,
		inStatefun.PREPARE_EGRESS,
		sessionID,
		easyjson.NewJSONObjectWithKeyValue("payload", response).GetPtr(),
		nil,
	)
}

func preEgress(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	session := ctx.GetObjectContext()
	clientID := session.GetByPath("client_id").AsStringDefault("")

	if clientID == "" {
		slog.Warn("empty client id")
		return
	}

	if err := ctx.Signal(sf.JetstreamGlobalSignal, inStatefun.EGRESS, clientID, ctx.Payload, nil); err != nil {
		slog.Warn(err.Error())
	}
}

func egress(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	if err := ctx.Egress(sf.NatsCoreEgress, ctx.Payload); err != nil {
		slog.Warn(err.Error())
	}
}