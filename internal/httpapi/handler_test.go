package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/auth"
	"lightgovernance/internal/config"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/httpapi"
	"lightgovernance/internal/repo"
	"lightgovernance/internal/scheduler"
	"lightgovernance/internal/service"
)

func setupHTTPTest(t *testing.T) (*httptest.Server, *repo.Store, *clock.Mock) {
	t.Helper()
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := repo.New(ctx, dir, clk, 1024*1024, true)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	ruleSvc := service.NewRuleService(st, clk)
	_, err = ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name:           "default-rule",
		LeadDepartment: "general-bureau",
		IsDefault:      true,
		CreatedBy:      "test",
	})
	require.NoError(t, err)

	cfg := config.Defaults()
	cfg.Auth.BootstrapUsers = []config.AuthBootstrapUser{
		{ID: "u-admin", Username: "admin", Password: "test-admin-password", Role: string(auth.RoleAdmin)},
		{ID: "u-patrol", Username: "lighting_assessor", Password: "test-patrol-password", Role: string(auth.RoleLightingAssessor)},
		{ID: "u-emergency", Username: "urban_management_reviewer", Password: "test-emergency-password", Role: string(auth.RoleUrbanManagementReviewer)},
	}
	cfg.Storage.DataDir = dir
	cfg.Auth.Required = false
	logger := applog.New("error", "json")
	httpSrv := httpapi.New(cfg, st, clk, logger, nil)
	sched := scheduler.New(clk, httpSrv.EscSvc(), st, cfg.Scheduler, logger)
	require.NoError(t, sched.Start(context.Background()))
	httpSrv.SetScheduler(sched)
	require.NoError(t, httpSrv.ReevalWorker().Start(context.Background()))
	t.Cleanup(func() { httpSrv.ReevalWorker().Stop() })
	t.Cleanup(func() { sched.Stop() })

	ts := httptest.NewServer(httpSrv.Handler())
	t.Cleanup(func() { ts.Close() })
	return ts, st, clk
}

func postJSON(t *testing.T, ts *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()
	return data
}

func TestHandler_RegisterItem(t *testing.T) {
	ts, _, _ := setupHTTPTest(t)

	resp := postJSON(t, ts, "/api/items",
		`{"external_ref":"REF-HTTP-001","title":"HTTP Test","reported_by":"user1"}`)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var item domain.ComplianceCase
	require.NoError(t, json.Unmarshal(readBody(t, resp), &item))
	assert.Equal(t, "HTTP Test", item.Title)
	assert.Equal(t, domain.StatusAdjudicated, item.Status)
	assert.Equal(t, "general-bureau", item.LeadDepartment)
}

func TestHandler_HealthzReady(t *testing.T) {
	ts, _, _ := setupHTTPTest(t)

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp2, err := http.Get(ts.URL + "/readyz")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var readyz map[string]any
	require.NoError(t, json.Unmarshal(readBody(t, resp2), &readyz))
	assert.Equal(t, "ready", readyz["status"])
}

func TestHandler_AuthLoginMeLogoutLifecycle(t *testing.T) {
	ts, _, _ := setupHTTPTest(t)
	loginResp := postJSON(t, ts, "/api/auth/login", `{"username":"lighting_assessor","password":"test-patrol-password"}`)
	require.Equal(t, http.StatusOK, loginResp.StatusCode)
	var login struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(readBody(t, loginResp), &login))
	require.NotEmpty(t, login.Token)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	meResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, meResp.StatusCode)
	meResp.Body.Close()

	logoutReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/logout", nil)
	require.NoError(t, err)
	logoutReq.Header.Set("Authorization", "Bearer "+login.Token)
	logoutResp, err := http.DefaultClient.Do(logoutReq)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, logoutResp.StatusCode)
	logoutResp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	meResp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, meResp.StatusCode)
	meResp.Body.Close()
}

