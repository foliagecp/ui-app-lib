package session

import (
	"encoding/json"
	"log/slog"
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
	SessionLifeTime          time.Duration = time.Hour * 24
	SessionInactivityTimeout time.Duration = time.Minute * 15
	SessionUpdateTimeout     time.Duration = time.Second * 10
)

func RegisterFunctions(runtime *statefun.Runtime) {
	statefun.NewFunctionType(runtime, internalStatefun.INGRESS, ingress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, internalStatefun.SESSION_CONTROL, sessionControl, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	statefun.NewFunctionType(runtime, internalStatefun.PREPARE_EGRESS, prepareEgress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
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

	// if err := checkTypes(ctxProcessor); err != nil {
	// 	slog.Warn(err.Error())
	// 	return
	// }

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, internalStatefun.SESSION_CONTROL, sessionID.String(), payload, nil); err != nil {
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
func sessionControl(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload
	params := ctxProcessor.GetObjectContext()

	log := slog.With("session_id", sessionID)

	if !params.IsNonEmptyObject() {
		now := time.Now()

		body := easyjson.NewJSONObject()
		body.SetByPath("life_time", easyjson.NewJSON(SessionLifeTime))
		body.SetByPath("inactivity_timeout", easyjson.NewJSON(SessionInactivityTimeout.String()))
		body.SetByPath("creation_time", easyjson.NewJSON(now.UnixNano()))
		body.SetByPath("last_activity_time", easyjson.NewJSON(now.UnixNano()))
		body.SetByPath("client_id", payload.GetByPath("client_id"))

		if err := common.CreateObject(ctxProcessor, sessionID, internalStatefun.SESSION_TYPE, body); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := common.CreateObjectsLink(ctxProcessor, internalStatefun.SESSIONS_ENTYPOINT, sessionID); err != nil {
			log.Warn(err.Error())
			return
		}

		if time.Since(now).Seconds() > 1 {
			log.Warn("session create", "time", time.Since(now))
		}

		if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_session_creation_time", "", []string{"id"}); err == nil {
			gaugeVec.With(prometheus.Labels{"id": sessionID}).Set(float64(time.Since(now).Microseconds()))
		}
	}

	in := IngressPayload{}
	if err := json.Unmarshal(payload.ToBytes(), &in); err != nil {
		log.Error(err.Error())
		return
	}

	switch {
	case in.Command != "":
		processSessionCommand(ctxProcessor, in.Command)
	case len(in.Controllers) > 0:
		processSessionControllers(ctxProcessor, in.Controllers)
	default:
		return
	}
}

func prepareEgress(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	payload := ctxProcessor.Payload
	session := ctxProcessor.GetObjectContext()
	log := slog.With("session_id", ctxProcessor.Self.ID)

	clientID := session.GetByPath("client_id").AsStringDefault("")

	if clientID == "" {
		log.Warn("Empty client id")
		return
	}

	egressPayload := payload
	if payload.PathExists("payload") {
		egressPayload = payload.GetByPath("payload").GetPtr()
	}

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, internalStatefun.EGRESS, clientID, egressPayload, nil); err != nil {
		log.Warn(err.Error())
	}
}

func processSessionCommand(ctx *sfplugins.StatefunContextProcessor, cmd string) {
	sessionID := ctx.Self.ID
	log := slog.With("session_id", sessionID)

	switch cmd {
	case "info":
		session := ctx.GetObjectContext()
		session.SetByPath("controllers", easyjson.JSONFromArray(common.GetChildrenUUIDSByLinkType(ctx, sessionID, internalStatefun.CONTROLLER_TYPE)))

		if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, internalStatefun.PREPARE_EGRESS, sessionID, session, nil); err != nil {
			log.Warn("Failed to send into egress", "error", err.Error())
		}
	case "unsub":
		controllers := common.GetChildrenUUIDSByLinkType(ctx, sessionID, internalStatefun.CONTROLLER_TYPE)

		for _, controllerUUID := range controllers {
			result, err := ctx.Request(sfplugins.AutoSelect, internalStatefun.CONTROLLER_UNSUB, controllerUUID, easyjson.NewJSONObject().GetPtr(), nil)
			if err := common.CheckRequestError(result, err); err != nil {
				log.Warn("Controller unsub failed", "error", err)
			}
		}

		unsubPayload := easyjson.NewJSONObject()
		unsubPayload.SetByPath("payload.command", easyjson.NewJSON("unsub"))
		unsubPayload.SetByPath("payload.status", easyjson.NewJSON("ok"))
		if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, internalStatefun.PREPARE_EGRESS, sessionID, &unsubPayload, nil); err != nil {
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

			link := common.OutLinkKeyPattern(sessionID, controllerID.String(), internalStatefun.CONTROLLER_TYPE)
			if _, err := ctx.GlobalCache.GetValue(link); err == nil {
				continue
			}

			setupPayload.SetByPath("name", easyjson.NewJSON(controllerName))
			setupPayload.SetByPath("object_id", easyjson.NewJSON(uuid))

			err := ctx.Signal(sfplugins.JetstreamGlobalSignal, internalStatefun.CONTROLLER_SETUP, controllerID.String(), &setupPayload, nil)
			if err != nil {
				log.Warn("Failed to send signal for controller setup", "error", err)
				continue
			}
		}
	}
}
