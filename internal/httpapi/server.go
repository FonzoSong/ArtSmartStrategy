// Package httpapi exposes the versioned HTTP API and its security boundary.
package httpapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/config"
	"github.com/arts-smart/strategy/internal/domain"
	"github.com/arts-smart/strategy/internal/report"
	"github.com/arts-smart/strategy/internal/repository"
	"github.com/arts-smart/strategy/internal/service"
	"github.com/google/uuid"
)

type claimsKey struct{}

// Server binds authenticated business handlers to PostgreSQL repositories.
type Server struct {
	Config config.Config
	Store  *repository.Postgres
	Auth   service.AuthService
	Log    *slog.Logger
}

// New creates the router with explicit CORS, timeout, and request logging.
func New(cfg config.Config, store *repository.Postgres, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	signer := service.TokenSigner{Secret: []byte(cfg.AppSecret)}
	server := &Server{
		Config: cfg,
		Store:  store,
		Auth:   service.AuthService{Store: store, Signer: signer},
		Log:    logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health/live", server.live)
	mux.HandleFunc("GET /api/health/ready", server.ready)
	mux.HandleFunc("GET /api/health", server.ready)
	mux.HandleFunc("GET /api/runtime-mode", server.runtimeMode)
	mux.HandleFunc("GET /api/auth/status", server.authStatus)
	mux.HandleFunc("POST /api/auth/setup", server.setup)
	mux.HandleFunc("POST /api/auth/register-team", server.register)
	mux.HandleFunc("POST /api/auth/login", server.login)
	mux.Handle("GET /api/auth/me", server.authenticated(http.HandlerFunc(server.me)))
	mux.Handle("POST /api/auth/logout", server.authenticated(http.HandlerFunc(server.logout)))
	mux.Handle("GET /api/contents", server.authenticated(http.HandlerFunc(server.contents)))
	mux.Handle("POST /api/contents", server.authenticated(server.editors(http.HandlerFunc(server.createContent))))
	mux.Handle("GET /api/contents/{id}", server.authenticated(http.HandlerFunc(server.content)))
	mux.Handle("GET /api/contents/{id}/workflow-events", server.authenticated(http.HandlerFunc(server.contentWorkflowEvents)))
	mux.Handle("PATCH /api/contents/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateContent))))
	mux.Handle("DELETE /api/contents/{id}", server.authenticated(server.editors(http.HandlerFunc(server.deleteContent))))
	mux.Handle("POST /api/contents/{id}/advance", server.authenticated(server.editors(http.HandlerFunc(server.advanceContent))))
	mux.Handle("POST /api/contents/advance-bulk", server.authenticated(server.editors(http.HandlerFunc(server.advanceBulkContents))))
	mux.Handle("POST /api/contents/bulk", server.authenticated(server.editors(http.HandlerFunc(server.bulkContents))))
	mux.Handle("GET /api/teams", server.authenticated(http.HandlerFunc(server.teams)))
	mux.Handle("GET /api/users", server.authenticated(http.HandlerFunc(server.users)))
	mux.Handle("POST /api/users", server.authenticated(server.owners(http.HandlerFunc(server.addUser))))
	mux.Handle("GET /api/performances", server.authenticated(http.HandlerFunc(server.performances)))
	mux.Handle("POST /api/performances", server.authenticated(server.editors(http.HandlerFunc(server.createPerformance))))
	mux.Handle("GET /api/performances/{id}", server.authenticated(http.HandlerFunc(server.performanceDetail)))
	mux.Handle("PATCH /api/performances/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updatePerformance))))
	mux.Handle("DELETE /api/performances/{id}", server.authenticated(server.editors(http.HandlerFunc(server.deletePerformance))))
	mux.Handle("POST /api/performances/{id}/plan", server.authenticated(server.editors(http.HandlerFunc(server.planPerformance))))
	mux.Handle("GET /api/tags", server.authenticated(http.HandlerFunc(server.tags)))
	mux.Handle("POST /api/tags", server.authenticated(server.editors(http.HandlerFunc(server.createTag))))
	mux.Handle("PATCH /api/tags/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateTag))))
	mux.Handle("DELETE /api/tags/{id}", server.authenticated(server.editors(http.HandlerFunc(server.deleteTag))))
	mux.Handle("POST /api/tags/{id}/merge", server.authenticated(server.editors(http.HandlerFunc(server.mergeTag))))
	mux.Handle("GET /api/settings", server.authenticated(http.HandlerFunc(server.settings)))
	mux.Handle("PUT /api/settings", server.authenticated(server.owners(http.HandlerFunc(server.updateSettings))))
	mux.Handle("GET /api/ai/configs", server.authenticated(http.HandlerFunc(server.aiConfigs)))
	mux.Handle("POST /api/ai/configs", server.authenticated(server.owners(http.HandlerFunc(server.createAIConfig))))
	mux.Handle("POST /api/ai/configs/{id}/activate", server.authenticated(server.owners(http.HandlerFunc(server.activateAIConfig))))
	mux.Handle("DELETE /api/ai/configs/{id}", server.authenticated(server.owners(http.HandlerFunc(server.deleteAIConfig))))
	mux.Handle("POST /api/ai/test", server.authenticated(server.editors(http.HandlerFunc(server.testAI))))
	mux.Handle("POST /api/ai/insight", server.authenticated(http.HandlerFunc(server.insight)))
	mux.Handle("POST /api/settings/sample-threshold", server.authenticated(server.owners(http.HandlerFunc(server.sampleThreshold))))
	mux.Handle("GET /api/rules", server.authenticated(http.HandlerFunc(server.rules)))
	mux.Handle("GET /api/rules/system", server.authenticated(http.HandlerFunc(server.systemRules)))
	mux.Handle("GET /api/rules/sets", server.authenticated(http.HandlerFunc(server.ruleSets)))
	mux.Handle("POST /api/rules/sets", server.authenticated(server.editors(http.HandlerFunc(server.createRuleSet))))
	mux.Handle("PATCH /api/rules/sets/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateRuleSet))))
	mux.Handle("DELETE /api/rules/sets/{id}", server.authenticated(server.editors(http.HandlerFunc(server.deleteRuleSet))))
	mux.Handle("PUT /api/rules/effective", server.authenticated(server.editors(http.HandlerFunc(server.setEffectiveRuleSet))))
	mux.Handle("POST /api/rules/sets/{id}/items", server.authenticated(server.editors(http.HandlerFunc(server.addRuleSetItem))))
	mux.Handle("PATCH /api/rules/sets/{id}/items/{itemId}", server.authenticated(server.editors(http.HandlerFunc(server.updateRuleSetItem))))
	mux.Handle("DELETE /api/rules/sets/{id}/items/{itemId}", server.authenticated(server.editors(http.HandlerFunc(server.deleteRuleSetItem))))
	mux.Handle("POST /api/rules/sets/preview", server.authenticated(server.editors(http.HandlerFunc(server.previewRuleSetItem))))
	mux.Handle("POST /api/rules", server.authenticated(server.editors(http.HandlerFunc(server.createRule))))
	mux.Handle("POST /api/rules/preview", server.authenticated(server.editors(http.HandlerFunc(server.previewRule))))
	mux.Handle("POST /api/rules/evaluate", server.authenticated(server.editors(http.HandlerFunc(server.evaluateRules))))
	mux.Handle("GET /api/rules/logs", server.authenticated(http.HandlerFunc(server.ruleLogs)))
	mux.Handle("PATCH /api/rules/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateRule))))
	mux.Handle("DELETE /api/rules/{id}", server.authenticated(server.editors(http.HandlerFunc(server.deleteRule))))
	mux.Handle("PATCH /api/teams/{id}", server.authenticated(server.owners(http.HandlerFunc(server.renameTeam))))
	mux.Handle("GET /api/stats/overview", server.authenticated(http.HandlerFunc(server.overview)))
	mux.Handle("GET /api/stats/dashboard", server.authenticated(http.HandlerFunc(server.dashboardStats)))
	mux.Handle("GET /api/stats/platform", server.authenticated(http.HandlerFunc(server.platformStats)))
	mux.Handle("GET /api/stats/trend", server.authenticated(http.HandlerFunc(server.trendStats)))
	mux.Handle("GET /api/stats/category", server.authenticated(http.HandlerFunc(server.categoryStats)))
	mux.Handle("GET /api/stats/heatmap", server.authenticated(http.HandlerFunc(server.heatmap)))
	mux.Handle("GET /api/stats/adoption", server.authenticated(http.HandlerFunc(server.adoptionStats)))
	mux.Handle("GET /api/stats/funnel", server.authenticated(http.HandlerFunc(server.funnelStats)))
	mux.Handle("GET /api/stats/diagnostics", server.authenticated(http.HandlerFunc(server.diagnostics)))
	mux.Handle("GET /api/stats/performances", server.authenticated(http.HandlerFunc(server.performanceComparison)))
	mux.Handle("GET /api/benchmark", server.authenticated(http.HandlerFunc(server.benchmarks)))
	mux.Handle("PUT /api/benchmark", server.authenticated(server.editors(http.HandlerFunc(server.updateBenchmark))))
	mux.Handle("GET /api/export/contents", server.authenticated(http.HandlerFunc(server.exportContentsCSV)))
	mux.Handle("GET /api/export/anonymized", server.authenticated(http.HandlerFunc(server.exportAnonymizedCSV)))
	mux.Handle("GET /api/schedule/export", server.authenticated(http.HandlerFunc(server.exportScheduleCSV)))
	mux.Handle("GET /api/schedule/adoption-gains", server.authenticated(http.HandlerFunc(server.adoptionGains)))
	mux.Handle("GET /api/audit", server.authenticated(http.HandlerFunc(server.audit)))
	mux.Handle("GET /api/hypotheses", server.authenticated(http.HandlerFunc(server.hypotheses)))
	mux.Handle("POST /api/hypotheses", server.authenticated(server.editors(http.HandlerFunc(server.createHypothesis))))
	mux.Handle("PATCH /api/hypotheses/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateHypothesis))))
	mux.Handle("GET /api/handover", server.authenticated(http.HandlerFunc(server.handover)))
	mux.Handle("GET /api/handover/export", server.authenticated(http.HandlerFunc(server.exportHandover)))
	mux.Handle("GET /api/report/export", server.authenticated(http.HandlerFunc(server.exportReport)))
	mux.Handle("GET /api/schedule", server.authenticated(http.HandlerFunc(server.schedule)))
	mux.Handle("POST /api/schedule/manual", server.authenticated(server.editors(http.HandlerFunc(server.manualSchedule))))
	mux.Handle("POST /api/schedule/manual-batch", server.authenticated(server.editors(http.HandlerFunc(server.manualScheduleBatch))))
	mux.Handle("GET /api/schedule/policy", server.authenticated(http.HandlerFunc(server.schedulePolicy)))
	mux.Handle("POST /api/schedule/adopt", server.authenticated(server.editors(http.HandlerFunc(server.adoptSchedules))))
	mux.Handle("PATCH /api/schedule/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateSchedule))))
	mux.Handle("GET /api/schedule/{id}/adjustments", server.authenticated(http.HandlerFunc(server.scheduleAdjustments)))
	mux.Handle("POST /api/schedule/{id}/publish", server.authenticated(server.editors(http.HandlerFunc(server.publishSchedule))))
	mux.Handle("POST /api/ai/recommend", server.authenticated(server.editors(http.HandlerFunc(server.generateRecommendations))))
	mux.Handle("GET /api/recommendations", server.authenticated(http.HandlerFunc(server.recommendations)))
	mux.Handle("PATCH /api/recommendations/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateRecommendation))))
	return server.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(s.Config.AllowedOrigins))
	for _, origin := range s.Config.AllowedOrigins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		r.Header.Set("X-Request-ID", requestID)
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; !ok {
				writeError(w, r, http.StatusForbidden, "origin_forbidden", "跨域来源未获允许")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		started := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), s.Config.RequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
		s.Log.Info("http request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "数据库暂不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "database": "connected", "version": "0.4.0"})
}

func (s *Server) runtimeMode(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"mode": "database", "persistent": true, "message": "PostgreSQL 数据库模式：修改将持久化保存。"})
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	var exists bool
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT EXISTS (SELECT 1 FROM user_account WHERE role_name='owner')`).Scan(&exists); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "读取账号状态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": !exists})
}

type authInput struct {
	TeamName string `json:"team_name"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var input authInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.Auth.Setup(r.Context(), input.TeamName, input.Name, input.Email, input.Password)
	if err != nil {
		s.authError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var input authInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.Auth.Register(r.Context(), input.TeamName, input.Name, input.Email, input.Password)
	if err != nil {
		s.authError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	result, err := s.Auth.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		s.authError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "请先登录")
			return
		}
		claims, err := s.Auth.Authenticate(r.Context(), strings.TrimSpace(header[7:]), 0)
		if err != nil {
			if errors.Is(err, service.ErrAuthBackend) {
				writeError(w, r, http.StatusServiceUnavailable, "auth_backend_unavailable", "认证数据服务暂不可用")
				return
			}
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims)))
	})
}

func (s *Server) editors(next http.Handler) http.Handler {
	return requireRole(next, domain.RoleOwner, domain.RoleEditor)
}

func (s *Server) owners(next http.Handler) http.Handler { return requireRole(next, domain.RoleOwner) }

func requireRole(next http.Handler, roles ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFrom(r)
		for _, role := range roles {
			if claims.Role == role {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, r, http.StatusForbidden, "forbidden", "当前账号无权执行此操作")
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	teamName, err := s.Store.TeamName(r.Context(), c.TeamID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "team_lookup_failed", "读取团队信息失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": c.UserID, "email": c.Email, "name": c.Name, "role": c.Role, "team_id": c.TeamID, "team_name": teamName})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.Logout(r.Context(), claimsFrom(r)); err != nil {
		writeError(w, r, http.StatusUnauthorized, "logout_failed", "登录状态已失效")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"logged_out": true})
}

func (s *Server) contents(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	items, err := s.Store.ListContents(r.Context(), teamID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "contents_failed", "读取内容记录失败")
		return
	}
	// Category counts come from the database aggregation so the tab badges show
	// the real totals instead of the current page length.
	counts, err := s.Store.ContentCategoryCounts(r.Context(), teamID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "contents_counts_failed", "读取内容分类统计失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items), "counts": counts})
}