func TestHandler_ListItemsPagination(t *testing.T) {
	ts, _, _ := setupHTTPTest(t)

	for i := 0; i < 25; i++ {
		body := fmt.Sprintf(`{"external_ref":"REF-PAGE-%d","title":"Page ComplianceCase %d","reported_by":"user1"}`, i, i)
		resp := postJSON(t, ts, "/api/items", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		resp.Body.Close()
	}

	resp, err := http.Get(ts.URL + "/api/items?page_size=10&page_offset=0")
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(readBody(t, resp), &result))
	assert.Equal(t, float64(25), result["total"])
	items := result["items"].([]any)
	assert.Len(t, items, 10)

	resp2, err := http.Get(ts.URL + "/api/items?page_size=10&page_offset=10")
	require.NoError(t, err)
	var result2 map[string]any
	require.NoError(t, json.Unmarshal(readBody(t, resp2), &result2))
	items2 := result2["items"].([]any)
	assert.Len(t, items2, 10)
	resp2.Body.Close()
}

func TestHandler_InvalidTransitionHTTP(t *testing.T) {
	ts, _, _ := setupHTTPTest(t)

	resp := postJSON(t, ts, "/api/items",
		`{"external_ref":"REF-HTTP-TRANS","title":"Transition HTTP","reported_by":"user1"}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var item domain.ComplianceCase
	json.Unmarshal(readBody(t, resp), &item)

	startResp, err := http.Post(ts.URL+"/api/items/"+item.ID+"/start?actor=u1", "application/json", bytes.NewReader([]byte("{}")))
	require.NoError(t, err)
	startResp.Body.Close()

	completeResp, err := http.Post(ts.URL+"/api/items/"+item.ID+"/complete?actor=u1", "application/json", bytes.NewReader([]byte("{}")))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, completeResp.StatusCode)
	completeResp.Body.Close()

	badResp, err := http.Post(ts.URL+"/api/items/"+item.ID+"/start?actor=u1", "application/json", bytes.NewReader([]byte("{}")))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, badResp.StatusCode)

	var errResp map[string]any
	json.Unmarshal(readBody(t, badResp), &errResp)
	errBody := errResp["error"].(map[string]any)
	assert.Equal(t, "INVALID_TRANSITION", errBody["code"])
}

func TestHandler_GetItemDetail(t *testing.T) {
	ts, _, _ := setupHTTPTest(t)

	resp := postJSON(t, ts, "/api/items",
		`{"external_ref":"REF-DETAdviceAIL-001","title":"Detail Test","reported_by":"user1"}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var item domain.ComplianceCase
	json.Unmarshal(readBody(t, resp), &item)

	detailResp, err := http.Get(ts.URL + "/api/items/" + item.ID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, detailResp.StatusCode)

	var detail map[string]any
	json.Unmarshal(readBody(t, detailResp), &detail)
	assert.NotNil(t, detail["item"])
	assert.NotNil(t, detail["assignments"])
}

func TestHandler_Backlog(t *testing.T) {
	ts, _, _ := setupHTTPTest(t)

	for i := 0; i < 3; i++ {
		body := fmt.Sprintf(`{"external_ref":"REF-BL-%d","title":"Backlog %d","reported_by":"user1"}`, i, i)
		resp := postJSON(t, ts, "/api/items", body)
		resp.Body.Close()
	}

	resp, err := http.Get(ts.URL + "/api/stats/backlog")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var stats map[string]any
	json.Unmarshal(readBody(t, resp), &stats)
	assert.NotNil(t, stats["status_counts"])
}

func TestHandler_EscalationViaScheduler(t *testing.T) {
	ts, st, clk := setupHTTPTest(t)

	resp := postJSON(t, ts, "/api/items",
		`{"external_ref":"REF-ESC-HTTP","title":"Escalation HTTP","reported_by":"user1"}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var item domain.ComplianceCase
	json.Unmarshal(readBody(t, resp), &item)

	startResp, err := http.Post(ts.URL+"/api/items/"+item.ID+"/start?actor=u1", "application/json", bytes.NewReader([]byte("{}")))
	require.NoError(t, err)
	startResp.Body.Close()

	clk.Add(73 * time.Hour)

	escSvc := service.NewEscalationService(st, clk, 48*time.Hour, 3)
	_, err = escSvc.CheckAndEscalate(context.Background())
	require.NoError(t, err)

	detailResp, err := http.Get(ts.URL + "/api/items/" + item.ID)
	require.NoError(t, err)
	var detail map[string]any
	json.Unmarshal(readBody(t, detailResp), &detail)
	itemMap := detail["item"].(map[string]any)
	assert.Equal(t, string(domain.StatusEscalated), itemMap["status"])
}
