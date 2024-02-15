package common

import (
	"fmt"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	sf "github.com/foliagecp/sdk/statefun/plugins"
)

func CreateObject(ctx *sf.StatefunContextProcessor, objectID, originType string, body easyjson.JSON) error {
	const op = "functions.cmdb.api.object.create"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("origin_type", easyjson.NewJSON(originType))
	payload.SetByPath("body", body)

	result, err := ctx.Request(sf.AutoSelect, op, objectID, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func DeleteObject(ctx *sf.StatefunContextProcessor, id string) error {
	const op = "functions.cmdb.api.object.delete"

	payload := easyjson.NewJSONObject()
	result, err := ctx.Request(sf.AutoSelect, op, id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func CreateObjectsLink(ctx *sf.StatefunContextProcessor, from, to string) error {
	const op = "functions.cmdb.api.objects.link.create"

	pattern := fmt.Sprintf(crud.InLinkKeyPrefPattern+crud.LinkKeySuff2Pattern, to, from, ">")
	if keys := ctx.GlobalCache.GetKeysByPattern(pattern); len(keys) > 0 {
		return nil
	}

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))
	payload.SetByPath("body", easyjson.NewJSONObject())

	result, err := ctx.Request(sf.AutoSelect, op, from, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func CreateTypesLink(ctx *sf.StatefunContextProcessor, from, to, objectLinkType string) error {
	const op = "functions.cmdb.api.types.link.create"

	link := OutLinkKeyPattern(from, to, "__type")
	if _, err := ctx.GlobalCache.GetValue(link); err == nil {
		return nil
	}

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))
	payload.SetByPath("body", easyjson.NewJSONObject())
	payload.SetByPath("object_link_type", easyjson.NewJSON(objectLinkType))

	result, err := ctx.Request(sf.AutoSelect, op, from, &payload, nil)
	if err != nil {
		return fmt.Errorf("create types link request: %w", err)
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("create types link: %v", result.GetByPath("payload.result"))
	}

	return nil
}

func DeleteObjectsLink(ctx *sf.StatefunContextProcessor, from, to string) error {
	const op = "functions.cmdb.api.objects.link.delete"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))

	result, err := ctx.Request(sf.AutoSelect, op, from, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func OutLinkKeyPattern(id, target string, linkType ...string) string {
	if len(linkType) > 0 {
		lt := linkType[0]

		return fmt.Sprintf(crud.OutLinkBodyKeyPrefPattern+crud.LinkKeySuff2Pattern,
			id, lt, target,
		)
	}

	return fmt.Sprintf(crud.OutLinkBodyKeyPrefPattern+crud.LinkKeySuff1Pattern,
		id, target,
	)
}

func InLinkKeyPattern(id, target string, linkType ...string) string {
	if len(linkType) > 0 {
		lt := linkType[0]

		return fmt.Sprintf(crud.InLinkKeyPrefPattern+crud.LinkKeySuff2Pattern,
			id, target, lt,
		)
	}

	return fmt.Sprintf(crud.InLinkKeyPrefPattern+crud.LinkKeySuff1Pattern,
		id, target,
	)
}
