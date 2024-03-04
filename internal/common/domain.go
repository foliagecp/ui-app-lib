package common

import "github.com/foliagecp/sdk/statefun"

func SetHubPreffix(d *statefun.Domain, id string) string {
	return d.CreateObjectIDWithHubDomain(id, false)
}
