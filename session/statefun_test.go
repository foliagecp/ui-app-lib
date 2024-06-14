package session_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	"github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/test"
	"github.com/foliagecp/ui-app-lib/adapter"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
	"github.com/foliagecp/ui-app-lib/session"
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
	s.OnAfterStartFunction(session.InitSchema, true)

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

	s.RegisterFunction(typename, session.Ingress, cfg)
	s.StartRuntime()

	clientID := uuid.New()
	payload := easyjson.NewJSONObject()

	err := s.Signal(plugins.AutoSignalSelect, typename, clientID.String(), &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_Ingress_SendSignal_Client() {
	typename := inStatefun.INGRESS
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, session.Ingress, cfg)
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

	s.RegisterFunction(typename, session.SessionRouter, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))

	err := s.Signal(plugins.AutoSignalSelect, typename, sessionID, &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_SessionRouter_InvalidCommand() {
	typename := inStatefun.SESSION_ROUTER
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, session.SessionRouter, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))
	payload.SetByPath("command", easyjson.NewJSON("START"))

	err := s.Signal(plugins.AutoSignalSelect, typename, sessionID, &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_SessionRouter_StartSession_Correct() {
	typename := inStatefun.SESSION_ROUTER
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, session.SessionRouter, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))
	payload.SetByPath("command", easyjson.NewJSON(session.START_SESSION))

	err := s.Signal(plugins.AutoSignalSelect, typename, sessionID, &payload, nil)
	s.NoError(err)
}

func (s *sessionTestSuite) Test_SessionRouter_Controllers_Correct() {
	typename := inStatefun.SESSION_ROUTER
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	s.RegisterFunction(typename, session.SessionRouter, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	controllers := map[string]session.Controller{
		"ctrl": {
			Body:  make(map[string]string),
			UUIDs: []string{"uuid"},
		},
	}

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))
	payload.SetByPath("viewer", easyjson.NewJSON(controllers))

	err := s.Signal(plugins.AutoSignalSelect, typename, sessionID, &payload, nil)
	s.NoError(err)

}

func (s *sessionTestSuite) Test_StartSession_Correct() {
	typename := inStatefun.SESSION_START
	cfg := *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1)

	// prepare crud, schema and related statefun
	crud.RegisterAllFunctionTypes(s.Runtime())
	s.OnAfterStartFunction(session.InitSchema, true)
	s.RegisterFunction(inStatefun.EGRESS, session.Egress, cfg)
	// -----------------------------------------

	s.RegisterFunction(typename, session.StartSession, cfg)
	s.StartRuntime()

	clientID := uuid.New().String()
	sessionID := generate.SessionID(clientID).String()

	payload := easyjson.NewJSONObject()
	payload.SetByPath("client_id", easyjson.NewJSON(clientID))

	err := s.Signal(plugins.AutoSignalSelect, typename, sessionID, &payload, nil)
	s.Require().NoError(err)

	sub, err := s.SubscribeEgress(inStatefun.EGRESS, clientID)
	s.Require().NoError(err)

	defer sub.Unsubscribe()

	msg, err := sub.NextMsg(5 * time.Second)
	s.Require().NoError(err)

	wantResponse := `{"payload":{"command":"START_SESSION","status":"ok"}}`
	s.Require().Equal(wantResponse, string(msg.Data))

	gotSession, err := s.CacheValue(sessionID)
	s.Require().NoError(err)

	s.Equal(clientID, gotSession.GetByPath("client_id").AsStringDefault(""))
	// TODO: check link from SESSION_ENTRYPOINT to session
}

func (s *sessionTestSuite) Test_StartController_Correct() {
	typename := inStatefun.SESSION_START_CONTROLLER

	crud.RegisterAllFunctionTypes(s.Runtime())
	s.RegisterFunction(inStatefun.CONTROLLER_START, adapter.StartController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	s.RegisterFunction(inStatefun.EGRESS, session.Egress, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	s.RegisterFunction(typename, session.StartController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	s.OnAfterStartFunction(session.InitSchema, true)

	err := s.StartRuntime()
	s.Require().NoError(err)

	cmdb, err := db.NewCMDBSyncClientFromRequestFunction(s.Request)
	s.Require().NoError(err)

	clientID := "1"
	sessionID := generate.SessionID(clientID).String()

	cmdb.ObjectCreate(sessionID, inStatefun.SESSION_TYPE, easyjson.NewJSONObjectWithKeyValue("client_id", easyjson.NewJSON(clientID)))

	plugin := map[string]map[string]session.Controller{
		"viewer": {
			"test_controller": {
				Body: map[string]string{
					"props": "@property:",
				},
				UUIDs: []string{"uuid_1", "uuid_2"},
			},
		},
	}

	sub, err := s.SubscribeEgress(inStatefun.EGRESS, clientID)
	s.Require().NoError(err)

	payload := easyjson.NewJSON(plugin)
	err = s.Signal(plugins.AutoSignalSelect, typename, sessionID, &payload, nil)
	s.Require().NoError(err)

	msg, err := sub.NextMsg(1 * time.Second)
	s.Require().NoError(err)

	wantPayload := `{"payload":{"command":"START_CONTROLLER","status":"ok"}}`
	s.JSONEq(wantPayload, string(msg.Data))
}
