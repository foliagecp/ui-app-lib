// Copyright 2023 NJWS Inc.

package uilib

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

//const clientIngressFunction = "ui.ingress"

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

	if err := checkClientTypes(ctxProcessor); err != nil {
		return
	}

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionInitFunction, sessionID, payload, nil); err != nil {
		slog.Warn(err.Error())
	}
}

const sessionInitFunction = "functions.client.session.init"

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
		now := time.Now().UnixNano()

		body := easyjson.NewJSONObject()
		body.SetByPath("life_time", easyjson.NewJSON(h.cfg.SessionLifeTime))
		body.SetByPath("inactivity_timeout", easyjson.NewJSON(h.cfg.SessionInactivityTimeout.String()))
		body.SetByPath("creation_time", easyjson.NewJSON(now))
		body.SetByPath("last_activity_time", easyjson.NewJSON(now))
		body.SetByPath("client_id", payload.GetByPath("client_id"))

		tx, err := beginTransaction(ctxProcessor, "full")
		if err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.createObject(ctxProcessor, sessionID, "session", &body); err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.createObjectsLink(ctxProcessor, "sessions_entrypoint", sessionID); err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.commit(ctxProcessor); err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionAutoControlFunction, sessionID, &body, nil); err != nil {
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

const clientEgressFunction = "ui.pre.egress"

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

const sessionDeleteFunction = "functions.client.session.delete"

func (h *statefunHandler) deleteSession(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, sessionDeleteFunction)
	if err != nil {
		return
	}

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, sessionDeleteFunction); err != nil {
			slog.Warn("Key mutex unlock", "caller", sessionDeleteFunction, "error", err)
		}
	}()

	deleteObjectPayload := easyjson.NewJSONObject()
	if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, "functions.graph.ll.api.object.delete", self.ID, &deleteObjectPayload, nil); err != nil {
		slog.Error("Cannot delete session", "session_id", self.ID, "error", err)
		return
	}

	if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, sessionUnsubFunction, self.ID, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
		slog.Warn("Session unsub failed", "error", err)
	}

	replyOk(ctxProcessor)
}

const sessionUnsubFunction = "functions.client.session.unsub"

func (h *statefunHandler) unsubSession(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	self := ctxProcessor.Self

	controllers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "controller")

	for _, controllerUUID := range controllers {
		if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerUnsubFunction, controllerUUID, easyjson.NewJSONObject().GetPtr(), nil); err != nil {
			slog.Warn("Controller unsub failed", "error", err)
		}
	}

	replyOk(ctxProcessor)
}

const sessionCommandFunction = "functions.client.session.command"

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

const sessionAutoControlFunction = "functions.client.session.auto.control"

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

const clientControllersSetFunction = "functions.client.controllers.set"

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
			typenameBytes := md5.Sum([]byte(controllerName + "_" + sessionID + "_" + uuid))
			typename := hex.EncodeToString(typenameBytes[:])

			setupPayload.SetByPath("name", easyjson.NewJSON(controllerName))
			setupPayload.SetByPath("object_id", easyjson.NewJSON(uuid))

			if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerSetupFunction, typename, &setupPayload, nil); err != nil {
				slog.Warn("Controller setup failed", "error", err)
			}

			linkExists := false

			links := getChildrenUUIDSByLinkType(ctxProcessor, sessionID, "controller")
			for _, v := range links {
				if v == typename {
					linkExists = true
					break
				}
			}

			if !linkExists {
				// create link
				tx, err := beginTransaction(ctxProcessor, "full")
				if err != nil {
					slog.Warn(err.Error())
					return
				}

				if err := tx.createObjectsLink(ctxProcessor, sessionID, typename); err != nil {
					slog.Warn(err.Error())
					return
				}

				if err := tx.commit(ctxProcessor); err != nil {
					slog.Warn(err.Error())
					return
				}
			}
		}
	}
}

func checkClientTypes(ctx *sfplugins.StatefunContextProcessor) error {
	tx, err := beginTransaction(ctx, "min")
	if err != nil {
		return err
	}

	if err := tx.createType(ctx, "session", easyjson.NewJSONObject().GetPtr()); err != nil {
		return err
	}

	if err := tx.createType(ctx, "controller", easyjson.NewJSONObject().GetPtr()); err != nil {
		return err
	}

	if err := tx.createTypesLink(ctx, "group", "session", "session"); err != nil {
		return err
	}

	if err := tx.createTypesLink(ctx, "session", "controller", "controller"); err != nil {
		return err
	}

	if err := tx.createTypesLink(ctx, "controller", "session", "subscriber"); err != nil {
		return err
	}

	if err := tx.createObject(ctx, "sessions_entrypoint", "group", easyjson.NewJSONObject().GetPtr()); err != nil {
		return err
	}

	if err := tx.createObjectsLink(ctx, "nav", "sessions_entrypoint"); err != nil {
		return err
	}

	if err := tx.commit(ctx); err != nil {
		return err
	}

	return nil
}
