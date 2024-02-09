// Copyright 2023 NJWS Inc.

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
)

func createObject(ctx *sf.StatefunContextProcessor, objectID, originType string, body easyjson.JSON) error {
	const op = "functions.cmdb.api.object.create"

	payload := easyjson.NewJSONObject()
	payload.SetByPath("origin_type", easyjson.NewJSON(originType))
	payload.SetByPath("body", body)

	result, err := ctx.Request(sf.GolangLocalRequest, op, objectID, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func deleteObject(ctx *sf.StatefunContextProcessor, id string) error {
	const op = "functions.cmdb.api.object.delete"

	payload := easyjson.NewJSONObject()
	result, err := ctx.Request(sf.GolangLocalRequest, op, id, &payload, nil)
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

	pattern := fmt.Sprintf(crud.InLinkKeyPrefPattern+crud.LinkKeySuff2Pattern, to, from, ">")
	if keys := ctx.GlobalCache.GetKeysByPattern(pattern); len(keys) > 0 {
		return nil
	}

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))
	payload.SetByPath("body", easyjson.NewJSONObject())

	result, err := ctx.Request(sf.GolangLocalRequest, op, from, &payload, nil)
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("%v", result.GetByPath("payload.result"))
	}

	return nil
}

func createTypesLink(ctx *sf.StatefunContextProcessor, from, to, objectLinkType string) error {
	const op = "functions.cmdb.api.types.link.create"

	link := outLinkKeyPattern(from, to, "__type")
	if _, err := ctx.GlobalCache.GetValue(link); err == nil {
		return nil
	}

	payload := easyjson.NewJSONObject()
	payload.SetByPath("to", easyjson.NewJSON(to))
	payload.SetByPath("body", easyjson.NewJSONObject())
	payload.SetByPath("object_link_type", easyjson.NewJSON(objectLinkType))

	result, err := ctx.Request(sf.GolangLocalRequest, op, from, &payload, nil)
	if err != nil {
		return fmt.Errorf("create types link request: %w", err)
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return fmt.Errorf("create types link: %v", result.GetByPath("payload.result"))
	}

	return nil
}

