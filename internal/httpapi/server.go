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
	"strconv"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/config"
	"github.com/arts-smart/strategy/internal/domain"
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
	mux.Handle("POST /api/contents/bulk", server.authenticated(server.editors(http.HandlerFunc(server.bulkContents))))
	mux.Handle("POST /api/contents/import-csv", server.authenticated(server.editors(http.HandlerFunc(server.importContentsCSV))))
	mux.Handle("GET /api/teams", server.authenticated(http.HandlerFunc(server.teams)))
	mux.Handle("GET /api/users", server.authenticated(http.HandlerFunc(server.users)))
	mux.Handle("POST /api/users", server.authenticated(server.owners(http.HandlerFunc(server.addUser))))
	mux.Handle("GET /api/performances", server.authenticated(http.HandlerFunc(server.performances)))
	mux.Handle("POST /api/performances", server.authenticated(server.editors(http.HandlerFunc(server.createPerformance))))
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
	mux.Handle("POST /api/rules", server.authenticated(server.editors(http.HandlerFunc(server.createRule))))
	mux.Handle("POST /api/rules/preview", server.authenticated(server.editors(http.HandlerFunc(server.previewRule))))
	mux.Handle("POST /api/rules/evaluate", server.authenticated(server.editors(http.HandlerFunc(server.evaluateRules))))
	mux.Handle("GET /api/rules/logs", server.authenticated(http.HandlerFunc(server.ruleLogs)))
	mux.Handle("PATCH /api/rules/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateRule))))
	mux.Handle("DELETE /api/rules/{id}", server.authenticated(server.editors(http.HandlerFunc(server.deleteRule))))
	mux.Handle("PATCH /api/teams/{id}", server.authenticated(server.owners(http.HandlerFunc(server.renameTeam))))
	mux.Handle("GET /api/stats/overview", server.authenticated(http.HandlerFunc(server.overview)))
	mux.Handle("GET /api/stats/platform", server.authenticated(http.HandlerFunc(server.platformStats)))
	mux.Handle("GET /api/stats/trend", server.authenticated(http.HandlerFunc(server.trendStats)))
	mux.Handle("GET /api/stats/category", server.authenticated(http.HandlerFunc(server.categoryStats)))
	mux.Handle("GET /api/stats/heatmap", server.authenticated(http.HandlerFunc(server.heatmap)))
	mux.Handle("GET /api/stats/adoption", server.authenticated(http.HandlerFunc(server.adoptionStats)))
	mux.Handle("GET /api/stats/funnel", server.authenticated(http.HandlerFunc(server.funnelStats)))
	mux.Handle("GET /api/stats/diagnostics", server.authenticated(http.HandlerFunc(server.diagnostics)))
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
	mux.Handle("GET /api/schedule/policy", server.authenticated(http.HandlerFunc(server.schedulePolicy)))
	mux.Handle("POST /api/schedule/adopt", server.authenticated(server.editors(http.HandlerFunc(server.adoptSchedules))))
	mux.Handle("PATCH /api/schedule/{id}", server.authenticated(server.editors(http.HandlerFunc(server.updateSchedule))))
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
	writeJSON(w, http.StatusOK, map[string]any{"mode": "database", "persistent": true, "message": "云端 PostgreSQL 数据库模式：修改将持久化保存。"})
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
	items, err := s.Store.ListContents(r.Context(), claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "contents_failed", "读取内容记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
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
	var input domain.Content
	if !decode(w, r, &input) {
		return
	}
	c := claimsFrom(r)
	if message := s.contentRuleFailure(r, c.TeamID, input.Title+"\n"+input.Notes, input.Tags, input.Category, input.WorkflowState); message != "" {
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
		if message := s.contentRuleFailure(r, c.TeamID, title+"\n"+notes, tags, category, state); message != "" {
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
	c := claimsFrom(r)
	item, err := s.Store.AdvanceWorkflow(r.Context(), c.TeamID, c.UserID, c.Name, r.PathValue("id"))
	if err != nil {
		writeError(w, r, http.StatusConflict, "workflow_conflict", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
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
	switch input.Action {
	case "delete":
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
	case "set_status":
		state := input.Status
		lifecycle := domain.LifecycleUnpublished
		if state == domain.WorkflowPublished || state == domain.LifecyclePublished {
			state = domain.WorkflowPublished
			lifecycle = domain.LifecyclePublished
		} else if state == domain.LifecycleUnpublished {
			state = domain.WorkflowUnpublished
		}
		if state != domain.WorkflowPublished && state != domain.WorkflowUnpublished {
			writeError(w, r, 422, "status_invalid", "批量状态只能设置为已发布或未发布")
			return
		}
		for _, id := range input.IDs {
			var before string
			if err = tx.QueryRowContext(r.Context(), `SELECT workflow_state FROM content WHERE id=$1 AND team_id=$2 FOR UPDATE`, id, c.TeamID).Scan(&before); err != nil {
				continue
			}
			if _, err = tx.ExecContext(r.Context(), `UPDATE content SET lifecycle=$1,workflow_state=$2,updated_at=NOW() WHERE id=$3 AND team_id=$4`, lifecycle, state, id, c.TeamID); err != nil {
				writeError(w, r, 500, "bulk_update_failed", "批量更新状态失败")
				return
			}
			if before != state {
				if _, err = tx.ExecContext(r.Context(), `INSERT INTO content_workflow_event(team_id,content_id,from_state,to_state,source,actor_user_id,created_at) VALUES($1,$2,$3,$4,'manual',$5,NOW())`, c.TeamID, id, before, state, c.UserID); err != nil {
					writeError(w, r, 500, "bulk_event_failed", "记录工作流变更失败")
					return
				}
			}
		}
	default:
		writeError(w, r, 422, "bulk_action_invalid", "不支持的批量操作")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, r, 500, "bulk_commit_failed", "提交批量操作失败")
		return
	}
	writeJSON(w, 200, map[string]any{"updated": len(input.IDs)})
}

func (s *Server) importContentsCSV(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, 400, "csv_missing", "请选择 CSV 文件")
		return
	}
	defer file.Close()
	reader := csv.NewReader(io.LimitReader(file, 10<<20))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		writeError(w, r, 422, "csv_invalid", "CSV 文件为空或格式无效")
		return
	}
	columns := map[string]int{}
	for i, name := range header {
		columns[strings.TrimPrefix(strings.TrimSpace(name), "\ufeff")] = i
	}
	warnings := []map[string]any{}
	imported := 0
	c := claimsFrom(r)
	line := 1
	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		line++
		if readErr != nil {
			warnings = append(warnings, map[string]any{"row": line, "message": "CSV 行无法解析"})
			continue
		}
		cell := func(name string) string {
			if index, ok := columns[name]; ok && index < len(row) {
				return strings.TrimSpace(row[index])
			}
			return ""
		}
		status := cell("状态标签")
		if status == "" {
			status = domain.LifecyclePublished
		}
		lifecycle := domain.LifecycleUnpublished
		if status == domain.LifecyclePublished {
			lifecycle = domain.LifecyclePublished
		}
		workflow := cell("工作流状态")
		if workflow == "" {
			if lifecycle == domain.LifecyclePublished {
				workflow = domain.WorkflowPublished
			} else {
				workflow = domain.WorkflowUnpublished
			}
		}
		publishedAt := time.Now()
		for _, layout := range []string{"2006-01-02 15:04", "2006-01-02 15:04:05", time.RFC3339Nano} {
			if parsed, parseErr := time.Parse(layout, cell("发布时间")); parseErr == nil {
				publishedAt = parsed
				break
			}
		}
		content := domain.Content{Title: cell("标题"), Category: cell("内容类型"), Platform: cell("平台"), PublishedAt: publishedAt, Duration: parseInt(cell("时长")), Views: int64(parseInt(cell("播放量"))), Likes: int64(parseInt(cell("点赞量"))), Comments: int64(parseInt(cell("评论量"))), Saves: int64(parseInt(cell("收藏量"))), Shares: int64(parseInt(cell("转发量"))), Completion: parseFloat(cell("看完比例")), Lifecycle: lifecycle, WorkflowState: workflow, Tags: splitTags(cell("标签")), Materials: cell("素材说明"), Owner: cell("责任人"), Notes: cell("备注"), ProfileVisits: int64(parseInt(cell("主页访问"))), Follows: int64(parseInt(cell("新增关注"))), Attendance: int64(parseInt(cell("到场人数"))), Tickets: int64(parseInt(cell("售票数"))), CommentText: cell("评论摘录"), Source: "csv"}
		if message := s.contentRuleFailure(r, c.TeamID, content.Title+"\n"+content.Notes, content.Tags, content.Category, content.WorkflowState); message != "" {
			warnings = append(warnings, map[string]any{"row": line, "message": message})
			continue
		}
		if _, saveErr := s.Store.CreateContent(r.Context(), c.TeamID, c.UserID, c.Name, content); saveErr != nil {
			warnings = append(warnings, map[string]any{"row": line, "message": saveErr.Error()})
			continue
		}
		imported++
	}
	writeJSON(w, 200, map[string]any{"imported": imported, "warnings": warnings})
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
func parseInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}
func parseFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}
func splitTags(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '|' || r == '，' || r == ',' })
	result := []string{}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, strings.TrimSpace(part))
		}
	}
	return result
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
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT cr.*,u.name AS updated_by FROM content_rule cr LEFT JOIN user_account u ON u.id=cr.updated_by_user_id WHERE cr.team_id=$1 ORDER BY cr.priority,cr.id`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "rules_failed", "读取内容规则失败")
		return
	}
	writeJSON(w, 200, rows)
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

func (s *Server) contentRuleFailure(r *http.Request, teamID int64, text string, tags []string, category, state string) string {
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT name,rule_type,rule_content,content_type,workflow_state FROM content_rule WHERE team_id=$1 AND enabled=TRUE AND trigger IN ('on_save','on_create') ORDER BY priority,id`, teamID)
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
	rows, err := s.Store.ListPerformances(r.Context(), claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "performances_failed", "读取演出预订失败")
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) createPerformance(w http.ResponseWriter, r *http.Request) {
	var input domain.Performance
	if !decode(w, r, &input) {
		return
	}
	c := claimsFrom(r)
	result, err := s.Store.CreatePerformanceBooking(r.Context(), c.TeamID, c.UserID, c.Name, input)
	if err != nil {
		writeError(w, r, 422, "performance_invalid", err.Error())
		return
	}
	writeJSON(w, 201, result)
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

type modelSettings struct {
	Name              string  `json:"name"`
	BaseURL           string  `json:"base_url"`
	Model             string  `json:"model"`
	APIKey            string  `json:"api_key"`
	MinSample         int     `json:"min_sample"`
	CompletionWeight  float64 `json:"score_completion_weight"`
	InteractionWeight float64 `json:"score_interaction_weight"`
	Temperature       float64 `json:"temperature"`
	RequestMode       string  `json:"request_mode"`
	AuthType          string  `json:"auth_type"`
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	var value modelSettings
	var storedKey string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT name,base_url,model,api_key,min_sample,score_completion_weight,score_interaction_weight,temperature,request_mode,auth_type FROM ai_config WHERE team_id=$1 AND enabled=TRUE ORDER BY updated_at DESC LIMIT 1`, c.TeamID).Scan(&value.Name, &value.BaseURL, &value.Model, &storedKey, &value.MinSample, &value.CompletionWeight, &value.InteractionWeight, &value.Temperature, &value.RequestMode, &value.AuthType)
	if errors.Is(err, sql.ErrNoRows) {
		keyConfigured := s.Config.AIAPIKey != ""
		writeJSON(w, 200, map[string]any{"configured": keyConfigured, "has_key": keyConfigured, "name": "默认模型", "base_url": s.Config.AIBaseURL, "model": s.Config.AIModel, "min_sample": 3, "score_completion_weight": 0.6, "score_interaction_weight": 0.4, "temperature": 0.3, "request_mode": "base", "auth_type": "bearer"})
		return
	}
	if err != nil {
		writeError(w, r, 500, "settings_failed", "读取模型配置失败")
		return
	}
	keyConfigured := storedKey != "" || s.Config.AIAPIKey != ""
	writeJSON(w, 200, map[string]any{"configured": keyConfigured, "has_key": keyConfigured, "name": value.Name, "base_url": value.BaseURL, "model": value.Model, "min_sample": value.MinSample, "score_completion_weight": value.CompletionWeight, "score_interaction_weight": value.InteractionWeight, "temperature": value.Temperature, "request_mode": value.RequestMode, "auth_type": value.AuthType})
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
	var input modelSettings
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
	var value modelSettings
	if !decode(w, r, &value) {
		return
	}
	if value.Name == "" {
		value.Name = "默认模型"
	}
	if value.BaseURL == "" {
		value.BaseURL = s.Config.AIBaseURL
	}
	if value.Model == "" {
		value.Model = s.Config.AIModel
	}
	if value.MinSample < 1 {
		value.MinSample = 3
	}
	if value.CompletionWeight == 0 {
		value.CompletionWeight = .6
	}
	if value.InteractionWeight == 0 {
		value.InteractionWeight = .4
	}
	if value.RequestMode == "" {
		value.RequestMode = "base"
	}
	if value.AuthType == "" {
		value.AuthType = "bearer"
	}
	apiKey := ""
	if value.APIKey != "" {
		encrypted, err := service.SealSecret(s.Config.AppSecret, value.APIKey)
		if err != nil {
			writeError(w, r, 500, "credential_encrypt_failed", "安全保存模型密钥失败")
			return
		}
		apiKey = encrypted
	}
	c := claimsFrom(r)
	var id int64
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id FROM ai_config WHERE team_id=$1 ORDER BY updated_at DESC LIMIT 1`, c.TeamID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if apiKey == "" && s.Config.AIAPIKey != "" {
			apiKey = ""
		}
		_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO ai_config(team_id,name,base_url,api_key,model,temperature,request_mode,auth_type,enabled,fallback_enabled,score_completion_weight,score_interaction_weight,min_sample,platform_weights_json,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,TRUE,TRUE,$9,$10,$11,'{}',NOW())`, c.TeamID, value.Name, value.BaseURL, apiKey, value.Model, value.Temperature, value.RequestMode, value.AuthType, value.CompletionWeight, value.InteractionWeight, value.MinSample)
	} else if err == nil {
		if apiKey == "" {
			_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE ai_config SET name=$1,base_url=$2,model=$3,temperature=$4,request_mode=$5,auth_type=$6,score_completion_weight=$7,score_interaction_weight=$8,min_sample=$9,updated_at=NOW() WHERE id=$10 AND team_id=$11`, value.Name, value.BaseURL, value.Model, value.Temperature, value.RequestMode, value.AuthType, value.CompletionWeight, value.InteractionWeight, value.MinSample, id, c.TeamID)
		} else {
			_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE ai_config SET name=$1,base_url=$2,api_key=$3,model=$4,temperature=$5,request_mode=$6,auth_type=$7,score_completion_weight=$8,score_interaction_weight=$9,min_sample=$10,updated_at=NOW() WHERE id=$11 AND team_id=$12`, value.Name, value.BaseURL, apiKey, value.Model, value.Temperature, value.RequestMode, value.AuthType, value.CompletionWeight, value.InteractionWeight, value.MinSample, id, c.TeamID)
		}
	}
	if err != nil {
		writeError(w, r, 500, "settings_save_failed", "保存模型配置失败")
		return
	}
	s.settings(w, r)
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r)
	var result map[string]any
	rows, err := s.Store.QueryMaps(r.Context(), `SELECT COUNT(*) FILTER (WHERE lifecycle='已发布') AS published_count, COUNT(*) FILTER (WHERE lifecycle='未发布') AS unpublished_count, COALESCE(SUM(views) FILTER (WHERE lifecycle='已发布'),0) AS total_views, COALESCE(SUM(likes) FILTER (WHERE lifecycle='已发布'),0) AS total_likes, COALESCE(AVG(completion) FILTER (WHERE lifecycle='已发布'),0) AS avg_completion, COUNT(*) FILTER (WHERE lifecycle='已发布') AS sample_count FROM content WHERE team_id=$1`, c.TeamID)
	if err != nil {
		writeError(w, r, 500, "stats_failed", "统计内容表现失败")
		return
	}
	if len(rows) > 0 {
		result = rows[0]
	} else {
		result = map[string]any{}
	}
	writeJSON(w, http.StatusOK, result)
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

func (s *Server) schedulePolicy(w http.ResponseWriter, r *http.Request) {
	var raw []byte
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT payload_json->'schedule_policy' FROM system_rule_set WHERE enabled=TRUE ORDER BY updated_at DESC LIMIT 1`).Scan(&raw)
	if err != nil {
		writeError(w, r, 503, "schedule_rules_unavailable", "数据库中没有可用的只读排期规则集")
		return
	}
	var policy map[string]any
	if err = json.Unmarshal(raw, &policy); err != nil {
		writeError(w, r, 503, "schedule_rules_invalid", "排期规则集格式无效")
		return
	}
	writeJSON(w, 200, policy)
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
	items, err := s.Store.ListTeamRows(r.Context(), `SELECT lifecycle,COUNT(*) AS count FROM content WHERE team_id=$1 GROUP BY lifecycle ORDER BY lifecycle`, claimsFrom(r).TeamID)
	if err != nil {
		writeError(w, r, 500, "diagnostics_failed", "读取运营提醒失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
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

func (s *Server) exportReport(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	team, _ := s.Store.TeamName(r.Context(), teamID)
	rows, err := s.Store.ListTeamRows(r.Context(), `SELECT category,platform,COUNT(*) AS sample_count,AVG(views) AS avg_views,AVG(completion) AS avg_completion FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' GROUP BY category,platform ORDER BY category,platform`, teamID)
	if err != nil {
		writeError(w, r, 500, "report_failed", "生成运营报告失败")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=export.json")
	writeJSON(w, 200, map[string]any{"team_name": team, "generated_at": time.Now().UTC(), "published_performance": rows})
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
	items, err := s.Store.ListSchedules(r.Context(), claimsFrom(r).TeamID, start, end)
	if err != nil {
		writeError(w, r, 500, "schedule_failed", "读取排期日历失败")
		return
	}
	writeJSON(w, 200, items)
}

func (s *Server) manualSchedule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ContentID   string `json:"content_id"`
		PlannedDate string `json:"planned_date"`
		PlannedTime string `json:"planned_time"`
	}
	if !decode(w, r, &input) {
		return
	}
	date := input.PlannedDate
	if len(date) >= 10 {
		date = date[:10]
	}
	date, err := repository.ParseScheduleDate(date)
	if err != nil {
		writeError(w, r, 422, "invalid_date", err.Error())
		return
	}
	if _, err = time.Parse("15:04", input.PlannedTime); err != nil {
		writeError(w, r, 422, "invalid_time", "发布时间格式必须为 HH:MM")
		return
	}
	c := claimsFrom(r)
	var platform string
	if err = s.Store.DB.QueryRowContext(r.Context(), `SELECT platform FROM content WHERE id=$1 AND team_id=$2`, input.ContentID, c.TeamID).Scan(&platform); err != nil {
		writeError(w, r, 404, "content_not_found", "排期目标内容不存在")
		return
	}
	if err = s.validateSlot(r.Context(), c.TeamID, date, input.PlannedTime, platform); err != nil {
		writeError(w, r, 422, "schedule_policy_rejected", err.Error())
		return
	}
	item, err := s.Store.CreateManualSchedule(r.Context(), c.TeamID, c.UserID, c.Name, input.ContentID, date, input.PlannedTime)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, r, 404, "content_not_found", "内容记录不存在")
		} else {
			writeError(w, r, 409, "schedule_conflict", err.Error())
		}
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) validateSlot(ctx context.Context, teamID int64, date, clock, platform string) error {
	var raw []byte
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT payload_json->'schedule_policy' FROM system_rule_set WHERE enabled=TRUE ORDER BY updated_at DESC LIMIT 1`).Scan(&raw); err != nil {
		return errors.New("数据库中没有可用的只读排期规则集")
	}
	var policy map[string]any
	if err := json.Unmarshal(raw, &policy); err != nil {
		return errors.New("排期规则集格式无效")
	}
	planned, err := time.Parse("2006-01-02", date)
	if err != nil {
		return errors.New("排期日期无效")
	}
	today := time.Now()
	horizon := intSetting(policy["planning_horizon_days"], 14)
	if date < today.Format("2006-01-02") || date > today.AddDate(0, 0, horizon-1).Format("2006-01-02") {
		return errors.New("计划日期超出规则集排期周期")
	}
	if !hasString(stringValues(policy["weekdays"]), weekdayName(planned.Weekday())) {
		return errors.New("计划日期不符合规则集允许星期")
	}
	if !hasString(stringValues(policy["time_windows"]), clock) {
		return errors.New("计划时间不符合规则集允许时间窗")
	}
	maxDaily := intSetting(policy["max_per_day"], 2)
	var daily int
	if err = s.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule WHERE team_id=$1 AND status='待发布' AND planned_date=$2::date`, teamID, date).Scan(&daily); err != nil {
		return err
	}
	if maxDaily > 0 && daily >= maxDaily {
		return errors.New("计划日期超过规则集每日发布频率")
	}
	maxWeekly := intSetting(policy["max_per_platform_per_week"], 2)
	year, week := planned.ISOWeek()
	var weekly int
	if err = s.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule WHERE team_id=$1 AND status='待发布' AND platform=$2 AND EXTRACT(ISOYEAR FROM planned_date)=$3 AND EXTRACT(WEEK FROM planned_date)=$4`, teamID, platform, year, week).Scan(&weekly); err != nil {
		return err
	}
	if maxWeekly > 0 && weekly >= maxWeekly {
		return errors.New("计划日期超过该平台每周发布频率")
	}
	return nil
}

func stringValues(raw any) []string {
	values, _ := raw.([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func intSetting(raw any, fallback int) int {
	if value, ok := raw.(float64); ok {
		return int(value)
	}
	return fallback
}
func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Server) adoptSchedules(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RecommendationID string           `json:"recommendation_id"`
		Items            []map[string]any `json:"items"`
	}
	if !decode(w, r, &input) {
		return
	}
	items := make([]repository.AdoptedSchedule, 0, len(input.Items))
	for _, row := range input.Items {
		item := repository.AdoptedSchedule{}
		item.TargetContentID, _ = row["target_content_id"].(string)
		item.PlannedDate, _ = row["planned_date"].(string)
		item.TimeSlot, _ = row["time_slot"].(string)
		item.Title, _ = row["title"].(string)
		items = append(items, item)
	}
	var raw []byte
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT payload_json->'schedule_policy' FROM system_rule_set WHERE enabled=TRUE ORDER BY updated_at DESC LIMIT 1`).Scan(&raw)
	if err != nil {
		writeError(w, r, 503, "schedule_rules_unavailable", "数据库中没有可用的只读排期规则集")
		return
	}
	var policy map[string]any
	if err = json.Unmarshal(raw, &policy); err != nil {
		writeError(w, r, 503, "schedule_rules_invalid", "排期规则集格式无效")
		return
	}
	c := claimsFrom(r)
	created, err := s.Store.AdoptRecommendations(r.Context(), c.TeamID, c.UserID, c.Name, input.RecommendationID, items, policy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, r, 404, "recommendation_not_found", "排期建议不存在")
		} else {
			writeError(w, r, 422, "schedule_adoption_failed", err.Error())
		}
		return
	}
	writeJSON(w, 200, map[string]int{"created": created})
}

