package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type openAIRouteGroupRepoStub struct {
	groups map[int64]*service.Group
}

func (s *openAIRouteGroupRepoStub) Create(context.Context, *service.Group) error { return nil }
func (s *openAIRouteGroupRepoStub) GetByID(_ context.Context, id int64) (*service.Group, error) {
	return s.groups[id], nil
}
func (s *openAIRouteGroupRepoStub) GetByIDLite(_ context.Context, id int64) (*service.Group, error) {
	return s.groups[id], nil
}
func (s *openAIRouteGroupRepoStub) Update(context.Context, *service.Group) error { return nil }
func (s *openAIRouteGroupRepoStub) Delete(context.Context, int64) error          { return nil }
func (s *openAIRouteGroupRepoStub) DeleteCascade(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (s *openAIRouteGroupRepoStub) List(context.Context, pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *openAIRouteGroupRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *openAIRouteGroupRepoStub) ListActive(context.Context) ([]service.Group, error) {
	return nil, nil
}
func (s *openAIRouteGroupRepoStub) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}
func (s *openAIRouteGroupRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return false, nil
}
func (s *openAIRouteGroupRepoStub) GetAccountCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (s *openAIRouteGroupRepoStub) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s *openAIRouteGroupRepoStub) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (s *openAIRouteGroupRepoStub) BindAccountsToGroup(context.Context, int64, []int64) error {
	return nil
}
func (s *openAIRouteGroupRepoStub) UpdateSortOrders(context.Context, []service.GroupSortOrderUpdate) error {
	return nil
}

func TestOpenAIRouteGroupRuntimeSwitchesPerRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	plusID := int64(1)
	proID := int64(2)
	plusGroup := &service.Group{ID: plusID, Name: "Plus", Platform: service.PlatformOpenAI, Status: service.StatusActive}
	proGroup := &service.Group{ID: proID, Name: "Pro", Platform: service.PlatformOpenAI, Status: service.StatusActive}
	apiKey := &service.APIKey{
		ID:            100,
		UserID:        42,
		User:          &service.User{ID: 42, Status: service.StatusActive},
		GroupID:       &plusID,
		Group:         plusGroup,
		RouteGroupIDs: []int64{plusID, proID},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	billingCfg := &config.Config{RunMode: config.RunModeSimple}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, billingCfg, nil)
	defer billing.Stop()

	h := &OpenAIGatewayHandler{
		gatewayService:      &service.OpenAIGatewayService{},
		apiKeyService:       service.NewAPIKeyService(nil, nil, &openAIRouteGroupRepoStub{groups: map[int64]*service.Group{plusID: plusGroup, proID: proGroup}}, nil, nil, nil, &config.Config{}),
		billingCacheService: billing,
	}

	body := []byte(`{"model":"gpt-5"}`)
	rt := newOpenAIRouteGroupRuntime(h, c, zap.NewNop(), apiKey, nil, "gpt-5", body, service.ReplaceModelInBody)
	require.Same(t, plusGroup, rt.currentAPIKey.Group)
	require.Equal(t, plusID, *rt.currentAPIKey.GroupID)

	require.True(t, rt.switchToNext("test"))
	require.Equal(t, proID, *rt.currentAPIKey.GroupID)
	require.Same(t, proGroup, rt.currentAPIKey.Group)

	nextRequest := newOpenAIRouteGroupRuntime(h, c, zap.NewNop(), apiKey, nil, "gpt-5", body, service.ReplaceModelInBody)
	require.Equal(t, plusID, *nextRequest.currentAPIKey.GroupID)
	require.Same(t, plusGroup, nextRequest.currentAPIKey.Group)
}
