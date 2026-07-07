package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type openAIRouteGroupRuntime struct {
	h                   *OpenAIGatewayHandler
	c                   *gin.Context
	reqLog              *zap.Logger
	originalAPIKey      *service.APIKey
	currentAPIKey       *service.APIKey
	currentSubscription *service.UserSubscription
	routeGroupIDs       []int64
	routeGroupIndex     int
	reqModel            string
	body                []byte
	replaceModel        openAIModelBodyReplaceFunc
	channelMapping      service.ChannelMappingResult
	forwardBody         []byte
	requestPlatform     string
}

func newOpenAIRouteGroupRuntime(
	h *OpenAIGatewayHandler,
	c *gin.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subscription *service.UserSubscription,
	reqModel string,
	body []byte,
	replaceModel openAIModelBodyReplaceFunc,
) *openAIRouteGroupRuntime {
	routeGroupIDs := apiKeyEffectiveRouteGroupIDs(apiKey)
	rt := &openAIRouteGroupRuntime{
		h:                   h,
		c:                   c,
		reqLog:              reqLog,
		originalAPIKey:      apiKey,
		currentAPIKey:       apiKey,
		currentSubscription: subscription,
		routeGroupIDs:       routeGroupIDs,
		routeGroupIndex:     routeGroupIndex(routeGroupIDs, apiKey.GroupID),
		reqModel:            reqModel,
		body:                body,
		replaceModel:        replaceModel,
	}
	rt.refreshDerived()
	return rt
}

func (rt *openAIRouteGroupRuntime) refreshDerived() {
	if rt == nil || rt.h == nil || rt.h.gatewayService == nil || rt.currentAPIKey == nil {
		return
	}
	rt.requestPlatform = openAICompatibleRequestPlatform(rt.currentAPIKey)
	rt.channelMapping, _ = rt.h.gatewayService.ResolveChannelMappingAndRestrict(rt.c.Request.Context(), rt.currentAPIKey.GroupID, rt.reqModel)
	rt.forwardBody = openAIModelMappedBody(rt.body, rt.channelMapping.Mapped, rt.channelMapping.MappedModel, rt.replaceModel)
}

func (rt *openAIRouteGroupRuntime) switchToNext(reason string) bool {
	if rt == nil || rt.h == nil || rt.h.apiKeyService == nil || rt.originalAPIKey == nil {
		return false
	}
	for rt.routeGroupIndex+1 < len(rt.routeGroupIDs) {
		rt.routeGroupIndex++
		nextGroupID := rt.routeGroupIDs[rt.routeGroupIndex]
		nextGroup, err := rt.h.apiKeyService.ResolveRouteGroupByID(rt.c.Request.Context(), nextGroupID)
		if err != nil {
			rt.warn("openai.route_group_resolve_failed", reason, zap.Int64("group_id", nextGroupID), zap.Error(err))
			continue
		}
		if nextGroup == nil {
			rt.warn("openai.route_group_resolve_empty", reason, zap.Int64("group_id", nextGroupID))
			continue
		}
		if !nextGroup.IsActive() {
			rt.warn("openai.route_group_unavailable", reason,
				zap.Int64("group_id", nextGroupID),
				zap.String("status", nextGroup.Status),
			)
			continue
		}
		if !openAIRouteGroupPlatformCompatible(rt.originalAPIKey.Group, nextGroup) {
			rt.warn("openai.route_group_platform_mismatch", reason,
				zap.Int64("group_id", nextGroupID),
				zap.String("platform", nextGroup.Platform),
			)
			continue
		}

		nextAPIKey := cloneAPIKeyWithGroup(rt.originalAPIKey, nextGroup)
		nextSubscription, ok := rt.resolveRouteGroupSubscription(nextAPIKey, nextGroup, reason)
		if !ok {
			continue
		}
		if err := rt.h.billingCacheService.CheckBillingEligibility(
			rt.c.Request.Context(),
			nextAPIKey.User,
			nextAPIKey,
			nextGroup,
			nextSubscription,
			service.QuotaPlatform(rt.c.Request.Context(), nextAPIKey),
		); err != nil {
			rt.warn("openai.route_group_billing_ineligible", reason,
				zap.Int64("group_id", nextGroupID),
				zap.Error(err),
			)
			continue
		}

		rt.currentAPIKey = nextAPIKey
		rt.currentSubscription = nextSubscription
		rt.refreshDerived()
		rt.info("openai.route_group_circuit_break", reason, zap.Int64("next_group_id", nextGroupID))
		return true
	}
	return false
}

func (rt *openAIRouteGroupRuntime) resolveRouteGroupSubscription(apiKey *service.APIKey, group *service.Group, reason string) (*service.UserSubscription, bool) {
	if group == nil || !group.IsSubscriptionType() {
		return nil, true
	}
	userID := int64(0)
	if apiKey != nil {
		userID = apiKey.UserID
		if userID == 0 && apiKey.User != nil {
			userID = apiKey.User.ID
		}
	}
	if userID <= 0 {
		rt.warn("openai.route_group_subscription_ineligible", reason,
			zap.Int64("group_id", group.ID),
			zap.String("error", "missing user id"),
		)
		return nil, false
	}
	sub, err := rt.h.apiKeyService.GetActiveSubscriptionForGroup(rt.c.Request.Context(), userID, group.ID)
	if err != nil {
		rt.warn("openai.route_group_subscription_ineligible", reason,
			zap.Int64("group_id", group.ID),
			zap.Error(err),
		)
		return nil, false
	}
	return sub, true
}

func openAIRouteGroupPlatformCompatible(original, next *service.Group) bool {
	if next == nil {
		return false
	}
	if next.Platform != service.PlatformOpenAI && next.Platform != service.PlatformGrok {
		return false
	}
	if original == nil || original.Platform == "" {
		return true
	}
	return next.Platform == original.Platform
}

func (rt *openAIRouteGroupRuntime) resetFailoverState(
	switchCount *int,
	failedAccountIDs *map[int64]struct{},
	sameAccountRetryCount *map[int64]int,
	lastFailoverErr **service.UpstreamFailoverError,
) {
	if switchCount != nil {
		*switchCount = 0
	}
	if failedAccountIDs != nil {
		*failedAccountIDs = make(map[int64]struct{})
	}
	if sameAccountRetryCount != nil {
		*sameAccountRetryCount = make(map[int64]int)
	}
	if lastFailoverErr != nil {
		*lastFailoverErr = nil
	}
}

func (rt *openAIRouteGroupRuntime) warn(msg, reason string, fields ...zap.Field) {
	if rt == nil || rt.reqLog == nil {
		return
	}
	fields = append(fields, zap.String("reason", reason))
	rt.reqLog.Warn(msg, fields...)
}

func (rt *openAIRouteGroupRuntime) info(msg, reason string, fields ...zap.Field) {
	if rt == nil || rt.reqLog == nil {
		return
	}
	fields = append(fields, zap.String("reason", reason))
	rt.reqLog.Info(msg, fields...)
}
