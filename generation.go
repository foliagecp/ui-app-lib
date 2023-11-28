package uilib

import (
	"crypto/md5"
	"encoding/hex"

	"github.com/google/uuid"
)

var namespace = mustGenerateNamespace("uilib")

func mustGenerateNamespace(base string) uuid.UUID {
	padding := make([]byte, 16-len(base))
	u, err := uuid.FromBytes(append(padding, []byte(base)...))
	if err != nil {
		panic(err)
	}
	return u

}

func generateUUID(id string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(id))
}

func generateSessionID(clientID string) uuid.UUID {
	return generateUUID("session_client_" + clientID)
}

func generateTxID(id string) string {
	h := md5.Sum([]byte(id))
	return hex.EncodeToString(h[:])
}