func (s *Server) content(w http.ResponseWriter, r *http.Request) {
	item, err := s.Store.GetContent(r.Context(), claimsFrom(r).TeamID, r.PathValue("id"))
	if err != nil {
		notFoundOrError(w, r, err, "内容记录不存在")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) contentWorkflowEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.QueryMaps(r.Context(), `SELECT e.from_state,e.to_state,e.source,e.created_at,u.name AS operator FROM content_workflow_event e LEFT JOIN user_account u ON u.id=e.actor_user_id WHERE e.team_id=$1 AND e.content_id=$2 ORDER BY e.created_at`, claimsFrom(r).TeamID, r.PathValue("id"))
	if err != nil {
		writeError(w, r, 500, "workflow_events_failed", "读取工作流记录失败")
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) createContent(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		domain.Content
		PublishedAt flexibleTime `json:"published_at"`
	}
	if !decode(w, r, &payload) {
		return
	}
	input := payload.Content
	input.PublishedAt = payload.PublishedAt.Time
	// New records always start at the beginning of the workflow; the client
	// cannot pick an initial state.
	input.WorkflowState = domain.WorkflowProductionPending
	input.Lifecycle = domain.LifecycleUnpublished
	c := claimsFrom(r)
	facts := contentFacts(input)
	if message := s.contentRuleFailure(r, c.TeamID, facts, input.Title+"\n"+input.Notes, input.Tags, input.Category, input.WorkflowState); message != "" {
		writeError(w, r, 422, "content_rule_failed", message)
		return
	}
	item, err := s.Store.CreateContent(r.Context(), c.TeamID, c.UserID, c.Name, input)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "content_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateContent(w http.ResponseWriter, r *http.Request) {
	var input map[string]any
	if !decode(w, r, &input) {
		return
	}
	c := claimsFrom(r)
	if title, ok := input["title"].(string); ok {
		notes, _ := input["notes"].(string)
		category, _ := input["category"].(string)
		state, _ := input["workflow_state"].(string)
		var tags []string
		if raw, ok := input["tags"].([]any); ok {
			for _, item := range raw {
				if value, ok := item.(string); ok {
					tags = append(tags, value)
				}
			}
		}
		facts := map[string]any{"title": title, "notes": notes, "category": category, "workflow_state": state}
		if item, loadErr := s.Store.GetContent(r.Context(), c.TeamID, r.PathValue("id")); loadErr == nil {
			for key, value := range item {
				if _, exists := facts[key]; !exists {
					facts[key] = value
				}
			}
		}
		for key, value := range input {
			facts[key] = value
		}
		facts["category"], facts["workflow_state"], facts["title"], facts["notes"] = category, state, title, notes
		if message := s.contentRuleFailure(r, c.TeamID, facts, title+"\n"+notes, tags, category, state); message != "" {
			writeError(w, r, 422, "content_rule_failed", message)
			return
		}
	}
	item, err := s.Store.UpdateContent(r.Context(), c.TeamID, c.UserID, c.Name, r.PathValue("id"), input)
	if err != nil {
		notFoundOrError(w, r, err, "更新内容失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteContent(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	if err := s.Store.DeleteContent(r.Context(), c.TeamID, c.Name, r.PathValue("id")); err != nil {
		notFoundOrError(w, r, err, "删除内容失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) advanceContent(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Fields map[string]any `json:"fields"`
	}
	if r.ContentLength > 0 && !decode(w, r, &input) {
		return
	}
	c := claimsFrom(r)
	item, err := s.Store.AdvanceWorkflow(r.Context(), c.TeamID, c.UserID, c.Name, r.PathValue("id"), input.Fields)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, r, 404, "content_not_found", "内容记录不存在")
			return
		}
		writeError(w, r, http.StatusConflict, "workflow_conflict", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// advanceBulkContents moves the selected records one workflow step using the
// same transition rule as the single-record endpoint. Each record reports its own
// result so partial failures stay visible instead of becoming a silent success.
func (s *Server) advanceBulkContents(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Target string `json:"target"`
		Items  []struct {
			ID     string         `json:"id"`
			Fields map[string]any `json:"fields"`
		} `json:"items"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.Items) == 0 {
		writeError(w, r, 422, "bulk_empty", "请先选择内容记录")
		return
	}
	if len(input.Items) > 200 {
		writeError(w, r, 422, "bulk_too_large", "单次批量推进最多 200 条记录")
		return
	}
	if !domain.IsWorkflowState(input.Target) {
		writeError(w, r, 422, "workflow_target_invalid", "目标工作流状态无效")
		return
	}
	items := make([]repository.TransitionItem, 0, len(input.Items))
	for _, item := range input.Items {
		if !regexpUUID(item.ID) {
			writeError(w, r, 422, "content_id_invalid", "内容 ID 无效")
			return
		}
		items = append(items, repository.TransitionItem{ID: item.ID, Fields: item.Fields})
	}
	c := claimsFrom(r)
	outcome, err := s.Store.TransitionContents(r.Context(), c.TeamID, c.UserID, c.Name, input.Target, items)
	if err != nil {
		writeError(w, r, 500, "workflow_advance_failed", "批量推进工作流失败")
		return
	}
	writeJSON(w, 200, outcome)
}

func (s *Server) bulkContents(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"`
		Status string   `json:"status"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.IDs) == 0 {
		writeError(w, r, 422, "bulk_empty", "请先选择内容记录")
		return
	}
	for _, id := range input.IDs {
		if !regexpUUID(id) {
			writeError(w, r, 422, "content_id_invalid", "批量内容 ID 无效")
			return
		}
	}
	c := claimsFrom(r)
	// Workflow state changes are not accepted here: they must go through the
	// validated transitions in /api/contents/advance-bulk and /api/schedule.
	if input.Action != "delete" {
		writeError(w, r, 422, "bulk_action_invalid", "不支持的批量操作：状态变更请使用工作流或排期接口")
		return
	}
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, r, 500, "bulk_failed", "批量操作失败")
		return
	}
	defer tx.Rollback()
	placeholders := make([]string, len(input.IDs))
	args := make([]any, 0, len(input.IDs)+2)
	args = append(args, c.TeamID)
	for i, id := range input.IDs {
		args = append(args, id)
		placeholders[i] = "$" + strconv.Itoa(i+2)
	}
	// Plans belong to their content record, so remove them first: the schedule
	// foreign key would otherwise reject the content deletion.
	if _, execErr := tx.ExecContext(r.Context(), `DELETE FROM schedule WHERE team_id=$1 AND content_id IN (`+strings.Join(placeholders, ",")+`)`, args...); execErr != nil {
		writeError(w, r, 500, "bulk_delete_failed", "批量删除关联排期失败")
		return
	}
	result, execErr := tx.ExecContext(r.Context(), `DELETE FROM content WHERE team_id=$1 AND id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if execErr != nil {
		writeError(w, r, 500, "bulk_delete_failed", "批量删除失败")
		return
	}
	count, _ := result.RowsAffected()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_log(team_id,actor,action,entity_type,entity_id,detail,created_at) VALUES($1,$2,'bulk_delete','content','bulk',$3,NOW())`, c.TeamID, c.Name, fmt.Sprintf("批量删除 %d 条内容", count)); err != nil {
		writeError(w, r, 500, "bulk_audit_failed", "记录批量操作失败")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, r, 500, "bulk_commit_failed", "提交批量操作失败")
		return
	}
	writeJSON(w, 200, map[string]any{"updated": len(input.IDs)})
}

func (s *Server) exportContentsCSV(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListContents(r.Context(), claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "export_failed", "导出内容失败")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=arts-smart-contents.csv")
	writer := csv.NewWriter(w)
	writer.Write([]string{"ID", "标题", "内容类型", "平台", "发布时间", "状态标签", "工作流状态", "播放量", "点赞量", "评论量", "收藏量", "转发量", "标签", "负责人", "备注"})
	for _, item := range items {
		writer.Write([]string{fmt.Sprint(item["id"]), fmt.Sprint(item["title"]), fmt.Sprint(item["category"]), fmt.Sprint(item["platform"]), fmt.Sprint(item["published_at"]), fmt.Sprint(item["lifecycle"]), fmt.Sprint(item["workflow_state"]), fmt.Sprint(item["views"]), fmt.Sprint(item["likes"]), fmt.Sprint(item["comments"]), fmt.Sprint(item["saves"]), fmt.Sprint(item["shares"]), fmt.Sprint(item["tags"]), fmt.Sprint(item["owner"]), fmt.Sprint(item["notes"])})
	}
	writer.Flush()
}

func (s *Server) exportAnonymizedCSV(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListContents(r.Context(), claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "export_failed", "导出脱敏内容失败")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=arts-smart-anonymized.csv")
	writer := csv.NewWriter(w)
	writer.Write([]string{"内容类型", "平台", "发布时间", "状态", "播放量", "互动量", "完播率"})
	for _, item := range items {
		if item["lifecycle"] != domain.LifecyclePublished {
			continue
		}
		writer.Write([]string{fmt.Sprint(item["category"]), fmt.Sprint(item["platform"]), fmt.Sprint(item["published_at"]), fmt.Sprint(item["lifecycle"]), fmt.Sprint(item["views"]), fmt.Sprint(item["total_interactions"]), fmt.Sprint(item["completion"])})
	}
	writer.Flush()
}

func (s *Server) exportScheduleCSV(w http.ResponseWriter, r *http.Request) {
	today := time.Now()
	items, err := s.Store.ListSchedules(r.Context(), claimsFrom(r).TeamID, today.Format("2006-01")+"-01", today.AddDate(0, 12, 0).Format("2006-01")+"-01")
	if err != nil {
		writeError(w, r, 500, "schedule_export_failed", "导出排期失败")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=arts-smart-schedule.csv")
	writer := csv.NewWriter(w)
	writer.Write([]string{"日期", "时间", "标题", "类型", "平台", "负责人", "来源", "状态"})
	for _, item := range items {
		writer.Write([]string{fmt.Sprint(item["planned_date"]), fmt.Sprint(item["planned_time"]), fmt.Sprint(item["title"]), fmt.Sprint(item["category"]), fmt.Sprint(item["platform"]), fmt.Sprint(item["owner"]), fmt.Sprint(item["source_label"]), fmt.Sprint(item["status"])})
	}
	writer.Flush()
}

func regexpUUID(value string) bool {
	return len(value) == 36 && value[8] == '-' && value[13] == '-' && value[18] == '-' && value[23] == '-'
}

func (s *Server) teams(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	name, err := s.Store.TeamName(r.Context(), c.TeamID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "team_lookup_failed", "读取团队失败")
		return
	}
	writeJSON(w, http.StatusOK, []map[string]any{{"id": c.TeamID, "name": name}})
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT id,email,name,role_name AS role,created_at FROM user_account WHERE team_id=$1 ORDER BY created_at`, c.TeamID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "users_failed", "读取团队成员失败")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) addUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Role != domain.RoleEditor && input.Role != domain.RoleViewer {
		writeError(w, r, 422, "invalid_role", "成员角色只能是 editor 或 viewer")
		return
	}
	hash, err := service.HashPassword(input.Password)
	if err != nil {
		writeError(w, r, 500, "password_hash_failed", "生成密码摘要失败")
		return
	}
	c := claimsFrom(r)
	user, err := s.Store.AddMember(r.Context(), c.TeamID, c.Name, input.Email, input.Name, input.Role, hash)
	if err != nil {
		s.authError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": user.ID, "email": user.Email, "name": user.Name, "role": user.Role})
}

func (s *Server) renameTeam(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || teamID != c.TeamID {
		writeError(w, r, 404, "not_found", "团队不存在")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeError(w, r, 422, "invalid_team_name", "团队名称不能为空")
		return
	}
	_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE team SET name=$1 WHERE id=$2`, strings.TrimSpace(input.Name), teamID)
	if err != nil {
		writeError(w, r, 500, "team_update_failed", "更新团队信息失败")
		return
	}
	writeJSON(w, 200, map[string]any{"id": teamID, "name": strings.TrimSpace(input.Name)})
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT cr.*,u.name AS updated_by,l.result AS last_result,l.detail AS last_detail,l.created_at AS last_executed_at FROM content_rule cr LEFT JOIN user_account u ON u.id=cr.updated_by_user_id LEFT JOIN LATERAL (SELECT result,detail,created_at FROM rule_execution_log rel WHERE rel.rule_id=cr.id AND rel.team_id=cr.team_id ORDER BY rel.created_at DESC LIMIT 1) l ON TRUE WHERE cr.team_id=$1 ORDER BY cr.priority,cr.id`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "rules_failed", "读取内容规则失败")
		return
	}
	writeJSON(w, 200, rows)
}

// systemRuleSet is a read-only, versioned rule set shipped with the service.
type systemRuleSet struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
	UpdatedAt   string `json:"updated_at"`
	Description string `json:"description"`
	RuleCount   int64  `json:"rule_count"`
	Effective   bool   `json:"effective"`
}

// systemRules exposes the read-only rule set for display. It is never editable
// through the API and does not replace team-authored content rules.
func (s *Server) systemRules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.QueryMaps(r.Context(), `SELECT s.id,s.name,s.version,s.enabled,s.updated_at,COALESCE(s.payload_json->>'description','') AS description,jsonb_array_length(COALESCE(s.payload_json->'rules','[]'::jsonb)) AS rule_count,(s.id=(SELECT e.id FROM system_rule_set e WHERE e.enabled=TRUE ORDER BY e.updated_at DESC LIMIT 1)) AS effective FROM system_rule_set s ORDER BY s.name,s.version`)
	if err != nil {
		writeError(w, r, 500, "system_rules_failed", "读取系统规则集失败")
		return
	}
	items := make([]systemRuleSet, 0, len(rows))
	for _, row := range rows {
		items = append(items, systemRuleSet{
			ID:          intField(row, "id"),
			Name:        stringField(row, "name"),
			Version:     stringField(row, "version"),
			Enabled:     boolField(row, "enabled"),
			UpdatedAt:   timestampField(row, "updated_at"),
			Description: stringField(row, "description"),
			RuleCount:   intField(row, "rule_count"),
			Effective:   boolField(row, "effective"),
		})
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

type ruleInput struct {
	Name          string `json:"name"`
	RuleType      string `json:"rule_type"`
	RuleContent   string `json:"rule_content"`
	ContentType   string `json:"content_type"`
	WorkflowState string `json:"workflow_state"`
	Priority      int    `json:"priority"`
	Enabled       bool   `json:"enabled"`
	Notes         string `json:"notes"`
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var in ruleInput
	if !decode(w, r, &in) {
		return
	}
	if err := validateRuleInput(in); err != nil {
		writeError(w, r, 422, "rule_invalid", err.Error())
		return
	}
	c := claimsFrom(r)
	var collision bool
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM content_rule WHERE team_id=$1 AND priority=$2 AND enabled=TRUE)`, c.TeamID, in.Priority).Scan(&collision); err != nil {
		writeError(w, r, 500, "rule_check_failed", "检查规则优先级失败")
		return
	}
	if collision && in.Enabled {
		writeError(w, r, 409, "rule_priority_conflict", "同一团队已有相同优先级的启用规则")
		return
	}
	var id int64
	err := s.Store.DB.QueryRowContext(r.Context(), `INSERT INTO content_rule(team_id,name,priority,enabled,trigger,logic,conditions_json,scope_json,actions_json,rule_type,rule_content,content_type,workflow_state,notes,created_by_user_id,updated_by_user_id,created_at,updated_at) VALUES($1,$2,$3,$4,'on_save','AND','{}','{}','{}',$5,$6,$7,$8,$9,$10,$10,NOW(),NOW()) RETURNING id`, c.TeamID, strings.TrimSpace(in.Name), in.Priority, in.Enabled, in.RuleType, in.RuleContent, in.ContentType, in.WorkflowState, in.Notes, c.UserID).Scan(&id)
	if err != nil {
		writeError(w, r, 500, "rule_save_failed", "保存规则失败")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": in.Name, "priority": in.Priority, "enabled": in.Enabled, "rule_type": in.RuleType, "rule_content": in.RuleContent, "content_type": in.ContentType, "workflow_state": in.WorkflowState, "notes": in.Notes, "updated_by_user_id": c.UserID})
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "not_found", "规则不存在")
		return
	}
	var in map[string]any
	if !decode(w, r, &in) {
		return
	}
	c := claimsFrom(r)
	var current ruleInput
	err = s.Store.DB.QueryRowContext(r.Context(), `SELECT name,rule_type,rule_content,content_type,workflow_state,priority,enabled,notes FROM content_rule WHERE id=$1 AND team_id=$2`, id, c.TeamID).Scan(&current.Name, &current.RuleType, &current.RuleContent, &current.ContentType, &current.WorkflowState, &current.Priority, &current.Enabled, &current.Notes)
	if err != nil {
		notFoundOrError(w, r, err, "规则不存在")
		return
	}
	if v, ok := in["name"].(string); ok {
		current.Name = v
	}
	if v, ok := in["rule_type"].(string); ok {
		current.RuleType = v
	}
	if v, ok := in["rule_content"].(string); ok {
		current.RuleContent = v
	}
	if v, ok := in["content_type"].(string); ok {
		current.ContentType = v
	}
	if v, ok := in["workflow_state"].(string); ok {
		current.WorkflowState = v
	}
	if v, ok := in["notes"].(string); ok {
		current.Notes = v
	}
	if v, ok := in["enabled"].(bool); ok {
		current.Enabled = v
	}
	if v, ok := in["priority"].(float64); ok {
		current.Priority = int(v)
	}
	if err = validateRuleInput(current); err != nil {
		writeError(w, r, 422, "rule_invalid", err.Error())
		return
	}
	var collision bool
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM content_rule WHERE team_id=$1 AND id<>$2 AND priority=$3 AND enabled=TRUE)`, c.TeamID, id, current.Priority).Scan(&collision)
	if collision && current.Enabled {
		writeError(w, r, 409, "rule_priority_conflict", "同一团队已有相同优先级的启用规则")
		return
	}
	_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE content_rule SET name=$1,rule_type=$2,rule_content=$3,content_type=$4,workflow_state=$5,priority=$6,enabled=$7,notes=$8,updated_by_user_id=$9,updated_at=NOW() WHERE id=$10 AND team_id=$11`, current.Name, current.RuleType, current.RuleContent, current.ContentType, current.WorkflowState, current.Priority, current.Enabled, current.Notes, c.UserID, id, c.TeamID)
	if err != nil {
		writeError(w, r, 500, "rule_update_failed", "更新规则失败")
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "name": current.Name, "priority": current.Priority, "enabled": current.Enabled, "rule_type": current.RuleType, "rule_content": current.RuleContent, "content_type": current.ContentType, "workflow_state": current.WorkflowState, "notes": current.Notes, "updated_by_user_id": c.UserID})
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "not_found", "规则不存在")
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM content_rule WHERE id=$1 AND team_id=$2`, id, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "rule_delete_failed", "删除规则失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, r, 404, "not_found", "规则不存在")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) previewRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rule ruleInput `json:"rule"`
		Text string    `json:"text"`
		Tags []string  `json:"tags"`
	}
	if !decode(w, r, &in) {
		return
	}
	result, err := service.EvaluateRule(in.Rule.RuleType, in.Rule.RuleContent, in.Text, in.Tags)
	if err != nil {
		writeError(w, r, 422, "rule_evaluation_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"matched": result.Matched, "result": result.Result})
}

func (s *Server) evaluateRules(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ContentID string `json:"content_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	c := claimsFrom(r)
	item, err := s.Store.GetContent(r.Context(), c.TeamID, in.ContentID)
	if err != nil {
		notFoundOrError(w, r, err, "内容记录不存在")
		return
	}
	text := fmt.Sprint(item["title"], "\n", item["notes"])
	tags := stringSlice(item["tags"])
	rules, err := s.Store.ListTeamRows(r.Context(), `SELECT id,name,priority,enabled,rule_type,rule_content,content_type,workflow_state FROM content_rule WHERE team_id=$1 AND enabled=TRUE ORDER BY priority,id`, c.TeamID)
	if err != nil {
		writeError(w, r, 500, "rules_failed", "读取内容规则失败")
		return
	}
	matched := 0
	for _, row := range rules {
		if (row["content_type"] != "" && row["content_type"] != item["category"]) || (row["workflow_state"] != "" && row["workflow_state"] != item["workflow_state"]) {
			continue
		}
		result, evalErr := service.EvaluateRule(fmt.Sprint(row["rule_type"]), fmt.Sprint(row["rule_content"]), text, tags)
		detail := result.Result
		if evalErr != nil {
			detail = evalErr.Error()
			result.Matched = true
		}
		if result.Matched {
			matched++
		}
		_, _ = s.Store.DB.ExecContext(r.Context(), `INSERT INTO rule_execution_log(team_id,rule_id,rule_name,content_id,trigger,result,detail,created_at) VALUES($1,$2,$3,$4,'manual',$5,$6,NOW())`, c.TeamID, row["id"], row["name"], in.ContentID, detail, result.Result)
	}
	writeJSON(w, 200, map[string]int{"checked": len(rules), "matched": matched})
}

func (s *Server) ruleLogs(w http.ResponseWriter, r *http.Request) {
	limit := ParseIntQuery(r, "limit", 20)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT rule_name,content_id,trigger,result,detail,created_at FROM rule_execution_log WHERE team_id=$1 ORDER BY created_at DESC LIMIT `+strconv.Itoa(limit), claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "rule_logs_failed", "读取规则日志失败")
		return
	}
	writeJSON(w, 200, rows)
}

