// Copyright 2023 NJWS Inc.

package uilib

import (
	"github.com/foliagecp/sdk/statefun"
	"github.com/foliagecp/ui-app-lib/adapter"
	"github.com/foliagecp/ui-app-lib/session"
)

func RegisterAllFunctions(runtime *statefun.Runtime) {
	session.RegisterFunctions(runtime)
	adapter.RegisterFunctions(runtime)
}
