package session

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
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
	statefun.NewFunctionType(runtime, inStatefun.SESSION_CONTROL, sessionControl, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.PREPARE_EGRESS, prepareEgress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, inStatefun.EGRESS, egress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))

	if runtime.Domain.Name() == runtime.Domain.HubDomainName() {
		runtime.RegisterOnAfterStartFunction(initSchema, false)
	}
}

func initSchema(runtime *statefun.Runtime) error {
	if err := common.CreateType(runtime, inStatefun.SESSION_TYPE, easyjson.NewJSONObject()); err != nil {
		return err
	}

	if err := common.CreateTypesLink(runtime, runtime.Domain.CreateObjectIDWithHubDomain(crud.BUILT_IN_TYPE_GROUP, false), inStatefun.SESSION_TYPE, inStatefun.SESSION_TYPE); err != nil {
		return err
	}

	if err := common.CreateObject(runtime, inStatefun.SESSIONS_ENTYPOINT, runtime.Domain.CreateObjectIDWithHubDomain(crud.BUILT_IN_TYPE_GROUP, false), easyjson.NewJSONObject()); err != nil {
		return err
	}

	return nil
}

/*
Payload:

	{
		command: "info" | "unsub",
		controllers: {
			controller_name {
				body: {},
				uuids: []
			}
		}
	}
*/
func ingress(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	id := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload
	sessionID := generate.SessionID(id)

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.SESSION_CONTROL, sessionID.String(), payload, nil); err != nil {
		slog.Warn(err.Error())
	}
}

/*
	{
		client_id: "id",
		command: "info" | "unsub",
		controllers: {
			some_controller_name {
				body: {},
				uuids: []
			}
		}
	}
*/
func sessionControl(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	sessionID := ctx.Self.ID
	payload := ctx.Payload
	params := ctx.GetObjectContext()
	sCtx := common.NewStatefunContext(ctx)

	log := slog.With("session_id", sessionID)

	if !params.IsNonEmptyObject() {
		now := time.Now()

		body := easyjson.NewJSONObject()
		body.SetByPath("life_time", easyjson.NewJSON(SessionLifeTime))
		body.SetByPath("inactivity_timeout", easyjson.NewJSON(SessionInactivityTimeout.String()))
		body.SetByPath("creation_time", easyjson.NewJSON(now.UnixNano()))
		body.SetByPath("last_activity_time", easyjson.NewJSON(now.UnixNano()))
		body.SetByPath("client_id", payload.GetByPath("client_id"))

		if err := common.CreateObject(sCtx, sessionID, inStatefun.SESSION_TYPE, body); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := common.CreateObjectsLink(sCtx, inStatefun.SESSIONS_ENTYPOINT, sessionID, sessionID); err != nil {
			log.Warn(err.Error())
			return
		}

		if time.Since(now).Seconds() > 1 {
			log.Warn("session create", "time", time.Since(now))
		}
	}

	in := IngressPayload{}
	if err := json.Unmarshal(payload.ToBytes(), &in); err != nil {
		log.Error(err.Error())
		return
	}

	switch {
	case in.Command != "":
		processSessionCommand(ctx, in.Command)
	case len(in.Controllers) > 0:
		processSessionControllers(ctx, in.Controllers)
	default:
		return
	}
}

func prepareEgress(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	payload := ctx.Payload
	session := ctx.GetObjectContext()
	log := slog.With("session_id", ctx.Self.ID)

	clientID := session.GetByPath("client_id").AsStringDefault("")

	if clientID == "" {
		log.Warn("Empty client id")
		return
	}

	egressPayload := payload
	if payload.PathExists("payload") {
		egressPayload = payload.GetByPath("payload").GetPtr()
	}

	if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.EGRESS, clientID, egressPayload, nil); err != nil {
		log.Warn(err.Error())
	}
}

func egress(_ sfplugins.StatefunExecutor, ctx *sfplugins.StatefunContextProcessor) {
	log := slog.With("id", ctx.Self.ID)

	if err := ctx.Egress(sfplugins.NatsCoreEgress, ctx.Payload); err != nil {
		log.Warn(err.Error())
	}
}

func processSessionCommand(ctx *sfplugins.StatefunContextProcessor, cmd string) {
	sessionID := ctx.Self.ID
	log := slog.With("session_id", sessionID)

	switch cmd {
	case "info":
		if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.PREPARE_EGRESS, sessionID, ctx.GetObjectContext(), nil); err != nil {
			log.Warn("Failed to send into egress", "error", err.Error())
		}
	case "unsub":
		controllers := common.GetChildrenUUIDSByLinkType(ctx, sessionID, inStatefun.CONTROLLER_TYPE)

		for _, controllerUUID := range controllers {
			result, err := ctx.Request(sfplugins.AutoRequestSelect, inStatefun.CONTROLLER_UNSUB, controllerUUID, easyjson.NewJSONObject().GetPtr(), nil)
			if err := common.CheckRequestError(result, err); err != nil {
				log.Warn("Controller unsub failed", "error", err)
			}
		}

		unsubPayload := easyjson.NewJSONObject()
		unsubPayload.SetByPath("payload.command", easyjson.NewJSON("unsub"))
		unsubPayload.SetByPath("payload.status", easyjson.NewJSON("ok"))
		if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.PREPARE_EGRESS, sessionID, &unsubPayload, nil); err != nil {
			log.Warn("Failed to send into egress", "error", err.Error())
		}
	}
}

func processSessionControllers(ctx *sfplugins.StatefunContextProcessor, controllers map[string]Controller) {
	sessionID := ctx.Self.ID
	log := slog.With("session_id", sessionID)

	for controllerName, controller := range controllers {
		body := easyjson.NewJSON(controller.Body)
		setupPayload := easyjson.NewJSONObjectWithKeyValue("declaration", body)

		for _, uuid := range controller.UUIDs {
			// setup controller
			controllerID := generate.UUID(controllerName + uuid + body.ToString())
			controllerIDWithDomain := ctx.Domain.CreateObjectIDWithDomain(ctx.Domain.GetDomainFromObjectID(uuid), controllerID.String(), false)
			// link := common.OutTargetLink(sessionID, controllerID.String())
			// if _, err := ctx.Domain.Cache().GetValue(link); err == nil {
			// 	continue
			// }

			setupPayload.SetByPath("name", easyjson.NewJSON(controllerName))
			setupPayload.SetByPath("object_id", easyjson.NewJSON(uuid))

			err := ctx.Signal(sfplugins.JetstreamGlobalSignal, inStatefun.CONTROLLER_SETUP, controllerIDWithDomain, &setupPayload, nil)
			if err != nil {
				log.Warn("Failed to send signal for controller setup", "error", err)
				continue
			}
		}
	}
}
