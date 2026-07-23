package handler

import (
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	openAIRouteGroupBaseCooldown = 30 * time.Second
	openAIRouteGroupMaxCooldown  = 2 * time.Minute
	openAIRouteGroupProbeLease   = 30 * time.Second
)

type openAIRouteGroupCircuitKey struct {
	groupID  int64
	platform string
	model    string
}

type openAIRouteGroupCircuitState struct {
	failureCount     int
	unavailableUntil time.Time
	probeUntil       time.Time
	lastReason       string
}

type openAIRouteGroupCircuitBreaker struct {
	mu           sync.Mutex
	entries      map[openAIRouteGroupCircuitKey]*openAIRouteGroupCircuitState
	now          func() time.Time
	baseCooldown time.Duration
	maxCooldown  time.Duration
	probeLease   time.Duration
}

func newOpenAIRouteGroupCircuitBreaker() *openAIRouteGroupCircuitBreaker {
	return &openAIRouteGroupCircuitBreaker{
		entries:      make(map[openAIRouteGroupCircuitKey]*openAIRouteGroupCircuitState),
		now:          time.Now,
		baseCooldown: openAIRouteGroupBaseCooldown,
		maxCooldown:  openAIRouteGroupMaxCooldown,
		probeLease:   openAIRouteGroupProbeLease,
	}
}

func (h *OpenAIGatewayHandler) openAIRouteGroupCircuitBreaker() *openAIRouteGroupCircuitBreaker {
	if h == nil {
		return nil
	}
	if h.routeGroupCircuitBreaker == nil {
		h.routeGroupCircuitBreaker = newOpenAIRouteGroupCircuitBreaker()
	}
	return h.routeGroupCircuitBreaker
}

func (b *openAIRouteGroupCircuitBreaker) allow(key openAIRouteGroupCircuitKey) (bool, bool, time.Time) {
	if b == nil || key.groupID <= 0 {
		return true, false, time.Time{}
	}
	now := b.nowTime()
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.entries[key]
	if !ok {
		return true, false, time.Time{}
	}
	if !state.unavailableUntil.IsZero() && now.Before(state.unavailableUntil) {
		return false, false, state.unavailableUntil
	}
	if !state.probeUntil.IsZero() && now.Before(state.probeUntil) {
		return false, false, state.probeUntil
	}
	state.probeUntil = now.Add(b.probeLease)
	return true, true, time.Time{}
}

func (b *openAIRouteGroupCircuitBreaker) reportFailure(key openAIRouteGroupCircuitKey, reason string) time.Time {
	if b == nil || key.groupID <= 0 {
		return time.Time{}
	}
	now := b.nowTime()
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.entries[key]
	if !ok {
		state = &openAIRouteGroupCircuitState{}
		b.entries[key] = state
	}
	state.failureCount++
	state.lastReason = reason
	state.probeUntil = time.Time{}
	state.unavailableUntil = now.Add(b.cooldownForFailures(state.failureCount))
	return state.unavailableUntil
}

func (b *openAIRouteGroupCircuitBreaker) reportSuccess(key openAIRouteGroupCircuitKey) {
	if b == nil || key.groupID <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.entries, key)
}

func (b *openAIRouteGroupCircuitBreaker) cancelProbe(key openAIRouteGroupCircuitKey) {
	if b == nil || key.groupID <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if state, ok := b.entries[key]; ok {
		state.probeUntil = time.Time{}
	}
}

func (b *openAIRouteGroupCircuitBreaker) nowTime() time.Time {
	if b == nil || b.now == nil {
		return time.Now()
	}
	return b.now()
}

func (b *openAIRouteGroupCircuitBreaker) cooldownForFailures(failures int) time.Duration {
	if b == nil {
		return openAIRouteGroupBaseCooldown
	}
	base := b.baseCooldown
	if base <= 0 {
		base = openAIRouteGroupBaseCooldown
	}
	maxCooldown := b.maxCooldown
	if maxCooldown <= 0 {
		maxCooldown = openAIRouteGroupMaxCooldown
	}
	if failures < 1 {
		failures = 1
	}
	cooldown := base
	for i := 1; i < failures && cooldown < maxCooldown; i++ {
		cooldown *= 2
	}
	if cooldown > maxCooldown {
		return maxCooldown
	}
	return cooldown
}

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
	rt.skipUnavailableInitialRouteGroup()
	return rt
}

