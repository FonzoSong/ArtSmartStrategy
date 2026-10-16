package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
	"github.com/arts-smart/strategy/internal/service"
)

func parseScheduleRequest(t *testing.T, body string, teamID int64) (scheduleRecommendationRequest, int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/ai/recommend", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-Id", "12")
	input, status, code, _ := decodeScheduleRecommendationRequest(httptest.NewRecorder(), req, teamID)
	return input, status, code
}

func TestDecodeScheduleRecommendationRequestRequiresJSONBodyAndProjectScope(t *testing.T) {
	for _, body := range []string{"", "{"} {
		_, status, code := parseScheduleRequest(t, body, 12)
		if status != 400 || code != "invalid_json" {
			t.Fatalf("body %q: status/code = %d/%s", body, status, code)
		}
	}
	_, status, code := parseScheduleRequest(t, `{"contentIds":[]}`, 12)
	if status != 400 || code != "project_id_required" {
		t.Fatalf("missing project scope: status/code = %d/%s", status, code)
	}
	_, status, code = parseScheduleRequest(t, `{"projectId":13,"contentIds":[]}`, 12)
	if status != 403 || code != "project_forbidden" {
		t.Fatalf("cross-team scope: status/code = %d/%s", status, code)
	}
}

func TestGenerateRecommendationsRejectsCapturedZeroLengthBodyBeforeDatabaseAccess(t *testing.T) {
	server := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest("POST", "/api/ai/recommend", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-Id", "12")
	req = req.WithContext(context.WithValue(req.Context(), claimsKey{}, domain.Claims{TeamID: 12}))
	response := httptest.NewRecorder()
	server.generateRecommendations(response, req)
	if response.Code != 400 {
		t.Fatalf("zero-length request status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "invalid_json" {
		t.Fatalf("zero-length request error = %#v, decode err=%v", body, err)
	}
}

func TestDecodeScheduleRecommendationRequestAcceptsFrontendContract(t *testing.T) {
	input, status, code := parseScheduleRequest(t, `{"projectId":12,"contentIds":["asset-1","asset-2"],"strategy":"balanced","performanceId":"show-7","constraints":{"startDate":"2026-10-17","workingDays":[1,2,3,4,5],"dailyCapacityHours":8,"includeWeekends":false}}`, 12)
	if status != 0 {
		t.Fatalf("status/code = %d/%s", status, code)
	}
	if input.ProjectID != 12 || input.Strategy != "balanced" || input.PerformanceID != "show-7" || len(input.ContentIDs) != 2 || len(input.Constraints.WorkingDays) != 5 || input.Constraints.DailyCapacityHours != 8 || input.Constraints.IncludeWeekends == nil || *input.Constraints.IncludeWeekends {
		t.Fatalf("decoded request = %#v", input)
	}
}

func TestDecodeScheduleRecommendationRequestRejectsInvalidIDsAndConstraints(t *testing.T) {
	for _, body := range []string{
		`{"projectId":12,"contentIds":["a","a"]}`,
		`{"projectId":12,"contentIds":[""]}`,
		`{"projectId":12,"strategy":"unknown"}`,
		`{"projectId":12,"constraints":{"startDate":"17-10-2026"}}`,
		`{"projectId":12,"constraints":{"workingDays":[1,1]}}`,
		`{"projectId":12,"constraints":{"dailyCapacityHours":25}}`,
		`{"projectId":12,"notAllowed":true}`,
	} {
		_, status, code := parseScheduleRequest(t, body, 12)
		if status != 400 || code == "" {
			t.Errorf("body %s: status/code = %d/%s", body, status, code)
		}
	}
}

func TestDecodeScheduleRecommendationRequestRequiresJSONContentType(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/ai/recommend", strings.NewReader(`{"projectId":12}`))
	req.Header.Set("X-Team-Id", "12")
	_, status, code, _ := decodeScheduleRecommendationRequest(httptest.NewRecorder(), req, 12)
	if status != 415 || code != "content_type_required" {
		t.Fatalf("status/code = %d/%s", status, code)
	}
}

func TestDecodeScheduleRecommendationRequestRejectsMismatchedTeamHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/ai/recommend", strings.NewReader(`{"projectId":12}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-Id", "13")
	_, status, code, _ := decodeScheduleRecommendationRequest(httptest.NewRecorder(), req, 12)
	if status != 403 || code != "team_forbidden" {
		t.Fatalf("status/code = %d/%s", status, code)
	}
}

func TestApplyScheduleRecommendationConstraintsOnlyNarrowsTeamPolicy(t *testing.T) {
	start := time.Date(2026, 10, 17, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	window, ok := domain.HorizonWindow(start, 7)
	if !ok {
		t.Fatal("expected valid policy window")
	}
	weekdays := []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}
	policy := service.SlotPolicy{HorizonDays: 7, Weekdays: weekdays, MaxPerDay: 2}
	includeWeekends := false
	constrained, constrainedWindow, planningNow, err := applyScheduleRecommendationConstraints(policy, window, start.Add(9*time.Hour), service.AIRecommendationConstraints{
		StartDate:       "2026-10-19",
		WorkingDays:     []int{1, 2, 3, 4, 5},
		IncludeWeekends: &includeWeekends,
	})
	if err != nil {
		t.Fatal(err)
	}
	if constrainedWindow.Start.Format("2006-01-02") != "2026-10-19" || constrainedWindow.Days() != 5 || constrained.HorizonDays != 5 || planningNow.Format("2006-01-02") != "2026-10-19" {
		t.Fatalf("constrained window/policy/time = %#v / %#v / %s", constrainedWindow, constrained, planningNow)
	}
	if len(constrained.Weekdays) != 5 || constrained.MaxPerDay != policy.MaxPerDay {
		t.Fatalf("constraints widened or changed the team policy: %#v", constrained)
	}
	if _, _, _, err = applyScheduleRecommendationConstraints(policy, window, start, service.AIRecommendationConstraints{StartDate: "2026-11-01"}); err == nil {
		t.Fatal("a start date outside the team window must be rejected")
	}
}
