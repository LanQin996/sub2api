package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestOpsMonitoringEnabledCachesHotPathSetting(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	repo.values[SettingKeyOpsMonitoringEnabled] = "false"
	svc := &OpsService{
		settingRepo: repo,
		cfg:         &config.Config{Ops: config.OpsConfig{Enabled: true}},
	}
	svc.initRuntimeSettings(context.Background())

	for range 100 {
		if svc.IsMonitoringEnabled(context.Background()) {
			t.Fatal("expected monitoring to be disabled")
		}
	}
	if got := repo.getMultipleCalls; got != 1 {
		t.Fatalf("GetMultiple calls = %d, want 1", got)
	}
	if got := repo.getValueCalls; got != 0 {
		t.Fatalf("GetValue calls = %d, want 0", got)
	}
}

func TestOpsMonitoringHardSwitchSkipsSettingLookup(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := &OpsService{
		settingRepo: repo,
		cfg:         &config.Config{Ops: config.OpsConfig{Enabled: false}},
	}

	if svc.IsMonitoringEnabled(context.Background()) {
		t.Fatal("expected hard switch to disable monitoring")
	}
	if got := repo.getValueCalls; got != 0 {
		t.Fatalf("GetValue calls = %d, want 0", got)
	}
	if got := repo.getMultipleCalls; got != 0 {
		t.Fatalf("GetMultiple calls = %d, want 0", got)
	}
}
