// Copyright 2023 NJWS Inc.

package uilib

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/common"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

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
func (h *statefunHandler) initClient(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	id := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload
	sessionID := generateSessionID(id)

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionInitFunction, sessionID, payload, nil); err != nil {
		slog.Warn(err.Error())
	}
}

/*
Payload:

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
func (h *statefunHandler) initSession(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload

	params := ctxProcessor.GetObjectContext()

	if !params.IsNonEmptyObject() {
		createSessionPayload := easyjson.NewJSONObject()
		createSessionPayload.SetByPath("session_id", easyjson.NewJSON(sessionID))
		createSessionPayload.SetByPath("client_id", payload.GetByPath("client_id"))

		result, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, sessionCreateFunction, "sessions", createSessionPayload.GetPtr(), nil)
		if err != nil {
			slog.Error("Session creation failed", "error", err)
			return
		}

		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionAutoControlFunction, sessionID, result.GetByPath("result").GetPtr(), nil); err != nil {
			slog.Warn(err.Error())
		}
	} else {
		params.SetByPath("last_activity_time", easyjson.NewJSON(time.Now().UnixNano()))
		ctxProcessor.SetObjectContext(params)
	}

	if payload.PathExists("command") {
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionCommandFunction, sessionID, payload, nil); err != nil {
			slog.Warn(err.Error())
		}
	} else if payload.PathExists("controllers") {
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientControllersSetFunction, sessionID, payload, nil); err != nil {
			slog.Warn(err.Error())
		}
	}
}

/*
Payload: *easyjson.JSON
*/
func (h *statefunHandler) clientEgress(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	payload := ctxProcessor.Payload
	session := ctxProcessor.GetObjectContext()

	if !session.IsNonEmptyObject() {
		return
	}

	clientID := session.GetByPath("client_id").AsStringDefault("")

	if clientID == "" {
		return
	}

	egressPayload := payload
	if payload.PathExists("payload") {
		egressPayload = payload.GetByPath("payload").GetPtr()
	}

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, h.cfg.EgressTopic, clientID, egressPayload, nil); err != nil {
		slog.Warn(err.Error())
	}
}

/*
Payload:

	{
		client_id: "id",
	}
*/
func (h *statefunHandler) createSession(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	payload := ctxProcessor.Payload
	queryID := common.GetQueryID(ctxProcessor)

	sessionID := payload.GetByPath("session_id").AsStringDefault("")
	if sessionID == "" {
		return
	}

	now := time.Now().UnixNano()

	body := easyjson.NewJSONObject()
	body.SetByPath("life_time", easyjson.NewJSON(h.cfg.SessionLifeTime))
	body.SetByPath("inactivity_timeout", easyjson.NewJSON(h.cfg.SessionInactivityTimeout.String()))
	body.SetByPath("creation_time", easyjson.NewJSON(now))
	body.SetByPath("last_activity_time", easyjson.NewJSON(now))
	body.SetByPath("client_id", payload.GetByPath("client_id"))

	_, err := ctxProcessor.Request(sfplugins.GolangLocalRequest,
		"functions.graph.ll.api.object.create",
		sessionID,
		easyjson.NewJSONObjectWithKeyValue("body", body).GetPtr(),
		easyjson.NewJSONNull().GetPtr(),
	)
	if err != nil {
		slog.Error("Creation session object failed", "error", err)
		return
	}

	if err := createLink(ctxProcessor, self.ID, sessionID, "session", easyjson.NewJSONObject().GetPtr(), sessionID); err != nil {
		slog.Error("Cannot create links between sessions and session", "session_id", sessionID, "error", err)
		return
	}

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", body)

	common.ReplyQueryID(queryID, &result, ctxProcessor)
}

func (h *statefunHandler) deleteSession(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	const op = "deleteSession"

	self := ctxProcessor.Self
	queryID := common.GetQueryID(ctxProcessor)

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, op)
	if err != nil {
		return
	}

	deleteObjectPayload := easyjson.NewJSONObject()
	deleteObjectPayload.SetByPath("query_id", easyjson.NewJSON(queryID))
	if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, "functions.graph.ll.api.object.delete", self.ID, &deleteObjectPayload, nil); err != nil {
		slog.Error("Cannot delete session", "session_id", self.ID, "error", err)
		return
	}

	if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, op); err != nil {
		slog.Warn("Key mutex unlock", "caller", op, "error", err)
	}

	if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, sessionUnsubFunction, self.ID, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
		slog.Warn("Session unsub failed", "error", err)
	}

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", easyjson.NewJSON(""))

	common.ReplyQueryID(queryID, &result, ctxProcessor)
}

func (h *statefunHandler) unsubSession(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self
	queryID := common.GetQueryID(ctxProcessor)

	controllers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "controller")

	for _, controllerUUID := range controllers {
		if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerUnsubFunction, controllerUUID, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
			slog.Warn("Controller unsub failed", "error", err)
		}
	}

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", easyjson.NewJSON(""))

	common.ReplyQueryID(queryID, &result, ctxProcessor)
}

