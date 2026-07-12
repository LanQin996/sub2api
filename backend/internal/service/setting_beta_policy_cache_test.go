package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func TestBetaPolicySettingsCachesAndClonesHotPathValue(t *testing.T) {
	original := &BetaPolicySettings{Rules: []BetaPolicyRule{{
		BetaToken:      "cached-beta",
		Action:         BetaPolicyActionFilter,
		Scope:          BetaPolicyScopeAll,
		ModelWhitelist: []string{"model-*"},
	}}}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	repo := newRuntimeSettingRepoStub()
	var calls atomic.Int32
	repo.getValueFn = func(key string) (string, error) {
		calls.Add(1)
		return string(raw), nil
	}
	svc := NewSettingService(repo, nil)

	first, err := svc.GetBetaPolicySettings(context.Background())
	if err != nil {
		t.Fatalf("first GetBetaPolicySettings: %v", err)
	}
	first.Rules[0].Action = BetaPolicyActionBlock
	first.Rules[0].ModelWhitelist[0] = "mutated"

	second, err := svc.GetBetaPolicySettings(context.Background())
	if err != nil {
		t.Fatalf("second GetBetaPolicySettings: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("GetValue calls = %d, want 1", got)
	}
	if second.Rules[0].Action != BetaPolicyActionFilter || second.Rules[0].ModelWhitelist[0] != "model-*" {
		t.Fatalf("cached settings were mutated through caller copy: %+v", second.Rules[0])
	}
}

func TestSetBetaPolicySettingsCannotBeOverwrittenByStaleLoad(t *testing.T) {
	oldSettings := &BetaPolicySettings{Rules: []BetaPolicyRule{{
		BetaToken: "old-beta",
		Action:    BetaPolicyActionFilter,
		Scope:     BetaPolicyScopeAll,
	}}}
	oldRaw, err := json.Marshal(oldSettings)
	if err != nil {
		t.Fatalf("marshal old settings: %v", err)
	}

	repo := newRuntimeSettingRepoStub()
	loadStarted := make(chan struct{})
	releaseLoad := make(chan struct{})
	setCalled := make(chan struct{})
	repo.getValueFn = func(string) (string, error) {
		close(loadStarted)
		<-releaseLoad
		return string(oldRaw), nil
	}
	repo.setFn = func(string, string) error {
		close(setCalled)
		return nil
	}
	svc := NewSettingService(repo, nil)

	loadDone := make(chan struct{})
	go func() {
		defer close(loadDone)
		_, _ = svc.GetBetaPolicySettings(context.Background())
	}()
	<-loadStarted

	updated := &BetaPolicySettings{Rules: []BetaPolicyRule{{
		BetaToken: "new-beta",
		Action:    BetaPolicyActionBlock,
		Scope:     BetaPolicyScopeAll,
	}}}
	setDone := make(chan error, 1)
	go func() { setDone <- svc.SetBetaPolicySettings(context.Background(), updated) }()

	select {
	case <-setCalled:
		close(releaseLoad)
		<-loadDone
		<-setDone
		t.Fatal("settings write entered the repository before the stale load completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseLoad)
	<-loadDone
	if err := <-setDone; err != nil {
		t.Fatalf("SetBetaPolicySettings: %v", err)
	}

	got, err := svc.GetBetaPolicySettings(context.Background())
	if err != nil {
		t.Fatalf("GetBetaPolicySettings after concurrent set: %v", err)
	}
	if len(got.Rules) != 1 || got.Rules[0].BetaToken != "new-beta" {
		t.Fatalf("stale load overwrote updated cache: %+v", got)
	}
}

func TestSetBetaPolicySettingsRefreshesCacheImmediately(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	repo.values[SettingKeyBetaPolicySettings] = `{"rules":[]}`
	svc := NewSettingService(repo, nil)
	if _, err := svc.GetBetaPolicySettings(context.Background()); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	updated := &BetaPolicySettings{Rules: []BetaPolicyRule{{
		BetaToken: "new-beta",
		Action:    BetaPolicyActionBlock,
		Scope:     BetaPolicyScopeAll,
	}}}
	if err := svc.SetBetaPolicySettings(context.Background(), updated); err != nil {
		t.Fatalf("SetBetaPolicySettings: %v", err)
	}
	got, err := svc.GetBetaPolicySettings(context.Background())
	if err != nil {
		t.Fatalf("GetBetaPolicySettings after set: %v", err)
	}
	if len(got.Rules) != 1 || got.Rules[0].BetaToken != "new-beta" {
		t.Fatalf("unexpected cached settings after set: %+v", got)
	}
}
