package common

import (
	"errors"
	"fmt"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	sf "github.com/foliagecp/sdk/statefun/plugins"
)

type StatefunClient interface {
	Request(requestProvider sf.RequestProvider, typename, id string, payload, options *easyjson.JSON) (*easyjson.JSON, error)
}

func CreateObject(c StatefunClient, objectID, originType string, body easyjson.JSON) error {
	const op = "functions.cmdb.api.object.create"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("origin_type", easyjson.NewJSON(originType))
	payload.SetByPath("body", body)

	result, err := c.Request(sf.AutoRequestSelect, op, objectID, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("details"))
	}

	return nil
}

func CreateType(c StatefunClient, name string, body easyjson.JSON) error {
	const op = "functions.cmdb.api.type.create"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("body", body)

	result, err := c.Request(sf.AutoRequestSelect, op, name, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("details"))
	}

	return nil
}

func DeleteObject(c StatefunClient, id string) error {
	const op = "functions.cmdb.api.object.delete"

	payload := easyjson.NewJSONObject()
	result, err := c.Request(sf.AutoRequestSelect, op, id, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("details"))
	}

	return nil
}

func CreateObjectsLink(c StatefunClient, from, to, name string) error {
	const op = "functions.cmdb.api.objects.link.create"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))
	payload.SetByPath("name", easyjson.NewJSON(name))
	payload.SetByPath("body", easyjson.NewJSONObject())

	result, err := c.Request(sf.AutoRequestSelect, op, from, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("details"))
	}

	return nil
}

func CreateTypesLink(c StatefunClient, from, to, objectLinkType string) error {
	const op = "functions.cmdb.api.types.link.create"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))
	payload.SetByPath("body", easyjson.NewJSONObject())
	payload.SetByPath("object_type", easyjson.NewJSON(objectLinkType))

	result, err := c.Request(sf.AutoRequestSelect, op, from, &payload, nil)
	if err != nil {
		return fmt.Errorf("create types link request: %w", err)
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		details, _ := result.GetByPath("details").AsString()
		if strings.Contains(details, "already exists") {
			return nil
		}

		return fmt.Errorf("create types link: %v", details)
	}

	return nil
}

func DeleteObjectsLink(c StatefunClient, from, to string) error {
	const op = "functions.cmdb.api.objects.link.delete"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))

	result, err := c.Request(sf.AutoRequestSelect, op, from, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("details"))
	}

	return nil
}

func FindObjectType(c StatefunClient, id string) (string, error) {
	const op = "functions.cmdb.api.find_object_type"

	result, err := c.Request(sf.AutoRequestSelect, op, id, nil, nil)
	if err != nil {
		return "", err
	}

	if t, ok := result.GetByPath("data.type").AsString(); ok {
		return t, nil
	}

	return "", errors.New("undefined type")
}

func OutTargetLink(source, linkname string) string {
	return fmt.Sprintf(crud.OutLinkTargetKeyPrefPattern+crud.LinkKeySuff1Pattern, source, linkname)
}

func OutLinkType(source, ltype string, target ...string) string {
	if len(target) > 0 {
		return fmt.Sprintf(crud.OutLinkTypeKeyPrefPattern+crud.LinkKeySuff2Pattern, source, ltype, target[0])
	}
	return fmt.Sprintf(crud.OutLinkTypeKeyPrefPattern+crud.LinkKeySuff1Pattern, source, ltype)
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