func validateRuleInput(in ruleInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("规则名称不能为空")
	}
	if in.Priority < 0 {
		return errors.New("规则优先级不能为负数")
	}
	switch in.RuleType {
	case "required", "format", "sensitive_words", "length", "tag_constraint":
	default:
		return errors.New("规则类型无效")
	}
	if _, err := service.EvaluateRule(in.RuleType, in.RuleContent, "", nil); err != nil {
		if in.RuleType == "required" || in.RuleType == "length" || in.RuleType == "tag_constraint" {
			return nil
		}
		return err
	}
	return nil
}

// contentRuleFailure evaluates the team's *effective* rule set against the
// content being saved.
//
//   - a custom rule set holds validation rules: a match rejects the content;
//   - the system rule set holds governance rules: its matches are evaluated
//     against real data and recorded in the execution log instead of rejecting.
//
// Either way the rules that run are the ones selected in team_rule_selection, so
// the page and the engine cannot drift apart.
func (s *Server) contentRuleFailure(r *http.Request, teamID int64, facts map[string]any, text string, tags []string, category, state string) string {
	kind, setID, err := s.effectiveRuleSelection(r.Context(), teamID)
	if err != nil {
		return "无法读取生效规则集"
	}
	if setID == 0 {
		return ""
	}
	if kind == "custom" {
		rows, err := s.Store.ListTeamRows(r.Context(), `SELECT name,rule_type,rule_content,content_type,workflow_state FROM content_rule_set_item WHERE rule_set_id=$1 AND enabled=TRUE ORDER BY priority,id`, setID)
		if err != nil {
			return "无法加载内容校验规则"
		}
		for _, row := range rows {
			if row["content_type"] != "" && row["content_type"] != category {
				continue
			}
			if row["workflow_state"] != "" && row["workflow_state"] != state {
				continue
			}
			result, err := service.EvaluateRule(fmt.Sprint(row["rule_type"]), fmt.Sprint(row["rule_content"]), text, tags)
			if err != nil {
				return "规则 " + fmt.Sprint(row["name"]) + " 配置错误：" + err.Error()
			}
			if result.Matched {
				return "未通过内容规则：" + fmt.Sprint(row["name"])
			}
		}
		return ""
	}
	s.logGovernanceHits(r, teamID, setID, facts)
	return ""
}

// logGovernanceHits evaluates the system rule set's rules against the content and
// records the outcome so the rules page shows what the engine actually did.
func (s *Server) logGovernanceHits(r *http.Request, teamID, setID int64, facts map[string]any) {
	var raw []byte
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT payload_json->'rules' FROM system_rule_set WHERE id=$1`, setID).Scan(&raw); err != nil {
		return
	}
	rules, err := service.ParseGovernanceRules(raw)
	if err != nil {
		return
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		matched, reason, evalErr := service.EvaluateGovernanceRule(rule, facts)
		result := "no_match"
		detail := reason
		if evalErr != nil {
			result = "error"
			detail = evalErr.Error()
		} else if matched {
			result = "matched"
		}
		contentID := stringField(facts, "id")
		if contentID == "" {
			contentID = "pending"
		}
		_, _ = s.Store.DB.ExecContext(r.Context(), `INSERT INTO rule_execution_log(team_id,rule_id,rule_name,content_id,trigger,result,detail,created_at) VALUES($1,NULL,$2,$3,'content.save',$4,$5,NOW())`, teamID, rule.Name, contentID, result, detail)
	}
}

func stringSlice(raw any) []string {
	items, _ := raw.([]string)
	if items != nil {
		return items
	}
	values, _ := raw.([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func (s *Server) performances(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	// The expiry rule is applied before reading, so a show that has already
	// started is reported as 已演出 without any client-side guesswork. Already
	// cancelled shows are never rewritten.
	if _, err := s.Store.SettlePerformanceStatuses(r.Context(), teamID); err != nil {
		writeError(w, r, 500, "performances_failed", "更新演出状态失败")
		return
	}
	rows, err := s.Store.ListPerformances(r.Context(), teamID)
	if err != nil {
		writeError(w, r, 500, "performances_failed", "读取演出预订失败")
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) createPerformance(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Title           string       `json:"title"`
		Venue           string       `json:"venue"`
		StartsAt        flexibleTime `json:"starts_at"`
		BookingStatus   string       `json:"booking_status"`
		PerformanceType string       `json:"performance_type"`
		TicketLink      string       `json:"ticket_link"`
		OwnerUserID     *int64       `json:"owner_user_id"`
		Notes           string       `json:"notes"`
	}
	if !decode(w, r, &payload) {
		return
	}
	input := domain.Performance{
		Title:           payload.Title,
		Venue:           payload.Venue,
		StartsAt:        payload.StartsAt.Time,
		BookingStatus:   payload.BookingStatus,
		PerformanceType: payload.PerformanceType,
		TicketLink:      payload.TicketLink,
		OwnerUserID:     payload.OwnerUserID,
		Notes:           payload.Notes,
	}
	c := claimsFrom(r)
	result, err := s.Store.CreatePerformanceBooking(r.Context(), c.TeamID, c.UserID, c.Name, input)
	if err != nil {
		writeError(w, r, 422, "performance_invalid", err.Error())
		return
	}
	writeJSON(w, 201, result)
}

// performanceDetail returns one show together with its aggregated promotional
// content, its real effect data and the pre-heat timeline.
func (s *Server) performanceDetail(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	id := r.PathValue("id")
	if _, err := s.Store.SettlePerformanceStatuses(r.Context(), teamID); err != nil {
		writeError(w, r, 500, "performance_failed", "更新演出状态失败")
		return
	}
	detail, err := s.Store.GetPerformance(r.Context(), teamID, id)
	if err != nil {
		notFoundOrError(w, r, err, "演出不存在")
		return
	}
	content, err := s.Store.GetPerformanceContent(r.Context(), teamID, id)
	if err != nil {
		writeError(w, r, 500, "performance_content_failed", "读取演出关联内容失败")
		return
	}
	var daysToStart *int
	if startsAt, ok := timeField(detail, "starts_at"); ok {
		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		showDay := time.Date(startsAt.Year(), startsAt.Month(), startsAt.Day(), 0, 0, 0, 0, startsAt.Location())
		days := int(showDay.Sub(today).Hours() / 24)
		daysToStart = &days
	}
	writeJSON(w, 200, map[string]any{
		"performance":    detail,
		"days_to_start":  daysToStart,
		"published":      content["published"],
		"unpublished":    content["unpublished"],
		"timeline":       content["timeline"],
		"counts":         map[string]any{"content_count": intField(detail, "content_count"), "published_content_count": intField(detail, "published_content_count"), "unpublished_content_count": intField(detail, "unpublished_content_count"), "schedulable_content_count": intField(detail, "schedulable_content_count"), "scheduled_content_count": intField(detail, "scheduled_content_count")},
		"status_options": domain.PerformanceStatuses(),
	})
}

// performancePatch decodes a partial show update. Unknown keys are rejected and
// only the keys actually present are written.
func performancePatch(input map[string]any) (domain.Performance, map[string]bool, error) {
	var perf domain.Performance
	fields := map[string]bool{}
	for key, value := range input {
		switch key {
		case "title", "venue", "booking_status", "performance_type", "ticket_link", "notes":
			text, ok := value.(string)
			if !ok {
				return perf, fields, fmt.Errorf("%s 必须是文本", key)
			}
			switch key {
			case "title":
				perf.Title = text
			case "venue":
				perf.Venue = text
			case "booking_status":
				perf.BookingStatus = text
			case "performance_type":
				perf.PerformanceType = text
			case "ticket_link":
				perf.TicketLink = text
			case "notes":
				perf.Notes = text
			}
			fields[key] = true
		case "starts_at":
			raw, ok := value.(string)
			if !ok {
				return perf, fields, errors.New("演出时间格式必须为 RFC3339 或 YYYY-MM-DDTHH:MM")
			}
			parsed, err := parseFlexibleTime(raw)
			if err != nil {
				return perf, fields, err
			}
			perf.StartsAt = parsed
			fields["starts_at"] = true
		case "owner_user_id":
			switch typed := value.(type) {
			case nil:
				perf.OwnerUserID = nil
			case float64:
				id := int64(typed)
				perf.OwnerUserID = &id
			default:
				return perf, fields, errors.New("负责人 ID 无效")
			}
			fields["owner_user_id"] = true
		case "id", "team_id", "owner", "created_at":
			// Server-managed: accepted but ignored so a read-modify-write of a
			// fetched record does not fail on its own metadata.
		default:
			return perf, fields, fmt.Errorf("不支持的字段：%s", key)
		}
	}
	return perf, fields, nil
}

// parseFlexibleTime accepts the timestamp layouts the web client can submit.
func parseFlexibleTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, errors.New("演出时间为必填项")
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("演出时间格式必须为 RFC3339 或 YYYY-MM-DDTHH:MM")
}

func (s *Server) updatePerformance(w http.ResponseWriter, r *http.Request) {
	var input map[string]any
	if !decode(w, r, &input) {
		return
	}
	perf, fields, err := performancePatch(input)
	if err != nil {
		writeError(w, r, 422, "performance_invalid", err.Error())
		return
	}
	if len(fields) == 0 {
		writeError(w, r, 422, "performance_invalid", "没有需要更新的字段")
		return
	}
	c := claimsFrom(r)
	item, err := s.Store.UpdatePerformanceBooking(r.Context(), c.TeamID, c.Name, r.PathValue("id"), perf, fields)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, r, 404, "performance_not_found", "演出不存在")
			return
		}
		writeError(w, r, 422, "performance_invalid", err.Error())
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) deletePerformance(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	if err := s.Store.DeletePerformanceBooking(r.Context(), c.TeamID, c.Name, r.PathValue("id")); err != nil {
		notFoundOrError(w, r, err, "删除演出失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// planPerformance generates the whole-show pre-heat schedule for one 正式演出.
// It reuses the shared candidate, history, rule-set and adoption machinery, so
// the produced items are stored as a normal recommendation and adopted through
// POST /api/schedule/adopt instead of a parallel scheduling flow.
func (s *Server) planPerformance(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	show, err := s.Store.GetPerformance(r.Context(), c.TeamID, r.PathValue("id"))
	if err != nil {
		notFoundOrError(w, r, err, "演出不存在")
		return
	}
	startsAt, ok := timeField(show, "starts_at")
	if !ok {
		writeError(w, r, 422, "performance_invalid", "演出时间无效，无法生成排期")
		return
	}
	if !startsAt.After(time.Now()) {
		writeError(w, r, 422, "performance_started", "演出时间已过，无法生成预热排期")
		return
	}
	// Only unpublished videos that already finished production can be planned,
	// which is the same eligibility rule the suggestion page uses.
	rows, err := s.Store.QueryMaps(r.Context(), `
		SELECT id,title,category,platform,owner,lifecycle,workflow_state
		FROM content
		WHERE team_id=$1 AND performance_id=$2 AND lifecycle='未发布' AND workflow_state='待排期'
		  AND NOT EXISTS (SELECT 1 FROM schedule WHERE schedule.team_id=content.team_id AND schedule.content_id=content.id AND schedule.status='待发布')
		ORDER BY created_at, id`, c.TeamID, r.PathValue("id"))
	if err != nil {
		writeError(w, r, 500, "candidate_query_failed", "读取演出预热内容失败")
		return
	}
	candidates := make([]domain.Content, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, domain.Content{
			ID: stringField(row, "id"), Title: stringField(row, "title"),
			Category: stringField(row, "category"), Platform: stringField(row, "platform"),
			Owner: stringField(row, "owner"), Lifecycle: stringField(row, "lifecycle"),
			WorkflowState: stringField(row, "workflow_state"),
		})
	}
	policy, window, ruleErr := s.loadPlanningRules(r.Context())
	if ruleErr != nil {
		writeRuleSetError(w, r, ruleErr)
		return
	}
	policy.ExistingDaily, policy.ExistingPlatformWeekly = map[string]int{}, map[string]int{}
	if err = s.loadExistingScheduleLoad(r.Context(), c.TeamID, &policy); err != nil {
		writeError(w, r, 500, "schedule_capacity_failed", "读取既有排期频率失败")
		return
	}
	samples, err := s.loadPublishedSamples(r.Context(), c.TeamID)
	if err != nil {
		writeError(w, r, 500, "history_query_failed", "读取已发布历史表现失败")
		return
	}
	items, insufficient := service.BuildPerformancePlan(service.PerformancePlanInput{
		Now:        time.Now(),
		ShowTitle:  stringField(show, "title"),
		StartsAt:   startsAt,
		Candidates: candidates,
		Samples:    samples,
		Policy:     policy,
	})
	items = s.annotateSuggestionValidity(r.Context(), c.TeamID, items, policy, window)
	referenceCount := 0
	for _, item := range items {
		referenceCount += item.ReferenceSampleCount
	}
	status := "performance_plan"
	fallbackReason := ""
	if len(candidates) == 0 {
		status = "no_candidates"
		fallbackReason = "该演出下没有符合排期条件的未发布内容：内容需先推进到「待排期」且尚未有生效排期。"
	}
	if insufficient && len(items) > 0 {
		fallbackReason = "样本不足，当前排期基于通用规则生成。"
	}
	payload := map[string]any{"performance_id": r.PathValue("id"), "recommendations": items}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		writeError(w, r, 500, "recommendation_encode_failed", "编码排期失败")
		return
	}
	id := uuid.NewString()
	_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO recommendation(id,team_id,generated_at,payload_json,source_model,source_label,confidence,reference_count,analysis_status,fallback_reason,adopted) VALUES($1,$2,NOW(),$3,'rules',$4,$5,$6,$7,$8,FALSE)`,
		id, c.TeamID, string(payloadJSON), "整场排期（规则引擎）", map[bool]string{true: "low", false: "medium"}[insufficient], referenceCount, status, fallbackReason)
	if err != nil {
		writeError(w, r, 500, "recommendation_save_failed", "保存整场排期失败")
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": id, "performance_id": r.PathValue("id"),
		"source": "整场排期（规则引擎）", "source_label": "整场排期（规则引擎）",
		"analysis_status": status, "reference_count": referenceCount,
		"confidence": map[bool]string{true: "low", false: "medium"}[insufficient],
		"confidence_note": map[bool]string{
			true:  "团队历史样本不足，排期主要依据规则集与内容类型搭配。",
			false: "基于该团队自己的历史发布数据；结果不构成效果保证。",
		}[insufficient],
		"fallback_reason": fallbackReason,
		"recommendations": items,
	})
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTags(r.Context(), claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "tags_failed", "读取 Tag 库失败")
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &input) {
		return
	}
	result, err := s.Store.CreateTag(r.Context(), claimsFrom(r).TeamID, claimsFrom(r).UserID, input.Name)
	if err != nil {
		writeError(w, r, 422, "tag_invalid", err.Error())
		return
	}
	writeJSON(w, 201, result)
}

