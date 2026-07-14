package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"log/slog"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	dataType                 = "sub2api-data"
	legacyDataType           = "sub2api-bundle"
	dataVersion              = 1
	dataPageCap              = 1000
	dataImportPrivacyTimeout = 20 * time.Second
)

type DataPayload struct {
	Type       string        `json:"type,omitempty"`
	Version    int           `json:"version,omitempty"`
	ExportedAt string        `json:"exported_at"`
	Proxies    []DataProxy   `json:"proxies"`
	Accounts   []DataAccount `json:"accounts"`
	// SkippedShadows 记录导出时被排除的 spark 影子账号数量(见 ExportData)。仅作可见性提示,
	// 导入侧忽略该字段;omitempty 保持向后兼容。
	SkippedShadows int `json:"skipped_shadows,omitempty"`
}

type DataProxy struct {
	ProxyKey        string `json:"proxy_key"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	Status          string `json:"status"`
	ExpiresAt       *int64 `json:"expires_at,omitempty"`        // unix 秒，与 DataAccount.ExpiresAt 风格一致
	FallbackMode    string `json:"fallback_mode,omitempty"`     // none/direct/proxy
	BackupProxyName string `json:"backup_proxy_name,omitempty"` // 备用代理 name（跨实例按 name 反查）
	ExpiryWarnDays  int    `json:"expiry_warn_days,omitempty"`
}

// DataAccount 是管理员显式备份导出使用的账号结构，故意不走 dto.Account 的脱敏路径，
// Credentials 原文返回。这是"管理员备份"这一显式行为的一部分；如未来需要导出脱敏版本，
// 应新增独立结构而非修改这里。
// 注意:本结构不含 parent_account_id/quota_dimension——spark 影子账号在 ExportData 处被显式
// 排除(影子不持凭据、通用凭据型导入强制 credentials 非空无法重建父子链接),不在此表达。
// 影子的独立调度配置(priority/并发/分组/status 管理员可单独调)亦不在本备份范围,属已知局限
// (外审第6轮裁决:保持排除 + 前端警告,而非升级格式做完整往返)。
type DataAccount struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes,omitempty"`
	Platform           string         `json:"platform"`
	Type               string         `json:"type"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra,omitempty"`
	ProxyKey           *string        `json:"proxy_key,omitempty"`
	Concurrency        int            `json:"concurrency"`
	Priority           int            `json:"priority"`
	RateMultiplier     *float64       `json:"rate_multiplier,omitempty"`
	ExpiresAt          *int64         `json:"expires_at,omitempty"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired,omitempty"`
}

type DataImportRequest struct {
	Data                    DataPayload `json:"data"`
	SkipDefaultGroupBind    *bool       `json:"skip_default_group_bind"`
	GroupIDs                []int64     `json:"group_ids,omitempty"`
	ConfirmMixedChannelRisk bool        `json:"confirm_mixed_channel_risk,omitempty"`
}

type DataImportResult struct {
	ProxyCreated   int               `json:"proxy_created"`
	ProxyReused    int               `json:"proxy_reused"`
	ProxyFailed    int               `json:"proxy_failed"`
	AccountCreated int               `json:"account_created"`
	AccountFailed  int               `json:"account_failed"`
	Errors         []DataImportError `json:"errors,omitempty"`
}

type DataImportError struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	ProxyKey string `json:"proxy_key,omitempty"`
	Message  string `json:"message"`
}

func buildProxyKey(protocol, host string, port int, username, password string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", strings.TrimSpace(protocol), strings.TrimSpace(host), port, strings.TrimSpace(username), strings.TrimSpace(password))
}