func deleteObjectsLink(ctx *sf.StatefunContextProcessor, from, to string) error {
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

type Link struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Type   string   `json:"type,omitempty"`
	Tags   []string `json:"tags,omitempty"`
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

func getLinksByType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []Link {
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

func getOutLinkTypes(ctx *sf.StatefunContextProcessor, uuid string) []string {
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
		case "__object", "__type", "__types", "__objects", _SESSION_TYPE, _CONTROLLER_TYPE, _CONTROLLER_SUBJECT_TYPE, _SUBSCRIBER_TYPE:
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	return result
}

func getInOutLinkTypes(ctx *sf.StatefunContextProcessor, uuid string) []string {
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
		case "__object", "__type", "__types", "__objects", _SESSION_TYPE, _CONTROLLER_TYPE, _CONTROLLER_SUBJECT_TYPE, _SUBSCRIBER_TYPE:
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
		case "__object", "__type", "__types", "__objects", _SESSION_TYPE, _CONTROLLER_TYPE, _CONTROLLER_SUBJECT_TYPE, _SUBSCRIBER_TYPE:
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	return result
}

func getChildrenUUIDSByLinkType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []string {
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

func getParentsTypes(ctx *sf.StatefunContextProcessor, uuid string) []string {
	result := make([]string, 0)

	for _, v := range getParentsUUIDSByLinkType(ctx, uuid, "__type") {
		if _, err := ctx.GlobalCache.GetValue(outLinkKeyPattern("types", v, "__type")); err != nil {
			continue
		}

		result = append(result, v)
	}

	sort.Strings(result)

	return result
}

func getParentsUUIDSByLinkType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []string {
	result := make([]string, 0)

	pattern := inLinkKeyPattern(uuid, ">")

	for _, key := range ctx.GlobalCache.GetKeysByPattern(pattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-1]
		if linkType != filterLinkType {
			continue
		}

		lastkey := split[len(split)-2]
		result = append(result, lastkey)
	}

	sort.Strings(result)

	return result
}

type typesRouteView struct {
	Type  string      `json:"type"`
	Nodes []routeNode `json:"nodes"`
	Links []Link      `json:"links"`
}

type routeNode struct {
	ID      string        `json:"uuid"`
	Name    string        `json:"name"`
	Depth   int           `json:"depth"`
	Objects []routeObject `json:"objects"`
}

type routeObject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func typesNavigation(ctx *sf.StatefunContextProcessor, id string, forward, backward int) easyjson.JSON {
	types := getChildrenUUIDSByLinkType(ctx, id, "__type")
	if len(types) == 0 {
		return easyjson.NewJSONObjectWithKeyValue("status", easyjson.NewJSON("failed"))
	}

	originType := types[0]
	route := traverseTypesGraph(ctx, originType, forward, backward)

	return easyjson.NewJSON(route)
}

func traverseTypesGraph(ctx *sf.StatefunContextProcessor, startID string, forward, backward int) typesRouteView {
	maxForward := forward
	if forward < 0 {
		maxForward = 10 // make const
	}

	maxBackward := backward
	if backward < 0 {
		maxBackward = 10 // make const
	}

	links := make([]Link, 0)
	nodes := make([]routeNode, 0)

	if maxForward > 0 {
		stack := []routeNode{
			{ID: startID, Name: startID, Depth: 0},
		}

		for len(stack) > 0 {
			current := stack[0]
			stack = stack[1:]

			if current.Depth < maxForward {
				for _, v := range getChildrenUUIDSByLinkType(ctx, current.ID, "__type") {
					node := routeNode{
						ID:      v,
						Name:    v,
						Depth:   current.Depth + 1,
						Objects: getTypeObjects(ctx, v),
					}

					link := Link{
						Source: current.ID,
						Target: v,
					}

					links = append(links, link)
					nodes = append(nodes, node)
					stack = append(stack, node)
				}
			}
		}
	}

	if maxBackward > 0 {
		stack := []routeNode{
			{ID: startID, Name: startID, Depth: 0},
		}

		for len(stack) > 0 {
			current := stack[0]
			stack = stack[1:]

			if -current.Depth < maxBackward {
				for _, v := range getParentsTypes(ctx, current.ID) {
					node := routeNode{
						ID:      v,
						Name:    v,
						Depth:   current.Depth - 1,
						Objects: getTypeObjects(ctx, v),
					}

					link := Link{
						Source: v,
						Target: current.ID,
					}

					links = append(links, link)
					nodes = append(nodes, node)
					stack = append(stack, node)
				}
			}
		}
	}

	fmt.Printf("nodes: %v\n", nodes)
	fmt.Printf("links: %v\n", links)

	return typesRouteView{
		Type:  startID,
		Links: links,
		Nodes: nodes,
	}
}

func getTypeObjects(ctx *sf.StatefunContextProcessor, typeID string) []routeObject {
	keys := getChildrenUUIDSByLinkType(ctx, typeID, "__object")
	objects := make([]routeObject, 0, len(keys))

	for _, object := range keys {
		objects = append(objects, routeObject{
			Name: object,
			ID:   object,
		})
	}

	return objects
}

func replyOk(ctx *sf.StatefunContextProcessor) {
	reply(ctx, "ok", easyjson.NewJSONObject())
}

func replyError(ctx *sf.StatefunContextProcessor, err error) {
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

func reply(ctx *sf.StatefunContextProcessor, status string, data easyjson.JSON) {
	qid := common.GetQueryID(ctx)
	reply := easyjson.NewJSONObject()
	reply.SetByPath("status", easyjson.NewJSON(status))
	reply.SetByPath("result", data)
	common.ReplyQueryID(qid, easyjson.NewJSONObjectWithKeyValue("payload", reply).GetPtr(), ctx)
}