func (rt *openAIRouteGroupRuntime) refreshDerived() {
	if rt == nil || rt.h == nil || rt.h.gatewayService == nil || rt.currentAPIKey == nil {
		return
	}
	rt.requestPlatform = openAICompatibleRequestPlatform(rt.c.Request.Context(), rt.currentAPIKey)
	rt.channelMapping, _ = rt.h.gatewayService.ResolveChannelMappingAndRestrict(rt.c.Request.Context(), rt.currentAPIKey.GroupID, rt.reqModel)
	rt.forwardBody = openAIModelMappedBody(rt.body, rt.channelMapping.Mapped, rt.channelMapping.MappedModel, rt.replaceModel)
}

func (rt *openAIRouteGroupRuntime) skipUnavailableInitialRouteGroup() {
	if rt == nil || len(rt.routeGroupIDs) <= 1 || rt.routeGroupIndex < 0 || rt.routeGroupIndex >= len(rt.routeGroupIDs) {
		return
	}
	currentGroupID := rt.routeGroupIDs[rt.routeGroupIndex]
	if rt.allowRouteGroup(currentGroupID, "initial") {
		return
	}
	originalIndex := rt.routeGroupIndex
	for nextIndex := rt.routeGroupIndex + 1; nextIndex < len(rt.routeGroupIDs); nextIndex++ {
		if rt.activateRouteGroupIndex(nextIndex, "initial_circuit_skip", false, true) {
			rt.info("openai.route_group_initial_circuit_skip", "initial",
				zap.Int64("skipped_group_id", currentGroupID),
				zap.Int64("next_group_id", rt.routeGroupIDs[nextIndex]),
			)
			return
		}
	}
	rt.routeGroupIndex = originalIndex
}

func (rt *openAIRouteGroupRuntime) switchToNext(reason string) bool {
	if rt == nil || rt.h == nil || rt.h.apiKeyService == nil || rt.originalAPIKey == nil {
		return false
	}
	rt.markCurrentRouteGroupUnavailable(reason)
	for nextIndex := rt.routeGroupIndex + 1; nextIndex < len(rt.routeGroupIDs); nextIndex++ {
		if rt.activateRouteGroupIndex(nextIndex, reason, true, true) {
			rt.info("openai.route_group_circuit_break", reason, zap.Int64("next_group_id", rt.routeGroupIDs[nextIndex]))
			return true
		}
	}
	return false
}

func (rt *openAIRouteGroupRuntime) activateRouteGroupIndex(index int, reason string, checkBilling bool, respectCircuit bool) bool {
	if rt == nil || rt.h == nil || rt.h.apiKeyService == nil || rt.originalAPIKey == nil {
		return false
	}
	if index < 0 || index >= len(rt.routeGroupIDs) {
		return false
	}
	nextGroupID := rt.routeGroupIDs[index]
	if respectCircuit && !rt.allowRouteGroup(nextGroupID, reason) {
		return false
	}
	if currentGroupID, ok := rt.currentRouteGroupID(); ok && currentGroupID == nextGroupID {
		rt.routeGroupIndex = index
		return true
	}

	nextGroup, err := rt.h.apiKeyService.ResolveRouteGroupByID(rt.c.Request.Context(), nextGroupID)
	if err != nil {
		rt.cancelRouteGroupProbe(nextGroupID)
		rt.warn("openai.route_group_resolve_failed", reason, zap.Int64("group_id", nextGroupID), zap.Error(err))
		return false
	}
	if nextGroup == nil {
		rt.cancelRouteGroupProbe(nextGroupID)
		rt.warn("openai.route_group_resolve_empty", reason, zap.Int64("group_id", nextGroupID))
		return false
	}
	if !nextGroup.IsActive() {
		rt.cancelRouteGroupProbe(nextGroupID)
		rt.warn("openai.route_group_unavailable", reason,
			zap.Int64("group_id", nextGroupID),
			zap.String("status", nextGroup.Status),
		)
		return false
	}
	if !openAIRouteGroupPlatformCompatible(rt.originalAPIKey.Group, nextGroup) {
		rt.cancelRouteGroupProbe(nextGroupID)
		rt.warn("openai.route_group_platform_mismatch", reason,
			zap.Int64("group_id", nextGroupID),
			zap.String("platform", nextGroup.Platform),
		)
		return false
	}

	nextAPIKey := cloneAPIKeyWithGroup(rt.originalAPIKey, nextGroup)
	nextSubscription, ok := rt.resolveRouteGroupSubscription(nextAPIKey, nextGroup, reason)
	if !ok {
		rt.cancelRouteGroupProbe(nextGroupID)
		return false
	}
	if checkBilling && rt.h.billingCacheService != nil {
		if err := rt.h.billingCacheService.CheckBillingEligibility(
			rt.c.Request.Context(),
			nextAPIKey.User,
			nextAPIKey,
			nextGroup,
			nextSubscription,
			service.QuotaPlatform(rt.c.Request.Context(), nextAPIKey),
		); err != nil {
			rt.cancelRouteGroupProbe(nextGroupID)
			rt.warn("openai.route_group_billing_ineligible", reason,
				zap.Int64("group_id", nextGroupID),
				zap.Error(err),
			)
			return false
		}
	}

	rt.routeGroupIndex = index
	rt.currentAPIKey = nextAPIKey
	rt.currentSubscription = nextSubscription
	rt.refreshDerived()
	return true
}

