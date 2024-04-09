package session

import (
	"fmt"
	"testing"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	"github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/test"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type sessionTestSuite struct {
	test.StatefunTestSuite
}

func TestSessionTestSuite(t *testing.T) {
	suite.Run(t, new(sessionTestSuite))
}

func (s *sessionTestSuite) Test_InitSchema() {
	crud.RegisterAllFunctionTypes(s.Runtime())
	s.OnAfterStartFunction(initSchema, true)

	err := s.StartRuntime()
	s.NoError(err)

	_, err = s.CacheValue(inStatefun.SESSION_TYPE)
	s.NoError(err)

	_, err = s.CacheValue(inStatefun.SESSIONS_ENTYPOINT)
	s.NoError(err)

	// TODO: check links
}

func (s *sessionTestSuite) Test_Ingress_SendSignal_Code() {
	typename := inStatefun.INGRESS
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, ingress, cfg)
	s.StartRuntime()

	clientID := uuid.New()
	payload := easyjson.NewJSONObject()

	err := s.Signal(plugins.JetstreamGlobalSignal, typename, clientID.String(), &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_Ingress_SendSignal_Client() {
	typename := inStatefun.INGRESS
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, ingress, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()

	subj := fmt.Sprintf(`$SI.hub.signal.hub.%s.%s`, typename, clientID)
	data := []byte(`{"payload":{}}`)

	err := s.Publish(subj, data)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_SessionRouter_EmptyCommand() {
	typename := inStatefun.SESSION_ROUTER
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, sessionRouter, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))

	err := s.Signal(plugins.JetstreamGlobalSignal, typename, sessionID, &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_SessionRouter_InvalidCommand() {
	typename := inStatefun.SESSION_ROUTER
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, sessionRouter, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))
	payload.SetByPath("command", easyjson.NewJSON("START"))

	err := s.Signal(plugins.JetstreamGlobalSignal, typename, sessionID, &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_SessionRouter_StartSession_Correct() {
	typename := inStatefun.SESSION_ROUTER
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, sessionRouter, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))
	payload.SetByPath("command", easyjson.NewJSON(START_SESSION))

	err := s.Signal(plugins.JetstreamGlobalSignal, typename, sessionID, &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_SessionRouter_Controllers_Correct() {
	typename := inStatefun.SESSION_ROUTER
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, sessionRouter, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	controllers := map[string]Controller{
		"ctrl": {
			Body:  make(map[string]string),
			UUIDs: []string{"uuid"},
		},
	}

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))
	payload.SetByPath("controllers", easyjson.NewJSON(controllers))

	err := s.Signal(plugins.JetstreamGlobalSignal, typename, sessionID, &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_StartSession_Correct() {
	typename := inStatefun.SESSION_START
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	// prepare crud, schema and related statefun
	crud.RegisterAllFunctionTypes(s.Runtime())
	s.OnAfterStartFunction(initSchema, true)
	s.RegisterFunction(inStatefun.PREPARE_EGRESS, preEgress, cfg)
	s.RegisterFunction(inStatefun.EGRESS, egress, cfg)
	// -----------------------------------------

	s.RegisterFunction(typename, startSession, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))

	err := s.Signal(plugins.JetstreamGlobalSignal, typename, sessionID, &payload, nil)
	s.Require().NoError(err)

	sub, err := s.SubscribeEgress(inStatefun.EGRESS, clientID)
	s.Require().NoError(err)

	defer sub.Unsubscribe()

	msg, err := sub.NextMsg(1 * time.Second)
	s.Require().NoError(err)

	wantResponse := `{"payload":{"command":"START_SESSION","status":"ok"}}`
	s.Require().Equal(wantResponse, string(msg.Data))

	gotSession, err := s.CacheValue(sessionID)
	s.Require().NoError(err)

	s.Equal(clientID, gotSession.GetByPath("client_id").AsStringDefault(""))
	s.Equal(SessionInactivityTimeout.String(), gotSession.GetByPath("inactivity_timeout").AsStringDefault(""))
	s.Equal(SessionLifeTime.String(), gotSession.GetByPath("life_time").AsStringDefault(""))

	// TODO: check link from SESSION_ENTRYPOINT to session
}
