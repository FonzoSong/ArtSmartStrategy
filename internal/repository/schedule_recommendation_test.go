package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/arts-smart/strategy/internal/domain"
)

func TestAdoptRecommendationCreatesScheduleAndStoresPriority(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := New(db)
	teamID, actorID := int64(7), int64(11)
	recommendationID, contentID, scheduleID := "recommendation-1", "content-1", "schedule-1"
	planned := time.Now().AddDate(0, 0, 2)
	date := planned.Format("2006-01-02")
	payload, err := json.Marshal(map[string]any{"recommendations": []map[string]any{{"target_content_id": contentID, "planned_date": date, "time_slot": "19:30", "title": "原建议", "owner": "原负责人", "priority": "medium", "priority_order": 1, "reason": "原理由", "risk_notes": "原风险"}}})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT payload_json FROM recommendation").WithArgs(recommendationID, teamID).WillReturnRows(sqlmock.NewRows([]string{"payload_json"}).AddRow(string(payload)))
	mock.ExpectQuery("SELECT planned_date,platform FROM schedule").WithArgs(teamID).WillReturnRows(sqlmock.NewRows([]string{"planned_date", "platform"}))
	mock.ExpectQuery("SELECT title,category,platform,owner,workflow_state,lifecycle FROM content").WithArgs(contentID, teamID).WillReturnRows(sqlmock.NewRows([]string{"title", "category", "platform", "owner", "workflow_state", "lifecycle"}).AddRow("原建议", "演出宣传", "视频号", "原负责人", domain.WorkflowSchedulePending, domain.LifecycleUnpublished))
	mock.ExpectQuery("SELECT id FROM schedule").WithArgs(teamID, contentID).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO schedule").WithArgs(sqlmock.AnyArg(), teamID, recommendationID, contentID, date, "19:30", "新标题", "演出宣传", "视频号", "编辑负责人", "根据共享 Tag 表现", "high", 2, "冲突风险", actorID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(scheduleID))
	mock.ExpectExec("UPDATE content SET workflow_state").WithArgs(domain.WorkflowScheduled, contentID, teamID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO content_workflow_event").WithArgs(teamID, contentID, domain.WorkflowSchedulePending, domain.WorkflowScheduled, actorID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "adopt_schedule", "schedule", scheduleID, "采纳排期建议").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "apply_schedule_recommendation", "recommendation", recommendationID, "采纳 1 条排期建议").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE recommendation SET payload_json").WithArgs(sqlmock.AnyArg(), recommendationID, teamID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	outcome, err := repository.AdoptRecommendations(context.Background(), teamID, actorID, "负责人", recommendationID, []AdoptedSchedule{{TargetContentID: contentID, PlannedDate: date, TimeSlot: "19:30", Title: "新标题", Owner: "编辑负责人", Priority: "high", PriorityOrder: 2, Reason: "根据共享 Tag 表现", RiskNotes: "冲突风险"}}, domain.PlanningRules{HorizonDays: 14, Weekdays: []string{domain.WeekdayLabel(planned.Weekday())}, TimeWindows: []string{"19:30"}, MaxPerDay: 2, MaxPerPlatformPerWeek: 3})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Created != 1 || outcome.Adjusted != 0 || len(outcome.Recommendations) != 1 {
		t.Fatalf("unexpected adoption outcome: %#v", outcome)
	}
	item := outcome.Recommendations[0]
	if item["adopted"] != true || item["schedule_id"] != scheduleID || item["priority"] != "high" || item["priority_order"] != 2 || item["risk_notes"] != "冲突风险" {
		t.Fatalf("adopted recommendation was not normalized and stored: %#v", item)
	}
	if item["day_of_week"] != domain.WeekdayLabel(planned.Weekday()) {
		t.Fatalf("edited date retained stale weekday: got %v", item["day_of_week"])
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptRecommendationAdjustsExistingScheduleInOneTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := New(db)
	teamID, actorID := int64(7), int64(11)
	recommendationID, contentID, scheduleID := "recommendation-2", "content-2", "schedule-2"
	previousDate := time.Now().AddDate(0, 0, 2)
	planned := time.Now().AddDate(0, 0, 3)
	oldDate, date := previousDate.Format("2006-01-02"), planned.Format("2006-01-02")
	previous := map[string]any{"id": scheduleID, "recommendation_id": "older-recommendation", "planned_date": oldDate, "planned_time": "18:30", "title": "旧标题", "category": "幕后记录", "platform": "视频号", "owner": "旧负责人", "reason": "人工计划", "source": "manual", "priority": "low", "priority_order": 3, "risk_notes": "原冲突说明"}
	payload, err := json.Marshal(map[string]any{"recommendations": []map[string]any{{"target_content_id": contentID, "planned_date": date, "time_slot": "19:30", "title": "新标题", "owner": "新负责人", "priority": "high", "priority_order": 1, "reason": "Tag 样本建议", "risk_notes": "需核对制作进度"}}})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT payload_json FROM recommendation").WithArgs(recommendationID, teamID).WillReturnRows(sqlmock.NewRows([]string{"payload_json"}).AddRow(string(payload)))
	mock.ExpectQuery("SELECT planned_date,platform FROM schedule").WithArgs(teamID).WillReturnRows(sqlmock.NewRows([]string{"planned_date", "platform"}).AddRow(previousDate, "视频号"))
	mock.ExpectQuery("SELECT title,category,platform,owner,workflow_state,lifecycle FROM content").WithArgs(contentID, teamID).WillReturnRows(sqlmock.NewRows([]string{"title", "category", "platform", "owner", "workflow_state", "lifecycle"}).AddRow("新标题", "幕后记录", "视频号", "内容负责人", domain.WorkflowScheduled, domain.LifecycleUnpublished))
	mock.ExpectQuery("SELECT id FROM schedule").WithArgs(teamID, contentID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(scheduleID))
	mock.ExpectQuery("SELECT to_char").WithArgs(scheduleID, teamID).WillReturnRows(sqlmock.NewRows([]string{"planned_date", "planned_time", "title", "category", "platform", "owner", "reason", "source", "priority", "priority_order", "risk_notes", "recommendation_id"}).AddRow(oldDate, "18:30", "旧标题", "幕后记录", "视频号", "旧负责人", "人工计划", "manual", "low", 3, "原冲突说明", "older-recommendation"))
	mock.ExpectExec("UPDATE schedule SET recommendation_id").WithArgs(recommendationID, date, "19:30", "新标题", "新负责人", "Tag 样本建议", "high", 1, "需核对制作进度", scheduleID, teamID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "adjust_schedule", "schedule", scheduleID, "AI 排期建议调整现有排期").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "adopt_schedule", "schedule", scheduleID, "采纳排期建议").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "apply_schedule_recommendation", "recommendation", recommendationID, "采纳 1 条排期建议").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE recommendation SET payload_json").WithArgs(sqlmock.AnyArg(), recommendationID, teamID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	outcome, err := repository.AdoptRecommendations(context.Background(), teamID, actorID, "负责人", recommendationID, []AdoptedSchedule{{TargetContentID: contentID, PlannedDate: date, TimeSlot: "19:30", Title: "新标题", Owner: "新负责人", Priority: "high", PriorityOrder: 1, Reason: "Tag 样本建议", RiskNotes: "需核对制作进度", PreviousSchedule: previous}}, domain.PlanningRules{HorizonDays: 14, Weekdays: []string{domain.WeekdayLabel(planned.Weekday())}, TimeWindows: []string{"19:30"}, MaxPerDay: 2, MaxPerPlatformPerWeek: 2})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Created != 0 || outcome.Adjusted != 1 || outcome.Recommendations[0]["schedule_id"] != scheduleID {
		t.Fatalf("unexpected adjustment outcome: %#v", outcome)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRevertRecommendationDeletesNewScheduleAndRestoresWorkflow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := New(db)
	teamID, actorID := int64(7), int64(11)
	recommendationID, contentID, scheduleID := "recommendation-1", "content-1", "schedule-1"
	date := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	payload, err := json.Marshal(map[string]any{"recommendations": []map[string]any{{"target_content_id": contentID, "planned_date": date, "time_slot": "19:30", "title": "新标题", "owner": "负责人", "priority": "high", "priority_order": 2, "risk_notes": "冲突风险", "reason": "根据共享 Tag 表现", "adopted": true, "schedule_id": scheduleID}}})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT payload_json FROM recommendation").WithArgs(recommendationID, teamID).WillReturnRows(sqlmock.NewRows([]string{"payload_json"}).AddRow(string(payload)))
	mock.ExpectQuery("SELECT content_id,status,source,COALESCE").WithArgs(scheduleID, teamID).WillReturnRows(sqlmock.NewRows([]string{"content_id", "status", "source", "recommendation_id", "planned_date", "planned_time", "title", "owner", "priority", "priority_order", "risk_notes", "reason"}).AddRow(contentID, "待发布", "auto", recommendationID, date, "19:30", "新标题", "负责人", "high", 2, "冲突风险", "根据共享 Tag 表现"))
	mock.ExpectQuery("SELECT workflow_state FROM content").WithArgs(contentID, teamID).WillReturnRows(sqlmock.NewRows([]string{"workflow_state"}).AddRow(domain.WorkflowScheduled))
	mock.ExpectExec("DELETE FROM schedule").WithArgs(scheduleID, teamID, recommendationID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE content SET workflow_state").WithArgs(domain.WorkflowSchedulePending, contentID, teamID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO content_workflow_event").WithArgs(teamID, contentID, domain.WorkflowScheduled, domain.WorkflowSchedulePending, actorID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "revert_schedule", "schedule", scheduleID, "撤回 AI 排期建议的应用").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE recommendation SET payload_json").WithArgs(sqlmock.AnyArg(), recommendationID, teamID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "revert_schedule_recommendation", "recommendation", recommendationID, fmt.Sprintf("撤回 %d 条已应用排期", 1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	reverted, err := repository.RevertRecommendation(context.Background(), teamID, actorID, "负责人", recommendationID)
	if err != nil {
		t.Fatal(err)
	}
	if reverted != 1 {
		t.Fatalf("reverted=%d, want 1", reverted)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRevertRecommendationRestoresPreviousScheduleSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := New(db)
	teamID, actorID := int64(7), int64(11)
	recommendationID, contentID, scheduleID := "recommendation-3", "content-3", "schedule-3"
	oldDate := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	newDate := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	previous := map[string]any{"id": scheduleID, "recommendation_id": "older-recommendation", "planned_date": oldDate, "planned_time": "18:30", "title": "旧标题", "category": "幕后记录", "platform": "视频号", "owner": "旧负责人", "reason": "人工计划", "source": "manual", "priority": "low", "priority_order": 3, "risk_notes": "原冲突说明"}
	payload, err := json.Marshal(map[string]any{"recommendations": []map[string]any{{"target_content_id": contentID, "planned_date": newDate, "time_slot": "19:30", "title": "新标题", "owner": "新负责人", "priority": "high", "priority_order": 1, "risk_notes": "建议冲突说明", "reason": "Tag 样本建议", "adopted": true, "schedule_id": scheduleID, "previous_schedule": previous}}})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT payload_json FROM recommendation").WithArgs(recommendationID, teamID).WillReturnRows(sqlmock.NewRows([]string{"payload_json"}).AddRow(string(payload)))
	mock.ExpectQuery("SELECT content_id,status,source,COALESCE").WithArgs(scheduleID, teamID).WillReturnRows(sqlmock.NewRows([]string{"content_id", "status", "source", "recommendation_id", "planned_date", "planned_time", "title", "owner", "priority", "priority_order", "risk_notes", "reason"}).AddRow(contentID, "待发布", "auto", recommendationID, newDate, "19:30", "新标题", "新负责人", "high", 1, "建议冲突说明", "Tag 样本建议"))
	mock.ExpectQuery("SELECT workflow_state FROM content").WithArgs(contentID, teamID).WillReturnRows(sqlmock.NewRows([]string{"workflow_state"}).AddRow(domain.WorkflowScheduled))
	mock.ExpectExec("UPDATE schedule SET recommendation_id").WithArgs("older-recommendation", oldDate, "18:30", "旧标题", "幕后记录", "视频号", "旧负责人", "人工计划", "manual", "low", 3, "原冲突说明", scheduleID, teamID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "revert_schedule", "schedule", scheduleID, "撤回 AI 排期建议的应用").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE recommendation SET payload_json").WithArgs(sqlmock.AnyArg(), recommendationID, teamID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_log").WithArgs(teamID, "负责人", "revert_schedule_recommendation", "recommendation", recommendationID, fmt.Sprintf("撤回 %d 条已应用排期", 1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	reverted, err := repository.RevertRecommendation(context.Background(), teamID, actorID, "负责人", recommendationID)
	if err != nil {
		t.Fatal(err)
	}
	if reverted != 1 {
		t.Fatalf("reverted=%d, want 1", reverted)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRevertRecommendationDoesNotOverwritePostApplyEdits(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := New(db)
	teamID := int64(7)
	recommendationID, contentID, scheduleID := "recommendation-1", "content-1", "schedule-1"
	date := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	payload := fmt.Sprintf(`{"recommendations":[{"target_content_id":%q,"planned_date":%q,"time_slot":"19:30","title":"AI 标题","owner":"负责人","priority":"high","priority_order":2,"risk_notes":"冲突风险","reason":"AI 理由","adopted":true,"schedule_id":%q}]}`, contentID, date, scheduleID)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT payload_json FROM recommendation").WithArgs(recommendationID, teamID).WillReturnRows(sqlmock.NewRows([]string{"payload_json"}).AddRow(payload))
	mock.ExpectQuery("SELECT content_id,status,source,COALESCE").WithArgs(scheduleID, teamID).WillReturnRows(sqlmock.NewRows([]string{"content_id", "status", "source", "recommendation_id", "planned_date", "planned_time", "title", "owner", "priority", "priority_order", "risk_notes", "reason"}).AddRow(contentID, "待发布", "manual", recommendationID, date, "19:30", "用户已改标题", "负责人", "high", 2, "冲突风险", "AI 理由"))
	mock.ExpectRollback()

	if _, err = repository.RevertRecommendation(context.Background(), teamID, 11, "负责人", recommendationID); err == nil {
		t.Fatal("revert overwrote a schedule edited after application")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
