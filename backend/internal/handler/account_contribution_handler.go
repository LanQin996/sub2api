package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AccountContributionHandler struct {
	service *service.AccountContributionService
}

func NewAccountContributionHandler(service *service.AccountContributionService) *AccountContributionHandler {
	return &AccountContributionHandler{service: service}
}

type contributionAuthURLRequest struct {
	ProxyID     *int64 `json:"proxy_id"`
	RedirectURI string `json:"redirect_uri"`
}

type submitOpenAIContributionRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
	State       string `json:"state" binding:"required"`
	RedirectURI string `json:"redirect_uri"`
	ProxyID     *int64 `json:"proxy_id"`
	ProxyURL    string `json:"proxy_url"`
	Name        string `json:"name"`
}

type submitOpenAIJSONContributionRequest struct {
	Data     contributionDataPayload   `json:"data"`
	Accounts []contributionDataAccount `json:"accounts"`
	ProxyID  *int64                    `json:"proxy_id"`
	ProxyURL string                    `json:"proxy_url"`
}

type contributionDataPayload struct {
	Type     string                    `json:"type,omitempty"`
	Version  int                       `json:"version,omitempty"`
	Proxies  []map[string]any          `json:"proxies"`
	Accounts []contributionDataAccount `json:"accounts"`
}

type contributionDataAccount struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes"`
	Platform           string         `json:"platform"`
	Type               string         `json:"type"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra"`
	Concurrency        int            `json:"concurrency"`
	Priority           int            `json:"priority"`
	ExpiresAt          *int64         `json:"expires_at"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired"`
}

type approveContributionRequest struct {
	GroupIDs    []int64 `json:"group_ids" binding:"required"`
	Concurrency *int    `json:"concurrency"`
	Priority    *int    `json:"priority"`
}

func contributionAccountsFromRequest(req submitOpenAIJSONContributionRequest) []service.OpenAIJSONContributionAccount {
	accounts := req.Accounts
	if len(accounts) == 0 {
		accounts = req.Data.Accounts
	}
	inputAccounts := make([]service.OpenAIJSONContributionAccount, 0, len(accounts))
	for i := range accounts {
		inputAccounts = append(inputAccounts, service.OpenAIJSONContributionAccount{
			Name:               accounts[i].Name,
			Notes:              accounts[i].Notes,
			Platform:           accounts[i].Platform,
			Type:               accounts[i].Type,
			Credentials:        accounts[i].Credentials,
			Extra:              accounts[i].Extra,
			Concurrency:        accounts[i].Concurrency,
			Priority:           accounts[i].Priority,
			ExpiresAt:          accounts[i].ExpiresAt,
			AutoPauseOnExpired: accounts[i].AutoPauseOnExpired,
		})
	}
	return inputAccounts
}

func (h *AccountContributionHandler) GenerateOpenAIAuthURL(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req contributionAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.service.GenerateOpenAIAuthURL(c.Request.Context(), subject.UserID, req.ProxyID, req.RedirectURI)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *AccountContributionHandler) SubmitOpenAI(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req submitOpenAIContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	account, err := h.service.SubmitOpenAI(c.Request.Context(), subject.UserID, service.SubmitOpenAIContributionInput{
		SessionID: req.SessionID, Code: req.Code, State: req.State, RedirectURI: req.RedirectURI, ProxyID: req.ProxyID, ProxyURL: req.ProxyURL, Name: req.Name,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *AccountContributionHandler) SubmitOpenAIJSON(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req submitOpenAIJSONContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.Data.Proxies) > 0 {
		response.BadRequest(c, "JSON contribution import does not support proxy import; choose an existing proxy in the request instead")
		return
	}
	result, err := h.service.SubmitOpenAIJSON(c.Request.Context(), subject.UserID, service.SubmitOpenAIJSONContributionInput{
		Accounts: contributionAccountsFromRequest(req),
		ProxyID:  req.ProxyID,
		ProxyURL: req.ProxyURL,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountContributionHandler) PreviewOpenAIJSON(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req submitOpenAIJSONContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.Data.Proxies) > 0 {
		response.BadRequest(c, "JSON contribution import does not support proxy import; choose an existing proxy in the request instead")
		return
	}
	preview, err := h.service.PreviewOpenAIJSON(c.Request.Context(), subject.UserID, service.SubmitOpenAIJSONContributionInput{
		Accounts: contributionAccountsFromRequest(req),
		ProxyID:  req.ProxyID,
		ProxyURL: req.ProxyURL,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *AccountContributionHandler) ListMine(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.service.ListMine(c.Request.Context(), subject.UserID, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.Account, 0, len(items))
	for i := range items {
		out = append(out, *dto.AccountFromService(&items[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

func (h *AccountContributionHandler) Revoke(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.service.Revoke(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *AccountContributionHandler) ListRewards(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.service.ListRewards(c.Request.Context(), subject.UserID, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, page, pageSize)
}

func (h *AccountContributionHandler) GetRewardSummary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	summary, err := h.service.GetRewardSummary(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summary)
}

func (h *AccountContributionHandler) ListPending(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	status := c.DefaultQuery("status", service.ContributionStatusPending)
	items, result, err := h.service.ListByStatus(c.Request.Context(), status, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.Account, 0, len(items))
	for i := range items {
		out = append(out, *dto.AccountFromService(&items[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

func (h *AccountContributionHandler) Approve(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req approveContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	account, err := h.service.Approve(c.Request.Context(), id, service.ApproveContributionInput{GroupIDs: req.GroupIDs, Concurrency: req.Concurrency, Priority: req.Priority})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *AccountContributionHandler) Reject(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.service.Reject(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}
