

package uilib

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	"github.com/foliagecp/sdk/statefun/logger"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/adapter"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
	"github.com/foliagecp/ui-app-lib/session"
)

const (
	sessionsWatchTimeout     = 60 * time.Second
	sessionInactivityTimeout = 12 * time.Hour
	maxSessionsCount         = 10
)

func sessionsKeeper(runtime *statefun.Runtime) {
	dbc, err := db.NewDBSyncClientFromRequestFunction(runtime.Request)
	if err != nil {
		logger.Logf(logger.ErrorLevel, "ui-app-lib: cannot start sessionsKeeper, dbc creation error %s\n", err.Error())
		return
	}

	for {
		time.Sleep(sessionsWatchTimeout)
		now := time.Now().Unix()

		ids, err := dbc.Query.JPGQLCtraQuery(inStatefun.SESSION_TYPE, fmt.Sprintf(".*[type('%s')]", crud.OBJECT_TYPELINK))
		if err != nil {
			slog.Error(err.Error())
			return
		}

		// Closing too old sessions -------------------------------------------
		stillOpenedSessionsUpdateTimes := map[string]int64{}
		for _, sessionId := range ids {
			sdata, err := dbc.Graph.VertexRead(sessionId)
			if err == nil {
				updatedAt := int64(sdata.GetByPath("body.updated_at").AsNumericDefault(0))
				if updatedAt+int64(sessionInactivityTimeout.Seconds()) < now {
					runtime.Signal(sf.JetstreamGlobalSignal, inStatefun.SESSION_CLOSE, sessionId, nil, nil)
				} else {
					stillOpenedSessionsUpdateTimes[sessionId] = updatedAt
				}
			}
		}
		// --------------------------------------------------------------------

		// Closing exceeding sessions -----------------------------------------
		exceedingSessionsCount := len(stillOpenedSessionsUpdateTimes) - maxSessionsCount
		if exceedingSessionsCount > 0 {
			sessionIdsFromOldest2Newest := make([]string, 0, len(stillOpenedSessionsUpdateTimes))
			for key := range stillOpenedSessionsUpdateTimes {
				sessionIdsFromOldest2Newest = append(sessionIdsFromOldest2Newest, key)
			}

			sort.Slice(sessionIdsFromOldest2Newest, func(i, j int) bool {
				return stillOpenedSessionsUpdateTimes[sessionIdsFromOldest2Newest[i]] < stillOpenedSessionsUpdateTimes[sessionIdsFromOldest2Newest[j]]
			})

			for i := 0; i < exceedingSessionsCount; i++ {
				runtime.Signal(sf.JetstreamGlobalSignal, inStatefun.SESSION_CLOSE, sessionIdsFromOldest2Newest[i], nil, nil)
			}
		}
		// --------------------------------------------------------------------
	}

}

func RegisterAllFunctions(runtime *statefun.Runtime) {
	session.RegisterFunctions(runtime)
	adapter.RegisterFunctions(runtime)

	go sessionsKeeper(runtime)
}
