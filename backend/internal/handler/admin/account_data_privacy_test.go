package admin

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type importPrivacyAdminService struct {
	*stubAdminService

	mu            sync.Mutex
	createdInputs []*service.CreateAccountInput
	nextAccountID int64
	started       chan string
	contextValid  chan bool
	release       chan struct{}
	completed     chan struct{}
}

func newImportPrivacyAdminService() *importPrivacyAdminService {
	return &importPrivacyAdminService{
		stubAdminService: newStubAdminService(),
		started:          make(chan string, 4),
		contextValid:     make(chan bool, 4),
		release:          make(chan struct{}, 4),
		completed:        make(chan struct{}, 4),
	}
}

func (s *importPrivacyAdminService) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	s.mu.Lock()
	s.nextAccountID++
	id := s.nextAccountID
	s.createdInputs = append(s.createdInputs, input)
	s.mu.Unlock()

	return &service.Account{
		ID:          id,
		Name:        input.Name,
		Platform:    input.Platform,
		Type:        input.Type,
		Credentials: input.Credentials,
		Extra:       input.Extra,
		Status:      service.StatusActive,
	}, nil
}

func (s *importPrivacyAdminService) recordPrivacyCall(ctx context.Context, operation string, account *service.Account) string {
	deadline, hasDeadline := ctx.Deadline()
	remaining := time.Until(deadline)
	s.contextValid <- hasDeadline && ctx.Err() == nil && remaining > 0 && remaining <= dataImportPrivacyTimeout
	s.started <- fmt.Sprintf("%s:%s", operation, account.Name)
	<-s.release
	s.completed <- struct{}{}
	return ""
}

func (s *importPrivacyAdminService) EnsureOpenAIPrivacy(ctx context.Context, account *service.Account) string {
	return s.recordPrivacyCall(ctx, "ensure-openai", account)
}

func (s *importPrivacyAdminService) EnsureAntigravityPrivacy(ctx context.Context, account *service.Account) string {
	return s.recordPrivacyCall(ctx, "ensure-antigravity", account)
}

func (s *importPrivacyAdminService) ForceOpenAIPrivacy(ctx context.Context, account *service.Account) string {
	return s.recordPrivacyCall(ctx, "force-openai", account)
}

func (s *importPrivacyAdminService) ForceAntigravityPrivacy(ctx context.Context, account *service.Account) string {
	return s.recordPrivacyCall(ctx, "force-antigravity", account)
}

func TestImportDataDefersAndSerializesPrivacySetup(t *testing.T) {
	adminSvc := newImportPrivacyAdminService()
	handler := &AccountHandler{adminService: adminSvc}

	result, err := handler.importData(context.Background(), DataImportRequest{
		Data: DataPayload{Accounts: []DataAccount{
			{
				Name:        "openai-oauth",
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "openai-token"},
			},
			{
				Name:        "antigravity-oauth",
				Platform:    service.PlatformAntigravity,
				Type:        service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "antigravity-token"},
			},
			{
				Name:        "openai-api-key",
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "openai-key"},
			},
		}},
	})

	require.NoError(t, err)
	require.Equal(t, 3, result.AccountCreated)
	require.Zero(t, result.AccountFailed)
	require.Len(t, adminSvc.createdInputs, 3)
	for _, input := range adminSvc.createdInputs {
		require.True(t, input.DeferPrivacySetup)
	}

	require.Equal(t, "ensure-openai:openai-oauth", receivePrivacyCall(t, adminSvc.started))
	require.True(t, receivePrivacyContextValidity(t, adminSvc.contextValid))
	assertNoPrivacyCall(t, adminSvc.started)
	adminSvc.release <- struct{}{}
	receivePrivacyCompletion(t, adminSvc.completed)

	require.Equal(t, "force-antigravity:antigravity-oauth", receivePrivacyCall(t, adminSvc.started))
	require.True(t, receivePrivacyContextValidity(t, adminSvc.contextValid))
	assertNoPrivacyCall(t, adminSvc.started)
	adminSvc.release <- struct{}{}
	receivePrivacyCompletion(t, adminSvc.completed)
	assertNoPrivacyCall(t, adminSvc.started)
}

func receivePrivacyContextValidity(t *testing.T, contexts <-chan bool) bool {
	t.Helper()
	select {
	case valid := <-contexts:
		return valid
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for privacy context")
		return false
	}
}

func receivePrivacyCall(t *testing.T, calls <-chan string) string {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for privacy setup")
		return ""
	}
}

func receivePrivacyCompletion(t *testing.T, completed <-chan struct{}) {
	t.Helper()
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for privacy setup completion")
	}
}

func assertNoPrivacyCall(t *testing.T, calls <-chan string) {
	t.Helper()
	select {
	case call := <-calls:
		t.Fatalf("privacy setup was not serialized or ran more than once: %s", call)
	case <-time.After(25 * time.Millisecond):
	}
}
