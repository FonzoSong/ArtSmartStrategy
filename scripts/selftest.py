# SPDX-License-Identifier: Apache-2.0
"""Run end-to-end checks against the configured PostgreSQL database."""

from __future__ import annotations

import json
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from dotenv import load_dotenv

PROJECT_ROOT = Path(__file__).resolve().parents[1]
load_dotenv(PROJECT_ROOT / ".env")

if not os.getenv("DATABASE_URL", "").startswith("postgresql"):
    raise SystemExit("请先配置 PostgreSQL DATABASE_URL，再运行 selftest.py")
os.environ["SEED_ON_START"] = "true"
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from backend.main import app  # noqa: E402
from fastapi.testclient import TestClient  # noqa: E402


class FakeModel(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", "0"))
        self.rfile.read(n)
        body = {
            "choices": [
                {
                    "message": {
                        "content": json.dumps(
                            {
                                "recommendations": [
                                    {
                                        "content_type": "艺术科普",
                                        "platform": "内容社区C",
                                        "day_of_week": "周四",
                                        "time_slot": "19:30",
                                        "title": "从一次练习开始讲起",
                                        "reason": "这组建议来自模拟模型自测。",
                                        "reference_sample_count": 3,
                                        "confidence": "low",
                                    }
                                ]
                            },
                            ensure_ascii=False,
                        )
                    }
                }
            ]
        }
        raw = json.dumps(body, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *_):
        pass


def run():
    fake = ThreadingHTTPServer(("127.0.0.1", 0), FakeModel)
    threading.Thread(target=fake.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{fake.server_port}/v1"
    with TestClient(app) as client:
        checks = []

        def check(name, condition):
            if not condition:
                raise AssertionError(name)
            checks.append(name)
            print(f"PASS {name}")

        check(
            "protected API requires login",
            client.get("/api/contents").status_code == 401,
        )
        setup = client.post(
            "/api/auth/setup",
            json={
                "email": "owner@example.test",
                "password": "selftest-password",
                "name": "测试负责人",
                "team_name": "自测团队",
            },
        )
        check("first-run administrator setup", setup.status_code == 200)
        client.headers.update(
            {
                "Authorization": "Bearer " + setup.json()["access_token"],
                "X-Team-Id": "1",
            }
        )
        check(
            "health and database", client.get("/api/health").status_code == 200
        )
        initial = client.get("/api/contents").json()
        check(
            "seed records loaded",
            initial["count"] == 3
            and {x["lifecycle"] for x in initial["items"]} == {"已发布"},
        )
        check(
            "small sample suppresses category mean",
            client.get("/api/stats/category").json()["items"][0]["views"]
            is None,
        )
        platform_stats = client.get("/api/stats/platform").json()
        check(
            "platform totals remain visible below sample threshold",
            platform_stats["items"][0]["total_views"] is not None
            and platform_stats["items"][0]["total_views"] > 0,
        )
        rule = client.post("/api/ai/recommend").json()
        check(
            "model-free recommendation fallback",
            rule["source"] == "rule"
            and "规则分析" in rule["source_label"]
            and len(rule["recommendations"]) == 3,
        )
        record = {
            "title": "排练细节｜动作拆解练习",
            "category": "技艺展示",
            "platform": "短视频平台B",
            "published_at": "2026-10-06T19:30:00",
            "duration": 45,
            "views": 2100,
            "likes": 180,
            "comments": 20,
            "saves": 35,
            "shares": 12,
            "completion": 71,
            "tags": ["排练", "技巧"],
            "lifecycle": "已发布",
            "profile_visits": 30,
            "follows": 4,
            "attendance": 2,
            "tickets": 1,
            "comment_text": "动作清楚，喜欢这个细节",
        }
        created = client.post("/api/contents", json=record)
        check(
            "create a content asset",
            created.status_code == 200
            and created.json()["item"]["title"] == record["title"],
        )
        cid = created.json()["item"]["id"]
        check(
            "content appears in live overview",
            client.get("/api/stats/overview").json()["total_posts"] == 4,
        )
        check(
            "tag dictionary receives content tags",
            any(x["name"] == "排练" for x in client.get("/api/tags").json()),
        )
        rule_data = {
            "name": "高播放内容自动标记",
            "priority": 200,
            "enabled": True,
            "trigger": "content.create",
            "logic": "all",
            "conditions": [
                {"field": "views", "operator": "gte", "value": 1000},
                {"field": "completion", "operator": "gte", "value": 60},
            ],
            "scope": {
                "platforms": ["短视频平台B"],
                "categories": [],
                "statuses": [],
            },
            "actions": [
                {"type": "add_tag", "value": "规则命中"},
                {"type": "set_status", "value": "未发布"},
            ],
        }
        rule = client.post("/api/rules", json=rule_data)
        check("create combined scoped content rule", rule.status_code == 200)
        conflict_data = {
            **rule_data,
            "name": "冲突规则",
            "actions": [{"type": "add_tag", "value": "不同标签"}],
        }
        check(
            "conflicting equal-priority rule rejected",
            client.post("/api/rules", json=conflict_data).status_code == 409,
        )
        matched_record = {
            **record,
            "title": "规则命中内容",
            "platform": "短视频平台B",
        }
        matched_create = client.post("/api/contents", json=matched_record)
        matched_item = (
            matched_create.json()["item"]
            if matched_create.status_code == 200
            else {}
        )
        check(
            "rule actions apply on content create",
            matched_create.status_code == 200
            and matched_item["lifecycle"] == "未发布"
            and "规则命中" in matched_item["tags"],
        )
        no_match_record = {
            **record,
            "title": "规则未命中内容",
            "platform": "短视频平台A",
            "views": 200,
        }
        no_match = client.post("/api/contents", json=no_match_record)
        check(
            "rule no-match is logged",
            no_match.status_code == 200
            and any(
                x["result"] == "no_match"
                for x in client.get("/api/rules/logs").json()
            ),
        )
        manual_rule = {
            "name": "手动指定负责人",
            "priority": 50,
            "enabled": True,
            "trigger": "manual",
            "logic": "all",
            "conditions": [],
            "scope": {},
            "actions": [{"type": "set_owner", "value": "规则负责人"}],
        }
        saved_manual = client.post("/api/rules", json=manual_rule)
        evaluated = client.post("/api/rules/evaluate", json={"content_id": cid})
        check(
            "manual rule trigger and action",
            saved_manual.status_code == 200
            and evaluated.status_code == 200
            and evaluated.json()["matched"] == 1
            and client.get(f"/api/contents/{cid}").json()["owner"]
            == "规则负责人",
        )
        toggle = client.patch(
            f"/api/rules/{rule.json()['id']}", json={"enabled": False}
        )
        check(
            "rule can be disabled",
            toggle.status_code == 200 and toggle.json()["enabled"] is False,
        )
        csv = '平台,发布时间,标题,播放量,点赞量,看完比例,状态标签,责任人,主页访问\n短视频平台A,2026-10-07 18:00,"CSV 导入, 标题",1250,90,65,,张老师,28\n'
        imp = client.post(
            "/api/contents/import-csv",
            files={"file": ("test.csv", csv.encode(), "text/csv")},
        )
        imported = (
            imp.json()["items"][0]
            if imp.status_code == 200 and imp.json()["items"]
            else {}
        )
        check(
            "CSV import fields and default published status",
            imp.status_code == 200
            and imp.json()["imported"] == 1
            and imported["title"] == "CSV 导入, 标题"
            and imported["lifecycle"] == "已发布"
            and imported["owner"] == "张老师"
            and imported["profile_visits"] == 28,
        )
        full_csv = client.get("/api/export/contents")
        check(
            "complete content CSV export includes status label",
            full_csv.status_code == 200
            and "状态标签" in full_csv.text
            and "arts-smart-content.csv"
            in full_csv.headers.get("content-disposition", ""),
        )
        status_change = client.patch(
            f"/api/contents/{cid}", json={"lifecycle": "创意"}
        )
        overview_status = client.get("/api/stats/overview").json()
        check(
            "legacy status input maps to unpublished and stats exclude it",
            status_change.status_code == 200
            and status_change.json()["lifecycle"] == "未发布"
            and overview_status["total_posts"]
            + overview_status["unpublished_count"]
            == overview_status["total_assets"],
        )
        bulk = client.post(
            "/api/contents/bulk",
            json={"ids": [cid], "action": "set_status", "status": "已发布"},
        )
        check(
            "bulk content status update",
            bulk.status_code == 200
            and client.get(f"/api/contents/{cid}").json()["lifecycle"]
            == "已发布",
        )
        check(
            "minimum sample setting",
            client.post(
                "/api/settings/sample-threshold", json={"min_sample": 2}
            ).json()["min_sample"]
            == 2,
        )
        check(
            "heatmap endpoint",
            len(client.get("/api/stats/heatmap").json()["items"]) == 35,
        )
        other = client.post(
            "/api/auth/register-team",
            json={
                "email": "other@example.test",
                "password": "selftest-password",
                "name": "另一团队",
                "team_name": "另一支体验团队",
            },
        )
        check("create isolated team account", other.status_code == 200)
        check(
            "team isolation",
            client.get(
                "/api/contents",
                headers={"X-Team-Id": str(other.json()["user"]["team_id"])},
            ).status_code
            == 403,
        )
        viewer = client.post(
            "/api/users",
            json={
                "email": "viewer@example.test",
                "password": "selftest-password",
                "name": "只读成员",
                "role": "viewer",
            },
        )
        check("create viewer account", viewer.status_code == 200)
        viewer_login = client.post(
            "/api/auth/login",
            json={
                "email": "viewer@example.test",
                "password": "selftest-password",
            },
        ).json()
        check(
            "viewer role is read-only",
            client.post(
                "/api/contents",
                headers={
                    "Authorization": "Bearer " + viewer_login["access_token"],
                    "X-Team-Id": "1",
                },
                json=record,
            ).status_code
            == 403,
        )
        viewer_headers = {
            "Authorization": "Bearer " + viewer_login["access_token"],
            "X-Team-Id": "1",
        }
        check(
            "viewer cannot use bulk content action",
            client.post(
                "/api/contents/bulk",
                headers=viewer_headers,
                json={"ids": [cid], "action": "set_status", "status": "未发布"},
            ).status_code
            == 403,
        )
        check(
            "viewer cannot manage rules",
            client.post(
                "/api/rules",
                headers=viewer_headers,
                json={
                    "name": "只读规则",
                    "actions": [{"type": "add_tag", "value": "x"}],
                },
            ).status_code
            == 403,
        )
        # Exercise actual server-proxied model generation against a local mock server.
        config = client.get("/api/settings").json()
        config.update(
            {
                "base_url": base,
                "model": "selftest-model",
                "api_key": "selftest-key",
                "request_mode": "base",
                "fallback_enabled": True,
            }
        )
        saved = client.put("/api/settings", json=config)
        check(
            "persist model proxy settings",
            saved.status_code == 200 and saved.json()["configured"],
        )
        live = client.post("/api/ai/recommend").json()
        check(
            "AI recommendation proxy and JSON parsing",
            live["source"] == "selftest-model"
            and live["recommendations"][0]["title"] == "从一次练习开始讲起",
        )
        adopted = client.post(
            "/api/schedule/adopt",
            json={
                "recommendation_id": live["id"],
                "items": live["recommendations"],
            },
        )
        check(
            "adopt recommendation into persistent schedule",
            adopted.status_code == 200 and adopted.json()["created"] == 1,
        )
        sched = client.get("/api/schedule").json()[0]
        check(
            "schedule read after adoption",
            sched["title"] == "从一次练习开始讲起" and "owner" in sched,
        )
        month_start = sched["planned_date"][:7] + "-01"
        month_end = sched["planned_date"]
        check(
            "schedule date range query for monthly view",
            any(
                x["id"] == sched["id"]
                for x in client.get(
                    f"/api/schedule?start={month_start}&end={month_end}"
                ).json()
            ),
        )
        patched = client.patch(
            f"/api/schedule/{sched['id']}",
            json={
                "planned_time": "20:00",
                "title": "调整后的建议标题",
                "owner": "排期负责人",
            },
        )
        check(
            "edit persisted schedule and owner",
            patched.status_code == 200
            and patched.json()["planned_time"] == "20:00"
            and next(
                x
                for x in client.get("/api/schedule").json()
                if x["id"] == sched["id"]
            )["owner"]
            == "排期负责人",
        )
        feedback = {
            "title": "调整后的建议标题",
            "category": "艺术科普",
            "platform": "内容社区C",
            "published_at": "2026-10-10T20:00:00",
            "views": 1800,
            "likes": 140,
            "comments": 15,
            "saves": 24,
            "shares": 10,
            "completion": 72,
            "profile_visits": 60,
            "follows": 6,
            "attendance": 4,
            "tickets": 3,
            "comment_text": "讲得清楚",
        }
        result = client.post(
            f"/api/schedule/{sched['id']}/publish", json=feedback
        )
        check(
            "publish feedback closes loop",
            result.status_code == 200
            and result.json()["content"]["adopted"] is True,
        )
        check(
            "adoption comparison endpoint",
            client.get("/api/stats/adoption").json()["adopted"]["sample_count"]
            == 1,
        )
        schedule_export = client.get("/api/schedule/export")
        check(
            "schedule CSV export filename",
            schedule_export.status_code == 200
            and "arts-smart-schedule.csv"
            in schedule_export.headers.get("content-disposition", "")
            and "负责人" in schedule_export.text
            and schedule_export.headers.get("content-type", "").startswith(
                "text/csv"
            ),
        )
        pdf = client.get("/api/report/export")
        check(
            "PDF report export filename",
            pdf.status_code == 200
            and pdf.content.startswith(b"%PDF")
            and len(pdf.content) > 1000
            and "Artsmart-report.pdf"
            in pdf.headers.get("content-disposition", ""),
        )
        hand = client.get("/api/handover/export?format=markdown")
        check(
            "handover Markdown export filename",
            hand.status_code == 200
            and "运营交接包" in hand.text
            and "handover.md" in hand.headers.get("content-disposition", ""),
        )
        hand_pdf = client.get("/api/handover/export?format=pdf")
        check(
            "handover PDF export filename",
            hand_pdf.status_code == 200
            and hand_pdf.content.startswith(b"%PDF")
            and "handover.pdf"
            in hand_pdf.headers.get("content-disposition", ""),
        )
        anon = client.get("/api/export/anonymized")
        check(
            "anonymized CSV export filename",
            anon.status_code == 200
            and "标题" not in anon.text
            and "状态标签" in anon.text
            and "anonymized-content.csv"
            in anon.headers.get("content-disposition", "")
            and anon.headers.get("content-type", "").startswith("text/csv"),
        )
        route_paths = {route.path for route in app.routes}
        check(
            "image recognition endpoint removed",
            "/api/contents/import-image" not in route_paths,
        )
        check(
            "sample restore endpoint removed",
            "/api/seed/reset" not in route_paths,
        )
        check(
            "audit log records operations",
            len(client.get("/api/audit").json()) >= 4,
        )
        print(f"\n{len(checks)} end-to-end checks passed.")
    fake.shutdown()


if __name__ == "__main__":
    run()
