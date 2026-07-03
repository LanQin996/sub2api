package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
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

type SubmitOpenAIJSONContributionInput struct {
	Accounts []OpenAIJSONContributionAccount
	ProxyID  *int64
}

type OpenAIJSONContributionAccount struct {
	Name               string
	Notes              *string
	Platform           string
	Type               string
	Credentials        map[string]any
	Extra              map[string]any
	Concurrency        int
	Priority           int
	ExpiresAt          *int64
	AutoPauseOnExpired *bool
}

type ContributionImportResult struct {
	Total   int                       `json:"total"`
	Created int                       `json:"created"`
	Failed  int                       `json:"failed"`
	Items   []ContributionImportItem  `json:"items,omitempty"`
	Errors  []ContributionImportError `json:"errors,omitempty"`
}

type ContributionImportItem struct {
	Index     int    `json:"index"`
	Name      string `json:"name,omitempty"`
	AccountID int64  `json:"account_id,omitempty"`
	Action    string `json:"action"`
	Message   string `json:"message,omitempty"`
}

type ContributionImportError struct {
	Index   int    `json:"index"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
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
	return s.createPendingOpenAIContribution(ctx, userID, pendingOpenAIContributionInput{
		Name:                    name,
		Platform:                PlatformOpenAI,
		Type:                    AccountTypeOAuth,
		Credentials:             credentials,
		Extra:                   extra,
		ProxyID:                 input.ProxyID,
		Concurrency:             1,
		Priority:                100,
		AutoPauseOnExpired:      true,
		ContributionIdentityKey: identityKey,
	})
}

func (s *AccountContributionService) SubmitOpenAIJSON(ctx context.Context, userID int64, input SubmitOpenAIJSONContributionInput) (ContributionImportResult, error) {
	result := ContributionImportResult{Total: len(input.Accounts)}
	if userID <= 0 {
		return result, ErrUserNotFound
	}
	if len(input.Accounts) == 0 {
		return result, infraerrors.BadRequest("CONTRIBUTION_IMPORT_EMPTY", "accounts is required")
	}
	if len(input.Accounts) > 100 {
		return result, infraerrors.BadRequest("CONTRIBUTION_IMPORT_TOO_MANY", "too many accounts in one import")
	}

	for i := range input.Accounts {
		item := input.Accounts[i]
		name := strings.TrimSpace(item.Name)
		if err := validateOpenAIJSONContributionAccount(item); err != nil {
			result.Failed++
			result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
			result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
			continue
		}

		enrichOpenAIContributionCredentialsFromIDToken(item.Credentials)
		identityKey := contributionIdentityKey(item.Credentials)
		if identityKey == "" {
			err := infraerrors.BadRequest("CONTRIBUTION_IDENTITY_MISSING", "cannot determine contributed OpenAI account identity")
			result.Failed++
			result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
			result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
			continue
		}

		extra := sanitizedContributionExtra(item.Extra)
		extra["contribution_identity_key"] = identityKey
		extra["contribution_source"] = "user_openai_json"

		if name == "" {
			for _, candidate := range []string{
				strings.TrimSpace(fmt.Sprint(item.Credentials["email"])),
				strings.TrimSpace(fmt.Sprint(item.Credentials["chatgpt_account_id"])),
				strings.TrimSpace(fmt.Sprint(item.Credentials["chatgpt_user_id"])),
			} {
				if candidate != "" && candidate != "<nil>" {
					name = candidate
					break
				}
			}
		}
		if name == "" {
			name = "OpenAI JSON Contribution"
		}
		concurrency := item.Concurrency
		if concurrency <= 0 {
			concurrency = 1
		}
		priority := item.Priority
		if priority <= 0 {
			priority = 100
		}
		autoPause := true
		if item.AutoPauseOnExpired != nil {
			autoPause = *item.AutoPauseOnExpired
		}
		var expiresAt *time.Time
		if item.ExpiresAt != nil && *item.ExpiresAt > 0 {
			v := time.Unix(*item.ExpiresAt, 0).UTC()
			expiresAt = &v
		}

		account, err := s.createPendingOpenAIContribution(ctx, userID, pendingOpenAIContributionInput{
			Name:                    name,
			Notes:                   item.Notes,
			Platform:                PlatformOpenAI,
			Type:                    AccountTypeOAuth,
			Credentials:             item.Credentials,
			Extra:                   extra,
			ProxyID:                 input.ProxyID,
			Concurrency:             concurrency,
			Priority:                priority,
			ExpiresAt:               expiresAt,
			AutoPauseOnExpired:      autoPause,
			ContributionIdentityKey: identityKey,
		})
		if err != nil {
			result.Failed++
			result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
			result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
			continue
		}

		result.Created++
		result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, AccountID: account.ID, Action: "created"})
	}

	return result, nil
}

type pendingOpenAIContributionInput struct {
	Name                    string
	Notes                   *string
	Platform                string
	Type                    string
	Credentials             map[string]any
	Extra                   map[string]any
	ProxyID                 *int64
	Concurrency             int
	Priority                int
	ExpiresAt               *time.Time
	AutoPauseOnExpired      bool
	ContributionIdentityKey string
}

func (s *AccountContributionService) createPendingOpenAIContribution(ctx context.Context, userID int64, input pendingOpenAIContributionInput) (*Account, error) {
	identityKey := strings.TrimSpace(input.ContributionIdentityKey)
	if identityKey == "" {
		identityKey = contributionIdentityKey(input.Credentials)
	}
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
	extra := input.Extra
	if extra == nil {
		extra = map[string]any{}
	}
	extra["contribution_identity_key"] = identityKey
	now := time.Now()
	account := &Account{
		Name:                    input.Name,
		Notes:                   input.Notes,
		Platform:                input.Platform,
		Type:                    input.Type,
		Credentials:             input.Credentials,
		Extra:                   extra,
		ProxyID:                 input.ProxyID,
		Concurrency:             input.Concurrency,
		Priority:                input.Priority,
		Status:                  StatusDisabled,
		Schedulable:             false,
		OwnerUserID:             &userID,
		ContributionStatus:      ContributionStatusPending,
		ContributionSubmittedAt: &now,
		ExpiresAt:               input.ExpiresAt,
		AutoPauseOnExpired:      input.AutoPauseOnExpired,
	}
	if err := s.accountRepo.Create(ctx, account); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "idx_accounts_contribution_identity") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, ErrContributionDuplicate
		}
		return nil, err
	}
	return account, nil
}

func validateOpenAIJSONContributionAccount(item OpenAIJSONContributionAccount) error {
	if strings.TrimSpace(item.Platform) != "" && strings.ToLower(strings.TrimSpace(item.Platform)) != PlatformOpenAI {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_PLATFORM_UNSUPPORTED", "only openai account JSON is supported")
	}
	if strings.TrimSpace(item.Type) != "" && strings.ToLower(strings.TrimSpace(item.Type)) != AccountTypeOAuth {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_TYPE_UNSUPPORTED", "only openai oauth account JSON is supported")
	}
	if len(item.Credentials) == 0 {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_CREDENTIALS_REQUIRED", "account credentials is required")
	}
	if strings.TrimSpace(fmt.Sprint(item.Credentials["access_token"])) == "" || strings.TrimSpace(fmt.Sprint(item.Credentials["access_token"])) == "<nil>" {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_ACCESS_TOKEN_REQUIRED", "openai oauth credentials require access_token")
	}
	if item.Concurrency < 0 {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_INVALID_CONCURRENCY", "concurrency must be >= 0")
	}
	if item.Priority < 0 {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_INVALID_PRIORITY", "priority must be >= 0")
	}
	return nil
}

func sanitizedContributionExtra(input map[string]any) map[string]any {
	out := make(map[string]any, len(input)+2)
	for key, value := range input {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			continue
		}
		if strings.HasPrefix(normalized, "contribution_") {
			continue
		}
		out[key] = value
	}
	return out
}

func enrichOpenAIContributionCredentialsFromIDToken(credentials map[string]any) {
	if credentials == nil {
		return
	}
	idToken, _ := credentials["id_token"].(string)
	if strings.TrimSpace(idToken) == "" {
		return
	}
	claims, err := openai.DecodeIDToken(idToken)
	if err != nil {
		return
	}
	userInfo := claims.GetUserInfo()
	if userInfo == nil {
		return
	}
	setIfMissing := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if existing, _ := credentials[key].(string); strings.TrimSpace(existing) == "" {
			credentials[key] = value
		}
	}
	setIfMissing("email", userInfo.Email)
	setIfMissing("plan_type", userInfo.PlanType)
	setIfMissing("chatgpt_account_id", userInfo.ChatGPTAccountID)
	setIfMissing("chatgpt_user_id", userInfo.ChatGPTUserID)
	setIfMissing("organization_id", userInfo.OrganizationID)
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
