package egress

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/system"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

const egressDelim = "="

var (
	sessionID2ClientIDCache sync.Map
)

func SendToSessionEgress(ctx *sf.StatefunContextProcessor, sessionID string, payload *easyjson.JSON) error {
	cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)

	var clientID string
	if value, ok := sessionID2ClientIDCache.Load(sessionID); ok {
		clientID = value.(string)
	} else {
		// sessionID2ClientIDCache size control ---------------------
		cacheLength := 0
		sessionID2ClientIDCache.Range(func(key, value any) bool {
			cacheLength++
			return true
		})
		if cacheLength > 1000 {
			sessionID2ClientIDCache.Range(func(key, value any) bool {
				sessionID2ClientIDCache.Delete(key)
				return true
			})
		}
		// ----------------------------------------------------------

		session, err := cmdb.ObjectRead(sessionID)
		if err != nil {
			return err
		}

		cid, ok := session.GetByPath("body.client_id").AsString()
		if !ok {
			return err
		}
		clientID = cid
		sessionID2ClientIDCache.Store(sessionID, clientID)
	}

	return ctx.Signal(sf.AutoSignalSelect, inStatefun.EGRESS, generateEgressID(clientID), payload, nil)
}

func ClientIDFromEgressID(id string) string {
	if len(id) == 0 {
		return id
	}

	split := strings.Split(id, egressDelim)

	return split[0]
}

func generateEgressID(clientID string) string {
	s := make([]byte, 5)
	_, err := rand.Read(s)
	system.MsgOnErrorReturn(err)
	hash := md5.Sum(s)
	hex := hex.EncodeToString(hash[:])
	return clientID + egressDelim + hex
}
