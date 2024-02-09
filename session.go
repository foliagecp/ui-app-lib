

package uilib

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/foliagecp/easyjson"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/prometheus/client_golang/prometheus"
)

type IngressPayload struct {
	Command     string                `json:"command,omitempty"`
	Controllers map[string]Controller `json:"controllers,omitempty"`
}

type Controller struct {
	Body  map[string]string `json:"body"`
	UUIDs []string          `json:"uuids"`
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
func (h *statefunHandler) ingress(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	id := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload
	sessionID := generateSessionID(id) // uuid

	if err := checkTypes(ctxProcessor); err != nil {
		slog.Warn(err.Error())
		return
	}

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionFunction, sessionID.String(), payload, nil); err != nil {
		slog.Warn(err.Error())
	}
}

const sessionFunction = "functions.ui.app.session"

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
func (h *statefunHandler) session(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload
	params := ctxProcessor.GetObjectContext()

	log := slog.With("session_id", sessionID)

	if !params.IsNonEmptyObject() {
		now := time.Now()

		body := easyjson.NewJSONObject()
		body.SetByPath("life_time", easyjson.NewJSON(h.cfg.SessionLifeTime))
		body.SetByPath("inactivity_timeout", easyjson.NewJSON(h.cfg.SessionInactivityTimeout.String()))
		body.SetByPath("creation_time", easyjson.NewJSON(now.UnixNano()))
		body.SetByPath("last_activity_time", easyjson.NewJSON(now.UnixNano()))
		body.SetByPath("client_id", payload.GetByPath("client_id"))

		if err := createObject(ctxProcessor, sessionID, _SESSION_TYPE, body); err != nil {
			log.Warn(err.Error())
			return
		}

		if err := createObjectsLink(ctxProcessor, _SESSIONS_ENTYPOINT, sessionID); err != nil {
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

func processSessionCommand(ctx *sfplugins.StatefunContextProcessor, cmd string) {
	sessionID := ctx.Self.ID
	log := slog.With("session_id", sessionID)

	switch cmd {
	case "info":
		session := ctx.GetObjectContext()
		session.SetByPath("controllers", easyjson.JSONFromArray(getChildrenUUIDSByLinkType(ctx, sessionID, _CONTROLLER_TYPE)))

		if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, sessionID, session, nil); err != nil {
			log.Warn("Failed to send into egress", "error", err.Error())
		}
	case "unsub":
		controllers := getChildrenUUIDSByLinkType(ctx, sessionID, _CONTROLLER_TYPE)

		for _, controllerUUID := range controllers {
			result, err := ctx.Request(sfplugins.GolangLocalRequest, controllerUnsubFunction, controllerUUID, easyjson.NewJSONObject().GetPtr(), nil)
			if err := checkRequestError(result, err); err != nil {
				log.Warn("Controller unsub failed", "error", err)
			}
		}

		unsubPayload := easyjson.NewJSONObject()
		unsubPayload.SetByPath("payload.command", easyjson.NewJSON("unsub"))
		unsubPayload.SetByPath("payload.status", easyjson.NewJSON("ok"))
		if err := ctx.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, sessionID, &unsubPayload, nil); err != nil {
			log.Warn("Failed to send into egress", "error", err.Error())
		}
	}
}

func processSessionControllers(ctx *sfplugins.StatefunContextProcessor, controllers map[string]Controller) {
	sessionID := ctx.Self.ID
	log := slog.With("session_id", sessionID)

	for controllerName, controller := range controllers {
		body := easyjson.NewJSON(controller.Body)
		setupPayload := easyjson.NewJSONObjectWithKeyValue(_CONTROLLER_DECLARATION, body)

		for _, uuid := range controller.UUIDs {
			// setup controller
			controllerID := generateUUID(controllerName + uuid + body.ToString())

			link := outLinkKeyPattern(sessionID, controllerID.String(), _CONTROLLER_TYPE)
			if _, err := ctx.GlobalCache.GetValue(link); err == nil {
				continue
			}

			setupPayload.SetByPath("name", easyjson.NewJSON(controllerName))
			setupPayload.SetByPath("object_id", easyjson.NewJSON(uuid))

			err := ctx.Signal(sfplugins.JetstreamGlobalSignal, controllerSetupFunction, controllerID.String(), &setupPayload, nil)
			if err != nil {
				log.Warn("Failed to send signal for controller setup", "error", err)
				continue
			}
		}
	}
}

const clientEgressFunction = "functions.ui.app.egress"

/*
Payload: *easyjson.JSON
*/
func (h *statefunHandler) egress(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
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

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, h.cfg.EgressTopic, clientID, egressPayload, nil); err != nil {
		log.Warn(err.Error())
	}
}

func checkTypes(ctx *sfplugins.StatefunContextProcessor) error {
	start := time.Now()

	defer func() {
		if gaugeVec, err := system.GlobalPrometrics.EnsureGaugeVecSimple("ui_app_lib_check_types", "", []string{}); err == nil {
			gaugeVec.With(prometheus.Labels{}).Set(float64(time.Since(start).Microseconds()))
		}
	}()

	links := []string{
		outLinkKeyPattern("types", _SESSION_TYPE, "__type"),
		outLinkKeyPattern("types", _CONTROLLER_TYPE, "__type"),
		outLinkKeyPattern("types", _CONTROLLER_RESULT_TYPE, "__type"),
		outLinkKeyPattern("group", _SESSION_TYPE, "__type"),
		outLinkKeyPattern(_SESSION_TYPE, _CONTROLLER_TYPE, "__type"),
		outLinkKeyPattern(_CONTROLLER_TYPE, _SESSION_TYPE, "__type"),
		outLinkKeyPattern(_CONTROLLER_TYPE, _CONTROLLER_RESULT_TYPE, "__type"),
		outLinkKeyPattern(_SESSIONS_ENTYPOINT, "group", "__type"),
		outLinkKeyPattern("group", _SESSIONS_ENTYPOINT, "__object"),
		outLinkKeyPattern("objects", _SESSIONS_ENTYPOINT, "__object"),
		outLinkKeyPattern("nav", _SESSIONS_ENTYPOINT, "group"),
	}

	needs := make([]string, 0)

	for _, v := range links {
		if _, err := ctx.GlobalCache.GetValue(v); err != nil {
			needs = append(needs, v)
		}
	}

	if len(needs) == 0 {
		return nil
	}

	tx, err := beginTransaction(ctx, pool.GetTxID(), "min")
	if err != nil {
		return err
	}

	if err := tx.initTypes(ctx); err != nil {
		return err
	}

	if err := tx.commit(ctx); err != nil {
		return err
	}

	return nil
}
