package decorators

import (
	"sort"
	"strings"

	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/internal/common"
)

/*
	Request: {
		"link_type"
	}

	Response: {
		"status"
		"message"
		"data": []string
	}
*/
func childrenUUIDsByLinkType(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	filterLinkType, ok := ctx.Payload.GetByPath("link_type").AsString()
	if !ok {
		errResponse(ctx, "missing link_type")
		return
	}

	result := make([]string, 0)
	pattern := common.OutLinkType(ctx.Self.ID, filterLinkType, ">")
	keys := ctx.Domain.Cache().GetKeysByPattern(pattern)

	for _, key := range keys {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		lastkey := split[len(split)-1]
		result = append(result, lastkey)
	}

	sort.Strings(result)

	okResponse(ctx, result)
}
