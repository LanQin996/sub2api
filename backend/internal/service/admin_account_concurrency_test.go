//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAccountConcurrencyDefaultsInvalidGrokOAuthToOne(t *testing.T) {
	require.Equal(t, 1, normalizeAccountConcurrency(PlatformGrok, AccountTypeOAuth, 0))
	require.Equal(t, 1, normalizeAccountConcurrency(PlatformGrok, AccountTypeOAuth, -5))
}

func TestNormalizeAccountConcurrencyPreservesExplicitValues(t *testing.T) {
	require.Equal(t, 50, normalizeAccountConcurrency(PlatformGrok, AccountTypeOAuth, 50))
	require.Equal(t, 2, normalizeAccountConcurrency(PlatformOpenAI, AccountTypeOAuth, 2))
	require.Equal(t, 2, normalizeAccountConcurrency(PlatformGrok, AccountTypeAPIKey, 2))
}

type createAccountRollbackRepoStub struct {
	AccountRepository
	bindErr               error
	cancel                context.CancelFunc
	createdID             int64
	deletedID             int64
	deleteContextCanceled bool
	deleteContextDeadline bool
}

func (s *createAccountRollbackRepoStub) Create(_ context.Context, account *Account) error {
	account.ID = 42
	s.createdID = account.ID
	return nil
}

func (s *createAccountRollbackRepoStub) BindGroups(_ context.Context, _ int64, _ []int64) error {
	if s.cancel != nil {
		s.cancel()
	}
	return s.bindErr
}

func (s *createAccountRollbackRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedID = id
	s.deleteContextCanceled = ctx.Err() != nil
	_, s.deleteContextDeadline = ctx.Deadline()
	return nil
}

func TestCreateAccountRollsBackWhenBindingGroupsFails(t *testing.T) {
	bindErr := errors.New("bind groups failed")
	ctx, cancel := context.WithCancel(context.Background())
	repo := &createAccountRollbackRepoStub{bindErr: bindErr, cancel: cancel}
	svc := &adminServiceImpl{accountRepo: repo}

	account, err := svc.CreateAccount(ctx, &CreateAccountInput{
		Name:        "account",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "secret"},
		GroupIDs:    []int64{11},
	})

	require.Nil(t, account)
	require.ErrorIs(t, err, bindErr)
	require.Equal(t, int64(42), repo.createdID)
	require.Equal(t, int64(42), repo.deletedID)
	require.False(t, repo.deleteContextCanceled, "rollback must survive request cancellation")
	require.True(t, repo.deleteContextDeadline, "rollback must have a bounded deadline")
}

type createAccountPrivacyRepoStub struct {
	AccountRepository
	updated chan struct{}
}

func (s *createAccountPrivacyRepoStub) Create(_ context.Context, account *Account) error {
	account.ID = 43
	return nil
}

func (s *createAccountPrivacyRepoStub) UpdateExtra(_ context.Context, _ int64, _ map[string]any) error {
	if s.updated != nil {
		s.updated <- struct{}{}
	}
	return nil
}

func TestCreateAccountPrivacySetupDefaultsToAsync(t *testing.T) {
	privacyCalled := make(chan struct{}, 1)
	privacyUpdated := make(chan struct{}, 1)
	svc := &adminServiceImpl{
		accountRepo: &createAccountPrivacyRepoStub{updated: privacyUpdated},
		privacyClientFactory: func(string) (*req.Client, error) {
			privacyCalled <- struct{}{}
			return nil, errors.New("privacy client unavailable")
		},
	}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "openai-account",
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeOAuth,
		Credentials:          map[string]any{"access_token": "token"},
		SkipDefaultGroupBind: true,
	})

	require.NoError(t, err)
	require.NotNil(t, account)
	select {
	case <-privacyCalled:
	case <-time.After(time.Second):
		t.Fatal("default account creation did not start privacy setup")
	}
	select {
	case <-privacyUpdated:
	case <-time.After(time.Second):
		t.Fatal("default account privacy setup did not finish")
	}
}

func TestCreateAccountCanDeferPrivacySetup(t *testing.T) {
	privacyCalled := make(chan struct{}, 1)
	svc := &adminServiceImpl{
		accountRepo: &createAccountPrivacyRepoStub{},
		privacyClientFactory: func(string) (*req.Client, error) {
			privacyCalled <- struct{}{}
			return nil, errors.New("privacy client unavailable")
		},
	}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "openai-account",
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeOAuth,
		Credentials:          map[string]any{"access_token": "token"},
		SkipDefaultGroupBind: true,
		DeferPrivacySetup:    true,
	})

	require.NoError(t, err)
	require.NotNil(t, account)
	select {
	case <-privacyCalled:
		t.Fatal("deferred account creation unexpectedly started privacy setup")
	case <-time.After(50 * time.Millisecond):
	}
}
