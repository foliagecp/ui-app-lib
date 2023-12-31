package uilib

import (
	"fmt"

	"github.com/foliagecp/easyjson"
	sf "github.com/foliagecp/sdk/statefun/plugins"
)

const (
	_TX_BEGIN               = "functions.cmdb.tx.begin"
	_TX_CLONE               = "functions.cmdb.tx.clone"
	_TX_CREATE_OBJECT       = "functions.cmdb.tx.object.create"
	_TX_CREATE_TYPE         = "functions.cmdb.tx.type.create"
	_TX_CREATE_OBJECTS_LINK = "functions.cmdb.tx.objects.link.create"
	_TX_CREATE_TYPES_LINK   = "functions.cmdb.tx.types.link.create"
	_TX_UPDATE_OBJECT       = "functions.cmdb.tx.object.update"
	_TX_DELETE_OBJECT       = "functions.cmdb.tx.object.delete"
	_TX_DELETE_OBJECTS_LINK = "functions.cmdb.tx.objects.link.delete"
	_TX_COMMIT              = "functions.cmdb.tx.commit"
)

type txHelper struct {
	id string
}

type beginTxType struct {
	Mode    string              `json:"mode"`
	Objects map[string]struct{} `json:"objects,omitempty"`
}

func beginTransaction(ctx *sf.StatefunContextProcessor, id, mode string) (*txHelper, error) {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("clone", easyjson.NewJSON(mode))

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_BEGIN, id, &payload, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return nil, fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return &txHelper{
		id: id,
	}, nil
}

func beginTransactionWithTypes(ctx *sf.StatefunContextProcessor, id string, types map[string]beginTxType) (*txHelper, error) {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("clone", easyjson.NewJSON("with_types"))
	payload.SetByPath("types", easyjson.NewJSON(types))

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_BEGIN, id, &payload, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return nil, fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return &txHelper{
		id: id,
	}, nil
}

func (t *txHelper) commit(ctx *sf.StatefunContextProcessor, mode ...string) error {
	payload := easyjson.NewJSONObject()
	//payload.SetByPath("debug", easyjson.NewJSON(true))
	if len(mode) > 0 {
		payload.SetByPath("mode", easyjson.NewJSON(mode[0]))
	}

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_COMMIT, t.id, &payload, nil)
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func (t *txHelper) upsertObject(ctx *sf.StatefunContextProcessor, objectID, originType string, body easyjson.JSON) error {
	if _, err := ctx.GlobalCache.GetValue(objectID); err != nil {
		if err := t.createObject(ctx, objectID, originType, &body); err != nil {
			return err
		}
	} else {
		//update
	}

	return nil
}

func (t *txHelper) createObjectsLinkIfNotExists(ctx *sf.StatefunContextProcessor, from, to string) error {
	// pattern := fmt.Sprintf("%s.out.ltp_oid-bdy.>.%s", from, to)
	// ctx.GlobalCache.GetKeysByPattern(pattern)

	if err := t.createObjectsLink(ctx, from, to); err != nil {
		return err
	}

	return nil
}

func (t *txHelper) createObjectsLink(ctx *sf.StatefunContextProcessor, from, to string) error {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("from", easyjson.NewJSON(from))
	payload.SetByPath("to", easyjson.NewJSON(to))

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_CREATE_OBJECTS_LINK, t.id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func (t *txHelper) createTypesLink(ctx *sf.StatefunContextProcessor, from, to, objectLinkType string) error {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("from", easyjson.NewJSON(from))
	payload.SetByPath("to", easyjson.NewJSON(to))
	payload.SetByPath("object_link_type", easyjson.NewJSON(objectLinkType))

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_CREATE_TYPES_LINK, t.id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func (t *txHelper) createObject(ctx *sf.StatefunContextProcessor, objectID, originType string, body *easyjson.JSON) error {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("id", easyjson.NewJSON(objectID))
	payload.SetByPath("origin_type", easyjson.NewJSON(originType))
	payload.SetByPath("body", *body)

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_CREATE_OBJECT, t.id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func (t *txHelper) updateObject(ctx *sf.StatefunContextProcessor, objectID string, body *easyjson.JSON, mode ...string) error {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("id", easyjson.NewJSON(objectID))
	payload.SetByPath("body", *body)
	if len(mode) > 0 {
		payload.SetByPath("mode", easyjson.NewJSON(mode))
	}

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_UPDATE_OBJECT, t.id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func (t *txHelper) createType(ctx *sf.StatefunContextProcessor, typeID string, body *easyjson.JSON) error {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("id", easyjson.NewJSON(typeID))
	payload.SetByPath("body", *body)

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_CREATE_TYPE, t.id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func (t *txHelper) deleteObject(ctx *sf.StatefunContextProcessor, id string) error {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("id", easyjson.NewJSON(id))

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_DELETE_OBJECT, t.id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func (t *txHelper) deleteObjectsLink(ctx *sf.StatefunContextProcessor, from, to string) error {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("from", easyjson.NewJSON(from))
	payload.SetByPath("to", easyjson.NewJSON(to))

	result, err := ctx.Request(sf.GolangLocalRequest, _TX_DELETE_OBJECTS_LINK, t.id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func (tx *txHelper) initTypes(ctx *sf.StatefunContextProcessor) error {
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

	return nil
}
