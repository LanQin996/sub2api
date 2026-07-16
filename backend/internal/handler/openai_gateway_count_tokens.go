package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CountTokens handles Anthropic-compatible POST /v1/messages/count_tokens for OpenAI groups.
// It validates billing and routes to an OpenAI token-count bridge without taking concurrency slots
// or recording usage.
func (h *OpenAIGatewayHandler) CountTokens(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.count_tokens",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	if apiKey.Group != nil && !apiKey.Group.AllowMessagesDispatch {
		h.anthropicErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group does not allow /v1/messages dispatch")
		return
	}

	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.anthropicErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)
	if err != nil {
		logRequestBodyParseFailure(reqLog, body, err)
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	if parsedReq.Model == "" {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	reqModel := parsedReq.Model
	routingModel := service.NormalizeOpenAICompatRequestedModel(reqModel)
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", parsedReq.Stream))

	setOpsRequestContext(c, reqModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(false, false)))

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	routeRuntime := newOpenAIRouteGroupRuntime(h, c, reqLog, apiKey, subscription, reqModel, body, h.gatewayService.ReplaceModelInBody)
	if err := h.billingCacheService.CheckBillingEligibility(
		c.Request.Context(),
		routeRuntime.currentAPIKey.User,
		routeRuntime.currentAPIKey,
		routeRuntime.currentAPIKey.Group,
		routeRuntime.currentSubscription,
		service.QuotaPlatform(c.Request.Context(), routeRuntime.currentAPIKey),
	); err != nil {
		reqLog.Info("openai_count_tokens.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.anthropicErrorResponse(c, status, code, message)
		return
	}

	requestStart := time.Now()
	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	var selection *service.AccountSelectionResult
	defaultMappedModel := ""
	for {
		currentRoutingModel := routingModel
		defaultMappedModel = resolveOpenAIMessagesDispatchMappedModel(routeRuntime.currentAPIKey, reqModel)
		if defaultMappedModel != "" {
			currentRoutingModel = defaultMappedModel
		}
		var err error
		selection, _, err = h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			routeRuntime.currentAPIKey.GroupID,
			"",
			sessionHash,
			currentRoutingModel,
			nil,
			service.OpenAIUpstreamTransportAny,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
			false,
			false,
			routeRuntime.requestPlatform,
		)
		service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
		if err != nil {
			reqLog.Warn("openai_count_tokens.account_select_failed", zap.Error(err))
			if routeRuntime.switchToNext("selection_no_available") {
				continue
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, routeRuntime.currentAPIKey, currentRoutingModel, reqModel, routeRuntime.requestPlatform)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
			}
			h.anthropicErrorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		if selection == nil || selection.Account == nil {
			if routeRuntime.switchToNext("selection_empty") {
				continue
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, routeRuntime.currentAPIKey, currentRoutingModel, reqModel, routeRuntime.requestPlatform)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.anthropicErrorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		break
	}

	account := selection.Account
	setOpsSelectedAccount(c, account.ID, account.Platform)
	if selection.Acquired && selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}
	forwardBody := routeRuntime.forwardBody

	if err := h.gatewayService.ForwardCountTokensAsAnthropic(c.Request.Context(), c, account, forwardBody, defaultMappedModel); err != nil {
		reqLog.Error("openai_count_tokens.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		return
	}
	routeRuntime.reportSuccess()
}