func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	var fields map[string]any
	if !decode(w, r, &fields) {
		return
	}
	if date, ok := fields["planned_date"].(string); ok {
		parsed, err := repository.ParseScheduleDate(date)
		if err != nil {
			writeError(w, r, 422, "invalid_date", err.Error())
			return
		}
		fields["planned_date"] = parsed
	}
	item, err := s.Store.UpdateSchedule(r.Context(), claimsFrom(r).TeamID, r.PathValue("id"), fields)
	if err != nil {
		notFoundOrError(w, r, err, "排期不存在或更新失败")
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
	rows, err := s.Store.QueryMaps(r.Context(), `SELECT id,title,category,platform,owner,lifecycle,workflow_state FROM content WHERE team_id=$1 AND lifecycle='未发布' AND workflow_state<>'已发布' AND NOT EXISTS (SELECT 1 FROM schedule WHERE schedule.team_id=content.team_id AND schedule.content_id=content.id AND schedule.status='待发布') ORDER BY created_at`, c.TeamID)
	if err != nil {
		writeError(w, r, 500, "candidate_query_failed", "读取待排期内容失败")
		return
	}
	candidates := make([]domain.Content, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, domain.Content{ID: fmt.Sprint(row["id"]), Title: fmt.Sprint(row["title"]), Category: fmt.Sprint(row["category"]), Platform: fmt.Sprint(row["platform"]), Owner: fmt.Sprint(row["owner"]), Lifecycle: fmt.Sprint(row["lifecycle"]), WorkflowState: fmt.Sprint(row["workflow_state"])})
	}
	var policy service.SlotPolicy
	var rawPolicy []byte
	err = s.Store.DB.QueryRowContext(r.Context(), `SELECT payload_json->'schedule_policy' FROM system_rule_set WHERE enabled=TRUE ORDER BY updated_at DESC LIMIT 1`).Scan(&rawPolicy)
	if err != nil {
		writeError(w, r, 503, "schedule_rules_unavailable", "数据库中没有可用的只读排期规则集")
		return
	}
	if err = json.Unmarshal(rawPolicy, &policy); err != nil || policy.HorizonDays < 1 || len(policy.TimeWindows) == 0 {
		writeError(w, r, 503, "schedule_rules_invalid", "排期规则集字段无效")
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
	for _, item := range generated.Recommendations {
		candidate, ok := byID[item.TargetContentID]
		if !ok || usedContent[item.TargetContentID] || !allowedTimes[item.TimeSlot] || strings.TrimSpace(item.Reason) == "" {
			return nil, errors.New("模型建议包含无效内容或时间")
		}
		planned, parseErr := time.Parse("2006-01-02", item.PlannedDate)
		if parseErr != nil || planned.Before(time.Now().AddDate(0, 0, 1).Truncate(24*time.Hour)) || planned.After(time.Now().AddDate(0, 0, policy.HorizonDays)) || len(allowedWeekdays) > 0 && !allowedWeekdays[weekdayName(planned.Weekday())] {
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
