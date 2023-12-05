// Copyright 2023 NJWS Inc.

package uilib

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/common"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

func createLink(ctx *sfplugins.StatefunContextProcessor, from, to, linkType string, body *easyjson.JSON, tags ...string) error {
	const op = "functions.graph.api.link.create"

	link := easyjson.NewJSONObject()
	link.SetByPath("descendant_uuid", easyjson.NewJSON(to))
	link.SetByPath("link_type", easyjson.NewJSON(linkType))

	if body == nil {
		link.SetByPath("link_body", easyjson.NewJSONObject())
	} else {
		link.SetByPath("link_body", *body)
	}

	if len(tags) > 0 {
		link.SetByPath("link_body.tags", easyjson.JSONFromArray(tags))
	}

	if _, err := ctx.Request(sfplugins.GolangLocalRequest, op, from, &link, nil); err != nil {
		return fmt.Errorf("create link: %w", err)
	}

	return nil
}

func getOutLinkTypes(ctx *sfplugins.StatefunContextProcessor, uuid string) []string {
	pattern := uuid + ".out.ltp_oid-bdy.>"

	result := make([]string, 0)
	visited := make(map[string]struct{})

	for _, key := range ctx.GlobalCache.GetKeysByPattern(pattern) {
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

func getChildrenUUIDSByLinkType(ctx *sfplugins.StatefunContextProcessor, uuid, filterLinkType string) []string {
	result := make([]string, 0)

	pattern := uuid + ".out.ltp_oid-bdy.>"
	if filterLinkType != "" {
		pattern = uuid + ".out.ltp_oid-bdy." + filterLinkType + ".>"
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