/*
Payload:

	{
		command: "info" | "unsub",
	}
*/
func (h *statefunHandler) sessionCommand(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload

	cmd := payload.GetByPath("command").AsStringDefault("")

	switch cmd {
	case "unsub":
		if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, sessionUnsubFunction, sessionID, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
			slog.Warn("Session unsub failed", "error", err)
		}

		unsubPayload := easyjson.NewJSONObject()
		unsubPayload.SetByPath("payload.command", payload.GetByPath("command"))
		unsubPayload.SetByPath("payload.status", easyjson.NewJSON("ok"))
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, sessionID, &unsubPayload, nil); err != nil {
			slog.Warn(err.Error())
		}
	case "info":
		session := ctxProcessor.GetObjectContext()
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, sessionID, session, nil); err != nil {
			slog.Warn(err.Error())
		}
	}
}

/*
Payload:

	{
		creation_time: 1695292826803661600,
	}
*/
func (h *statefunHandler) sessionAutoControl(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload

	session := ctxProcessor.GetObjectContext()

	if !session.IsNonEmptyObject() {
		return
	}

	// If session object has changed
	if payload.GetByPath("creation_time").AsNumericDefault(0) != session.GetByPath("creation_time").AsNumericDefault(1) {
		return
	}

	inactivityTimeout, err := time.ParseDuration(session.GetByPath("inactivity_timeout").AsStringDefault(h.cfg.SessionInactivityTimeout.String()))
	if err != nil {
		deleteReason := easyjson.NewJSONObjectWithKeyValue("reason", easyjson.NewJSON("invalid session parameter \"inactivity_timeout\""))
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionDeleteFunction, sessionID, &deleteReason, nil); err != nil {
			slog.Warn(err.Error())
		}

		return
	}

	lastActivityTime := int64(session.GetByPath("last_activity_time").AsNumericDefault(0))

	if lastActivityTime+inactivityTimeout.Nanoseconds() < time.Now().UnixNano() {
		deleteReason := easyjson.NewJSONObjectWithKeyValue("reason", easyjson.NewJSON("inactivity_timeout"))
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionDeleteFunction, sessionID, &deleteReason, nil); err != nil {
			slog.Warn(err.Error())
		}

		return
	}

	lifeTime, err := time.ParseDuration(session.GetByPath("life_time").AsStringDefault(h.cfg.SessionLifeTime.String()))
	if err != nil {
		deleteReason := easyjson.NewJSONObjectWithKeyValue("reason", easyjson.NewJSON("invalid session parameter \"life_time\""))
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionDeleteFunction, sessionID, &deleteReason, nil); err != nil {
			slog.Warn(err.Error())
		}

		return
	}

	creationTime := int64(session.GetByPath("creation_time").AsNumericDefault(0))
	if creationTime+lifeTime.Nanoseconds() < time.Now().UnixNano() {
		deleteReason := easyjson.NewJSONObjectWithKeyValue("reason", easyjson.NewJSON("life_time"))
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionDeleteFunction, sessionID, &deleteReason, nil); err != nil {
			slog.Warn(err.Error())
		}

		return
	}

	time.Sleep(h.cfg.SessionUpdateTimeout)

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionAutoControlFunction, sessionID, payload, nil); err != nil {
		slog.Warn(err.Error())
	}
}

/*
Payload:

	{
		controller1_name: {
			body: {...},
			uuids: [...]
		},
		controller2_name: {
			body: {...},
			uuids: [...]
		},
		...
	}
*/
func (h *statefunHandler) setSessionController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := ctxProcessor.Self.ID
	payload := ctxProcessor.Payload

	session := ctxProcessor.GetObjectContext()

	if !session.IsNonEmptyObject() {
		return
	}

	if !payload.PathExists("controllers") || !payload.GetByPath("controllers").IsObject() {
		return
	}

	for _, controllerName := range payload.GetByPath("controllers").ObjectKeys() {
		path := fmt.Sprintf("controllers.%s", controllerName)
		controller := payload.GetByPath(path)

		uuids, _ := controller.GetByPath("uuids").AsArrayString()
		body := controller.GetByPath("body")
		setupPayload := easyjson.NewJSONObjectWithKeyValue("body", body)

		for _, uuid := range uuids {
			// setup controller
			typename := controllerName + "_" + sessionID + "_" + uuid
			go func() {
				if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerSetupFunction, typename, &setupPayload, nil); err != nil {
					slog.Warn("Controller setup failed", "error", err)
				}
			}()

			linkExists := false

			links := getChildrenUUIDSByLinkType(ctxProcessor, sessionID, "controller")
			for _, v := range links {
				if v == typename {
					linkExists = true
				}
			}

			if !linkExists {
				// create link
				if err := createLink(ctxProcessor, sessionID, typename, "controller", easyjson.NewJSONObject().GetPtr(), typename); err != nil {
					slog.Warn("Cannot create link", "error", err)
				}
			}
		}
	}
}
