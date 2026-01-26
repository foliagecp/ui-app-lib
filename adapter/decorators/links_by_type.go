package decorators

import (
	"context"

	"github.com/foliagecp/sdk/statefun/logger"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/internal/common"
)

type link struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Type   string   `json:"type,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

/*
	Request: {
		"link_type"
	}

	Response: {
		"status"
		"message"
		"data": []{
			"source"
			"target"
			"type"
			"tags"
		}
	}
*/
func linksByType(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	filterLinkType, ok := ctx.Payload.GetByPath("link_type").AsString()
	if !ok {
		errResponse(ctx, "missing link_type")
		return
	}

	db := common.MustDBClient(ctx.Request)
	data, err := db.Graph.VertexRead(ctx.Self.ID, true)
	if err != nil {
		logger.GetLogger().Error(context.TODO(), err.Error())
		return
	}

	result := []link{}

	for i := 0; i < data.GetByPath("links.out.names").ArraySize(); i++ {
		linkType := data.GetByPath("links.out.types").ArrayElement(i).AsStringDefault("")
		toId := data.GetByPath("links.out.ids").ArrayElement(i).AsStringDefault("")
		if linkType != filterLinkType {
			continue
		}
		result = append(result, link{
			Source: ctx.Self.ID,
			Target: ctx.Domain.GetObjectIDByShadowObjectID(toId),
			Type:   filterLinkType,
		})
	}

	for i := 0; i < data.GetByPath("links.in").ArraySize(); i++ {
		j := data.GetByPath("links.in").ArrayElement(i)
		objectID := j.GetByPath("from").AsStringDefault("")
		linkType := j.GetByPath("type").AsStringDefault("")

		if linkType != filterLinkType {
			continue
		}

		result = append(result, link{
			Source: ctx.Domain.GetObjectIDByShadowObjectID(objectID),
			Target: ctx.Self.ID,
			Type:   filterLinkType,
		})
	}

	okResponse(ctx, result)
}
