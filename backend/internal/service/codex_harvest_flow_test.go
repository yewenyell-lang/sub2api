package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/mihomo"
	"github.com/stretchr/testify/require"
)

func TestHarvestFlowResolvesSubscriptionNameWithoutChangingEventID(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"node_names":{"node-test":"日本 东京 01"}}`), 0600))
	m := mihomo.New(dir)
	t.Cleanup(m.Close)
	recordCodexHarvestNode("node-test", "LoadBalance", 1)
	snapshot := BuildCodexHarvestFlow(context.Background(), &config.Config{Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{HarvestProxyURL: mihomo.Endpoint}}}, nil, nil)
	require.Equal(t, "node-test", snapshot.Events[0].Node)
	require.Equal(t, "日本 东京 01", snapshot.Events[0].NodeName)
	require.Equal(t, "node-test", snapshot.Stages[0].Node)
	require.Equal(t, "日本 东京 01", snapshot.Stages[0].NodeName)
}

func TestExternalHarvestProxyDoesNotQueryOrInheritSidecar(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)
	var queries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		queries.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	sidecarControllerCache.Store(&cachedSidecarController{until: time.Now().Add(time.Hour), controller: server.URL})
	t.Cleanup(func() { sidecarControllerCache.Store(nil) })
	recordCodexHarvestNode("previous-mihomo-node", "LoadBalance", 1)
	for _, proxy := range []string{"http://user:password@residential.example:8080", ""} {
		snapshot := BuildCodexHarvestFlow(context.Background(), &config.Config{Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{HarvestProxyURL: proxy}}}, nil, nil)
		require.Empty(t, snapshot.Sidecar.Error)
		require.Empty(t, snapshot.Sidecar.Now)
		require.Empty(t, snapshot.Stages[0].Node)
		require.NotEqual(t, "fail", snapshot.Stages[0].Status)
		require.False(t, snapshot.Sidecar.Reachable, "configuration is not a health probe")
		require.Empty(t, watchCodexHarvestExit(proxy)())
		if proxy == "" {
			require.Equal(t, "unconfigured", snapshot.Sidecar.Mode)
		} else {
			require.Equal(t, "external", snapshot.Sidecar.Mode)
		}
	}
	require.Zero(t, queries.Load())
	local := observeCodexHarvestProxy(context.Background(), mihomo.Endpoint)
	require.Equal(t, "mihomo", local.Mode)
	require.NotEmpty(t, local.Error, "a selected but failing local sidecar still reports its error")
	require.Positive(t, queries.Load())
}

func TestCodexHarvestFlowRecordsProbeTicketAndSelect(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)

	account := ticketTestAccount(2)
	account.Name = "20x"
	recordCodexHarvestNode("airport-node-1", "load-balance", 340)
	recordCodexHarvestProbe(account, "gpt-6-astra", "invalid_state", lastCodexHarvestNode(), "", 200, 312, 11, 292, 10)
	recordCodexHarvestTicketReject(account, "gpt-6-astra", "response_mismatch", 312, 11)
	recordCodexHarvestSelect(account, "gpt-6-astra", "skip", "ticket_unavailable", "")
	recordCodexHarvestSelect(account, "gpt-6-astra", "skip", "ticket_unavailable", "")

	events := listCodexHarvestFlowEvents()
	require.Len(t, events, 4)
	require.Equal(t, "node", events[0].Stage)
	require.Equal(t, "airport-node-1", events[0].Node)
	require.Equal(t, "probe_miss", events[1].Kind)
	require.Equal(t, 312, events[1].Length)
	require.Equal(t, 292, events[1].ExpectedLength)
	require.Equal(t, "reject", events[2].Kind)
	require.Equal(t, "skip", events[3].Kind)
}

func TestBuildCodexHarvestFlowSnapshotAndStages(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)

	account := ticketTestAccount(2)
	account.Name = "20x"
	account.Extra = map[string]any{
		openAICodexTicketExtraKey("gpt-6-astra"): &openAICodexTicket{
			AccountID:  2,
			Model:      "gpt-6-astra",
			State:      fakeCodexTicketState(292),
			Length:     292,
			Blocks:     10,
			ExpiresAt:  time.Now().Add(time.Hour),
			CapturedAt: time.Now(),
			Identity:   ticketIdentity(account),
		},
	}
	recordCodexHarvestProbe(account, "gpt-6-astra", "success", "airport-node-2", "", 200, 292, 10, 292, 10)
	recordCodexHarvestTicketStore(account, &openAICodexTicket{Model: "gpt-6-astra", Length: 292, Blocks: 10}, false)
	recordCodexHarvestSelect(account, "gpt-6-astra", "selected", "", "")

	snapshot := BuildCodexHarvestFlow(context.Background(), &config.Config{
		Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{
			Enabled:      true,
			FailClosed:   true,
			TargetLength: 292,
			Models:       []string{"gpt-6-astra", "gpt-5.6-sol"},
		}},
	}, nil, []Account{*account})

	require.True(t, snapshot.Harvest.Enabled)
	require.True(t, snapshot.Harvest.FailClosed)
	require.Equal(t, 292, snapshot.Harvest.TargetLength)
	require.Len(t, snapshot.Accounts, 1)
	require.Equal(t, 1, snapshot.Accounts[0].ReadyCount)
	require.False(t, snapshot.Accounts[0].SkipHarvest)
	require.True(t, snapshot.Accounts[0].InScope)
	require.Equal(t, 1, snapshot.Counts.TicketsReady)
	require.Equal(t, 1, snapshot.Counts.ProbeHit)
	require.Equal(t, 1, snapshot.Counts.TicketAccept)
	require.Equal(t, 1, snapshot.Counts.SelectOK)
	require.Len(t, snapshot.Stages, 5)
	require.Equal(t, "probe", snapshot.Stages[1].ID)
	require.Equal(t, "ok", snapshot.Stages[1].Status)
	require.Equal(t, "shape", snapshot.Stages[2].ID)
	require.Equal(t, "ok", snapshot.Stages[2].Status)
	require.Equal(t, 292, snapshot.Stages[2].Length)
	require.Equal(t, 10, snapshot.Stages[2].Blocks)
	require.Equal(t, 292, snapshot.Stages[2].ExpectedLength)
	var kinds []string
	for _, event := range snapshot.Events {
		kinds = append(kinds, event.Kind)
	}
	require.Contains(t, kinds, "selected")
}

func TestBuildCodexHarvestFlowSkipHarvestAccount(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)

	account := ticketTestAccount(5)
	account.Name = "5x"
	account.Extra = map[string]any{
		OpenAICodexSkipHarvestExtraKey: true,
		openAICodexTicketExtraKey("gpt-6-astra"): &openAICodexTicket{
			AccountID:  5,
			Model:      "gpt-6-astra",
			State:      fakeCodexTicketState(292),
			Length:     292,
			Blocks:     10,
			ExpiresAt:  time.Now().Add(time.Hour),
			CapturedAt: time.Now(),
			Identity:   ticketIdentity(account),
		},
	}
	snapshot := BuildCodexHarvestFlow(context.Background(), &config.Config{
		Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{
			Enabled:      true,
			FailClosed:   true,
			TargetLength: 292,
			Models:       []string{"gpt-6-astra", "gpt-5.6-sol"},
		}},
	}, nil, []Account{*account})
	require.Len(t, snapshot.Accounts, 1)
	require.True(t, snapshot.Accounts[0].SkipHarvest)
	require.False(t, snapshot.Accounts[0].InScope)
	require.Equal(t, 1, snapshot.Accounts[0].ReadyCount)
}

func TestClipFlowTextKeepsRunes(t *testing.T) {
	require.Equal(t, "账号名称", clipFlowText("账号名称", 80))
	require.Equal(t, "账号", clipFlowText("账号名称", 2))
	require.Empty(t, clipFlowText("abc", 0))
}

func TestBuildCodexHarvestFlowAppliesRuntimeEnabled(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)
	account := ticketTestAccount(2)
	account.Name = "20x"
	account.Extra = map[string]any{
		openAICodexTicketExtraKey("gpt-6-astra"): &openAICodexTicket{
			AccountID:  2,
			Model:      "gpt-6-astra",
			State:      fakeCodexTicketState(292),
			Length:     292,
			Blocks:     10,
			ExpiresAt:  time.Now().Add(time.Hour),
			CapturedAt: time.Now(),
			Identity:   ticketIdentity(account),
		},
	}
	disabled := BuildCodexHarvestFlow(context.Background(), &config.Config{
		Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{
			Enabled:      false,
			FailClosed:   true,
			TargetLength: 292,
			Models:       []string{"gpt-6-astra", "gpt-5.6-sol"},
		}},
	}, nil, []Account{*account})
	require.True(t, disabled.Harvest.FailClosed)
	require.False(t, disabled.Harvest.Enabled)
	require.Len(t, disabled.Accounts, 1)
	require.Empty(t, disabled.Accounts[0].Tickets)

	enabled := BuildCodexHarvestFlow(context.Background(), &config.Config{
		Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{
			Enabled:      true,
			FailClosed:   true,
			TargetLength: 292,
			Models:       []string{"gpt-6-astra", "gpt-5.6-sol"},
		}},
	}, nil, []Account{*account})
	require.True(t, enabled.Harvest.Enabled)
	require.NotEmpty(t, enabled.Accounts[0].Tickets)
	require.Equal(t, 1, enabled.Accounts[0].ReadyCount)
}

func TestCodexHarvestFlowNodeStageForLoadBalance(t *testing.T) {
	stages := buildCodexHarvestFlowStages(CodexHarvestFlowSnapshot{
		Sidecar: CodexHarvestFlowSidecar{Reachable: true, Type: "LoadBalance", AllCount: 340},
	})
	require.Equal(t, "node", stages[0].ID)
	require.Equal(t, "ok", stages[0].Status)
	require.Contains(t, stages[0].Detail, "340")
}

func TestExitNodeFromConnectionsPrefersLatestChain(t *testing.T) {
	body := []byte(`{"connections":[
		{"start":"2026-09-20T01:00:00Z","chains":["old-node","CODEX-ROTATE"]},
		{"start":"2026-09-20T01:01:00Z","chains":["a1-pxsn - us","CODEX-ROTATE"]}
	]}`)
	require.Equal(t, "a1-pxsn - us", exitNodeFromConnections(body))
	require.Equal(t, "airport-1", nodeFromChains([]string{"airport-1", "CODEX-ROTATE"}))
	require.Empty(t, nodeFromChains([]string{"CODEX-ROTATE"}))
}

func TestCodexHarvestSelectFailedDebounces(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)
	recordCodexHarvestSelect(nil, "gpt-6-astra", "failed", "unavailable", "no available OpenAI accounts")
	recordCodexHarvestSelect(nil, "gpt-6-astra", "failed", "unavailable", "no available OpenAI accounts")
	require.Len(t, listCodexHarvestFlowEvents(), 1)
}

func TestShapeStageKeeps292AfterUnauthorizedProbe(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)
	account := ticketTestAccount(2)
	account.Name = "20x"
	recordCodexHarvestProbe(account, "gpt-6-astra", "success", "japan-08", "", 200, 292, 10, 292, 10)
	dead := ticketTestAccount(3)
	dead.Name = "5x"
	recordCodexHarvestProbe(dead, "gpt-6-astra", "response_incomplete_or_error", "japan-08", "", 401, 0, 0, 292, 10)

	snapshot := BuildCodexHarvestFlow(context.Background(), &config.Config{
		Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{
			Enabled:      true,
			FailClosed:   true,
			TargetLength: 292,
			Models:       []string{"gpt-6-astra", "gpt-5.6-sol"},
		}},
	}, nil, []Account{*account})
	require.Equal(t, "warn", snapshot.Stages[1].Status)
	require.Equal(t, "shape", snapshot.Stages[2].ID)
	require.Equal(t, "ok", snapshot.Stages[2].Status)
	require.Equal(t, 292, snapshot.Stages[2].Length)
}

func TestShapeStageFailsOnWrongTicketLength(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)
	account := ticketTestAccount(2)
	recordCodexHarvestProbe(account, "gpt-6-astra", "invalid_state", "japan-08", "", 200, 312, 11, 292, 10)
	stages := buildCodexHarvestFlowStages(CodexHarvestFlowSnapshot{Events: listCodexHarvestFlowEvents()})
	require.Equal(t, "fail", stages[2].Status)
	require.Contains(t, stages[2].Detail, "312")
}

func TestRecordCodexHarvestNodeReusesPoolSize(t *testing.T) {
	resetCodexHarvestFlow()
	t.Cleanup(resetCodexHarvestFlow)
	recordCodexHarvestNode("japan-07", "LoadBalance", 340)
	recordCodexHarvestNode("japan-08", "", 0)
	events := listCodexHarvestFlowEvents()
	require.Len(t, events, 2)
	require.Equal(t, "pool=340", events[1].Detail)
	require.Equal(t, "LoadBalance", events[1].Result)
}

func TestCodexHarvestFlowReadsManagedAndLegacyConfig(t *testing.T) {
	for _, name := range []string{"candidate.json", "config.yaml"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("DATA_DIR", dir)
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "mihomo-codex"), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "mihomo-codex", name), []byte(`{"external-controller":"127.0.0.1:9098","secret":"fixture-only"}`), 0600))
			sidecarControllerCache.Store(nil)
			t.Cleanup(func() { sidecarControllerCache.Store(nil) })
			got := loadCodexHarvestSidecarController()
			require.Equal(t, "http://127.0.0.1:9098", got.controller)
			require.Equal(t, "fixture-only", got.secret)
			require.Empty(t, got.err)
		})
	}
}
