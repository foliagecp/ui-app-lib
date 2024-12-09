package common

import (
	"fmt"

	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun/plugins"
)

func MustDBClient(request plugins.SFRequestFunc) db.DBSyncClient {
	c, err := db.NewDBSyncClientFromRequestFunction(request)
	if err != nil {
		panic(err)
	}
	return c
}

func OutTargetLink(source, linkname string) string {
	return fmt.Sprintf(crud.OutLinkTargetKeyPrefPattern+crud.KeySuff1Pattern, source, linkname)
}

func OutLinkType(source, ltype string, target ...string) string {
	if len(target) > 0 {
		return fmt.Sprintf(crud.OutLinkTypeKeyPrefPattern+crud.KeySuff2Pattern, source, ltype, target[0])
	}
	return fmt.Sprintf(crud.OutLinkTypeKeyPrefPattern+crud.KeySuff1Pattern, source, ltype)
}

func InLinkKeyPattern(id, target string, linkType ...string) string {
	if len(linkType) > 0 {
		lt := linkType[0]

		return fmt.Sprintf(crud.InLinkKeyPrefPattern+crud.KeySuff2Pattern,
			id, target, lt,
		)
	}

	return fmt.Sprintf(crud.InLinkKeyPrefPattern+crud.KeySuff1Pattern,
		id, target,
	)
}
