package decorators

import (
	"strings"

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

	c := common.MustCMDBClient(ctx.Request)
	result := make([]link, 0)

	outPattern := common.OutLinkType(ctx.Self.ID, filterLinkType, ">")
	for _, key := range ctx.Domain.Cache().GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		result = append(result, link{
			Source: ctx.Self.ID,
			Target: split[len(split)-1],
			Type:   filterLinkType,
		})
	}

	inPattern := common.InLinkKeyPattern(ctx.Self.ID, ">")

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		objectID := split[len(split)-2]

		linkType, err := common.ObjectType(c, objectID)
		if err != nil {
			continue
		}

		linkType = ctx.Domain.GetObjectIDWithoutDomain(linkType)

		if linkType != filterLinkType {
			continue
		}

		result = append(result, link{
			Source: objectID,
			Target: ctx.Self.ID,
			Type:   filterLinkType,
		})
	}

	okResponse(ctx, result)
}
