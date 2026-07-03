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
	Name        string `json:"name"`
}

type approveContributionRequest struct {
	GroupIDs    []int64 `json:"group_ids" binding:"required"`
	Concurrency *int    `json:"concurrency"`
	Priority    *int    `json:"priority"`
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
		SessionID: req.SessionID, Code: req.Code, State: req.State, RedirectURI: req.RedirectURI, ProxyID: req.ProxyID, Name: req.Name,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
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

func (h *AccountContributionHandler) ListPending(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.service.ListPending(c.Request.Context(), pagination.PaginationParams{Page: page, PageSize: pageSize})
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