func (h *AccountHandler) ExportData(c *gin.Context) {
	ctx := c.Request.Context()

	selectedIDs, err := parseAccountIDs(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	accounts, err := h.resolveExportAccounts(ctx, selectedIDs, c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 排除 spark 影子账号:影子不持凭据,通用凭据型导出无法表达父子链接、导入侧又强制 credentials
	// 非空——若混入会产出无法还原的坏备份(导入即失败)。影子的独立调度配置(priority/并发/分组/
	// status,管理员可单独调)随之不进备份,还原后需在重建的影子上重新调优;前端按 skipped_shadows
	// 提示用户(外审第5轮发现、第6轮裁决:保持排除 + 警告,不做完整往返)。
	skippedShadows := 0
	exportable := make([]service.Account, 0, len(accounts))
	for i := range accounts {
		if accounts[i].IsCredentialShadow() {
			skippedShadows++
			continue
		}
		exportable = append(exportable, accounts[i])
	}
	accounts = exportable
	if skippedShadows > 0 {
		slog.Info("export_skipped_spark_shadows", "count", skippedShadows)
	}

	includeProxies, err := parseIncludeProxies(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var proxies []service.Proxy
	if includeProxies {
		proxies, err = h.resolveExportProxies(ctx, accounts)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	} else {
		proxies = []service.Proxy{}
	}

	// 构建 id→name 映射，用于导出备用代理 name
	proxyNameByID := make(map[int64]string, len(proxies))
	for i := range proxies {
		proxyNameByID[proxies[i].ID] = proxies[i].Name
	}

	proxyKeyByID := make(map[int64]string, len(proxies))
	dataProxies := make([]DataProxy, 0, len(proxies))
	for i := range proxies {
		p := proxies[i]
		key := buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		proxyKeyByID[p.ID] = key

		var expiresAt *int64
		if p.ExpiresAt != nil {
			v := p.ExpiresAt.Unix()
			expiresAt = &v
		}
		var backupProxyName string
		if p.BackupProxyID != nil {
			backupProxyName = proxyNameByID[*p.BackupProxyID]
		}
		dataProxies = append(dataProxies, DataProxy{
			ProxyKey:        key,
			Name:            p.Name,
			Protocol:        p.Protocol,
			Host:            p.Host,
			Port:            p.Port,
			Username:        p.Username,
			Password:        p.Password,
			Status:          p.Status,
			ExpiresAt:       expiresAt,
			FallbackMode:    p.FallbackMode,
			BackupProxyName: backupProxyName,
			ExpiryWarnDays:  p.ExpiryWarnDays,
		})
	}

	dataAccounts := make([]DataAccount, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		var proxyKey *string
		if acc.ProxyID != nil {
			if key, ok := proxyKeyByID[*acc.ProxyID]; ok {
				proxyKey = &key
			}
		}
		var expiresAt *int64
		if acc.ExpiresAt != nil {
			v := acc.ExpiresAt.Unix()
			expiresAt = &v
		}
		dataAccounts = append(dataAccounts, DataAccount{
			Name:               acc.Name,
			Notes:              acc.Notes,
			Platform:           acc.Platform,
			Type:               acc.Type,
			Credentials:        acc.Credentials,
			Extra:              acc.Extra,
			ProxyKey:           proxyKey,
			Concurrency:        acc.Concurrency,
			Priority:           acc.Priority,
			RateMultiplier:     acc.RateMultiplier,
			ExpiresAt:          expiresAt,
			AutoPauseOnExpired: &acc.AutoPauseOnExpired,
		})
	}

	payload := DataPayload{
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		Proxies:        dataProxies,
		Accounts:       dataAccounts,
		SkippedShadows: skippedShadows,
	}

	response.Success(c, payload)
}

func (h *AccountHandler) ImportData(c *gin.Context) {
	var req DataImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := validateDataHeader(req.Data); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	executeAdminIdempotentJSON(c, "admin.accounts.import_data", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.importData(ctx, req)
	})
}

type dataImportGroupSelection map[string][]int64

type dataImportGroupCheckKey struct {
	platform        string
	mixedScheduling bool
}

func normalizeDataImportPlatform(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "claude" {
		return service.PlatformAnthropic
	}
	return platform
}

func dataImportMixedSchedulingEnabled(item DataAccount) bool {
	if normalizeDataImportPlatform(item.Platform) != service.PlatformAntigravity || item.Extra == nil {
		return false
	}
	enabled, _ := item.Extra["mixed_scheduling"].(bool)
	return enabled
}

func (s dataImportGroupSelection) forAccount(item DataAccount) []int64 {
	platform := normalizeDataImportPlatform(item.Platform)
	groupIDs := s[platform]
	if !dataImportMixedSchedulingEnabled(item) {
		return groupIDs
	}

	mixedGroupIDs := make([]int64, 0, len(groupIDs)+len(s[service.PlatformAnthropic])+len(s[service.PlatformGemini]))
	mixedGroupIDs = append(mixedGroupIDs, groupIDs...)
	mixedGroupIDs = append(mixedGroupIDs, s[service.PlatformAnthropic]...)
	mixedGroupIDs = append(mixedGroupIDs, s[service.PlatformGemini]...)
	return mixedGroupIDs
}

func (s dataImportGroupSelection) hasGroupsForAccount(item DataAccount) bool {
	platform := normalizeDataImportPlatform(item.Platform)
	if len(s[platform]) > 0 {
		return true
	}
	return dataImportMixedSchedulingEnabled(item) &&
		(len(s[service.PlatformAnthropic]) > 0 || len(s[service.PlatformGemini]) > 0)
}

func dataImportGroupRiskKey(item DataAccount) dataImportGroupCheckKey {
	return dataImportGroupCheckKey{
		platform:        normalizeDataImportPlatform(item.Platform),
		mixedScheduling: dataImportMixedSchedulingEnabled(item),
	}
}

func dataImportMixedChannel(platform string) string {
	switch normalizeDataImportPlatform(platform) {
	case service.PlatformAntigravity:
		return "Antigravity"
	case service.PlatformAnthropic:
		return "Anthropic"
	default:
		return ""
	}
}

func (h *AccountHandler) resolveImportGroups(ctx context.Context, ids []int64) (dataImportGroupSelection, error) {
	selection := make(dataImportGroupSelection)
	if len(ids) == 0 {
		return selection, nil
	}

	groupIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, groupID := range ids {
		if groupID <= 0 {
			return nil, infraerrors.BadRequest("INVALID_GROUP_IDS", "group_ids must contain positive integers")
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		groupIDs = append(groupIDs, groupID)
	}

	groups, err := h.adminService.GetAllGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list import groups: %w", err)
	}
	groupByID := make(map[int64]service.Group, len(groups))
	for i := range groups {
		groupByID[groups[i].ID] = groups[i]
	}

	for _, groupID := range groupIDs {
		group, ok := groupByID[groupID]
		if !ok {
			return nil, fmt.Errorf("validate group %d: %w", groupID, service.ErrGroupNotFound)
		}
		platform := normalizeDataImportPlatform(group.Platform)
		if platform == "" {
			return nil, infraerrors.BadRequest("INVALID_GROUP_PLATFORM", fmt.Sprintf("group %d has no platform", groupID))
		}
		selection[platform] = append(selection[platform], groupID)
	}

	return selection, nil
}

func validateDataImportGroupCoverage(payload DataPayload, selection dataImportGroupSelection) error {
	if len(selection) == 0 {
		return nil
	}

	missingPlatforms := make([]string, 0)
	seen := make(map[string]struct{})
	for i := range payload.Accounts {
		item := payload.Accounts[i]
		item.Platform = normalizeDataImportPlatform(item.Platform)
		if validateDataAccount(item) != nil {
			continue
		}
		if selection.hasGroupsForAccount(item) {
			continue
		}
		platform := item.Platform
		if _, ok := seen[platform]; ok {
			continue
		}
		seen[platform] = struct{}{}
		if platform == "" {
			platform = "<empty>"
		}
		missingPlatforms = append(missingPlatforms, platform)
	}
	if len(missingPlatforms) > 0 {
		return infraerrors.BadRequest(
			"IMPORT_GROUP_PLATFORM_MISMATCH",
			"selected groups do not cover account platforms: "+strings.Join(missingPlatforms, ", "),
		)
	}
	return nil
}

func (h *AccountHandler) validateDataImportMixedChannelRisk(ctx context.Context, payload DataPayload, selection dataImportGroupSelection) error {
	checked := make(map[dataImportGroupCheckKey]struct{}, len(selection))
	batchChannels := make(map[int64]string)
	for i := range payload.Accounts {
		item := payload.Accounts[i]
		item.Platform = normalizeDataImportPlatform(item.Platform)
		if validateDataAccount(item) != nil {
			continue
		}
		key := dataImportGroupRiskKey(item)
		if _, ok := checked[key]; ok {
			continue
		}
		groupIDs := selection.forAccount(item)
		if len(groupIDs) == 0 {
			continue
		}

		if err := h.adminService.CheckMixedChannelRisk(ctx, 0, item.Platform, groupIDs); err != nil {
			return err
		}
		checked[key] = struct{}{}

		channel := dataImportMixedChannel(item.Platform)
		if channel == "" {
			continue
		}
		for _, groupID := range groupIDs {
			if otherChannel, ok := batchChannels[groupID]; ok && otherChannel != channel {
				return &service.MixedChannelError{
					GroupID:         groupID,
					GroupName:       fmt.Sprintf("Group %d", groupID),
					CurrentPlatform: channel,
					OtherPlatform:   otherChannel,
				}
			}
			batchChannels[groupID] = channel
		}
	}
	return nil
}

func (h *AccountHandler) importData(ctx context.Context, req DataImportRequest) (DataImportResult, error) {
	skipDefaultGroupBind := true
	if req.SkipDefaultGroupBind != nil {
		skipDefaultGroupBind = *req.SkipDefaultGroupBind
	}

	dataPayload := req.Data
	result := DataImportResult{}
	groupSelection, err := h.resolveImportGroups(ctx, req.GroupIDs)
	if err != nil {
		return result, err
	}
	if err := validateDataImportGroupCoverage(dataPayload, groupSelection); err != nil {
		return result, err
	}
	if !req.ConfirmMixedChannelRisk {
		if err := h.validateDataImportMixedChannelRisk(ctx, dataPayload, groupSelection); err != nil {
			var mixedErr *service.MixedChannelError
			if errors.As(err, &mixedErr) {
				return result, infraerrors.Conflict("MIXED_CHANNEL_WARNING", mixedErr.Error())
			}
			return result, err
		}
	}

	existingProxies, err := h.listAllProxies(ctx)
	if err != nil {
		return result, err
	}

	proxyKeyToID := make(map[string]int64, len(existingProxies))
	// proxyNameToID 用于 backup_proxy_name 反查：DB 已有 + 本批次新建均会写入
	proxyNameToID := make(map[string]int64, len(existingProxies))
	for i := range existingProxies {
		p := existingProxies[i]
		key := buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		proxyKeyToID[key] = p.ID
		if p.Name != "" {
			proxyNameToID[p.Name] = p.ID
		}
	}

	for i := range dataPayload.Proxies {
		item := dataPayload.Proxies[i]
		key := item.ProxyKey
		if key == "" {
			key = buildProxyKey(item.Protocol, item.Host, item.Port, item.Username, item.Password)
		}
		if err := validateDataProxy(item); err != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:     "proxy",
				Name:     item.Name,
				ProxyKey: key,
				Message:  err.Error(),
			})
			continue
		}
		normalizedStatus := normalizeProxyStatus(item.Status)
		if existingID, ok := proxyKeyToID[key]; ok {
			proxyKeyToID[key] = existingID
			result.ProxyReused++
			if normalizedStatus != "" {
				if proxy, getErr := h.adminService.GetProxy(ctx, existingID); getErr == nil && proxy != nil && proxy.Status != normalizedStatus {
					// 同步 status 时传入完整字段，避免零值覆盖已存在代理的有效期/fallback 配置。
					var existingExpiresAt *time.Time
					if item.ExpiresAt != nil {
						t := time.Unix(*item.ExpiresAt, 0).UTC()
						existingExpiresAt = &t
					}
					existingFallbackMode := item.FallbackMode
					if existingFallbackMode == "" {
						existingFallbackMode = service.FallbackModeNone
					}
					var existingBackupProxyID *int64
					if item.BackupProxyName != "" {
						if bid, ok := proxyNameToID[item.BackupProxyName]; ok {
							existingBackupProxyID = &bid
						}
					}
					_, _ = h.adminService.UpdateProxy(ctx, existingID, &service.UpdateProxyInput{
						Status:         normalizedStatus,
						ExpiresAt:      existingExpiresAt,
						FallbackMode:   existingFallbackMode,
						BackupProxyID:  existingBackupProxyID,
						ExpiryWarnDays: item.ExpiryWarnDays,
						Name:           proxy.Name,
						Protocol:       proxy.Protocol,
						Host:           proxy.Host,
						Port:           proxy.Port,
						Username:       proxy.Username,
						Password:       proxy.Password,
					})
				}
			}
			continue
		}

		// 解析 expires_at（unix 秒 → *time.Time）
		var expiresAt *time.Time
		if item.ExpiresAt != nil {
			t := time.Unix(*item.ExpiresAt, 0).UTC()
			expiresAt = &t
		}

		// 解析 backup_proxy_name → backup_proxy_id
		fallbackMode := item.FallbackMode
		var backupProxyID *int64
		if item.BackupProxyName != "" {
			if bid, ok := proxyNameToID[item.BackupProxyName]; ok {
				backupProxyID = &bid
			} else {
				// 查不到备用代理：降级 fallback_mode=none，记录 warning
				fallbackMode = service.FallbackModeNone
				result.Errors = append(result.Errors, DataImportError{
					Kind:     "proxy",
					Name:     item.Name,
					ProxyKey: key,
					Message:  fmt.Sprintf("backup_proxy_name %q not found, fallback_mode downgraded to none", item.BackupProxyName),
				})
			}
		}

		created, createErr := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
			Name:           defaultProxyName(item.Name),
			Protocol:       item.Protocol,
			Host:           item.Host,
			Port:           item.Port,
			Username:       item.Username,
			Password:       item.Password,
			ExpiresAt:      expiresAt,
			FallbackMode:   fallbackMode,
			BackupProxyID:  backupProxyID,
			ExpiryWarnDays: item.ExpiryWarnDays,
		})
		if createErr != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:     "proxy",
				Name:     item.Name,
				ProxyKey: key,
				Message:  createErr.Error(),
			})
			continue
		}
		proxyKeyToID[key] = created.ID
		// 把新建代理的 name 也加入反查表，供后续批内代理引用
		if created.Name != "" {
			proxyNameToID[created.Name] = created.ID
		}
		result.ProxyCreated++

		if normalizedStatus != "" && normalizedStatus != created.Status {
			// 新建后同步 status 时，传入完整字段，避免零值覆盖刚创建的有效期/fallback 配置。
			_, _ = h.adminService.UpdateProxy(ctx, created.ID, &service.UpdateProxyInput{
				Status:         normalizedStatus,
				ExpiresAt:      expiresAt,
				FallbackMode:   fallbackMode,
				BackupProxyID:  backupProxyID,
				ExpiryWarnDays: item.ExpiryWarnDays,
				Name:           created.Name,
				Protocol:       created.Protocol,
				Host:           created.Host,
				Port:           created.Port,
				Username:       created.Username,
				Password:       created.Password,
			})
		}
	}

	// 批量导入统一延迟隐私设置，避免每个账号各起 goroutine。
	var privacySetupAccounts []*service.Account
	mixedChannelCheckedAtCreate := make(map[dataImportGroupCheckKey]bool, len(groupSelection))
	groupIDsByRiskKey := make(map[dataImportGroupCheckKey][]int64, len(groupSelection))

	for i := range dataPayload.Accounts {
		item := dataPayload.Accounts[i]
		item.Platform = normalizeDataImportPlatform(item.Platform)
		if err := validateDataAccount(item); err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: err.Error(),
			})
			continue
		}

		var proxyID *int64
		if item.ProxyKey != nil && *item.ProxyKey != "" {
			if id, ok := proxyKeyToID[*item.ProxyKey]; ok {
				proxyID = &id
			} else {
				result.AccountFailed++
				result.Errors = append(result.Errors, DataImportError{
					Kind:     "account",
					Name:     item.Name,
					ProxyKey: *item.ProxyKey,
					Message:  "proxy_key not found",
				})
				continue
			}
		}

		enrichCredentialsFromIDToken(&item)
		groupRiskKey := dataImportGroupRiskKey(item)
		groupIDs, ok := groupIDsByRiskKey[groupRiskKey]
		if !ok {
			groupIDs = groupSelection.forAccount(item)
			groupIDsByRiskKey[groupRiskKey] = groupIDs
		}
		skipMixedChannelCheck := req.ConfirmMixedChannelRisk || mixedChannelCheckedAtCreate[groupRiskKey]

		accountInput := &service.CreateAccountInput{
			Name:                  item.Name,
			Notes:                 item.Notes,
			Platform:              item.Platform,
			Type:                  item.Type,
			Credentials:           item.Credentials,
			Extra:                 item.Extra,
			ProxyID:               proxyID,
			Concurrency:           item.Concurrency,
			Priority:              item.Priority,
			RateMultiplier:        item.RateMultiplier,
			GroupIDs:              groupIDs,
			ExpiresAt:             item.ExpiresAt,
			AutoPauseOnExpired:    item.AutoPauseOnExpired,
			SkipDefaultGroupBind:  skipDefaultGroupBind,
			SkipMixedChannelCheck: len(groupIDs) > 0 && skipMixedChannelCheck,
			DeferPrivacySetup:     true,
		}

		created, err := h.adminService.CreateAccount(ctx, accountInput)
		if err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: err.Error(),
			})
			continue
		}
		// 收集 OpenAI/Antigravity OAuth 账号，稍后由单个后台任务顺序设置隐私。
		if created.Type == service.AccountTypeOAuth &&
			(created.Platform == service.PlatformOpenAI || created.Platform == service.PlatformAntigravity) {
			privacySetupAccounts = append(privacySetupAccounts, created)
		}
		if len(groupIDs) > 0 {
			mixedChannelCheckedAtCreate[groupRiskKey] = true
		}
		h.scheduleGrokImportProbe(created)
		result.AccountCreated++
	}

	// 单个后台任务顺序设置隐私，避免大量导入时出现 N 个 goroutine 和重复调用。
	if len(privacySetupAccounts) > 0 {
		adminSvc := h.adminService
		accounts := append([]*service.Account(nil), privacySetupAccounts...)
		go func() {
			bgCtx := context.Background()
			for _, acc := range accounts {
				func() {
					privacyCtx, cancel := context.WithTimeout(bgCtx, dataImportPrivacyTimeout)
					defer cancel()
					defer func() {
						if r := recover(); r != nil {
							slog.Error("import_account_privacy_panic",
								"account_id", acc.ID, "platform", acc.Platform, "recover", r)
						}
					}()
					switch acc.Platform {
					case service.PlatformOpenAI:
						adminSvc.EnsureOpenAIPrivacy(privacyCtx, acc)
					case service.PlatformAntigravity:
						adminSvc.ForceAntigravityPrivacy(privacyCtx, acc)
					}
				}()
			}
			slog.Info("import_account_privacy_done", "count", len(accounts))
		}()
	}

	return result, nil
}