func (rt *openAIRouteGroupRuntime) allowRouteGroup(groupID int64, reason string) bool {
	if rt == nil || rt.h == nil {
		return true
	}
	allowed, probing, until := rt.h.openAIRouteGroupCircuitBreaker().allow(rt.routeGroupCircuitKey(groupID))
	if !allowed {
		rt.info("openai.route_group_circuit_skip", reason,
			zap.Int64("group_id", groupID),
			zap.Time("until", until),
		)
		return false
	}
	if probing {
		rt.info("openai.route_group_circuit_probe", reason, zap.Int64("group_id", groupID))
	}
	return true
}

func (rt *openAIRouteGroupRuntime) cancelRouteGroupProbe(groupID int64) {
	if rt == nil || rt.h == nil {
		return
	}
	rt.h.openAIRouteGroupCircuitBreaker().cancelProbe(rt.routeGroupCircuitKey(groupID))
}

func (rt *openAIRouteGroupRuntime) markCurrentRouteGroupUnavailable(reason string) {
	if rt == nil || rt.h == nil || len(rt.routeGroupIDs) <= 1 {
		return
	}
	groupID, ok := rt.currentRouteGroupID()
	if !ok {
		return
	}
	until := rt.h.openAIRouteGroupCircuitBreaker().reportFailure(rt.routeGroupCircuitKey(groupID), reason)
	rt.warn("openai.route_group_circuit_open", reason,
		zap.Int64("group_id", groupID),
		zap.Time("until", until),
	)
}

func (rt *openAIRouteGroupRuntime) reportSuccess() {
	if rt == nil || rt.h == nil {
		return
	}
	groupID, ok := rt.currentRouteGroupID()
	if !ok {
		return
	}
	rt.h.openAIRouteGroupCircuitBreaker().reportSuccess(rt.routeGroupCircuitKey(groupID))
}

func (rt *openAIRouteGroupRuntime) currentRouteGroupID() (int64, bool) {
	if rt == nil {
		return 0, false
	}
	if rt.currentAPIKey != nil && rt.currentAPIKey.GroupID != nil {
		return *rt.currentAPIKey.GroupID, true
	}
	if rt.routeGroupIndex >= 0 && rt.routeGroupIndex < len(rt.routeGroupIDs) {
		return rt.routeGroupIDs[rt.routeGroupIndex], true
	}
	return 0, false
}

func (rt *openAIRouteGroupRuntime) routeGroupCircuitKey(groupID int64) openAIRouteGroupCircuitKey {
	key := openAIRouteGroupCircuitKey{groupID: groupID}
	if rt == nil {
		return key
	}
	if rt.originalAPIKey != nil {
		key.platform = openAICompatibleRequestPlatform(rt.c.Request.Context(), rt.originalAPIKey)
	}
	key.model = strings.ToLower(strings.TrimSpace(rt.reqModel))
	return key
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