func (s *Server) updateTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "not_found", "Tag 不存在")
		return
	}
	var input map[string]any
	if !decode(w, r, &input) {
		return
	}
	c := claimsFrom(r)
	var name string
	var active bool
	err = s.Store.DB.QueryRowContext(r.Context(), `SELECT name,is_active FROM tag WHERE id=$1 AND team_id=$2`, id, c.TeamID).Scan(&name, &active)
	if err != nil {
		notFoundOrError(w, r, err, "Tag 不存在")
		return
	}
	if value, ok := input["name"].(string); ok {
		name = value
	}
	if value, ok := input["active"].(bool); ok {
		active = value
	}
	display, normalized, err := repository.NormalizeTag(name)
	if err != nil {
		writeError(w, r, 422, "tag_invalid", err.Error())
		return
	}
	_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE tag SET name=$1,normalized_name=$2,is_active=$3 WHERE id=$4 AND team_id=$5`, display, normalized, active, id, c.TeamID)
	if err != nil {
		writeError(w, r, 409, "tag_conflict", "Tag 名称重复或无法更新")
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "name": display, "active": active})
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "not_found", "Tag 不存在")
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE tag SET is_active=FALSE WHERE id=$1 AND team_id=$2`, id, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "tag_delete_failed", "停用 Tag 失败")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, r, 404, "not_found", "Tag 不存在")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) mergeTag(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "not_found", "Tag 不存在")
		return
	}
	var input struct {
		TargetID int64 `json:"target_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.TargetID == sourceID {
		writeError(w, r, 422, "tag_merge_invalid", "不能将 Tag 合并到自身")
		return
	}
	c := claimsFrom(r)
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, r, 500, "tag_merge_failed", "合并 Tag 失败")
		return
	}
	defer tx.Rollback()
	var sourceExists, targetExists bool
	_ = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tag WHERE id=$1 AND team_id=$2)`, sourceID, c.TeamID).Scan(&sourceExists)
	_ = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tag WHERE id=$1 AND team_id=$2 AND is_active=TRUE)`, input.TargetID, c.TeamID).Scan(&targetExists)
	if !sourceExists || !targetExists {
		writeError(w, r, 404, "not_found", "源 Tag 或目标 Tag 不属于当前团队")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO content_tag(content_id,tag_id) SELECT content_id,$1 FROM content_tag WHERE tag_id=$2 ON CONFLICT DO NOTHING`, input.TargetID, sourceID); err != nil {
		writeError(w, r, 500, "tag_merge_failed", "合并关联内容失败")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM tag WHERE id=$1 AND team_id=$2`, sourceID, c.TeamID); err != nil {
		writeError(w, r, 500, "tag_merge_failed", "删除重复 Tag 失败")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, r, 500, "tag_merge_failed", "提交 Tag 合并失败")
		return
	}
	writeJSON(w, 200, map[string]any{"merged": true, "source_id": sourceID, "target_id": input.TargetID})
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	var value modelSettings
	var storedKey string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT name,base_url,model,api_key,min_sample,score_completion_weight,score_interaction_weight,temperature,request_mode,auth_type,enabled,fallback_enabled,COALESCE(platform_weights_json,'{}') FROM ai_config WHERE team_id=$1 ORDER BY updated_at DESC LIMIT 1`, c.TeamID).Scan(&value.Name, &value.BaseURL, &value.Model, &storedKey, &value.MinSample, &value.CompletionWeight, &value.InteractionWeight, &value.Temperature, &value.RequestMode, &value.AuthType, &value.Enabled, &value.FallbackEnabled, &value.PlatformWeightsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, settingsResponse(defaultModelSettings(s.Config.AIBaseURL, s.Config.AIModel), s.Config.AIAPIKey != "", "AI_API_KEY"))
		return
	}
	if err != nil {
		writeError(w, r, 500, "settings_failed", "读取模型配置失败")
		return
	}
	keyConfigured := storedKey != "" || s.Config.AIAPIKey != ""
	writeJSON(w, 200, settingsResponse(value, keyConfigured, "已保存的模型密钥"))
}

// settingsResponse never exposes the stored API key, only whether one is
// configured and a masked hint.
func settingsResponse(value modelSettings, keyConfigured bool, hint string) map[string]any {
	payload := map[string]any{
		"configured":               keyConfigured,
		"has_key":                  keyConfigured,
		"name":                     value.Name,
		"base_url":                 value.BaseURL,
		"model":                    value.Model,
		"min_sample":               value.MinSample,
		"score_completion_weight":  value.CompletionWeight,
		"score_interaction_weight": value.InteractionWeight,
		"temperature":              value.Temperature,
		"request_mode":             value.RequestMode,
		"auth_type":                value.AuthType,
		"enabled":                  value.Enabled,
		"fallback_enabled":         value.FallbackEnabled,
		"platform_weights":         json.RawMessage(orEmptyJSONObject(value.PlatformWeightsRaw)),
	}
	if keyConfigured {
		payload["key_hint"] = hint
	}
	return payload
}

func orEmptyJSONObject(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	return raw
}

func (s *Server) aiConfigs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT id,name,base_url,model,enabled,updated_at FROM ai_config WHERE team_id=$1 ORDER BY updated_at DESC`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "ai_configs_failed", "读取模型配置列表失败")
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) createAIConfig(w http.ResponseWriter, r *http.Request) {
	var input aiConfigInput
	if !decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Model) == "" || strings.TrimSpace(input.BaseURL) == "" {
		writeError(w, r, 422, "ai_config_invalid", "配置名称、Base URL 和模型名称必填")
		return
	}
	encrypted := ""
	if input.APIKey != "" {
		var err error
		encrypted, err = service.SealSecret(s.Config.AppSecret, input.APIKey)
		if err != nil {
			writeError(w, r, 500, "credential_encrypt_failed", "安全保存模型密钥失败")
			return
		}
	}
	if input.Temperature == 0 {
		input.Temperature = .3
	}
	if input.MinSample < 1 {
		input.MinSample = 3
	}
	if input.CompletionWeight == 0 {
		input.CompletionWeight = .6
	}
	if input.InteractionWeight == 0 {
		input.InteractionWeight = .4
	}
	c := claimsFrom(r)
	var id int64
	err := s.Store.DB.QueryRowContext(r.Context(), `INSERT INTO ai_config(team_id,name,base_url,api_key,model,temperature,request_mode,auth_type,enabled,fallback_enabled,score_completion_weight,score_interaction_weight,min_sample,platform_weights_json,updated_at) VALUES($1,$2,$3,$4,$5,$6,'base','bearer',FALSE,TRUE,$7,$8,$9,'{}',NOW()) RETURNING id`, c.TeamID, input.Name, input.BaseURL, encrypted, input.Model, input.Temperature, input.CompletionWeight, input.InteractionWeight, input.MinSample).Scan(&id)
	if err != nil {
		writeError(w, r, 500, "ai_config_save_failed", "保存模型配置失败")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": input.Name, "base_url": input.BaseURL, "model": input.Model, "enabled": false, "has_key": encrypted != ""})
}

func (s *Server) activateAIConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "not_found", "模型配置不存在")
		return
	}
	c := claimsFrom(r)
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, r, 500, "ai_config_activate_failed", "切换模型配置失败")
		return
	}
	defer tx.Rollback()
	var exists bool
	err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM ai_config WHERE id=$1 AND team_id=$2)`, id, c.TeamID).Scan(&exists)
	if err != nil || !exists {
		writeError(w, r, 404, "not_found", "模型配置不存在")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE ai_config SET enabled=FALSE WHERE team_id=$1`, c.TeamID); err != nil {
		writeError(w, r, 500, "ai_config_activate_failed", "停用旧模型配置失败")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE ai_config SET enabled=TRUE,updated_at=NOW() WHERE id=$1 AND team_id=$2`, id, c.TeamID); err != nil {
		writeError(w, r, 500, "ai_config_activate_failed", "启用模型配置失败")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, r, 500, "ai_config_activate_failed", "提交模型配置失败")
		return
	}
	writeJSON(w, 200, map[string]bool{"activated": true})
}

