// Copyright 2023 NJWS Inc.

package uiconnector

import (
	"fmt"
	"sort"
	"strings"

	"github.com/foliagecp/easyjson"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

func createLink(ctx *sfplugins.StatefunContextProcessor, from, to, linkType string, body *easyjson.JSON, tags ...string) error {
	const op = "functions.graph.ll.api.link.create"

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

	if _, err := ctx.GolangCallSync(op, from, &link, nil); err != nil {
		return fmt.Errorf("create link: %w", err)
	}

	return nil
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

func generateSessionID(id string) string {
	return "session_client_" + id
}
