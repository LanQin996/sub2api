package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var ErrContributionDuplicate = infraerrors.Conflict("CONTRIBUTION_DUPLICATE", "openai account has already been contributed")
var ErrContributionInvalidStatus = infraerrors.Conflict("CONTRIBUTION_INVALID_STATUS", "invalid contribution status")
var ErrContributionOwnership = infraerrors.NotFound("CONTRIBUTION_NOT_FOUND", "account contribution not found")

type AccountContributionService struct {
	accountRepo AccountRepository
	groupRepo   GroupRepository
	rewardRepo  ContributorRewardRepository
	oauth       *OpenAIOAuthService
}

func NewAccountContributionService(accountRepo AccountRepository, groupRepo GroupRepository, rewardRepo ContributorRewardRepository, oauth *OpenAIOAuthService) *AccountContributionService {
	return &AccountContributionService{accountRepo: accountRepo, groupRepo: groupRepo, rewardRepo: rewardRepo, oauth: oauth}
}

type SubmitOpenAIContributionInput struct {
	SessionID   string
	Code        string
	State       string
	RedirectURI string
	ProxyID     *int64
	Name        string
}

type ApproveContributionInput struct {
	GroupIDs    []int64
	Concurrency *int
	Priority    *int
}

func (s *AccountContributionService) GenerateOpenAIAuthURL(ctx context.Context, _ int64, proxyID *int64, redirectURI string) (*OpenAIAuthURLResult, error) {
	return s.oauth.GenerateAuthURL(ctx, proxyID, redirectURI, PlatformOpenAI)
}

func (s *AccountContributionService) SubmitOpenAI(ctx context.Context, userID int64, input SubmitOpenAIContributionInput) (*Account, error) {
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	tokenInfo, err := s.oauth.ExchangeCode(ctx, &OpenAIExchangeCodeInput{
		SessionID:   input.SessionID,
		Code:        input.Code,
		State:       input.State,
		RedirectURI: input.RedirectURI,
		ProxyID:     input.ProxyID,
	})
	if err != nil {
		return nil, err
	}
	credentials := s.oauth.BuildAccountCredentials(tokenInfo)
	identityKey := contributionIdentityKey(credentials)
	if identityKey == "" {
		return nil, infraerrors.BadRequest("CONTRIBUTION_IDENTITY_MISSING", "cannot determine contributed OpenAI account identity")
	}
	dups, err := s.accountRepo.FindByExtraField(ctx, "contribution_identity_key", identityKey)
	if err != nil {
		return nil, err
	}
	for i := range dups {
		if dups[i].OwnerUserID != nil && dups[i].ContributionStatus != "" {
			return nil, ErrContributionDuplicate
		}
	}
	extra := map[string]any{
		"contribution_identity_key": identityKey,
		"contribution_source":       "user_openai_oauth",
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		for _, candidate := range []string{tokenInfo.Email, tokenInfo.ChatGPTAccountID, tokenInfo.ChatGPTUserID} {
			if strings.TrimSpace(candidate) != "" {
				name = strings.TrimSpace(candidate)
				break
			}
		}
	}
	if name == "" {
		name = "OpenAI OAuth Contribution"
	}
	now := time.Now()
	account := &Account{
		Name:                    name,
		Platform:                PlatformOpenAI,
		Type:                    AccountTypeOAuth,
		Credentials:             credentials,
		Extra:                   extra,
		ProxyID:                 input.ProxyID,
		Concurrency:             1,
		Priority:                100,
		Status:                  StatusDisabled,
		Schedulable:             false,
		OwnerUserID:             &userID,
		ContributionStatus:      ContributionStatusPending,
		ContributionSubmittedAt: &now,
		AutoPauseOnExpired:      true,
	}
	if err := s.accountRepo.Create(ctx, account); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "idx_accounts_contribution_identity") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, ErrContributionDuplicate
		}
		return nil, err
	}
	return account, nil
}