func (s *Server) deleteAIConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, r, 404, "not_found", "模型配置不存在")
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM ai_config WHERE id=$1 AND team_id=$2`, id, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "ai_config_delete_failed", "删除模型配置失败")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, r, 404, "not_found", "模型配置不存在")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) testAI(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	model, key, err := s.modelCredential(r.Context(), c.TeamID)
	if err != nil {
		writeError(w, r, 422, "ai_config_invalid", err.Error())
		return
	}
	if key == "" {
		writeError(w, r, 422, "ai_key_missing", "尚未配置 AI_API_KEY；本地规则引擎仍可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, openAIURL(model.BaseURL, "/models"), nil)
	if err != nil {
		writeError(w, r, 422, "ai_url_invalid", "模型 Base URL 无效")
		return
	}
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		writeError(w, r, 502, "ai_connection_failed", "模型服务连接失败")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeError(w, r, 502, "ai_connection_failed", fmt.Sprintf("模型服务返回 HTTP %d", response.StatusCode))
		return
	}
	writeJSON(w, 200, map[string]any{"message": "模型连接成功", "elapsed_ms": time.Since(started).Milliseconds()})
}

func openAIURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	for strings.HasSuffix(base, "/v1/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + path
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var input modelSettingsRequest
	if !decode(w, r, &input) {
		return
	}
	c := claimsFrom(r)
	// Merge onto the stored configuration so unsent fields keep their value and
	// the analysis thresholds are not silently reset by the model form.
	current := defaultModelSettings(s.Config.AIBaseURL, s.Config.AIModel)
	var storedKey string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT name,base_url,model,api_key,min_sample,score_completion_weight,score_interaction_weight,temperature,request_mode,auth_type,enabled,fallback_enabled,COALESCE(platform_weights_json,'{}') FROM ai_config WHERE team_id=$1 ORDER BY updated_at DESC LIMIT 1`, c.TeamID).Scan(&current.Name, &current.BaseURL, &current.Model, &storedKey, &current.MinSample, &current.CompletionWeight, &current.InteractionWeight, &current.Temperature, &current.RequestMode, &current.AuthType, &current.Enabled, &current.FallbackEnabled, &current.PlatformWeightsRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, 500, "settings_failed", "读取模型配置失败")
		return
	}
	value, err := mergeModelSettings(current, input)
	if err != nil {
		writeError(w, r, 422, "settings_invalid", err.Error())
		return
	}
	apiKey := ""
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		encrypted, err := service.SealSecret(s.Config.AppSecret, strings.TrimSpace(*input.APIKey))
		if err != nil {
			writeError(w, r, 500, "credential_encrypt_failed", "安全保存模型密钥失败")
			return
		}
		apiKey = encrypted
	}
	var id int64
	err = s.Store.DB.QueryRowContext(r.Context(), `SELECT id FROM ai_config WHERE team_id=$1 ORDER BY updated_at DESC LIMIT 1`, c.TeamID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO ai_config(team_id,name,base_url,api_key,model,temperature,request_mode,auth_type,enabled,fallback_enabled,score_completion_weight,score_interaction_weight,min_sample,platform_weights_json,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NOW())`, c.TeamID, value.Name, value.BaseURL, apiKey, value.Model, value.Temperature, value.RequestMode, value.AuthType, value.Enabled, value.FallbackEnabled, value.CompletionWeight, value.InteractionWeight, value.MinSample, value.PlatformWeightsRaw)
	} else if err == nil {
		if apiKey == "" {
			_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE ai_config SET name=$1,base_url=$2,model=$3,temperature=$4,request_mode=$5,auth_type=$6,score_completion_weight=$7,score_interaction_weight=$8,min_sample=$9,enabled=$10,fallback_enabled=$11,platform_weights_json=$12,updated_at=NOW() WHERE id=$13 AND team_id=$14`, value.Name, value.BaseURL, value.Model, value.Temperature, value.RequestMode, value.AuthType, value.CompletionWeight, value.InteractionWeight, value.MinSample, value.Enabled, value.FallbackEnabled, value.PlatformWeightsRaw, id, c.TeamID)
		} else {
			_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE ai_config SET name=$1,base_url=$2,api_key=$3,model=$4,temperature=$5,request_mode=$6,auth_type=$7,score_completion_weight=$8,score_interaction_weight=$9,min_sample=$10,enabled=$11,fallback_enabled=$12,platform_weights_json=$13,updated_at=NOW() WHERE id=$14 AND team_id=$15`, value.Name, value.BaseURL, apiKey, value.Model, value.Temperature, value.RequestMode, value.AuthType, value.CompletionWeight, value.InteractionWeight, value.MinSample, value.Enabled, value.FallbackEnabled, value.PlatformWeightsRaw, id, c.TeamID)
		}
	}
	if err != nil {
		writeError(w, r, 500, "settings_save_failed", "保存模型配置失败")
		return
	}
	s.settings(w, r)
}

// overviewTopContent is one entry of the published-content ranking. The score is
// computed from the team's configured weights so it matches the analysis page.
type overviewTopContent struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Category        string  `json:"category"`
	Platform        string  `json:"platform"`
	Completion      float64 `json:"completion"`
	InteractionRate float64 `json:"interaction_rate"`
	Score           float64 `json:"score"`
	PublishedAt     string  `json:"published_at"`
}

// overviewResponse is the statistics contract consumed by the operations
// overview. Every field is always present, so the page never renders undefined.
type overviewResponse struct {
	PublishedCount    int64                `json:"published_count"`
	UnpublishedCount  int64                `json:"unpublished_count"`
	TotalContentCount int64                `json:"total_content_count"`
	TotalPosts        int64                `json:"total_posts"`
	TotalViews        int64                `json:"total_views"`
	TotalLikes        int64                `json:"total_likes"`
	TotalInteractions int64                `json:"total_interactions"`
	PlatformCount     int64                `json:"platform_count"`
	AvgCompletion     float64              `json:"avg_completion"`
	SampleCount       int64                `json:"sample_count"`
	TopContents       []overviewTopContent `json:"top_contents"`
	Weights           map[string]float64   `json:"weights"`
	Performances      performanceOverview  `json:"performances"`
}

// performanceOverview is the 正式演出 dimension of the operations overview. All
// values are derived from persisted shows, content links and plans; missing data
// is reported as zero or null instead of a fabricated number.
type performanceOverview struct {
	PreparingCount         int64    `json:"preparing_count"`
	WarmingUpCount         int64    `json:"warming_up_count"`
	PlayedCount            int64    `json:"played_count"`
	CanceledCount          int64    `json:"canceled_count"`
	LinkedPerformanceCount int64    `json:"linked_performance_count"`
	UnpublishedPromoCount  int64    `json:"unpublished_promo_count"`
	PlannedCount           int64    `json:"planned_count"`
	PlanTotal              int64    `json:"plan_total"`
	ScheduleCompletionRate *float64 `json:"schedule_completion_rate"`
	NextStartsAt           string   `json:"next_starts_at"`
	NextDaysToStart        *int     `json:"next_days_to_start"`
}

// buildOverviewResponse normalizes one aggregate row into the response contract.
// Missing or null columns become zero values and an empty ranking, never nil.
func buildOverviewResponse(row map[string]any, top []overviewTopContent, weights map[string]float64) overviewResponse {
	published := intField(row, "published_count")
	result := overviewResponse{
		PublishedCount:    published,
		UnpublishedCount:  intField(row, "unpublished_count"),
		TotalContentCount: intField(row, "total_content_count"),
		TotalPosts:        published,
		TotalViews:        intField(row, "total_views"),
		TotalLikes:        intField(row, "total_likes"),
		TotalInteractions: intField(row, "total_interactions"),
		PlatformCount:     intField(row, "platform_count"),
		AvgCompletion:     floatField(row, "avg_completion"),
		SampleCount:       intField(row, "sample_count"),
		TopContents:       top,
		Weights:           weights,
	}
	if result.TopContents == nil {
		result.TopContents = []overviewTopContent{}
	}
	return result
}

// overview returns the published-content aggregates plus a ranked list of the
// best performing published assets.
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	rows, err := s.Store.QueryMaps(r.Context(), `SELECT COUNT(*) AS total_content_count, COUNT(*) FILTER (WHERE lifecycle='已发布') AS published_count, COUNT(*) FILTER (WHERE lifecycle='未发布') AS unpublished_count, COALESCE(SUM(views) FILTER (WHERE lifecycle='已发布'),0) AS total_views, COALESCE(SUM(likes) FILTER (WHERE lifecycle='已发布'),0) AS total_likes, COALESCE(SUM(total_interactions) FILTER (WHERE lifecycle='已发布'),0) AS total_interactions, COUNT(DISTINCT platform) FILTER (WHERE lifecycle='已发布') AS platform_count, COALESCE(AVG(completion) FILTER (WHERE lifecycle='已发布'),0) AS avg_completion, COUNT(*) FILTER (WHERE lifecycle='已发布') AS sample_count FROM content WHERE team_id=$1`, c.TeamID)
	if err != nil {
		writeError(w, r, 500, "stats_failed", "统计内容表现失败")
		return
	}
	var row map[string]any
	if len(rows) > 0 {
		row = rows[0]
	}
	completionWeight, interactionWeight := .6, .4
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT score_completion_weight,score_interaction_weight FROM ai_config WHERE team_id=$1 ORDER BY updated_at DESC LIMIT 1`, c.TeamID).Scan(&completionWeight, &interactionWeight)
	weights := map[string]float64{"completion": completionWeight, "interaction": interactionWeight}
	top, err := s.Store.QueryMaps(r.Context(), `SELECT id,title,category,platform,published_at,views,total_interactions,completion FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' ORDER BY (completion*$2 + (CASE WHEN views=0 THEN 0 ELSE 100.0*total_interactions/views END)*$3) DESC, published_at DESC, id LIMIT 8`, c.TeamID, completionWeight, interactionWeight)
	if err != nil {
		writeError(w, r, 500, "stats_failed", "统计优质内容失败")
		return
	}
	response := buildOverviewResponse(row, rankTopContents(top, weights), weights)
	performances, err := s.performanceOverviewStats(r.Context(), c.TeamID)
	if err != nil {
		writeError(w, r, 500, "stats_failed", "统计演出维度失败")
		return
	}
	response.Performances = performances
	writeJSON(w, http.StatusOK, response)
}

// performanceOverviewStats aggregates the show dimension used by both the
// operations overview and the home page, so the two pages always agree.
func (s *Server) performanceOverviewStats(ctx context.Context, teamID int64) (performanceOverview, error) {
	stats := performanceOverview{}
	if _, err := s.Store.SettlePerformanceStatuses(ctx, teamID); err != nil {
		return stats, err
	}
	rows, err := s.Store.QueryMaps(ctx, `
		SELECT COUNT(*) FILTER (WHERE booking_status='筹备中') AS preparing_count,
		       COUNT(*) FILTER (WHERE booking_status='预热中') AS warming_up_count,
		       COUNT(*) FILTER (WHERE booking_status='已演出') AS played_count,
		       COUNT(*) FILTER (WHERE booking_status='已取消') AS canceled_count,
		       MIN(starts_at) FILTER (WHERE starts_at >= LOCALTIMESTAMP AND booking_status <> '已取消') AS next_starts_at
		FROM performance_booking WHERE team_id=$1`, teamID)
	if err != nil {
		return stats, err
	}
	if len(rows) > 0 {
		row := rows[0]
		stats.PreparingCount = intField(row, "preparing_count")
		stats.WarmingUpCount = intField(row, "warming_up_count")
		stats.PlayedCount = intField(row, "played_count")
		stats.CanceledCount = intField(row, "canceled_count")
		if nextAt, ok := timeField(row, "next_starts_at"); ok {
			now := time.Now()
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			nextDay := time.Date(nextAt.Year(), nextAt.Month(), nextAt.Day(), 0, 0, 0, 0, nextAt.Location())
			days := int(nextDay.Sub(today).Hours() / 24)
			stats.NextStartsAt = nextAt.Format(time.RFC3339)
			stats.NextDaysToStart = &days
		}
	}
	promo, err := s.Store.QueryMaps(ctx, `
		SELECT COUNT(DISTINCT c.performance_id) AS linked_performance_count,
		       COUNT(*) FILTER (WHERE c.lifecycle='未发布') AS unpublished_promo_count,
		       COUNT(*) AS plan_total,
		       COUNT(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM schedule s
		           WHERE s.team_id=c.team_id AND s.content_id=c.id AND s.status='待发布'
		       )) AS planned_count
		FROM content c WHERE c.team_id=$1 AND c.performance_id IS NOT NULL`, teamID)
	if err != nil {
		return stats, err
	}
	if len(promo) > 0 {
		row := promo[0]
		stats.LinkedPerformanceCount = intField(row, "linked_performance_count")
		stats.UnpublishedPromoCount = intField(row, "unpublished_promo_count")
		stats.PlanTotal = intField(row, "plan_total")
		stats.PlannedCount = intField(row, "planned_count")
		if stats.PlanTotal > 0 {
			rate := roundTo(100*float64(stats.PlannedCount)/float64(stats.PlanTotal), 2)
			stats.ScheduleCompletionRate = &rate
		}
	}
	return stats, nil
}

// rankTopContents computes the composite score in Go so the numeric type of the
// value is explicit, then applies a deterministic ordering.
func rankTopContents(rows []map[string]any, weights map[string]float64) []overviewTopContent {
	items := make([]overviewTopContent, 0, len(rows))
	for _, row := range rows {
		views := intField(row, "views")
		completion := floatField(row, "completion")
		interactionRate := 0.0
		if views > 0 {
			interactionRate = 100 * float64(intField(row, "total_interactions")) / float64(views)
		}
		items = append(items, overviewTopContent{
			ID:              stringField(row, "id"),
			Title:           stringField(row, "title"),
			Category:        stringField(row, "category"),
			Platform:        stringField(row, "platform"),
			Completion:      roundTo(completion, 2),
			InteractionRate: roundTo(interactionRate, 2),
			Score:           roundTo(completion*weights["completion"]+interactionRate*weights["interaction"], 2),
			PublishedAt:     timestampField(row, "published_at"),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if items[i].PublishedAt != items[j].PublishedAt {
			return items[i].PublishedAt > items[j].PublishedAt
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func (s *Server) platformStats(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT platform AS name,COUNT(*) AS sample_count,SUM(views) AS total_views,SUM(likes) AS total_likes FROM content WHERE team_id=$1 AND lifecycle='已发布' GROUP BY platform ORDER BY SUM(views) DESC`, c.TeamID)
	if err != nil {
		writeError(w, r, 500, "platform_stats_failed", "读取平台表现失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (s *Server) trendStats(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT date_trunc('month',published_at)::date AS month,COUNT(*) AS sample_count,SUM(views) AS views,SUM(total_interactions) AS total_interactions,AVG(completion) AS avg_completion FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' GROUP BY month ORDER BY month`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "trend_failed", "读取发布趋势失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

// Rule-set availability errors are answered with 503; everything else means the
// submitted plan itself was rejected.
var (
	errRuleSetUnavailable = errors.New("数据库中没有可用的只读排期规则集")
	errRuleSetInvalid     = errors.New("排期规则集格式无效")
)

// loadPlanningRules reads the effective read-only rule set and derives the
// inclusive planning window every scheduling path shares.
func (s *Server) loadPlanningRules(ctx context.Context) (service.SlotPolicy, domain.PlanningWindow, error) {
	var policy service.SlotPolicy
	var raw []byte
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT payload_json->'schedule_policy' FROM system_rule_set WHERE enabled=TRUE ORDER BY updated_at DESC LIMIT 1`).Scan(&raw); err != nil {
		return policy, domain.PlanningWindow{}, errRuleSetUnavailable
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		return policy, domain.PlanningWindow{}, errRuleSetInvalid
	}
	window, ok := policy.Window(time.Now())
	if !ok || len(policy.TimeWindows) == 0 {
		return policy, domain.PlanningWindow{}, errRuleSetInvalid
	}
	return policy, window, nil
}

// schedulePolicyResponse is the typed contract the web client uses to render
// the planning window and to pre-check edited suggestions.
type schedulePolicyResponse struct {
	HorizonDays           int      `json:"planning_horizon_days"`
	HorizonDefinition     string   `json:"horizon_definition"`
	TimeWindows           []string `json:"time_windows"`
	Weekdays              []string `json:"weekdays"`
	MaxPerDay             int      `json:"max_per_day"`
	MaxPerPlatformPerWeek int      `json:"max_per_platform_per_week"`
	WindowStart           string   `json:"window_start"`
	WindowEnd             string   `json:"window_end"`
}

func (s *Server) schedulePolicy(w http.ResponseWriter, r *http.Request) {
	policy, window, err := s.loadPlanningRules(r.Context())
	if err != nil {
		writeRuleSetError(w, r, err)
		return
	}
	writeJSON(w, 200, schedulePolicyResponse{
		HorizonDays:           policy.HorizonDays,
		HorizonDefinition:     "planning_horizon_days 为从今天起（含今天）共 N 个自然日",
		TimeWindows:           policy.TimeWindows,
		Weekdays:              policy.Weekdays,
		MaxPerDay:             policy.MaxPerDay,
		MaxPerPlatformPerWeek: policy.MaxPerPlatformPerWeek,
		WindowStart:           window.Start.Format("2006-01-02"),
		WindowEnd:             window.End.Format("2006-01-02"),
	})
}

// writeRuleSetError answers rule-set availability problems with 503 and plan
// rejections with 422, so the web client can tell them apart.
func writeRuleSetError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errRuleSetUnavailable) || errors.Is(err, errRuleSetInvalid) {
		writeError(w, r, http.StatusServiceUnavailable, "schedule_rules_unavailable", err.Error())
		return
	}
	writeError(w, r, http.StatusUnprocessableEntity, "schedule_policy_rejected", err.Error())
}

func (s *Server) adoptionGains(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT id AS content_id,title,adopted,completion,CASE WHEN views=0 THEN 0 ELSE ROUND(100.0*total_interactions/views,2) END AS interaction_rate,1 AS sample_count FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' AND recommendation_id IS NOT NULL ORDER BY published_at DESC`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "adoption_gains_failed", "读取排期采纳效果失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

func (s *Server) categoryStats(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT category AS name,COUNT(*) AS sample_count,AVG(views) AS views,AVG(completion) AS completion,CASE WHEN SUM(views)=0 THEN 0 ELSE 100.0*SUM(total_interactions)/SUM(views) END AS interaction_rate FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' GROUP BY category ORDER BY category`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "category_stats_failed", "读取内容类型表现失败")
		return
	}
	minSample := 3
	completionWeight, interactionWeight := .6, .4
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT min_sample,score_completion_weight,score_interaction_weight FROM ai_config WHERE team_id=$1 ORDER BY updated_at DESC LIMIT 1`, claimsFrom(r).TeamID).Scan(&minSample, &completionWeight, &interactionWeight)
	weights := map[string]float64{"completion": completionWeight, "interaction": interactionWeight}
	for _, row := range rows {
		if count, ok := row["sample_count"].(int64); ok && count < int64(minSample) {
			row["views"] = nil
			row["completion"] = nil
			row["interaction_rate"] = nil
		}
	}
	writeJSON(w, 200, map[string]any{"items": rows, "min_sample": minSample, "weights": weights})
}

func (s *Server) heatmap(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT ((EXTRACT(DOW FROM published_at)::int+6)%7) AS weekday,CASE WHEN EXTRACT(HOUR FROM published_at)<11 THEN '上午' WHEN EXTRACT(HOUR FROM published_at)<14 THEN '午间' WHEN EXTRACT(HOUR FROM published_at)<18 THEN '下午' WHEN EXTRACT(HOUR FROM published_at)<21 THEN '晚间' ELSE '夜间' END AS time_slot,COUNT(*) AS sample_count,AVG(completion) AS completion FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' GROUP BY weekday,time_slot ORDER BY weekday,time_slot`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "heatmap_failed", "读取星期与时段表现失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

func (s *Server) adoptionStats(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT adopted,AVG(completion) AS avg_completion,COUNT(*) AS sample_count FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' GROUP BY adopted`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "adoption_stats_failed", "读取采纳效果失败")
		return
	}
	result := map[string]any{"adopted": map[string]any{"avg_completion": nil, "sample_count": 0}, "not_adopted": map[string]any{"avg_completion": nil, "sample_count": 0}, "completion_difference": nil}
	for _, row := range rows {
		key := "not_adopted"
		if row["adopted"] == true {
			key = "adopted"
		}
		result[key] = map[string]any{"avg_completion": row["avg_completion"], "sample_count": row["sample_count"]}
	}
	writeJSON(w, 200, result)
}

func (s *Server) funnelStats(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT COALESCE(SUM(views),0) AS views,COALESCE(SUM(profile_visits),0) AS profile_visits,COALESCE(SUM(follows),0) AS follows,COALESCE(SUM(attendance),0) AS attendance,COALESCE(SUM(tickets),0) AS tickets,COUNT(*) AS sample_count FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布'`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "funnel_failed", "读取转化漏斗失败")
		return
	}
	if len(rows) == 0 {
		writeJSON(w, 200, map[string]any{})
		return
	}
	writeJSON(w, 200, rows[0])
}

func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.QueryMaps(r.Context(), `SELECT lifecycle,COUNT(*) AS count FROM content WHERE team_id=$1 GROUP BY lifecycle`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "diagnostics_failed", "读取运营提醒失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": buildDiagnostics(rows)})
}

// diagnosticItem is one plain-language operations reminder.
type diagnosticItem struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// buildDiagnostics turns lifecycle aggregates into reminders. The level drives
// the icon on the overview page, so it always uses a known value.
func buildDiagnostics(rows []map[string]any) []diagnosticItem {
	var published, unpublished int64
	for _, row := range rows {
		switch stringField(row, "lifecycle") {
		case domain.LifecyclePublished:
			published = intField(row, "count")
		case domain.LifecycleUnpublished:
			unpublished = intField(row, "count")
		}
	}
	items := make([]diagnosticItem, 0, 3)
	switch {
	case published == 0 && unpublished == 0:
		items = append(items, diagnosticItem{Level: "info", Message: "当前团队还没有内容记录，先录入一条内容资产再开始复盘。"})
	default:
		if published == 0 {
			items = append(items, diagnosticItem{Level: "info", Message: "还没有已发布内容，录入实际发布数据后才会生成效果分析。"})
		} else {
			items = append(items, diagnosticItem{Level: "good", Message: fmt.Sprintf("已有 %d 条已发布内容纳入复盘，样本越多结论越可靠。", published)})
		}
		if unpublished > 0 {
			items = append(items, diagnosticItem{Level: "warn", Message: fmt.Sprintf("还有 %d 条未发布内容尚未排期，建议生成排期建议并确认负责人。", unpublished)})
		}
	}
	return items
}

func (s *Server) sampleThreshold(w http.ResponseWriter, r *http.Request) {
	var input struct {
		MinSample int `json:"min_sample"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.MinSample < 1 || input.MinSample > 1000 {
		writeError(w, r, 422, "threshold_invalid", "样本门槛应为 1 到 1000")
		return
	}
	teamID := claimsFrom(r).TeamID
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE ai_config SET min_sample=$1,updated_at=NOW() WHERE team_id=$2`, input.MinSample, teamID)
	if err != nil {
		writeError(w, r, 500, "threshold_update_failed", "更新样本门槛失败")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, r, 409, "model_config_missing", "请先保存模型/分析设置")
		return
	}
	writeJSON(w, 200, map[string]any{"min_sample": input.MinSample})
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT actor,action,entity_type,entity_id,detail,created_at FROM audit_log WHERE team_id=$1 ORDER BY created_at DESC LIMIT 100`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "audit_failed", "读取操作审计失败")
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) hypotheses(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT id,statement,result,status,created_at FROM hypothesis WHERE team_id=$1 ORDER BY created_at DESC`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "hypotheses_failed", "读取运营假设失败")
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) createHypothesis(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Statement string `json:"statement"`
		Result    string `json:"result"`
		Status    string `json:"status"`
	}
	if !decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Statement) == "" {
		writeError(w, r, 422, "hypothesis_invalid", "假设内容不能为空")
		return
	}
	if input.Status == "" {
		input.Status = "待验证"
	}
	id := uuid.NewString()
	c := claimsFrom(r)
	_, err := s.Store.DB.ExecContext(r.Context(), `INSERT INTO hypothesis(id,team_id,statement,result,status,created_at) VALUES($1,$2,$3,$4,$5,NOW())`, id, c.TeamID, input.Statement, input.Result, input.Status)
	if err != nil {
		writeError(w, r, 500, "hypothesis_save_failed", "保存运营假设失败")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "statement": input.Statement, "result": input.Result, "status": input.Status})
}

