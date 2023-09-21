

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
func (h *statefunHandler) initClient(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	id := contextProcessor.Self.ID
	payload := contextProcessor.Payload
	sessionID := generateSessionID(id)

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	contextProcessor.Call(sessionInitFunction, sessionID, payload, nil)
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
func (h *statefunHandler) initSession(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := contextProcessor.Self.ID
	payload := contextProcessor.Payload

	params := contextProcessor.GetObjectContext()

	if !params.IsNonEmptyObject() {
		createSessionPayload := easyjson.NewJSONObject()
		createSessionPayload.SetByPath("session_id", easyjson.NewJSON(sessionID))
		createSessionPayload.SetByPath("client_id", payload.GetByPath("client_id"))

		result, err := contextProcessor.GolangCallSync(sessionCreateFunction, "sessions", createSessionPayload.GetPtr(), nil)
		if err != nil {
			slog.Error("Session creation failed", "error", err)
			return
		}
		contextProcessor.Call(sessionAutoControlFunction, sessionID, result.GetByPath("result").GetPtr(), nil)
	} else {
		params.SetByPath("last_activity_time", easyjson.NewJSON(time.Now().UnixNano()))
		contextProcessor.SetObjectContext(params)
	}

	if payload.PathExists("controllers") {
		contextProcessor.Call(clientControllersSetFunction, sessionID, payload, nil)
	}

	if payload.PathExists("command") {
		contextProcessor.Call(sessionCommandFunction, sessionID, payload, nil)
	}
}

/*
Payload: *easyjson.JSON
*/
func (h *statefunHandler) clientEgress(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	payload := contextProcessor.Payload
	session := contextProcessor.GetObjectContext()

	if !session.IsNonEmptyObject() {
		return
	}

	clientID := session.GetByPath("client_id").AsStringDefault("")

	if clientID == "" {
		return
	}

	contextProcessor.Egress(h.cfg.EgressTopic+"."+clientID, payload)
}

/*
Payload:

	{
		client_id: "id",
	}
*/
func (h *statefunHandler) createSession(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	self := contextProcessor.Self
	payload := contextProcessor.Payload
	queryID := common.GetQueryID(contextProcessor)

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

	_, err := contextProcessor.GolangCallSync(
		"functions.graph.ll.api.object.create",
		sessionID,
		easyjson.NewJSONObjectWithKeyValue("body", body).GetPtr(),
		easyjson.NewJSONNull().GetPtr(),
	)
	if err != nil {
		slog.Error("Creation session object failed", "error", err)
		return
	}

	if err := createLink(contextProcessor, self.ID, sessionID, "session", easyjson.NewJSONObject().GetPtr(), sessionID); err != nil {
		slog.Error("Cannot create links between sessions and session", "session_id", sessionID, "error", err)
		return
	}

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", body)

	common.ReplyQueryID(queryID, &result, contextProcessor)
}

func (h *statefunHandler) deleteSession(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	const op = "deleteSession"

	self := contextProcessor.Self
	queryID := common.GetQueryID(contextProcessor)

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, op)
	if err != nil {
		return
	}

	deleteObjectPayload := easyjson.NewJSONObject()
	deleteObjectPayload.SetByPath("query_id", easyjson.NewJSON(queryID))
	if _, err := contextProcessor.GolangCallSync("functions.graph.ll.api.object.delete", self.ID, &deleteObjectPayload, nil); err != nil {
		slog.Error("Cannot delete session", "session_id", self.ID, "error", err)
		return
	}

	if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, op); err != nil {
		slog.Warn("Key mutex unlock", "caller", op, "error", err)
	}

	if _, err := contextProcessor.GolangCallSync(sessionUnsubFunction, self.ID, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
		slog.Warn("Session unsub failed", "error", err)
	}

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", easyjson.NewJSON(""))

	common.ReplyQueryID(queryID, &result, contextProcessor)
}