func (h *AccountHandler) listAllProxies(ctx context.Context) ([]service.Proxy, error) {
	page := 1
	pageSize := dataPageCap
	var out []service.Proxy
	for {
		items, total, err := h.adminService.ListProxies(ctx, page, pageSize, "", "", "", "created_at", "desc")
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= int(total) || len(items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (h *AccountHandler) listAccountsFiltered(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode, sortBy, sortOrder string) ([]service.Account, error) {
	page := 1
	pageSize := dataPageCap
	var out []service.Account
	for {
		items, total, err := h.adminService.ListAccounts(ctx, page, pageSize, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= int(total) || len(items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (h *AccountHandler) resolveExportAccounts(ctx context.Context, ids []int64, c *gin.Context) ([]service.Account, error) {
	if len(ids) > 0 {
		accounts, err := h.adminService.GetAccountsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		out := make([]service.Account, 0, len(accounts))
		for _, acc := range accounts {
			if acc == nil {
				continue
			}
			out = append(out, *acc)
		}
		return out, nil
	}

	platform := c.Query("platform")
	accountType := c.Query("type")
	status := c.Query("status")
	privacyMode := strings.TrimSpace(c.Query("privacy_mode"))
	search := strings.TrimSpace(c.Query("search"))
	sortBy := c.DefaultQuery("sort_by", "name")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	if len(search) > 100 {
		search = search[:100]
	}

	groupID := int64(0)
	if groupIDStr := c.Query("group"); groupIDStr != "" {
		if groupIDStr == accountListGroupUngroupedQueryValue {
			groupID = service.AccountListGroupUngrouped
		} else {
			parsedGroupID, parseErr := strconv.ParseInt(groupIDStr, 10, 64)
			if parseErr != nil || parsedGroupID <= 0 {
				return nil, infraerrors.BadRequest("INVALID_GROUP_FILTER", "invalid group filter")
			}
			groupID = parsedGroupID
		}
	}

	return h.listAccountsFiltered(ctx, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
}

func (h *AccountHandler) resolveExportProxies(ctx context.Context, accounts []service.Account) ([]service.Proxy, error) {
	if len(accounts) == 0 {
		return []service.Proxy{}, nil
	}

	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for i := range accounts {
		if accounts[i].ProxyID == nil {
			continue
		}
		id := *accounts[i].ProxyID
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []service.Proxy{}, nil
	}

	return h.adminService.GetProxiesByIDs(ctx, ids)
}

func parseAccountIDs(c *gin.Context) ([]int64, error) {
	values := c.QueryArray("ids")
	if len(values) == 0 {
		raw := strings.TrimSpace(c.Query("ids"))
		if raw != "" {
			values = []string{raw}
		}
	}
	if len(values) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(values))
	for _, item := range values {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid account id: %s", part)
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func parseIncludeProxies(c *gin.Context) (bool, error) {
	raw := strings.TrimSpace(strings.ToLower(c.Query("include_proxies")))
	if raw == "" {
		return true, nil
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return true, fmt.Errorf("invalid include_proxies value: %s", raw)
	}
}

func validateDataHeader(payload DataPayload) error {
	if payload.Type != "" && payload.Type != dataType && payload.Type != legacyDataType {
		return fmt.Errorf("unsupported data type: %s", payload.Type)
	}
	if payload.Version != 0 && payload.Version != dataVersion {
		return fmt.Errorf("unsupported data version: %d", payload.Version)
	}
	if payload.Proxies == nil {
		return errors.New("proxies is required")
	}
	if payload.Accounts == nil {
		return errors.New("accounts is required")
	}
	return nil
}

func validateDataProxy(item DataProxy) error {
	if strings.TrimSpace(item.Protocol) == "" {
		return errors.New("proxy protocol is required")
	}
	if strings.TrimSpace(item.Host) == "" {
		return errors.New("proxy host is required")
	}
	if item.Port <= 0 || item.Port > 65535 {
		return errors.New("proxy port is invalid")
	}
	switch item.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("proxy protocol is invalid: %s", item.Protocol)
	}
	if item.Status != "" {
		normalizedStatus := normalizeProxyStatus(item.Status)
		if normalizedStatus != service.StatusActive && normalizedStatus != "inactive" {
			return fmt.Errorf("proxy status is invalid: %s", item.Status)
		}
	}
	return nil
}

func validateDataAccount(item DataAccount) error {
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("account name is required")
	}
	if strings.TrimSpace(item.Platform) == "" {
		return errors.New("account platform is required")
	}
	if strings.TrimSpace(item.Type) == "" {
		return errors.New("account type is required")
	}
	if len(item.Credentials) == 0 {
		return errors.New("account credentials is required")
	}
	switch item.Type {
	case service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey, service.AccountTypeUpstream:
	default:
		return fmt.Errorf("account type is invalid: %s", item.Type)
	}
	if item.RateMultiplier != nil && *item.RateMultiplier < 0 {
		return errors.New("rate_multiplier must be >= 0")
	}
	if item.Concurrency < 0 {
		return errors.New("concurrency must be >= 0")
	}
	if item.Priority < 0 {
		return errors.New("priority must be >= 0")
	}
	return nil
}

func defaultProxyName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "imported-proxy"
	}
	return name
}

// enrichCredentialsFromIDToken performs best-effort extraction of user info fields
// (email, plan_type, chatgpt_account_id, etc.) from id_token in credentials.
// Only applies to OpenAI OAuth accounts. Skips expired token errors silently.
// Existing credential values are never overwritten — only missing fields are filled.
func enrichCredentialsFromIDToken(item *DataAccount) {
	if item.Credentials == nil {
		return
	}
	// Only enrich OpenAI OAuth accounts
	platform := strings.ToLower(strings.TrimSpace(item.Platform))
	if platform != service.PlatformOpenAI {
		return
	}
	if strings.ToLower(strings.TrimSpace(item.Type)) != service.AccountTypeOAuth {
		return
	}

	idToken, _ := item.Credentials["id_token"].(string)
	if strings.TrimSpace(idToken) == "" {
		return
	}

	// DecodeIDToken skips expiry validation — safe for imported data
	claims, err := openai.DecodeIDToken(idToken)
	if err != nil {
		slog.Debug("import_enrich_id_token_decode_failed", "account", item.Name, "error", err)
		return
	}

	userInfo := claims.GetUserInfo()
	if userInfo == nil {
		return
	}

	// Fill missing fields only (never overwrite existing values)
	setIfMissing := func(key, value string) {
		if value == "" {
			return
		}
		if existing, _ := item.Credentials[key].(string); existing == "" {
			item.Credentials[key] = value
		}
	}

	setIfMissing("email", userInfo.Email)
	setIfMissing("plan_type", userInfo.PlanType)
	setIfMissing("chatgpt_account_id", userInfo.ChatGPTAccountID)
	setIfMissing("chatgpt_user_id", userInfo.ChatGPTUserID)
	setIfMissing("organization_id", userInfo.OrganizationID)
}

func normalizeProxyStatus(status string) string {
	normalized := strings.TrimSpace(strings.ToLower(status))
	switch normalized {
	case "":
		return ""
	case service.StatusActive:
		return service.StatusActive
	case "inactive", service.StatusDisabled:
		return "inactive"
	case "expired":
		// 导入 expired 代理按 inactive 处理，避免导入即触发到期改投逻辑
		return "inactive"
	default:
		return normalized
	}
}