func (s *Server) updateHypothesis(w http.ResponseWriter, r *http.Request) {
	var input map[string]any
	if !decode(w, r, &input) {
		return
	}
	status, _ := input["status"].(string)
	resultText, _ := input["result"].(string)
	if status == "" && resultText == "" {
		writeError(w, r, 422, "hypothesis_invalid", "没有可更新的假设字段")
		return
	}
	res, err := s.Store.DB.ExecContext(r.Context(), `UPDATE hypothesis SET status=COALESCE(NULLIF($1,''),status),result=COALESCE(NULLIF($2,''),result) WHERE id=$3 AND team_id=$4`, status, resultText, r.PathValue("id"), claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "hypothesis_update_failed", "更新运营假设失败")
		return
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		writeError(w, r, 404, "not_found", "运营假设不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"updated": true})
}

func (s *Server) handover(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	team, _ := s.Store.TeamName(r.Context(), teamID)
	stats, err := s.Store.ListTeamRows(r.Context(), `SELECT COUNT(*) FILTER(WHERE lifecycle='已发布') AS published_count,COUNT(*) FILTER(WHERE lifecycle='未发布') AS unpublished_count,COALESCE(SUM(views) FILTER(WHERE lifecycle='已发布'),0) AS total_views FROM content WHERE team_id=$1`, teamID)
	if err != nil {
		writeError(w, r, 500, "handover_failed", "生成交接数据失败")
		return
	}
	schedules, _ := s.Store.ListTeamRows(r.Context(), `SELECT id,title,planned_date,planned_time,status,owner FROM schedule WHERE team_id=$1 AND status='待发布' ORDER BY planned_date LIMIT 50`, teamID)
	writeJSON(w, 200, map[string]any{"team_name": team, "summary": stats, "upcoming_schedules": schedules})
}

func (s *Server) exportHandover(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	team, _ := s.Store.TeamName(r.Context(), teamID)
	stats, _ := s.Store.ListTeamRows(r.Context(), `SELECT COUNT(*) FILTER(WHERE lifecycle='已发布') AS published_count,COUNT(*) FILTER(WHERE lifecycle='未发布') AS unpublished_count,COALESCE(SUM(views) FILTER(WHERE lifecycle='已发布'),0) AS total_views FROM content WHERE team_id=$1`, teamID)
	schedules, _ := s.Store.ListTeamRows(r.Context(), `SELECT title,planned_date,planned_time,status,owner FROM schedule WHERE team_id=$1 AND status='待发布' ORDER BY planned_date LIMIT 50`, teamID)
	if r.URL.Query().Get("format") == "markdown" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=arts-smart-handover.md")
		_, _ = fmt.Fprintf(w, "# %s 团队交接\n\n## 内容概况\n\n%s\n\n## 待发布排期\n\n", team, fmt.Sprint(stats))
		for _, row := range schedules {
			_, _ = fmt.Fprintf(w, "- %s · %s %s · %s · %s\n", row["title"], row["planned_date"], row["planned_time"], row["status"], row["owner"])
		}
		return
	}
	writeJSON(w, 200, map[string]any{"team_name": team, "summary": stats, "upcoming_schedules": schedules})
}

// exportReport renders the operations report as a real PDF document. It uses the
// same filtered statistics as the dashboard, so the exported numbers always match
// the page the user is looking at.
func (s *Server) exportReport(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	filter, err := parseStatsFilter(r)
	if err != nil {
		writeError(w, r, 422, "stats_filter_invalid", err.Error())
		return
	}
	response, err := s.buildDashboard(r.Context(), teamID, filter)
	if err != nil {
		writeError(w, r, 500, "report_failed", "生成运营报告失败")
		return
	}
	team, _ := s.Store.TeamName(r.Context(), teamID)
	document := report.Document{
		TeamName:    team,
		GeneratedAt: response.GeneratedAt,
		Range:       response.RangeLabel,
		Filters:     response.Filters,
		Summary: [][2]string{
			{"已发布内容（条）", fmt.Sprintf("%d", response.Current.PublishedCount)},
			{"总播放量", fmt.Sprintf("%d", response.Current.TotalViews)},
			{"总点赞量", fmt.Sprintf("%d", response.Current.TotalLikes)},
			{"总评论量", fmt.Sprintf("%d", response.Current.TotalComments)},
			{"总收藏量", fmt.Sprintf("%d", response.Current.TotalSaves)},
			{"总互动量", fmt.Sprintf("%d（互动率 %.2f%%）", response.Current.TotalInteractions, response.Current.InteractionRate)},
			{"平均看完比例", fmt.Sprintf("%.2f%%", response.Current.AvgCompletion)},
		},
		Footer: "指标口径：仅统计 lifecycle = 已发布 且发布时间落在所选范围内的内容；互动量 = 点赞 + 评论 + 收藏 + 转发。",
	}
	platformTable := report.Table{Title: "平台表现", Headers: []string{"平台", "发布数", "播放量", "互动量", "互动率", "平均看完"}}
	for _, row := range response.Platforms {
		platformTable.Rows = append(platformTable.Rows, []string{row.Name, fmt.Sprintf("%d", row.PublishedCount), fmt.Sprintf("%d", row.TotalViews), fmt.Sprintf("%d", row.TotalInteractions), fmt.Sprintf("%.2f%%", row.InteractionRate), fmt.Sprintf("%.2f%%", row.AvgCompletion)})
	}
	document.Tables = append(document.Tables, platformTable)
	categoryTable := report.Table{Title: "内容类型表现", Headers: []string{"内容类型", "发布数", "播放量", "互动量"}}
	for _, row := range response.Categories {
		categoryTable.Rows = append(categoryTable.Rows, []string{row.Name, fmt.Sprintf("%d", row.PublishedCount), fmt.Sprintf("%d", row.TotalViews), fmt.Sprintf("%d", row.TotalInteractions)})
	}
	document.Tables = append(document.Tables, categoryTable)
	workflowTable := report.Table{Title: "工作流分布", Headers: []string{"内容分类", "数量"}, Note: "未发布与已发布为发布结果视图，其余为工作流状态。"}
	for _, row := range response.Workflow {
		workflowTable.Rows = append(workflowTable.Rows, []string{row.State, fmt.Sprintf("%d", row.Count)})
	}
	document.Tables = append(document.Tables, workflowTable)
	trendTable := report.Table{Title: "发布趋势", Headers: []string{"时间", "发布数", "播放量", "互动量", "平均看完"}}
	for _, point := range response.Trend {
		trendTable.Rows = append(trendTable.Rows, []string{point.Bucket, fmt.Sprintf("%d", point.PublishedCount), fmt.Sprintf("%d", point.TotalViews), fmt.Sprintf("%d", point.TotalInteractions), fmt.Sprintf("%.2f%%", point.AvgCompletion)})
	}
	document.Tables = append(document.Tables, trendTable)

	content, err := report.Render(document)
	if err != nil {
		writeError(w, r, 500, "report_render_failed", "生成 PDF 文件失败")
		return
	}
	filename := "arts-smart-report-" + time.Now().Format("20060102") + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) insight(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Question string `json:"question"`
	}
	if !decode(w, r, &input) {
		return
	}
	teamID := claimsFrom(r).TeamID
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT category,COUNT(*) AS sample_count,AVG(views) AS avg_views,AVG(completion) AS avg_completion FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' GROUP BY category ORDER BY AVG(completion) DESC`, teamID)
	if err != nil {
		writeError(w, r, 500, "insight_failed", "读取团队历史表现失败")
		return
	}
	count := 0
	for _, row := range rows {
		if n, ok := row["sample_count"].(int64); ok {
			count += int(n)
		}
	}
	answer := "当前团队还没有已发布内容样本，暂时不能从历史表现推断。"
	source := "规则引擎"
	if len(rows) > 0 {
		answer = fmt.Sprintf("基于当前团队 %d 条已发布样本，完播率最高的内容类型为 %v；该结论只描述现有记录，不代表未来表现。", count, rows[0]["category"])
	}
	model, key, configErr := s.modelCredential(r.Context(), teamID)
	if configErr == nil && key != "" && input.Question != "" {
		if generated, aiErr := s.callInsightText(r.Context(), model, key, input.Question, rows); aiErr == nil {
			answer = generated
			source = "AI"
		}
	}
	writeJSON(w, 200, map[string]any{"answer": answer, "source": source, "sample_count": count})
}

func (s *Server) callInsightText(ctx context.Context, model aiModelConfig, key, question string, summary []map[string]any) (string, error) {
	data, _ := json.Marshal(summary)
	payload, err := json.Marshal(map[string]any{"model": model.Model, "temperature": .2, "messages": []map[string]string{{"role": "system", "content": "回答只可依据给定的已发布团队统计。样本为空时说明无法推断，不得编造。简洁中文回答。"}, {"role": "user", "content": "问题：" + question + "\n数据：" + string(data)}}})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIURL(model.BaseURL, "/chat/completions"), strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("模型返回错误状态")
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil || len(result.Choices) == 0 {
		return "", errors.New("模型响应无效")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func (s *Server) benchmarks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT category,platform,completion_median,interaction_median,source_type,CASE WHEN source_type='manual' THEN '团队调整值' ELSE '团队自身历史' END AS source_label FROM benchmark WHERE team_id=$1 ORDER BY category,platform`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "benchmark_failed", "读取团队基准失败")
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) updateBenchmark(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Category    string  `json:"category"`
		Platform    string  `json:"platform"`
		Completion  float64 `json:"completion_median"`
		Interaction float64 `json:"interaction_median"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Category == "" || input.Completion < 0 || input.Completion > 100 || input.Interaction < 0 {
		writeError(w, r, 422, "benchmark_invalid", "基准值范围无效")
		return
	}
	c := claimsFrom(r)
	var id int64
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id FROM benchmark WHERE team_id=$1 AND category=$2 AND platform=$3 LIMIT 1`, c.TeamID, input.Category, input.Platform).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO benchmark(team_id,category,platform,completion_median,interaction_median,source_type) VALUES($1,$2,$3,$4,$5,'manual')`, c.TeamID, input.Category, input.Platform, input.Completion, input.Interaction)
	} else if err == nil {
		_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE benchmark SET completion_median=$1,interaction_median=$2,source_type='manual' WHERE id=$3 AND team_id=$4`, input.Completion, input.Interaction, id, c.TeamID)
	}
	if err != nil {
		writeError(w, r, 500, "benchmark_save_failed", "保存团队基准失败")
		return
	}
	writeJSON(w, 200, map[string]any{"saved": true})
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	if start == "" || end == "" {
		now := time.Now()
		start = now.Format("2006-01") + "-01"
		end = now.AddDate(0, 1, 0).Format("2006-01") + "-01"
	}
	if _, err := time.Parse("2006-01-02", start); err != nil {
		writeError(w, r, 422, "invalid_date", "开始日期格式无效")
		return
	}
	if _, err := time.Parse("2006-01-02", end); err != nil {
		writeError(w, r, 422, "invalid_date", "结束日期格式无效")
		return
	}
	teamID := claimsFrom(r).TeamID
	var items []map[string]any
	var err error
	if performanceID := strings.TrimSpace(r.URL.Query().Get("performance_id")); performanceID != "" {
		items, err = s.Store.ListSchedulesByPerformance(r.Context(), teamID, performanceID, start, end)
	} else {
		items, err = s.Store.ListSchedules(r.Context(), teamID, start, end)
	}
	if err != nil {
		writeError(w, r, 500, "schedule_failed", "读取排期日历失败")
		return
	}
	writeJSON(w, 200, items)
}

func (s *Server) manualSchedule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ContentID   string `json:"content_id"`
		Platform    string `json:"platform"`
		PlannedDate string `json:"planned_date"`
		PlannedTime string `json:"planned_time"`
		Owner       string `json:"owner"`
		Reason      string `json:"reason"`
	}
	if !decode(w, r, &input) {
		return
	}
	c := claimsFrom(r)
	request, err := s.buildScheduleRequest(r, c.TeamID, input.ContentID, input.Platform, input.PlannedDate, input.PlannedTime, input.Owner, input.Reason)
	if err != nil {
		writeRuleSetError(w, r, err)
		return
	}
	if err = s.validateSlot(r.Context(), c.TeamID, request.PlannedDate, request.PlannedTime, request.Platform, ""); err != nil {
		writeRuleSetError(w, r, err)
		return
	}
	item, err := s.Store.CreateManualSchedule(r.Context(), c.TeamID, c.UserID, c.Name, request)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, r, 404, "content_not_found", "内容记录不存在")
		case errors.Is(err, repository.ErrContentNotPlannable), errors.Is(err, repository.ErrContentPublished):
			writeError(w, r, 422, "content_not_plannable", err.Error())
		default:
			writeError(w, r, 409, "schedule_conflict", err.Error())
		}
		return
	}
	writeJSON(w, 201, item)
}

// manualScheduleBatch plans several 待排期 records at once. Each record carries
// its own platform, date and time, and the outcome is reported per record.
func (s *Server) manualScheduleBatch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Items []struct {
			ContentID   string `json:"content_id"`
			Platform    string `json:"platform"`
			PlannedDate string `json:"planned_date"`
			PlannedTime string `json:"planned_time"`
			Owner       string `json:"owner"`
			Reason      string `json:"reason"`
		} `json:"items"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.Items) == 0 {
		writeError(w, r, 422, "bulk_empty", "请先选择待排期内容")
		return
	}
	if len(input.Items) > 100 {
		writeError(w, r, 422, "bulk_too_large", "单次批量排期最多 100 条记录")
		return
	}
	c := claimsFrom(r)
	rules, window, err := loadPlanningRulesFrom(r.Context(), s)
	if err != nil {
		writeRuleSetError(w, r, err)
		return
	}
	requests := make([]repository.ScheduleRequest, 0, len(input.Items))
	for _, item := range input.Items {
		if !regexpUUID(item.ContentID) {
			writeError(w, r, 422, "content_id_invalid", "内容 ID 无效")
			return
		}
		if _, err = time.Parse("15:04", item.PlannedTime); err != nil {
			writeError(w, r, 422, "invalid_time", "发布时间格式必须为 HH:MM")
			return
		}
		date, parseErr := repository.ParsePlannedDate(item.PlannedDate)
		if parseErr != nil {
			writeError(w, r, 422, "invalid_date", parseErr.Error())
			return
		}
		if strings.TrimSpace(item.Platform) == "" {
			writeError(w, r, 422, "platform_required", "每条记录都需要选择发布平台")
			return
		}
		requests = append(requests, repository.ScheduleRequest{ContentID: item.ContentID, Platform: strings.TrimSpace(item.Platform), PlannedDate: date, PlannedTime: item.PlannedTime, Owner: item.Owner, Reason: item.Reason})
	}
	outcome, err := s.Store.ScheduleContents(r.Context(), c.TeamID, c.UserID, c.Name, requests, rules, window)
	if err != nil {
		writeError(w, r, 500, "schedule_batch_failed", "批量排期失败")
		return
	}
	writeJSON(w, 200, outcome)
}

