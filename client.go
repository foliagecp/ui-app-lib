// Copyright 2023 NJWS Inc.

package uilib

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/foliagecp/easyjson"
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
	sessionID := generateSessionID(id) // uuid

	if err := checkClientTypes(ctxProcessor); err != nil {
		slog.Warn(err.Error())
		return
	}

	payload.SetByPath("client_id", easyjson.NewJSON(id))

	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, sessionInitFunction, sessionID.String(), payload, nil); err != nil {
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
		now := time.Now()

		body := easyjson.NewJSONObject()
		body.SetByPath("life_time", easyjson.NewJSON(h.cfg.SessionLifeTime))
		body.SetByPath("inactivity_timeout", easyjson.NewJSON(h.cfg.SessionInactivityTimeout.String()))
		body.SetByPath("creation_time", easyjson.NewJSON(now.UnixNano()))
		body.SetByPath("last_activity_time", easyjson.NewJSON(now.UnixNano()))
		body.SetByPath("client_id", payload.GetByPath("client_id"))

		// clone part of graph
		tx, err := beginTransaction(ctxProcessor, generateTxID(sessionID), "with_types", _SESSION_TYPE, "group")
		if err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.createObject(ctxProcessor, sessionID, _SESSION_TYPE, &body); err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.createObjectsLink(ctxProcessor, _SESSIONS_ENTYPOINT, sessionID); err != nil {
			slog.Warn(err.Error())
			return
		}

		if err := tx.commit(ctxProcessor); err != nil {
			slog.Warn(err.Error())
			return
		}

		if time.Since(now).Seconds() > 1 {
			slog.Warn("session create", "session_id", sessionID, "tx_id", tx.id, "time", time.Since(now))
		}
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

	// ctxProcessor.ObjectMutexLock(false)
	// defer ctxProcessor.ObjectMutexUnlock()

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
	controllers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, _CONTROLLER_TYPE)

	for _, controllerUUID := range controllers {
		result, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerUnsubFunction, controllerUUID, easyjson.NewJSONObject().GetPtr(), nil)
		if err := checkRequestError(result, err); err != nil {
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
		session.SetByPath("controllers", easyjson.JSONFromArray(getChildrenUUIDSByLinkType(ctxProcessor, sessionID, _CONTROLLER_TYPE)))

		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, sessionID, session, nil); err != nil {
			slog.Warn(err.Error())
		}
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
		slog.Warn("session is empty")
		return
	}

	if !payload.PathExists("controllers") || !payload.GetByPath("controllers").IsObject() {
		slog.Warn("no controllers in payload")
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
			controllerID := generateUUID(controllerName + sessionID + uuid + body.ToString())

			setupPayload.SetByPath("name", easyjson.NewJSON(controllerName))
			setupPayload.SetByPath("object_id", easyjson.NewJSON(uuid))

			start := time.Now()
			result, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, controllerSetupFunction, controllerID.String(), &setupPayload, nil)
			if err := checkRequestError(result, err); err != nil {
				slog.Warn("Controller setup failed", "error", err)
				continue
			}

			if time.Since(start).Milliseconds() > 500 {
				slog.Warn("client setup controller", "id", sessionID, "ctrl_id", controllerID.String(), "dt", time.Since(start))
			}

			linkExists := false

			links := getChildrenUUIDSByLinkType(ctxProcessor, sessionID, _CONTROLLER_TYPE)
			for _, v := range links {
				if v == controllerID.String() {
					linkExists = true
					break
				}
			}

			if !linkExists {
				// create link
				tx, err := beginTransaction(ctxProcessor, generateTxID(sessionID), "with_types", _CONTROLLER_TYPE, _SESSION_TYPE)
				if err != nil {
					slog.Warn(err.Error())
					continue
				}

				if err := tx.createObjectsLink(ctxProcessor, sessionID, controllerID.String()); err != nil {
					slog.Warn(err.Error())
					continue
				}

				if err := tx.commit(ctxProcessor); err != nil {
					slog.Warn(err.Error())
					continue
				}
			}
		}
	}
}

func checkClientTypes(ctx *sfplugins.StatefunContextProcessor) error {
	links := []string{
		"types.out.ltp_oid-bdy.__type." + _SESSION_TYPE,
		"types.out.ltp_oid-bdy.__type." + _CONTROLLER_TYPE,
		"group.out.ltp_oid-bdy.__type." + _SESSION_TYPE,
		_SESSION_TYPE + ".out.ltp_oid-bdy.__type." + _CONTROLLER_TYPE,
		_CONTROLLER_TYPE + ".out.ltp_oid-bdy.__type." + _SESSION_TYPE,
		_SESSIONS_ENTYPOINT + ".out.ltp_oid-bdy.__type.group",
		"group.out.ltp_oid-bdy.__object." + _SESSIONS_ENTYPOINT,
		"objects.out.ltp_oid-bdy.__object." + _SESSIONS_ENTYPOINT,
		"nav.out.ltp_oid-bdy.group." + _SESSIONS_ENTYPOINT,
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

	tx, err := beginTransaction(ctx, generateTxID("uilib_init_types"), "min")
	if err != nil {
		return err
	}

	if err := tx.createType(ctx, _SESSION_TYPE, easyjson.NewJSONObject().GetPtr()); err != nil {
		return err
	}

	if err := tx.createType(ctx, _CONTROLLER_TYPE, easyjson.NewJSONObject().GetPtr()); err != nil {
		return err
	}

	if err := tx.createTypesLink(ctx, "group", _SESSION_TYPE, _SESSION_TYPE); err != nil {
		return err
	}

	if err := tx.createTypesLink(ctx, _SESSION_TYPE, _CONTROLLER_TYPE, _CONTROLLER_TYPE); err != nil {
		return err
	}

	if err := tx.createTypesLink(ctx, _CONTROLLER_TYPE, _SESSION_TYPE, _SUBSCRIBER_TYPE); err != nil {
		return err
	}

	if err := tx.createObject(ctx, _SESSIONS_ENTYPOINT, "group", easyjson.NewJSONObject().GetPtr()); err != nil {
		return err
	}

	if err := tx.createObjectsLink(ctx, "nav", _SESSIONS_ENTYPOINT); err != nil {
		return err
	}

	if err := tx.commit(ctx); err != nil {
		return err
	}

	return nil
}