func (s *AccountContributionService) ListMine(ctx context.Context, userID int64, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return s.accountRepo.ListContributionsByOwner(ctx, userID, params)
}

func (s *AccountContributionService) Revoke(ctx context.Context, userID, accountID int64) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.OwnerUserID == nil || *account.OwnerUserID != userID {
		return nil, ErrContributionOwnership
	}
	if account.ContributionStatus == ContributionStatusRevoked {
		return account, nil
	}
	now := time.Now()
	account.ContributionStatus = ContributionStatusRevoked
	account.ContributionRevokedAt = &now
	account.Status = StatusDisabled
	account.Schedulable = false
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	return s.accountRepo.GetByID(ctx, account.ID)
}

func (s *AccountContributionService) ListRewards(ctx context.Context, userID int64, params pagination.PaginationParams) ([]ContributorRewardLog, *pagination.PaginationResult, error) {
	return s.rewardRepo.ListByOwner(ctx, userID, params)
}

func (s *AccountContributionService) ListPending(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return s.accountRepo.ListContributionsByStatus(ctx, ContributionStatusPending, params)
}

func (s *AccountContributionService) Approve(ctx context.Context, accountID int64, input ApproveContributionInput) (*Account, error) {
	if len(input.GroupIDs) == 0 {
		return nil, infraerrors.BadRequest("CONTRIBUTION_GROUP_REQUIRED", "group_ids is required")
	}
	if err := validatePositiveIDs(input.GroupIDs); err != nil {
		return nil, err
	}
	for _, gid := range input.GroupIDs {
		if _, err := s.groupRepo.GetByIDLite(ctx, gid); err != nil {
			return nil, err
		}
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.OwnerUserID == nil || account.ContributionStatus != ContributionStatusPending {
		return nil, ErrContributionInvalidStatus
	}
	now := time.Now()
	account.ContributionStatus = ContributionStatusApproved
	account.ContributionApprovedAt = &now
	account.Status = StatusActive
	account.Schedulable = true
	if input.Concurrency != nil {
		if *input.Concurrency < 0 {
			return nil, infraerrors.BadRequest("CONTRIBUTION_INVALID_CONCURRENCY", "concurrency must be >= 0")
		}
		account.Concurrency = *input.Concurrency
	}
	if input.Priority != nil {
		account.Priority = *input.Priority
	}
	account.GroupIDs = input.GroupIDs
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	if err := s.accountRepo.BindGroups(ctx, account.ID, input.GroupIDs); err != nil {
		return nil, err
	}
	return s.accountRepo.GetByID(ctx, account.ID)
}

func (s *AccountContributionService) Reject(ctx context.Context, accountID int64) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.OwnerUserID == nil || account.ContributionStatus != ContributionStatusPending {
		return nil, ErrContributionInvalidStatus
	}
	account.ContributionStatus = ContributionStatusRejected
	account.Status = StatusDisabled
	account.Schedulable = false
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	return s.accountRepo.GetByID(ctx, account.ID)
}

func contributionIdentityKey(credentials map[string]any) string {
	chatgptUserID := strings.TrimSpace(fmt.Sprint(credentials["chatgpt_user_id"]))
	chatgptAccountID := strings.TrimSpace(fmt.Sprint(credentials["chatgpt_account_id"]))
	if chatgptUserID != "" && chatgptUserID != "<nil>" && chatgptAccountID != "" && chatgptAccountID != "<nil>" {
		return chatgptUserID + ":" + chatgptAccountID
	}
	accessToken := strings.TrimSpace(fmt.Sprint(credentials["access_token"]))
	if accessToken == "" || accessToken == "<nil>" {
		return ""
	}
	sum := sha256.Sum256([]byte(accessToken))
	return "token_sha256:" + hex.EncodeToString(sum[:])
}

func validatePositiveIDs(ids []int64) error {
	for _, id := range ids {
		if id <= 0 {
			return infraerrors.BadRequest("IDS_INVALID", "ids must be positive")
		}
	}
	return nil
}