// buildScheduleRequest normalizes and validates one manual plan.
func (s *Server) buildScheduleRequest(r *http.Request, teamID int64, contentID, platform, plannedDate, plannedTime, owner, reason string) (repository.ScheduleRequest, error) {
	date, err := repository.ParsePlannedDate(plannedDate)
	if err != nil {
		return repository.ScheduleRequest{}, err
	}
	if _, err = time.Parse("15:04", plannedTime); err != nil {
		return repository.ScheduleRequest{}, errors.New("发布时间格式必须为 HH:MM")
	}
	if strings.TrimSpace(platform) == "" {
		if err = s.Store.DB.QueryRowContext(r.Context(), `SELECT platform FROM content WHERE id=$1 AND team_id=$2`, contentID, teamID).Scan(&platform); err != nil {
			return repository.ScheduleRequest{}, sql.ErrNoRows
		}
	}
	return repository.ScheduleRequest{ContentID: contentID, Platform: strings.TrimSpace(platform), PlannedDate: date, PlannedTime: plannedTime, Owner: owner, Reason: reason}, nil
}

// loadPlanningRulesFrom is the package-level form of the server policy loader so
// handlers can share it without repeating the query.
func loadPlanningRulesFrom(ctx context.Context, s *Server) (domain.PlanningRules, domain.PlanningWindow, error) {
	policy, window, err := s.loadPlanningRules(ctx)
	if err != nil {
		return domain.PlanningRules{}, domain.PlanningWindow{}, err
	}
	return policy.Rules(), window, nil
}

// validateSlot applies the shared planning rules to a candidate plan, using the
// same window, weekday, time-window and frequency checks as recommendation
// adoption. excludeScheduleID keeps an already-planned row from counting itself
// when an existing plan is edited.
func (s *Server) validateSlot(ctx context.Context, teamID int64, date, clock, platform, excludeScheduleID string) error {
	rules, window, err := s.loadPlanningRules(ctx)
	if err != nil {
		return err
	}
	planned, err := time.Parse("2006-01-02", date)
	if err != nil {
		return domain.ErrSlotDateInvalid
	}
	var daily int
	if err = s.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule WHERE team_id=$1 AND status='待发布' AND planned_date=$2::date AND ($3='' OR id<>$3)`, teamID, date, excludeScheduleID).Scan(&daily); err != nil {
		return err
	}
	year, week := planned.ISOWeek()
	var weekly int
	if err = s.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule WHERE team_id=$1 AND status='待发布' AND platform=$2 AND EXTRACT(ISOYEAR FROM planned_date)=$3 AND EXTRACT(WEEK FROM planned_date)=$4 AND ($5='' OR id<>$5)`, teamID, platform, year, week, excludeScheduleID).Scan(&weekly); err != nil {
		return err
	}
	return rules.Rules().ValidateSlot(window, date, clock, domain.SlotCounts{SameDay: daily + 1, SamePlatformSameISOWeek: weekly + 1})
}

func (s *Server) adoptSchedules(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RecommendationID string           `json:"recommendation_id"`
		Items            []map[string]any `json:"items"`
	}
	if !decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.RecommendationID) == "" {
		writeError(w, r, 422, "recommendation_required", "缺少排期建议 ID")
		return
	}
	items := make([]repository.AdoptedSchedule, 0, len(input.Items))
	for _, row := range input.Items {
		item := repository.AdoptedSchedule{}
		item.TargetContentID, _ = row["target_content_id"].(string)
		item.PlannedDate, _ = row["planned_date"].(string)
		item.TimeSlot, _ = row["time_slot"].(string)
		item.Title, _ = row["title"].(string)
		if strings.TrimSpace(item.TargetContentID) == "" {
			writeError(w, r, 422, "invalid_item", "建议条目缺少目标内容")
			return
		}
		items = append(items, item)
	}
	policy, _, err := s.loadPlanningRules(r.Context())
	if err != nil {
		writeRuleSetError(w, r, err)
		return
	}
	c := claimsFrom(r)
	outcome, err := s.Store.AdoptRecommendations(r.Context(), c.TeamID, c.UserID, c.Name, input.RecommendationID, items, policy.Rules())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, r, 404, "recommendation_not_found", "排期建议不存在")
		} else {
			writeError(w, r, 422, "schedule_adoption_failed", err.Error())
		}
		return
	}
	writeJSON(w, 200, map[string]any{"created": outcome.Created, "recommendations": outcome.Recommendations})
}

