package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestTicketStandbyFeedbackDoesNotRevokeReplacement(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}}, nil)
	account := ticketTestAccount(41)
	makeTicket := func(state string) *openAICodexTicket {
		return &openAICodexTicket{Model: "gpt-6-astra", State: state, Length: 292, CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	}
	primary := fakeCodexTicketState(292)
	backup := primary[:len(primary)-4] + "BBBB"
	svc.storeOpenAICodexTicket(context.Background(), account, makeTicket(primary))
	svc.storeOpenAICodexTicket(context.Background(), account, makeTicket(backup))
	got := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	require.Equal(t, primary, got.State)
	require.Equal(t, backup, got.Standby.State)
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	req.Header.Set(openAICodexTurnStateHeader, primary)
	resp := &http.Response{StatusCode: 200, Header: http.Header{}}
	resp.Header.Set(openAICodexTurnStateHeader, "malformed")
	svc.observeCodexTicketResponse(req, resp, account)
	require.Equal(t, backup, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").State)
	svc.observeCodexTicketResponse(req, resp, account)
	require.Equal(t, backup, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").State)
	account.Credentials["chatgpt_account_id"] = "different-account"
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestTicket429DoesNotInvalidate(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}}, nil)
	account := ticketTestAccount(41)
	state := fakeCodexTicketState(292)
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{Model: "gpt-6-astra", State: state, Length: 292, ExpiresAt: time.Now().Add(time.Hour)})
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	req.Header.Set(openAICodexTurnStateHeader, state)
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	resp.Header.Set(openAICodexTurnStateHeader, "bad")
	svc.observeCodexTicketResponse(req, resp, account)
	require.True(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").valid(time.Now(), 292))
}

func TestTicketStandbyRecoveredAfterRestart(t *testing.T) {
	account := ticketTestAccount(41)
	state := fakeCodexTicketState(292)
	account.Extra = map[string]any{openAICodexTicketExtraKey("gpt-6-astra"): &openAICodexTicket{
		State: state, Length: 292, ExpiresAt: time.Now().Add(-time.Second), Identity: ticketIdentity(account),
		Standby: &openAICodexTicket{State: state, Length: 292, Identity: ticketIdentity(account), ExpiresAt: time.Now().Add(time.Hour)},
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	require.True(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").valid(time.Now(), 292))
	statuses := OpenAICodexTicketStatuses(account, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}}, time.Now())
	require.True(t, statuses[0].Ready)
}
