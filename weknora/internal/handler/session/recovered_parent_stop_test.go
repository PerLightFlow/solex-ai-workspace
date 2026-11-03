package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/agent/collaboration"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type recoveredStopMessages struct {
	interfaces.MessageService
	mu sync.Mutex
	m  types.Message
}

func (s *recoveredStopMessages) GetMessage(context.Context, string, string) (*types.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.m
	return &m, nil
}

func (s *recoveredStopMessages) UpdateMessage(_ context.Context, m *types.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = *m
	return nil
}

func (*recoveredStopMessages) IndexMessageToKB(context.Context, string, string, string, string) {}

type recoveredStopSessions struct {
	interfaces.SessionService
	run func(context.Context, *types.QARequest, *event.EventBus) error
}

func (*recoveredStopSessions) GetSession(_ context.Context, id string) (*types.Session, error) {
	return &types.Session{ID: id, TenantID: 7, UserID: "owner"}, nil
}

func (s *recoveredStopSessions) GetOwnedSession(ctx context.Context, id string) (*types.Session, error) {
	return s.GetSession(ctx, id)
}

func (s *recoveredStopSessions) AgentQA(ctx context.Context, req *types.QARequest, bus *event.EventBus) error {
	return s.run(ctx, req, bus)
}

type recoveredStopIdentity struct {
	interfaces.UserService
	interfaces.TenantMemberService
	interfaces.TenantService
	interfaces.CustomAgentService
}

func (*recoveredStopIdentity) GetUserByID(context.Context, string) (*types.User, error) {
	return &types.User{ID: "owner", IsActive: true}, nil
}

func (*recoveredStopIdentity) GetMembership(context.Context, string, uint64) (*types.TenantMember, error) {
	return &types.TenantMember{Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}, nil
}

func (*recoveredStopIdentity) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 7}, nil
}

func (*recoveredStopIdentity) GetAgentByID(context.Context, string) (*types.CustomAgent, error) {
	return &types.CustomAgent{ID: "research", TenantID: 7,
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning}}, nil
}

type recoveredStopModel struct {
	started   chan struct{}
	cancelled chan error
}

func (*recoveredStopModel) GetModelName() string { return "synthetic-stop-test" }
func (*recoveredStopModel) GetModelID() string   { return "synthetic-stop-test" }

func (*recoveredStopModel) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return nil, errors.New("unexpected non-streaming model call")
}

func (m *recoveredStopModel) ChatStream(ctx context.Context, _ []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	if opts.ToolChoice != "none" || opts.RuntimeNotice == "" || len(opts.Tools) != 0 {
		return nil, errors.New("expected tool-free budget summary")
	}
	chunks := make(chan types.StreamResponse, 1)
	chunks <- types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Content: "verified partial evidence"}
	close(m.started)
	go func() {
		<-ctx.Done()
		m.cancelled <- ctx.Err()
		close(chunks)
	}()
	return chunks, nil
}