func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	var fields map[string]any
	if !decode(w, r, &fields) {
		return
	}
	c := claimsFrom(r)
	scheduleID := r.PathValue("id")
	date, hasDate := fields["planned_date"].(string)
	clock, hasClock := fields["planned_time"].(string)
	if hasDate {
		parsed, err := repository.ParsePlannedDate(date)
		if err != nil {
			writeError(w, r, 422, "invalid_date", err.Error())
			return
		}
		date = parsed
		fields["planned_date"] = parsed
	}
	if hasDate || hasClock {
		var storedDate, storedClock, storedPlatform string
		if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT to_char(planned_date,'YYYY-MM-DD'),planned_time,platform FROM schedule WHERE id=$1 AND team_id=$2`, scheduleID, c.TeamID).Scan(&storedDate, &storedClock, &storedPlatform); err != nil {
			notFoundOrError(w, r, err, "排期不存在")
			return
		}
		if !hasDate {
			date = storedDate
		}
		if !hasClock {
			clock = storedClock
		}
		platform := storedPlatform
		if value, ok := fields["platform"].(string); ok && strings.TrimSpace(value) != "" {
			platform = value
		}
		if _, err := time.Parse("15:04", clock); err != nil {
			writeError(w, r, 422, "invalid_time", "发布时间格式必须为 HH:MM")
			return
		}
		if err := s.validateSlot(r.Context(), c.TeamID, date, clock, platform, scheduleID); err != nil {
			writeRuleSetError(w, r, err)
			return
		}
	}
	before, err := s.Store.QueryMaps(r.Context(), `SELECT s.*, CASE WHEN s.source='manual' THEN '用户手动' ELSE '系统自动' END AS source_label FROM schedule s WHERE s.id=$1 AND s.team_id=$2`, scheduleID, c.TeamID)
	if err != nil || len(before) == 0 {
		notFoundOrError(w, r, err, "排期不存在或更新失败")
		return
	}
	// A manual edit is persisted as such: the plan stops being reported as
	// machine-generated and the previous values are kept for later analysis.
	fields["source"] = "manual"
	item, err := s.Store.UpdateSchedule(r.Context(), c.TeamID, scheduleID, fields)
	if err != nil {
		notFoundOrError(w, r, err, "排期不存在或更新失败")
		return
	}
	beforeRow := repository.NormalizeScheduleRows(before)[0]
	if err = s.Store.LogScheduleAdjustment(r.Context(), c.TeamID, c.Name, scheduleID, describeScheduleRow(beforeRow), describeScheduleRow(item)); err != nil {
		writeError(w, r, 500, "schedule_audit_failed", "记录排期调整失败")
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) publishSchedule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title         string  `json:"title"`
		Category      string  `json:"category"`
		Platform      string  `json:"platform"`
		PublishedAt   string  `json:"published_at"`
		Views         int64   `json:"views"`
		Likes         int64   `json:"likes"`
		Comments      int64   `json:"comments"`
		Saves         int64   `json:"saves"`
		Shares        int64   `json:"shares"`
		Completion    float64 `json:"completion"`
		ProfileVisits int64   `json:"profile_visits"`
		Follows       int64   `json:"follows"`
		Attendance    int64   `json:"attendance"`
		Tickets       int64   `json:"tickets"`
		CommentText   string  `json:"comment_text"`
	}
	if !decode(w, r, &input) {
		return
	}
	var publishedAt time.Time
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, input.PublishedAt); err == nil {
			publishedAt = parsed
			break
		}
	}
	if publishedAt.IsZero() {
		writeError(w, r, 422, "invalid_published_at", "实际发布时间格式无效")
		return
	}
	result := domain.Content{Title: input.Title, Category: input.Category, Platform: input.Platform, PublishedAt: publishedAt, Views: input.Views, Likes: input.Likes, Comments: input.Comments, Saves: input.Saves, Shares: input.Shares, Completion: input.Completion, ProfileVisits: input.ProfileVisits, Follows: input.Follows, Attendance: input.Attendance, Tickets: input.Tickets, CommentText: input.CommentText}
	c := claimsFrom(r)
	err := s.Store.PublishSchedule(r.Context(), c.TeamID, c.UserID, c.Name, r.PathValue("id"), result)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, r, 404, "schedule_not_found", "排期不存在")
		} else {
			writeError(w, r, 422, "publish_failed", err.Error())
		}
		return
	}
	writeJSON(w, 200, map[string]any{"status": "已复盘", "prediction": map[string]any{"views_deviation_percent": nil}})
}

func (s *Server) generateRecommendations(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	// Schedule suggestions only cover content waiting for a schedule; every other
	// workflow state is filtered out in the query, so the rule cannot be bypassed
	// by the client.
	rows, err := s.Store.QueryMaps(r.Context(), `SELECT id,title,category,platform,owner,lifecycle,workflow_state FROM content WHERE team_id=$1 AND lifecycle='未发布' AND workflow_state='待排期' AND NOT EXISTS (SELECT 1 FROM schedule WHERE schedule.team_id=content.team_id AND schedule.content_id=content.id AND schedule.status='待发布') ORDER BY created_at`, c.TeamID)
	if performanceID := strings.TrimSpace(r.URL.Query().Get("performance_id")); performanceID != "" {
		// Optional show filter: the suggestion page can be scoped to one 正式演出
		// while still using this same generation path.
		rows, err = s.Store.QueryMaps(r.Context(), `SELECT id,title,category,platform,owner,lifecycle,workflow_state FROM content WHERE team_id=$1 AND performance_id=$2 AND lifecycle='未发布' AND workflow_state='待排期' AND NOT EXISTS (SELECT 1 FROM schedule WHERE schedule.team_id=content.team_id AND schedule.content_id=content.id AND schedule.status='待发布') ORDER BY created_at`, c.TeamID, performanceID)
	}
	if err != nil {
		writeError(w, r, 500, "candidate_query_failed", "读取待排期内容失败")
		return
	}
	candidates := make([]domain.Content, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, domain.Content{ID: fmt.Sprint(row["id"]), Title: fmt.Sprint(row["title"]), Category: fmt.Sprint(row["category"]), Platform: fmt.Sprint(row["platform"]), Owner: fmt.Sprint(row["owner"]), Lifecycle: fmt.Sprint(row["lifecycle"]), WorkflowState: fmt.Sprint(row["workflow_state"])})
	}
	policy, window, ruleErr := s.loadPlanningRules(r.Context())
	if ruleErr != nil {
		writeRuleSetError(w, r, ruleErr)
		return
	}
	policy.ExistingDaily = map[string]int{}
	policy.ExistingPlatformWeekly = map[string]int{}
	activeSchedules, activeErr := s.Store.DB.QueryContext(r.Context(), `SELECT planned_date,platform FROM schedule WHERE team_id=$1 AND status='待发布'`, c.TeamID)
	if activeErr != nil {
		writeError(w, r, 500, "schedule_capacity_failed", "读取既有排期频率失败")
		return
	}
	for activeSchedules.Next() {
		var planned time.Time
		var platform string
		if activeErr = activeSchedules.Scan(&planned, &platform); activeErr != nil {
			activeSchedules.Close()
			writeError(w, r, 500, "schedule_capacity_failed", "读取既有排期失败")
			return
		}
		policy.ExistingDaily[planned.Format("2006-01-02")]++
		year, week := planned.ISOWeek()
		key := platform + ":" + strconv.Itoa(year) + ":" + strconv.Itoa(week)
		policy.ExistingPlatformWeekly[key]++
	}
	if activeErr = activeSchedules.Err(); activeErr != nil {
		activeSchedules.Close()
		writeError(w, r, 500, "schedule_capacity_failed", "读取既有排期失败")
		return
	}
	activeSchedules.Close()
	var samples []service.PerformanceSample
	sampleRows, err := s.Store.DB.QueryContext(r.Context(), `SELECT category,platform,published_at,views,likes,comments,saves,shares,completion FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' ORDER BY published_at DESC`, c.TeamID)
	if err != nil {
		writeError(w, r, 500, "history_query_failed", "读取已发布历史表现失败")
		return
	}
	for sampleRows.Next() {
		var item service.PerformanceSample
		if err = sampleRows.Scan(&item.Category, &item.Platform, &item.PublishedAt, &item.Views, &item.Likes, &item.Comments, &item.Saves, &item.Shares, &item.Completion); err != nil {
			sampleRows.Close()
			writeError(w, r, 500, "history_decode_failed", "读取历史表现数据失败")
			return
		}
		samples = append(samples, item)
	}
	if err = sampleRows.Err(); err != nil {
		sampleRows.Close()
		writeError(w, r, 500, "history_query_failed", "读取已发布历史表现失败")
		return
	}
	sampleRows.Close()
	local := service.BuildRuleSuggestions(time.Now(), candidates, samples, policy)
	forceRule := strings.EqualFold(r.URL.Query().Get("force_rule"), "true")
	selected := local
	sourceLabel := "本地规则引擎"
	status := "rule_fallback"
	fallbackReason := "AI 未配置或未请求，使用数据库规则集生成建议。"
	if !forceRule && len(local) > 0 {
		modelConfig, key, keyErr := s.modelCredential(r.Context(), c.TeamID)
		if keyErr == nil && key != "" {
			if aiItems, aiErr := s.callAI(r.Context(), modelConfig, key, candidates, samples, policy); aiErr == nil {
				selected = aiItems
				sourceLabel = "AI 分析"
				status = "ai_completed"
				fallbackReason = ""
			} else {
				fallbackReason = "AI 调用失败（" + aiErr.Error() + "），已降级到本地规则引擎。"
			}
		} else if keyErr != nil {
			fallbackReason = "AI 配置不可用（" + keyErr.Error() + "），已降级到本地规则引擎。"
		} else {
			fallbackReason = "AI_API_KEY 未配置，已使用本地规则引擎。"
		}
	}
	if len(candidates) == 0 {
		status = "no_candidates"
		fallbackReason = "当前团队没有可排期的未发布内容。"
		selected = []service.Suggestion{}
	}
	selected = s.annotateSuggestionValidity(r.Context(), c.TeamID, selected, policy, window)
	referenceCount := 0
	for _, item := range selected {
		referenceCount += item.ReferenceSampleCount
	}
	payload := map[string]any{"recommendations": selected}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		writeError(w, r, 500, "recommendation_encode_failed", "编码排期建议失败")
		return
	}
	id := uuid.NewString()
	label := sourceLabel
	_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO recommendation(id,team_id,generated_at,payload_json,source_model,source_label,confidence,reference_count,analysis_status,fallback_reason,adopted) VALUES($1,$2,NOW(),$3,$4,$5,$6,$7,$8,$9,FALSE)`, id, c.TeamID, string(payloadJSON), map[bool]string{true: s.Config.AIModel, false: "rules"}[sourceLabel == "AI 分析"], label, "low", referenceCount, status, fallbackReason)
	if err != nil {
		writeError(w, r, 500, "recommendation_save_failed", "保存排期建议失败")
		return
	}
	confidenceNote := "样本量较少，建议结合实际排期。"
	if referenceCount >= 10 {
		confidenceNote = "基于同团队历史数据；结果不构成效果保证。"
	}
	writeJSON(w, 200, map[string]any{"id": id, "source": sourceLabel, "source_label": sourceLabel, "analysis_status": status, "reference_count": referenceCount, "confidence": "low", "confidence_note": confidenceNote, "fallback_reason": fallbackReason, "recommendations": selected})
}

// annotateSuggestionValidity re-checks each generated suggestion against the
// current rule set and team data. The reason comes from the same validation the
// adoption endpoint applies, so the client can disable the item and explain why
// instead of guessing. Target state and existing plans are loaded in two batched
// queries, so suggestion generation stays a single round trip per table even for
// large candidate lists.
func (s *Server) annotateSuggestionValidity(ctx context.Context, teamID int64, items []service.Suggestion, policy service.SlotPolicy, window domain.PlanningWindow) []service.Suggestion {
	rules := policy.Rules()
	targetIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.TargetContentID != "" {
			targetIDs = append(targetIDs, item.TargetContentID)
		}
	}
	targets := map[string]suggestionTarget{}
	scheduled := map[string]bool{}
	if len(targetIDs) > 0 {
		rows, err := s.Store.QueryMaps(ctx, `SELECT id,lifecycle,workflow_state,platform FROM content WHERE team_id=$1 AND id = ANY($2)`, teamID, targetIDs)
		if err != nil {
			return items
		}
		for _, row := range rows {
			targets[stringField(row, "id")] = suggestionTarget{Lifecycle: stringField(row, "lifecycle"), WorkflowState: stringField(row, "workflow_state"), Platform: stringField(row, "platform")}
		}
		rows, err = s.Store.QueryMaps(ctx, `SELECT content_id FROM schedule WHERE team_id=$1 AND status='待发布' AND content_id = ANY($2)`, teamID, targetIDs)
		if err != nil {
			return items
		}
		for _, row := range rows {
			scheduled[stringField(row, "content_id")] = true
		}
	}
	dayCounts := map[string]int{}
	for date, count := range policy.ExistingDaily {
		dayCounts[date] = count
	}
	weekCounts := map[string]int{}
	for key, count := range policy.ExistingPlatformWeekly {
		weekCounts[key] = count
	}
	annotated := make([]service.Suggestion, 0, len(items))
	for _, item := range items {
		reason, date, platform := suggestionBlockReason(item, rules, window, targets, scheduled, dayCounts, weekCounts)
		item.InvalidReason = reason
		if reason == "" {
			consumeSlot(date, platform, dayCounts, weekCounts)
		}
		annotated = append(annotated, item)
	}
	return annotated
}

// suggestionTarget is the preloaded planning state of one suggested target.
type suggestionTarget struct {
	Lifecycle     string
	WorkflowState string
	Platform      string
}

// suggestionBlockReason returns the first blocking reason for a suggestion, or
// an empty string when it can be adopted now. It is a pure function so the
// pre-check can be tested without a database.
func suggestionBlockReason(item service.Suggestion, rules domain.PlanningRules, window domain.PlanningWindow, targets map[string]suggestionTarget, scheduled map[string]bool, dayCounts, weekCounts map[string]int) (string, string, string) {
	date, err := repository.ParsePlannedDate(item.PlannedDate)
	if err != nil {
		return "计划日期格式无效", "", ""
	}
	if strings.TrimSpace(item.TargetContentID) == "" {
		return "建议缺少目标内容", date, ""
	}
	target, ok := targets[item.TargetContentID]
	if !ok {
		return "目标内容不存在或不属于当前团队", date, ""
	}
	platform := target.Platform
	if target.Lifecycle != domain.LifecycleUnpublished || target.WorkflowState == domain.WorkflowPublished {
		return "目标内容已发布，无需排期", date, platform
	}
	if !domain.CanSuggestSchedule(target.Lifecycle, target.WorkflowState) {
		return fmt.Sprintf("排期建议仅针对待排期内容（当前状态：%s）", target.WorkflowState), date, platform
	}
	if scheduled[item.TargetContentID] {
		return "该内容已有待发布排期", date, platform
	}
	planned, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "计划日期格式无效", date, platform
	}
	year, week := planned.ISOWeek()
	counts := domain.SlotCounts{SameDay: dayCounts[date] + 1, SamePlatformSameISOWeek: weekCounts[fmt.Sprintf("%s:%d:%d", platform, year, week)] + 1}
	if err = rules.ValidateSlot(window, date, item.TimeSlot, counts); err != nil {
		return err.Error(), date, platform
	}
	return "", date, platform
}

// consumeSlot reserves capacity for one accepted suggestion.
func consumeSlot(date, platform string, dayCounts, weekCounts map[string]int) {
	dayCounts[date]++
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return
	}
	year, week := parsed.ISOWeek()
	weekCounts[fmt.Sprintf("%s:%d:%d", platform, year, week)]++
}

type aiModelConfig struct {
	BaseURL string
	Model   string
}

func (s *Server) modelCredential(ctx context.Context, teamID int64) (aiModelConfig, string, error) {
	model := aiModelConfig{BaseURL: s.Config.AIBaseURL, Model: s.Config.AIModel}
	key := s.Config.AIAPIKey
	var storedURL, storedModel, storedKey string
	err := s.Store.DB.QueryRowContext(ctx, `SELECT base_url,model,api_key FROM ai_config WHERE team_id=$1 AND enabled=TRUE ORDER BY updated_at DESC LIMIT 1`, teamID).Scan(&storedURL, &storedModel, &storedKey)
	if err == nil {
		if storedURL != "" {
			model.BaseURL = storedURL
		}
		if storedModel != "" {
			model.Model = storedModel
		}
		if storedKey != "" {
			plain, openErr := service.OpenSecret(s.Config.AppSecret, storedKey)
			if openErr != nil {
				return model, "", errors.New("数据库模型凭据无法解密")
			}
			key = plain
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model, "", errors.New("读取模型配置失败")
	}
	return model, key, nil
}

func (s *Server) callAI(ctx context.Context, model aiModelConfig, key string, candidates []domain.Content, samples []service.PerformanceSample, policy service.SlotPolicy) ([]service.Suggestion, error) {
	promptData := map[string]any{"candidates": candidates, "published_history": samples, "schedule_policy": policy}
	encoded, err := json.Marshal(promptData)
	if err != nil {
		return nil, errors.New("准备 AI 分析输入失败")
	}
	body, err := json.Marshal(map[string]any{"model": model.Model, "temperature": 0.2, "messages": []map[string]string{{"role": "system", "content": "你是内容排期分析器。只根据提供的历史样本和规则集为未发布内容推荐。只返回 JSON 对象 {\"recommendations\":[{\"target_content_id\":string,\"planned_date\":\"YYYY-MM-DD\",\"time_slot\":\"HH:MM\",\"reason\":string}]}。不得生成输入外的内容 ID。"}, {"role": "user", "content": string(encoded)}}})
	if err != nil {
		return nil, errors.New("编码 AI 请求失败")
	}
	endpoint := openAIURL(model.BaseURL, "/chat/completions")
	requestCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, errors.New("构造 AI 请求失败")
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("调用超过 60 秒")
		}
		return nil, errors.New("网络请求失败")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("服务返回 HTTP %d", response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&completion); err != nil || len(completion.Choices) == 0 {
		return nil, errors.New("模型响应格式无效")
	}
	var generated struct {
		Recommendations []struct {
			TargetContentID string `json:"target_content_id"`
			PlannedDate     string `json:"planned_date"`
			TimeSlot        string `json:"time_slot"`
			Reason          string `json:"reason"`
		} `json:"recommendations"`
	}
	if err = json.Unmarshal([]byte(completion.Choices[0].Message.Content), &generated); err != nil {
		return nil, errors.New("模型没有返回预期 JSON")
	}
	byID := make(map[string]domain.Content, len(candidates))
	for _, item := range candidates {
		byID[item.ID] = item
	}
	allowedTimes := map[string]bool{}
	for _, clock := range policy.TimeWindows {
		allowedTimes[clock] = true
	}
	allowedWeekdays := map[string]bool{}
	for _, day := range policy.Weekdays {
		allowedWeekdays[day] = true
	}
	usedDaily := map[string]int{}
	for key, count := range policy.ExistingDaily {
		usedDaily[key] = count
	}
	usedWeekly := map[string]int{}
	for key, count := range policy.ExistingPlatformWeekly {
		usedWeekly[key] = count
	}
	usedContent := map[string]bool{}
	result := make([]service.Suggestion, 0, len(generated.Recommendations))
	window, windowOK := policy.Window(time.Now())
	if !windowOK {
		return nil, errors.New("排期规则集缺少有效周期")
	}
	for _, item := range generated.Recommendations {
		candidate, ok := byID[item.TargetContentID]
		if !ok || usedContent[item.TargetContentID] || !allowedTimes[item.TimeSlot] || strings.TrimSpace(item.Reason) == "" {
			return nil, errors.New("模型建议包含无效内容或时间")
		}
		planned, parseErr := time.Parse("2006-01-02", item.PlannedDate)
		if parseErr != nil || !window.Contains(item.PlannedDate) || len(allowedWeekdays) > 0 && !allowedWeekdays[weekdayName(planned.Weekday())] {
			return nil, errors.New("模型建议日期超出规则时间范围")
		}
		usedDaily[item.PlannedDate]++
		if policy.MaxPerDay > 0 && usedDaily[item.PlannedDate] > policy.MaxPerDay {
			return nil, errors.New("模型建议超过每日发布频率")
		}
		year, week := planned.ISOWeek()
		weekKey := candidate.Platform + ":" + strconv.Itoa(year) + ":" + strconv.Itoa(week)
		usedWeekly[weekKey]++
		if policy.MaxPerPlatformPerWeek > 0 && usedWeekly[weekKey] > policy.MaxPerPlatformPerWeek {
			return nil, errors.New("模型建议超过平台每周发布频率")
		}
		usedContent[item.TargetContentID] = true
		sampleCount := 0
		for _, sample := range samples {
			if sample.Category == candidate.Category && sample.Platform == candidate.Platform {
				sampleCount++
			}
		}
		confidence := "低"
		limit := "历史样本不足，建议结合团队判断。"
		if sampleCount >= 3 {
			confidence = "中"
		}
		if sampleCount >= 10 {
			confidence = "高"
			limit = "根据同团队已发布样本分析，实际表现可能不同。"
		}
		result = append(result, service.Suggestion{TargetContentID: candidate.ID, Title: candidate.Title, ContentType: candidate.Category, Platform: candidate.Platform, Owner: candidate.Owner, PlannedDate: item.PlannedDate, DayOfWeek: weekdayName(planned.Weekday()), TimeSlot: item.TimeSlot, Reason: item.Reason, SuggestionSource: "AI", ReferenceSampleCount: sampleCount, Confidence: confidence, Limitations: limit})
	}
	if len(result) == 0 && len(candidates) > 0 {
		return nil, errors.New("模型未返回有效建议")
	}
	return result, nil
}

func weekdayName(day time.Weekday) string {
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[day]
}

func (s *Server) recommendations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT id,generated_at,payload_json,source_model,source_label,confidence,reference_count,analysis_status,fallback_reason,adopted FROM recommendation WHERE team_id=$1 ORDER BY generated_at DESC LIMIT 20`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "recommendations_failed", "读取排期建议历史失败")
		return
	}
	for _, row := range rows {
		var payload map[string]any
		if raw, ok := row["payload_json"].(string); ok {
			_ = json.Unmarshal([]byte(raw), &payload)
		}
		for key, value := range payload {
			row[key] = value
		}
		delete(row, "payload_json")
		row["confidence_note"] = "建议可信度受匹配历史样本量限制。"
	}
	writeJSON(w, 200, rows)
}

func (s *Server) updateRecommendation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Recommendations []service.Suggestion `json:"recommendations"`
	}
	if !decode(w, r, &input) {
		return
	}
	payload, err := json.Marshal(map[string]any{"recommendations": input.Recommendations})
	if err != nil {
		writeError(w, r, 400, "invalid_recommendation", "建议格式无效")
		return
	}
	result, err := s.Store.DB.ExecContext(r.Context(), `UPDATE recommendation SET payload_json=$1 WHERE id=$2 AND team_id=$3`, string(payload), r.PathValue("id"), claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "recommendation_update_failed", "保存建议调整失败")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, r, 404, "not_found", "排期建议不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"recommendations": input.Recommendations})
}

func (s *Server) authError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusUnprocessableEntity
	code := "invalid_request"
	switch {
	case errors.Is(err, service.ErrInvalidCredentials), errors.Is(err, service.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, service.ErrForbidden):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, repository.ErrDuplicateEmail):
		status, code = http.StatusConflict, "duplicate_email"
	case errors.Is(err, repository.ErrOwnerExists):
		status, code = http.StatusConflict, "owner_exists"
	case errors.Is(err, service.ErrAuthBackend):
		status, code = http.StatusServiceUnavailable, "auth_backend_unavailable"
	case errors.Is(err, sql.ErrNoRows):
		status, code = http.StatusNotFound, "not_found"
	}
	writeError(w, r, status, code, err.Error())
}

func claimsFrom(r *http.Request) domain.Claims {
	claims, _ := r.Context().Value(claimsKey{}).(domain.Claims)
	return claims
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, r, 400, "invalid_json", "请求 JSON 无效")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{"detail": message, "error": domain.APIError{Code: code, Message: message, RequestID: r.Header.Get("X-Request-ID")}})
}

func notFoundOrError(w http.ResponseWriter, r *http.Request, err error, message string) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", message)
		return
	}
	writeError(w, r, http.StatusInternalServerError, "operation_failed", message)
}

func ParseIntQuery(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}
