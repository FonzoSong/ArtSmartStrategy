# SPDX-License-Identifier: Apache-2.0
"""Guard the standalone frontend routes and workflow-status wiring."""

from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
APP_SOURCE = (ROOT / "frontend" / "app.js").read_text(encoding="utf-8")
HTML_SOURCE = (ROOT / "frontend" / "index.html").read_text(encoding="utf-8")


def test_team_and_rules_have_distinct_navigation_routes() -> None:
    """The sidebar names and page renderer expose both management pages."""
    assert 'data-page="team">' in HTML_SOURCE
    assert "艺术团队管理" in HTML_SOURCE
    assert 'data-page="rules">' in HTML_SOURCE
    assert "内容规则集" in HTML_SOURCE
    assert "team:'艺术团队管理',rules:'内容规则集',tags:'Tag 库'" in APP_SOURCE
    assert "team:teamPage,rules:rulesPage,tags:tagsPage" in APP_SOURCE
    assert "window.addEventListener('popstate',routeFromLocation)" in APP_SOURCE
    assert "history.pushState(null,'',`#${page}`)" in APP_SOURCE
    assert "qs('#crumb').textContent=pages[state.page]" in APP_SOURCE
    assert "syncPageChrome();await render()" in APP_SOURCE


def test_model_settings_does_not_load_team_or_rule_modules() -> None:
    """Model configuration is isolated from team and rule management."""
    start = APP_SOURCE.index("async function settingsPage(){")
    end = APP_SOURCE.index("async function rulesPage(){", start)
    settings_source = APP_SOURCE[start:end]
    assert "/api/rules" not in settings_source
    assert "/api/users" not in settings_source
    assert "/api/teams" not in settings_source
    assert "团队与成员" not in settings_source
    assert "内容规则集" not in settings_source


def test_team_page_retains_member_creation_form() -> None:
    """The team page loads members and submits the existing invite endpoint."""
    start = APP_SOURCE.index("async function teamPage(){")
    end = APP_SOURCE.index("async function runRules(", start)
    team_source = APP_SOURCE[start:end]
    assert "api('/api/teams')" in team_source
    assert "api('/api/users')" in team_source
    assert 'id="userForm"' in team_source
    assert "成员姓名" in team_source


def test_asset_tabs_filter_by_workflow_state() -> None:
    """Published/unpublished grouping comes from workflow_state."""
    start = APP_SOURCE.index("function workflowAssetStatus(")
    end = APP_SOURCE.index("async function analysisPage(", start)
    status_source = APP_SOURCE[start:end]
    assert "record.workflow_state===WORKFLOW.published" in status_source
    assert "x.workflow_state===state.recordsStatus" in status_source
    assert "WORKFLOW_STATES.map(status=>" in APP_SOURCE
    assert "all.filter(x=>x.workflow_state===status).length" in APP_SOURCE
    assert "WORKFLOW_COLORS" in APP_SOURCE
    assert 'data-action="advance-workflow"' in APP_SOURCE
    assert 'data-action="manual-schedule"' in APP_SOURCE


def test_schedule_controls_call_recommendation_endpoint() -> None:
    """Both recommendation actions use the API and render explicit errors."""
    assert 'data-action="generate-rules"' in APP_SOURCE
    assert 'data-action="generate"' in APP_SOURCE
    assert "/api/ai/recommend" in APP_SOURCE
    assert "排期建议生成失败" in APP_SOURCE
