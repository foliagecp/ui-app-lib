package generate

import (
	"github.com/google/uuid"
)

var namespace = mustGenerateNamespace("ui-app-lib")

func mustGenerateNamespace(base string) uuid.UUID {
	padding := make([]byte, 16-len(base))
	u, err := uuid.FromBytes(append(padding, []byte(base)...))
	if err != nil {
		panic(err)
	}
	return u

}

func UUID(id string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(id))
}

func SessionID(clientID string) uuid.UUID {
	return UUID("session_client_" + clientID)
}