// Exercise the production recovery callback, engine Resume, HTTP stop endpoint
// and shared stream watcher together. Closing delegated work for the budget
// makes StopOwned a no-op; the stop marker must still cancel the final model
// stream, even after replaying an older suspension's terminal event.
func TestRecoveredParentBudgetSummaryHonorsHTTPStop(t *testing.T) {
	for _, oldTerminal := range []types.ResponseType{types.ResponseTypeComplete, types.ResponseTypeError} {
		t.Run(string(oldTerminal), func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "stop.db")+"?_busy_timeout=5000&_journal_mode=WAL"),
				&gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = raw.Close() })
			require.NoError(t, db.AutoMigrate(&collaboration.Record{}))
			messages := &recoveredStopMessages{m: types.Message{ID: "answer", SessionID: "session", RequestID: "turn"}}
			identity := &recoveredStopIdentity{}
			runtimeService := service.NewAgentCollaboration(db, identity, messages, identity, identity, identity)
			streams := stream.NewMemoryStreamManager()
			runtimeService.BindRecovery(nil, nil, streams)
			sessions := &recoveredStopSessions{}
			h := &Handler{messageService: messages, sessionService: sessions, streamManager: streams}
			h.SetCollaborationStopper(runtimeService.Stop)
			runtimeService.ParentEvents = h.RecoveredParentEvents
			runtimeService.Bind(sessions)
			ctx := types.WithCaller(t.Context(), types.Caller{TenantID: 7, UserID: "owner", Role: types.TenantRoleContributor})
			scope := collaboration.Scope{TenantID: 7, UserID: "owner", SessionID: "session", ID: "answer"}
			binding, err := runtimeService.Runtime.AcquireParent(ctx, scope)
			require.NoError(t, err)
			_, err = runtimeService.Runtime.Spawn(binding.Context(ctx), scope, "spawn", "research_child", "research", "{}", "task")
			require.NoError(t, err)
			require.NoError(t, streams.AppendEvent(ctx, scope.SessionID, scope.ID, interfaces.StreamEvent{
				ID: "previous-suspension", Type: oldTerminal, Done: true,
			}))
			model := &recoveredStopModel{started: make(chan struct{}), cancelled: make(chan error, 1)}
			sessions.run = func(ctx context.Context, req *types.QARequest, bus *event.EventBus) error {
				engine := agent.NewAgentEngine(&types.AgentConfig{MaxIterations: 1}, model,
					agenttools.NewToolRegistry(), bus, nil, nil, scope.SessionID, "")
				if engine == nil {
					return errors.New("could not construct test engine")
				}
				engine.SetCollaboration(collaboration.FromContext(ctx))
				_, err := engine.Resume(ctx, scope.SessionID, scope.ID, nil, req.Resume.Snapshot, nil)
				return err
			}
			request, err := json.Marshal(types.ResumableAgentRequest{AgentID: "research"})
			require.NoError(t, err)
			snapshot, err := json.Marshal(agent.RunSnapshot{Version: agent.RunSnapshotVersion,
				Query: "summarize synthetic evidence", CurrentRound: 1,
				CollaborationWait: true, BetweenRounds: true, Request: request})
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- runtimeService.Runtime.WakeParent(ctx, binding, snapshot) }()
			select {
			case <-model.started:
			case <-time.After(3 * time.Second):
				t.Fatal("recovery did not start its final summary")
			}
			group, err := runtimeService.Runtime.Store.Read(ctx, scope)
			require.NoError(t, err)
			require.Equal(t, "cancelled", group.Status)
			// Ensure the old terminal marker was replayed before the new stop.
			// A watcher that treats it as final will exit here and miss the stop.
			time.Sleep(400 * time.Millisecond)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(types.TenantIDContextKey.String(), uint64(7))
				c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(),
					types.Caller{TenantID: 7, UserID: "owner", Role: types.TenantRoleContributor}))
			})
			router.POST("/sessions/:session_id/stop", h.StopSession)
			requestHTTP := httptest.NewRequest(http.MethodPost, "/sessions/session/stop", strings.NewReader(`{"message_id":"answer"}`))
			requestHTTP.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, requestHTTP)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			select {
			case err := <-model.cancelled:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP stop did not cancel the recovered final model stream")
			}
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("recovered execution did not finish after stop")
			}
			message, err := messages.GetMessage(ctx, scope.SessionID, scope.ID)
			require.NoError(t, err)
			require.True(t, message.IsCompleted)
			require.Contains(t, message.Content, "verified partial evidence")
			events, _, err := streams.GetEvents(ctx, scope.SessionID, scope.ID, 0)
			require.NoError(t, err)
			require.NotEmpty(t, events)
			last := events[len(events)-1]
			require.Equal(t, types.ResponseTypeComplete, last.Type)
			require.Equal(t, message.Content, last.Data["final_content"])
		})
	}
}
