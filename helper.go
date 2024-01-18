

package uilib

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/common"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

func createObject(ctx *sfplugins.StatefunContextProcessor, objectID, originType string, body *easyjson.JSON) error {
	const op = "functions.cmdb.api.object.create"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("origin_type", easyjson.NewJSON(originType))
	payload.SetByPath("body", *body)

	result, err := ctx.Request(sf.GolangLocalRequest, op, objectID, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func createObjectsLink(ctx *sf.StatefunContextProcessor, from, to string) error {
	const op = "functions.cmdb.api.objects.link.create"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))

	result, err := ctx.Request(sf.GolangLocalRequest, op, from, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func deleteObjectsLink(ctx *sfplugins.StatefunContextProcessor, from, to string) error {
	const op = "functions.cmdb.api.objects.link.delete"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))

	result, err := ctx.Request(sf.GolangLocalRequest, op, from, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func createTypesLink(ctx *sfplugins.StatefunContextProcessor, from, to, objectLinkType string) error {
	tx, err := beginTransaction(ctx, pool.GetTxID(), "min")
	if err != nil {
		return err
	}

	if err := tx.createTypesLink(ctx, from, to, objectLinkType); err != nil {
		return err
	}

	return tx.commit(ctx)
}

type Link struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

func outLinkKeyPattern(id, target string, linkType ...string) string {
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

func inLinkKeyPattern(id, target string, linkType ...string) string {
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

func getLinksByType(ctx *sfplugins.StatefunContextProcessor, uuid, filterLinkType string) []Link {
	result := make([]Link, 0)

	outPattern := outLinkKeyPattern(uuid, ">", filterLinkType)
	for _, key := range ctx.GlobalCache.GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		result = append(result, Link{
			Source: uuid,
			Target: split[len(split)-1],
			Type:   filterLinkType,
		})
	}

	inPattern := inLinkKeyPattern(uuid, ">")
	for _, key := range ctx.GlobalCache.GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		ltype := split[len(split)-1]

		if ltype != filterLinkType {
			continue
		}

		result = append(result, Link{
			Source: split[len(split)-2],
			Target: uuid,
			Type:   filterLinkType,
		})
	}

	return result
}

func getOutLinkTypes(ctx *sfplugins.StatefunContextProcessor, uuid string) []string {
	outPattern := outLinkKeyPattern(uuid, ">")

	result := make([]string, 0)
	visited := make(map[string]struct{})

	for _, key := range ctx.GlobalCache.GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-2]

		// TODO: use builtin constants
		switch linkType {
		case "__object", "__type", "trigger", "controller", "__types", "__objects":
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	return result
}

func getInOutLinkTypes(ctx *sfplugins.StatefunContextProcessor, uuid string) []string {
	outPattern := outLinkKeyPattern(uuid, ">")

	result := make([]string, 0)
	visited := make(map[string]struct{})

	for _, key := range ctx.GlobalCache.GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-2]

		// TODO: use builtin constants
		switch linkType {
		case "__object", "__type", "trigger", "controller", "__types", "__objects":
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	inPattern := inLinkKeyPattern(uuid, ">")
	for _, key := range ctx.GlobalCache.GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-1]

		// TODO: use builtin constants
		switch linkType {
		case "__object", "__type", "trigger", "controller", "__types", "__objects":
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	return result
}

func getChildrenUUIDSByLinkType(ctx *sfplugins.StatefunContextProcessor, uuid, filterLinkType string) []string {
	result := make([]string, 0)

	pattern := outLinkKeyPattern(uuid, ">")
	if filterLinkType != "" {
		pattern = outLinkKeyPattern(uuid, ">", filterLinkType)
	}

	for _, key := range ctx.GlobalCache.GetKeysByPattern(pattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		lastkey := split[len(split)-1]
		result = append(result, lastkey)
	}

	sort.Strings(result)

	return result
}

func replyOk(ctx *sfplugins.StatefunContextProcessor) {
	reply(ctx, "ok", easyjson.NewJSONObject())
}

func replyError(ctx *sfplugins.StatefunContextProcessor, err error) {
	reply(ctx, "failed", easyjson.NewJSON(err.Error()))
}

func checkRequestError(result *easyjson.JSON, err error) error {
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return errors.New(result.GetByPath("payload.result").AsStringDefault("unknown error"))
	}

	return nil
}

func reply(ctx *sfplugins.StatefunContextProcessor, status string, data easyjson.JSON) {
	qid := common.GetQueryID(ctx)
	reply := easyjson.NewJSONObject()
	reply.SetByPath("status", easyjson.NewJSON(status))
	reply.SetByPath("result", data)
	common.ReplyQueryID(qid, easyjson.NewJSONObjectWithKeyValue("payload", reply).GetPtr(), ctx)
}
