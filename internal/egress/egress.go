package egress

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

const egressDelim = "="

func SendToSessionEgress(ctx *sf.StatefunContextProcessor, sessionID string, payload *easyjson.JSON) error {
	cmdb, _ := db.NewCMDBSyncClientFromRequestFunction(ctx.Request)

	session, err := cmdb.ObjectRead(sessionID)
	if err != nil {
		return err
	}

	clientID, ok := session.GetByPath("body.client_id").AsString()
	if !ok {
		return err
	}

	return ctx.Signal(sf.JetstreamGlobalSignal, inStatefun.EGRESS, generateEgressID(clientID), payload, nil)
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
	rand.Read(s)
	hash := md5.Sum(s)
	hex := hex.EncodeToString(hash[:])
	return clientID + egressDelim + hex
}
