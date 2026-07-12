package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestOpsMonitoringEnabledCachesHotPathSetting(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	var calls atomic.Int32
	repo.getValueFn = func(key string) (string, error) {
		if key != SettingKeyOpsMonitoringEnabled {
			t.Fatalf("unexpected setting key %q", key)
		}
		calls.Add(1)
		return "false", nil
	}
	svc := &OpsService{
		settingRepo: repo,
		cfg:         &config.Config{Ops: config.OpsConfig{Enabled: true}},
	}

	for range 100 {
		if svc.IsMonitoringEnabled(context.Background()) {
			t.Fatal("expected monitoring to be disabled")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("GetValue calls = %d, want 1", got)
	}
}

func TestOpsMonitoringHardSwitchSkipsSettingLookup(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	var calls atomic.Int32
	repo.getValueFn = func(string) (string, error) {
		calls.Add(1)
		return "true", nil
	}
	svc := &OpsService{
		settingRepo: repo,
		cfg:         &config.Config{Ops: config.OpsConfig{Enabled: false}},
	}

	if svc.IsMonitoringEnabled(context.Background()) {
		t.Fatal("expected hard switch to disable monitoring")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("GetValue calls = %d, want 0", got)
	}
}