func (h *statefunHandler) unsubSession(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	self := contextProcessor.Self
	queryID := common.GetQueryID(contextProcessor)

	controllers := getChildrenUUIDSByLinkType(contextProcessor, self.ID, "controller")

	for _, controllerUUID := range controllers {
		if _, err := contextProcessor.GolangCallSync(controllerUnsubFunction, controllerUUID, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
			slog.Warn("Controller unsub failed", "error", err)
		}
	}

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", easyjson.NewJSON(""))

	common.ReplyQueryID(queryID, &result, contextProcessor)
}

/*
Payload:

	{
		command: "info" | "unsub",
	}
*/
func (h *statefunHandler) sessionCommand(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := contextProcessor.Self.ID
	payload := contextProcessor.Payload

	cmd := payload.GetByPath("command").AsStringDefault("")

	switch cmd {
	case "unsub":
		if _, err := contextProcessor.GolangCallSync(sessionUnsubFunction, sessionID, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
			slog.Warn("Session unsub failed", "error", err)
		}

		unsubPayload := easyjson.NewJSONObject()
		unsubPayload.SetByPath("payload.command", payload.GetByPath("command"))
		unsubPayload.SetByPath("payload.status", easyjson.NewJSON("ok"))
		contextProcessor.Call(clientEgressFunction, sessionID, &unsubPayload, nil)
	case "info":
		session := contextProcessor.GetObjectContext()
		contextProcessor.Call(clientEgressFunction, sessionID, session, nil)
	}
}

/*
Payload:

	{
		creation_time: 1695292826803661600,
	}
*/
func (h *statefunHandler) sessionAutoControl(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := contextProcessor.Self.ID
	payload := contextProcessor.Payload

	session := contextProcessor.GetObjectContext()

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
		contextProcessor.Call(sessionDeleteFunction, sessionID, &deleteReason, nil)
		return
	}

	lastActivityTime := int64(session.GetByPath("last_activity_time").AsNumericDefault(0))

	if lastActivityTime+inactivityTimeout.Nanoseconds() < time.Now().UnixNano() {
		deleteReason := easyjson.NewJSONObjectWithKeyValue("reason", easyjson.NewJSON("inactivity_timeout"))
		contextProcessor.Call(sessionDeleteFunction, sessionID, &deleteReason, nil)
		return
	}

	lifeTime, err := time.ParseDuration(session.GetByPath("life_time").AsStringDefault(h.cfg.SessionLifeTime.String()))
	if err != nil {
		deleteReason := easyjson.NewJSONObjectWithKeyValue("reason", easyjson.NewJSON("invalid session parameter \"life_time\""))
		contextProcessor.Call(sessionDeleteFunction, sessionID, &deleteReason, nil)
		return
	}

	creationTime := int64(session.GetByPath("creation_time").AsNumericDefault(0))
	if creationTime+lifeTime.Nanoseconds() < time.Now().UnixNano() {
		deleteReason := easyjson.NewJSONObjectWithKeyValue("reason", easyjson.NewJSON("life_time"))
		contextProcessor.Call(sessionDeleteFunction, sessionID, &deleteReason, nil)
		return
	}

	time.Sleep(h.cfg.SessionUpdateTimeout)
	contextProcessor.Call(sessionAutoControlFunction, sessionID, payload, nil)
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
func (h *statefunHandler) setSessionController(executor sfplugins.StatefunExecutor, contextProcessor *sfplugins.StatefunContextProcessor) {
	sessionID := contextProcessor.Self.ID
	payload := contextProcessor.Payload

	session := contextProcessor.GetObjectContext()

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
				if _, err := contextProcessor.GolangCallSync(controllerSetupFunction, typename, &setupPayload, nil); err != nil {
					slog.Warn("Controller setup failed", "error", err)
				}
			}()

			linkExists := false

			links := getChildrenUUIDSByLinkType(contextProcessor, sessionID, "controller")
			for _, v := range links {
				if v == typename {
					linkExists = true
				}
			}

			if !linkExists {
				// create link
				if err := createLink(contextProcessor, sessionID, typename, "controller", easyjson.NewJSONObject().GetPtr(), typename); err != nil {
					slog.Warn("Cannot create link", "error", err)
				}
			}
		}
	}
}
