# SPDX-License-Identifier: Apache-2.0
"""FastAPI application and HTTP endpoints for 艺数智策."""

from __future__ import annotations

import csv
import io
import json
import os
import re
import statistics
import time
from collections import Counter, defaultdict
from contextlib import asynccontextmanager
from datetime import date, datetime, timedelta
from pathlib import Path
from typing import Any, AsyncIterator

import httpx
from fastapi import (
    Depends,
    FastAPI,
    File,
    HTTPException,
    Query,
    UploadFile,
)
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import Response
from fastapi.staticfiles import StaticFiles
from reportlab.pdfbase.ttfonts import TTFError
from sqlalchemy import (
    select,
)
from sqlalchemy.exc import SQLAlchemyError
from sqlalchemy.orm import (
    Session,
)

from backend import __version__
from backend.analytics import (
    day_label,
    group_analysis,
    interaction_rate,
    sample_notice,
    score_content,
    time_label,
)
from backend.auth import auth_claims, issue_token, require_editor, role, team_id
from backend.content import sync_tag_links
from backend.database import Base, engine, get_db
from backend.models import (
    AICallLog,
    AIConfig,
    AuditLog,
    Benchmark,
    Content,
    ContentTag,
    Hypothesis,
    Recommendation,
    RuleExecutionLog,
    Schedule,
    Tag,
    Team,
    User,
    UserSession,
)
from backend.rules import execute_content_rules
from backend.scheduler.advisor import build_schedule_recommendations
from backend.scheduler.rules_loader import load_rule_set_safe
from backend.schemas import (
    AdoptIn,
    AIConfigIn,
    AuthSetup,
    BulkContentIn,
    ContentIn,
    LoginIn,
    PublishIn,
    RuleIn,
    SampleSetting,
    TeamIn,
    UserInvite,
)
from backend.security import password_hash, verify_password
from backend.settings import (
    MIN_SAMPLE_DEFAULT,
    PLATFORMS,
    ROOT,
)
from backend.status import normalize_content_status


def team_or_404(db: Session, tid: int) -> Team:
    team = db.get(Team, tid)
    if not team:
        raise HTTPException(404, "团队不存在")
    return team


def get_config(db: Session, tid: int) -> AIConfig:
    config = db.scalar(
        select(AIConfig).where(
            AIConfig.team_id == tid, AIConfig.enabled.is_(True)
        )
    )
    if config:
        return config
    config = AIConfig(
        team_id=tid,
        min_sample=MIN_SAMPLE_DEFAULT,
        api_key=os.getenv("AI_API_KEY", ""),
        base_url=os.getenv("AI_BASE_URL", ""),
        model=os.getenv("AI_MODEL", ""),
    )
    db.add(config)
    db.flush()
    return config


def serialize_content(
    c: Content, config: AIConfig | None = None
) -> dict[str, Any]:
    try:
        tags = json.loads(c.tags_json or "[]")
    except (json.JSONDecodeError, TypeError):
        tags = []
    return {
        "id": c.id,
        "title": c.title,
        "category": c.category,
        "platform": c.platform,
        "published_at": c.published_at.isoformat(),
        "date": c.published_at.isoformat(timespec="minutes"),
        "duration": c.duration,
        "views": c.views,
        "likes": c.likes,
        "comments": c.comments,
        "saves": c.saves,
        "shares": c.shares,
        "total_interactions": c.total_interactions,
        "completion": c.completion,
        "tags": tags,
        "source": c.source,
        "lifecycle": normalize_content_status(c.lifecycle),
        "materials": c.materials,
        "owner": c.owner,
        "notes": c.notes,
        "profile_visits": c.profile_visits,
        "follows": c.follows,
        "attendance": c.attendance,
        "tickets": c.tickets,
        "comment_text": c.comment_text,
        "adopted": c.adopted,
        "recommendation_id": c.recommendation_id,
        "score": score_content(c, config) if config else None,
        "created_by": c.created_by,
        "created_at": c.created_at.isoformat(),
    }


def audit(
    db: Session,
    tid: int,
    action: str,
    entity: str,
    entity_id: str = "",
    detail: str = "",
    actor: str = "团队运营者",
):
    db.add(
        AuditLog(
            team_id=tid,
            actor=actor,
            action=action,
            entity_type=entity,
            entity_id=entity_id,
            detail=detail[:2000],
        )
    )


def record_from_input(
    data: ContentIn, tid: int, user: str = "团队运营者"
) -> Content:
    values = data.model_dump()
    tags = values.pop("tags", [])
    values["tags_json"] = json.dumps(tags, ensure_ascii=False)
    values["total_interactions"] = sum(
        values.get(k, 0) for k in ("likes", "comments", "saves", "shares")
    )
    values["team_id"] = tid
    values["created_by"] = user
    return Content(**values)


def all_contents(db: Session, tid: int) -> list[Content]:
    return list(
        db.scalars(
            select(Content)
            .where(Content.team_id == tid)
            .order_by(Content.published_at)
        ).all()
    )


def published_contents(db: Session, tid: int) -> list[Content]:
    return [
        item
        for item in all_contents(db, tid)
        if normalize_content_status(item.lifecycle) == "已发布"
    ]


def unpublished_contents(db: Session, tid: int) -> list[Content]:
    """Return only unpublished assets for scheduling and backlog workflows."""
    return [
        item
        for item in all_contents(db, tid)
        if normalize_content_status(item.lifecycle) == "未发布"
    ]


def model_endpoint(config: AIConfig) -> str:
    raw = config.base_url.strip().rstrip("/")
    if not raw:
        raise ValueError("尚未填写模型接口地址")
    if config.request_mode == "full":
        return raw
    return (
        raw if raw.endswith("/chat/completions") else raw + "/chat/completions"
    )


def model_headers(config: AIConfig) -> dict[str, str]:
    if config.auth_type == "api-key":
        return {"api-key": config.api_key, "Content-Type": "application/json"}
    if config.auth_type.startswith("custom:"):
        return {
            config.auth_type.split(":", 1)[1]: config.api_key,
            "Content-Type": "application/json",
        }
    return {
        "Authorization": f"Bearer {config.api_key}",
        "Content-Type": "application/json",
    }


def parse_model_json(text: str) -> dict[str, Any]:
    text = text.strip()
    text = re.sub(r"^```(?:json)?\s*|\s*```$", "", text)
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end <= start:
        raise ValueError("模型没有返回有效 JSON")
    data = json.loads(text[start : end + 1])
    if isinstance(data, list):
        data = {"recommendations": data}
    recs = data.get("recommendations", data.get("suggestions", []))
    if not isinstance(recs, list) or not recs:
        raise ValueError("模型返回结果中没有建议列表")
    normalized = []
    for rec in recs[:3]:
        normalized.append(
            {
                "content_type": rec.get(
                    "content_type", rec.get("category", "艺术科普")
                ),
                "platform": rec.get("platform", PLATFORMS[0]),
                "day_of_week": rec.get("day_of_week", rec.get("day", "周三")),
                "time_slot": rec.get("time_slot", rec.get("time", "19:30")),
                "title": rec.get(
                    "title", rec.get("topic", "围绕团队经验创作一条内容")
                ),
                "reason": rec.get(
                    "reason", "建议仅供参考，请结合团队实际安排。"
                ),
                "reference_sample_count": int(
                    rec.get("reference_sample_count", 0) or 0
                ),
                "confidence": rec.get("confidence", "low")
                if rec.get("confidence") in {"high", "medium", "low"}
                else "low",
            }
        )
    return {"recommendations": normalized}


def dataset_summary(records: list[Content], config: AIConfig) -> dict[str, Any]:
    threshold = config.min_sample
    by_category = group_analysis(records, "category", threshold, config)
    by_platform = group_analysis(records, "platform", threshold, config)
    combos: dict[tuple[str, int, int], list[Content]] = defaultdict(list)
    for c in records:
        combos[
            (c.category, c.published_at.weekday(), c.published_at.hour)
        ].append(c)
    best = []
    for (category, weekday, hour), group in combos.items():
        if len(group) >= threshold:
            best.append(
                {
                    "category": category,
                    "day": day_label(weekday),
                    "hour": hour,
                    "completion_mean": round(
                        statistics.mean(c.completion for c in group), 1
                    ),
                    "interaction_mean": round(
                        statistics.mean(interaction_rate(c) for c in group), 2
                    ),
                    "sample_count": len(group),
                }
            )
    best.sort(
        key=lambda item: (
            float(item["completion_mean"])
            + min(float(item["interaction_mean"]), 28.0)
        ),
        reverse=True,
    )
    records_summary = [
        {
            "category": c.category,
            "tags": json.loads(c.tags_json or "[]"),
            "platform": c.platform,
            "weekday": day_label(c.published_at.weekday()),
            "time_slot": time_label(c.published_at.hour),
            "views": c.views,
            "completion": c.completion,
            "score": score_content(c, config),
            "interactions": c.total_interactions,
        }
        for c in records[-200:]
    ]
    return {
        "sample_total": len(records),
        "minimum_sample_for_patterns": threshold,
        "category_aggregates": by_category,
        "platform_aggregates": by_platform,
        "best_time_combinations": best[:10],
        "records": records_summary,
    }


def report_pdf(
    db: Session, tid: int, records: list[Content], config: AIConfig
) -> bytes:
    from reportlab.lib import colors
    from reportlab.lib.pagesizes import A4
    from reportlab.pdfbase import pdfmetrics
    from reportlab.pdfbase.ttfonts import TTFont
    from reportlab.pdfgen import canvas

    font_candidates = [
        "/usr/share/fonts/google-droid-sans-fonts/DroidSansFallbackFull.ttf",
        "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
        "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
        "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
        "/System/Library/Fonts/PingFang.ttc",
    ]
    font = next((x for x in font_candidates if Path(x).exists()), None)
    if font:
        try:
            pdfmetrics.registerFont(TTFont("CJK", font, subfontIndex=0))
            font_name = "CJK"
        except (OSError, TTFError, ValueError):
            font_name = "Helvetica"
    else:
        font_name = "Helvetica"
    out = io.BytesIO()
    c = canvas.Canvas(out, pagesize=A4)
    width, height = A4
    c.setFillColor(colors.HexColor("#eee9dc"))
    c.rect(0, 0, width, height, fill=1, stroke=0)
    c.setFillColor(colors.HexColor("#302b24"))
    c.setFont(font_name, 20)
    c.drawString(42, height - 54, "艺数智策 运营复盘")
    c.setFont(font_name, 9)
    c.setFillColor(colors.HexColor("#81796d"))
    c.drawString(
        42,
        height - 74,
        f"生成时间 {datetime.now().strftime('%Y-%m-%d %H:%M')} · 仅基于团队主动录入的数据",
    )
    y = height - 112
    total = len(records)
    views = sum(x.views for x in records)
    inter = sum(x.total_interactions for x in records)
    completion = (
        statistics.mean([x.completion for x in records]) if records else 0
    )
    c.setFont(font_name, 11)
    c.setFillColor(colors.HexColor("#3d4b5c"))
    c.drawString(42, y, "传播表现概览")
    c.setFillColor(colors.HexColor("#302b24"))
    c.setFont(font_name, 10)
    c.drawString(
        42,
        y - 23,
        f"发布记录：{total} 条     总播放：{views:,}     总互动：{inter:,}     平均看完比例：{completion:.1f}%",
    )
    y -= 65
    c.setFillColor(colors.HexColor("#3d4b5c"))
    c.setFont(font_name, 11)
    c.drawString(42, y, "内容类型表现（样本不足时不展示均值）")
    groups = group_analysis(records, "category", config.min_sample, config)
    y -= 20
    c.setFont(font_name, 9)
    for g in groups:
        value = (
            f"平均看完 {g['completion']}% · 平均播放 {g['views']:,.0f}"
            if g["sample_count"] >= config.min_sample
            else "样本不足，继续积累记录"
        )
        c.setFillColor(colors.HexColor("#302b24"))
        c.drawString(
            46, y, f"{g['name']}  ·  {g['sample_count']} 条  ·  {value}"
        )
        c.setFillColor(colors.HexColor("#d7d0c2"))
        c.rect(300, y - 2, 180, 8, fill=1, stroke=0)
        if g["score"] is not None:
            c.setFillColor(colors.HexColor("#647383"))
            c.rect(
                300, y - 2, 180 * min(1, g["score"] / 100), 8, fill=1, stroke=0
            )
        y -= 22
    y -= 12
    c.setFillColor(colors.HexColor("#3d4b5c"))
    c.setFont(font_name, 11)
    c.drawString(42, y, "优质内容")
    y -= 20
    c.setFont(font_name, 9)
    ranked = sorted(
        records, key=lambda x: score_content(x, config), reverse=True
    )[:5]
    for item in ranked:
        text = f"{item.title[:36]} · {item.category} · {item.platform} · {score_content(item, config):.0f}分"
        c.setFillColor(colors.HexColor("#302b24"))
        c.drawString(46, y, text)
        y -= 19
    ruleset = load_rule_set_safe()
    rec = (
        build_schedule_recommendations(
            records,
            unpublished_contents(db, tid),
            ruleset,
            datetime.now(),
            config.min_sample,
            db.scalars(
                select(Schedule).where(
                    Schedule.team_id == tid,
                    Schedule.status == "待发布",
                )
            ).all(),
        )
        if ruleset is not None
        else []
    )
    y -= 10
    c.setFillColor(colors.HexColor("#a84f2c"))
    c.setFont(font_name, 11)
    c.drawString(42, y, "下周发布参考")
    y -= 20
    c.setFont(font_name, 9)
    c.setFillColor(colors.HexColor("#302b24"))
    for r in rec:
        if y < 95:
            c.showPage()
            y = height - 55
            c.setFont(font_name, 9)
        c.drawString(
            46,
            y,
            f"{r['day_of_week']} {r['time_slot']} · {r['content_type']} · {r['platform']} · {r['confidence']}信心",
        )
        y -= 14
        c.setFillColor(colors.HexColor("#81796d"))
        c.drawString(54, y, r["reason"][:70])
        c.setFillColor(colors.HexColor("#302b24"))
        y -= 20
    c.setFont(font_name, 8)
    c.setFillColor(colors.HexColor("#81796d"))
    c.drawString(
        42,
        35,
        "综合表现分根据看完比例和互动表现计算。小样本结论仅供参考，不代表行业均值。",
    )
    c.save()
    return out.getvalue()


def startup() -> None:
    """Create database tables without inserting business or demo data."""
    Base.metadata.create_all(engine)


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    """Run application startup and keep resources open until shutdown."""
    startup()
    yield


app = FastAPI(
    title="艺数智策 API",
    version=__version__,
    description="面向艺术团队的自有数据复盘系统",
    lifespan=lifespan,
)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["http://localhost:8000", "http://127.0.0.1:8000"],
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/api/health")
def health(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    team_or_404(db, tid)
    return {
        "status": "ok",
        "database": "connected",
        "team_id": tid,
        "version": app.version,
    }


@app.get("/api/auth/status")
def auth_status(db: Session = Depends(get_db)):
    return {"setup_required": db.scalar(select(User.id).limit(1)) is None}


def auth_result(db: Session, user: User) -> dict[str, Any]:
    token = issue_token(db, user)
    team = db.get(Team, user.team_id)
    db.commit()
    return {
        "access_token": token,
        "token_type": "bearer",
        "expires_in": 1209600,
        "user": {
            "id": user.id,
            "email": user.email,
            "name": user.name,
            "role": user.role_name,
            "team_id": user.team_id,
            "team_name": team.name if team else "",
        },
    }


@app.post("/api/auth/setup")
def setup_account(data: AuthSetup, db: Session = Depends(get_db)):
    if db.scalar(select(User.id).limit(1)) is not None:
        raise HTTPException(409, "管理员账号已经创建，请使用登录")
    team = db.get(Team, 1)
    if not team:
        team = Team(id=1, name=data.team_name)
        db.add(team)
    else:
        team.name = data.team_name
    if "@" not in data.email:
        raise HTTPException(422, "请输入有效邮箱地址")
    user = User(
        team_id=1,
        email=data.email.strip().lower(),
        name=data.name.strip(),
        role_name="owner",
        password_hash=password_hash(data.password),
    )
    db.add(user)
    db.flush()
    audit(
        db, 1, "setup", "user", str(user.id), "创建团队管理员", actor=user.name
    )
    return auth_result(db, user)


@app.post("/api/auth/register-team")
def register_team(data: AuthSetup, db: Session = Depends(get_db)):
    if "@" not in data.email:
        raise HTTPException(422, "请输入有效邮箱地址")
    if db.scalar(select(User).where(User.email == data.email.strip().lower())):
        raise HTTPException(409, "该邮箱已经注册")
    team = Team(name=data.team_name.strip())
    db.add(team)
    db.flush()
    user = User(
        team_id=team.id,
        email=data.email.strip().lower(),
        name=data.name.strip(),
        role_name="owner",
        password_hash=password_hash(data.password),
    )
    db.add(user)
    db.flush()
    audit(
        db,
        team.id,
        "register_team",
        "team",
        str(team.id),
        "创建新团队",
        actor=user.name,
    )
    return auth_result(db, user)


@app.post("/api/auth/login")
def login(data: LoginIn, db: Session = Depends(get_db)):
    user = db.scalar(
        select(User).where(User.email == data.email.strip().lower())
    )
    if not user or not verify_password(data.password, user.password_hash):
        raise HTTPException(401, "邮箱或密码不正确")
    return auth_result(db, user)


@app.get("/api/auth/me")
def current_account(
    claims: dict[str, Any] = Depends(auth_claims), db: Session = Depends(get_db)
):
    team = db.get(Team, claims["team_id"])
    return {
        "id": claims["user_id"],
        "email": claims["email"],
        "name": claims["name"],
        "role": claims["role"],
        "team_id": claims["team_id"],
        "team_name": team.name if team else "",
    }


@app.post("/api/auth/logout")
def logout(
    claims: dict[str, Any] = Depends(auth_claims), db: Session = Depends(get_db)
):
    session = db.get(UserSession, claims["session_id"])
    if session is None:
        raise HTTPException(401, "登录状态已失效")
    session.revoked = True
    db.commit()
    return {"logged_out": True}


@app.get("/api/users")
def list_users(
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    return [
        {
            "id": u.id,
            "email": u.email,
            "name": u.name,
            "role": u.role_name,
            "created_at": u.created_at.isoformat(),
        }
        for u in db.scalars(
            select(User).where(User.team_id == tid).order_by(User.created_at)
        ).all()
    ]


@app.post("/api/users")
def add_user(
    data: UserInvite,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(role),
):
    if _role != "owner":
        raise HTTPException(403, "只有团队负责人可以管理成员")
    email = data.email.strip().lower()
    if "@" not in email:
        raise HTTPException(422, "请输入有效邮箱地址")
    if data.role not in {"editor", "viewer"}:
        raise HTTPException(422, "成员角色只能是录入者或查看者")
    if db.scalar(select(User).where(User.email == email)):
        raise HTTPException(409, "该邮箱已经注册")
    user = User(
        team_id=tid,
        email=email,
        name=data.name.strip(),
        role_name=data.role,
        password_hash=password_hash(data.password),
    )
    db.add(user)
    db.flush()
    audit(
        db,
        tid,
        "invite",
        "user",
        str(user.id),
        f"新增成员 {email} / {data.role}",
    )
    db.commit()
    return {
        "id": user.id,
        "email": user.email,
        "name": user.name,
        "role": user.role_name,
    }


@app.get("/api/teams")
def teams(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    team = team_or_404(db, tid)
    return [{"id": team.id, "name": team.name}]


@app.post("/api/teams")
def create_team(
    data: TeamIn,
    db: Session = Depends(get_db),
    _role: str = Depends(require_editor),
):
    team = Team(name=data.name)
    db.add(team)
    db.flush()
    audit(db, team.id, "create", "team", str(team.id))
    db.commit()
    return {"id": team.id, "name": team.name}


@app.get("/api/contents")
def list_contents(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    team_or_404(db, tid)
    cfg = get_config(db, tid)
    rows = all_contents(db, tid)
    return {
        "items": [serialize_content(c, cfg) for c in rows],
        "count": len(rows),
    }


@app.post("/api/contents")
def create_content(
    data: ContentIn,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    team_or_404(db, tid)
    if data.completion > 100:
        raise HTTPException(422, "看完比例不能超过 100%")
    near = db.scalars(
        select(Content).where(
            Content.team_id == tid, Content.title == data.title
        )
    ).all()
    duplicate = any(
        abs((x.published_at - data.published_at).total_seconds()) < 3 * 86400
        for x in near
    )
    item = record_from_input(data, tid)
    db.add(item)
    db.flush()
    sync_tag_links(db, item, data.tags)
    execute_content_rules(db, tid, item, "content.create")
    audit(db, tid, "create", "content", item.id, data.title)
    db.commit()
    return {
        "item": serialize_content(item, get_config(db, tid)),
        "duplicate_warning": "发现标题相同且发布时间接近的记录，请确认是否重复。"
        if duplicate
        else None,
    }


@app.get("/api/contents/{content_id}")
def get_content(
    content_id: str, db: Session = Depends(get_db), tid: int = Depends(team_id)
):
    item = db.scalar(
        select(Content).where(Content.id == content_id, Content.team_id == tid)
    )
    if not item:
        raise HTTPException(404, "内容记录不存在")
    return serialize_content(item, get_config(db, tid))


@app.patch("/api/contents/{content_id}")
def update_content(
    content_id: str,
    data: dict[str, Any],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    item = db.scalar(
        select(Content).where(Content.id == content_id, Content.team_id == tid)
    )
    if not item:
        raise HTTPException(404, "内容记录不存在")
    allowed = set(ContentIn.model_fields) - {"tags"}
    for k, v in data.items():
        if k not in allowed and k != "tags":
            continue
        if k == "published_at" and isinstance(v, str):
            v = datetime.fromisoformat(v.replace("Z", "+00:00")).replace(
                tzinfo=None
            )
        if k == "tags":
            item.tags_json = json.dumps(v, ensure_ascii=False)
            sync_tag_links(db, item, v)
        elif k == "lifecycle":
            item.lifecycle = normalize_content_status(v)
        else:
            setattr(item, k, v)
    item.total_interactions = (
        item.likes + item.comments + item.saves + item.shares
    )
    execute_content_rules(db, tid, item, "content.update")
    audit(
        db,
        tid,
        "update",
        "content",
        item.id,
        json.dumps(data, ensure_ascii=False),
    )
    db.commit()
    return serialize_content(item, get_config(db, tid))


@app.delete("/api/contents/{content_id}")
def delete_content(
    content_id: str,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    item = db.scalar(
        select(Content).where(Content.id == content_id, Content.team_id == tid)
    )
    if not item:
        raise HTTPException(404, "内容记录不存在")
    audit(db, tid, "delete", "content", item.id, item.title)
    db.delete(item)
    db.commit()
    return {"deleted": True}


@app.get("/api/rules")
def list_rules(
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(role),
):
    del db, tid, _role
    ruleset = load_rule_set_safe()
    if ruleset is None:
        return []
    return [
        {"id": index, **rule}
        for index, rule in enumerate(
            sorted(
                ruleset["rules"],
                key=lambda rule: (-rule["priority"], rule["name"]),
            ),
            start=1,
        )
    ]


@app.post("/api/rules")
def create_rule(
    data: RuleIn,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    del data, db, tid
    raise HTTPException(405, "内容规则集为只读配置，请编辑项目规则 JSON 文件")


@app.patch("/api/rules/{rule_id}")
def update_rule(
    rule_id: int,
    data: dict[str, Any],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    del rule_id, data, db, tid
    raise HTTPException(405, "内容规则集为只读配置，请编辑项目规则 JSON 文件")


@app.delete("/api/rules/{rule_id}")
def delete_rule(
    rule_id: int,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    del rule_id, db, tid
    raise HTTPException(405, "内容规则集为只读配置，不能从应用内删除")


@app.get("/api/rules/logs")
def rule_logs(
    limit: int = Query(default=100, ge=1, le=500),
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(role),
):
    rows = db.scalars(
        select(RuleExecutionLog)
        .where(RuleExecutionLog.team_id == tid)
        .order_by(RuleExecutionLog.created_at.desc())
        .limit(limit)
    ).all()
    return [
        {
            "id": x.id,
            "rule_id": x.rule_id,
            "rule_name": x.rule_name,
            "content_id": x.content_id,
            "trigger": x.trigger,
            "result": x.result,
            "detail": x.detail,
            "created_at": x.created_at.isoformat(),
        }
        for x in rows
    ]


@app.post("/api/rules/evaluate")
def evaluate_rules(
    data: dict[str, str],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    item = db.scalar(
        select(Content).where(
            Content.id == data.get("content_id"), Content.team_id == tid
        )
    )
    if not item:
        raise HTTPException(404, "内容记录不存在")
    logs = execute_content_rules(db, tid, item, "manual")
    db.commit()
    return {
        "matched": sum(x.result == "matched" for x in logs),
        "checked": len(logs),
        "items": [
            {"rule_name": x.rule_name, "result": x.result, "detail": x.detail}
            for x in logs
        ],
    }


@app.post("/api/contents/bulk")
def bulk_contents(
    data: BulkContentIn,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    ids = list(dict.fromkeys(data.ids))
    items = db.scalars(
        select(Content).where(Content.team_id == tid, Content.id.in_(ids))
    ).all()
    if len(items) != len(ids):
        raise HTTPException(404, "部分内容记录不存在或不属于当前团队")
    if data.action == "delete":
        for item in items:
            audit(
                db, tid, "delete", "content", item.id, f"批量删除：{item.title}"
            )
            db.delete(item)
    elif data.action == "set_status":
        if data.status not in {"已发布", "未发布"}:
            raise HTTPException(422, "状态只能是已发布或未发布")
        for item in items:
            previous = item.lifecycle
            item.lifecycle = data.status
            execute_content_rules(db, tid, item, "content.update")
            audit(
                db,
                tid,
                "bulk_status",
                "content",
                item.id,
                f"{previous} → {data.status}",
            )
    else:
        raise HTTPException(422, "批量操作不支持")
    db.commit()
    return {"updated": len(items), "action": data.action}


@app.post("/api/contents/import-csv")
async def import_csv(
    file: UploadFile = File(...),
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    raw = await file.read()
    try:
        text = raw.decode("utf-8-sig")
    except UnicodeDecodeError:
        text = raw.decode("gb18030", errors="replace")
    rows = list(csv.DictReader(io.StringIO(text)))
    aliases = {
        "平台": "platform",
        "发布平台": "platform",
        "发布时间": "published_at",
        "标题": "title",
        "标题文案": "title",
        "内容类型": "category",
        "时长": "duration",
        "视频时长": "duration",
        "播放量": "views",
        "点赞量": "likes",
        "评论量": "comments",
        "收藏量": "saves",
        "转发量": "shares",
        "看完比例": "completion",
        "完播率": "completion",
        "状态标签": "lifecycle",
        "状态": "lifecycle",
        "标签": "tags",
        "素材说明": "materials",
        "责任人": "owner",
        "备注": "notes",
        "主页访问": "profile_visits",
        "新增关注": "follows",
        "到场人数": "attendance",
        "售票数": "tickets",
        "评论摘录": "comment_text",
    }
    mapped = []
    for row in rows:
        normalized = {
            aliases.get(k.strip(), k.strip()): v for k, v in row.items() if k
        }
        mapped.append(normalized)
    required = {"platform", "published_at", "title", "views", "likes"}
    missing = (
        sorted(required - set().union(*(set(r) for r in mapped)))
        if mapped
        else ["platform", "published_at", "title", "views", "likes"]
    )
    if missing:
        raise HTTPException(
            422,
            {
                "message": "CSV 缺少必要字段",
                "missing": missing,
                "template": "平台,发布时间,标题,播放量,点赞量",
            },
        )
    added = []
    warnings = []
    for i, r in enumerate(mapped, 2):
        try:
            published = datetime.fromisoformat(
                str(r["published_at"]).replace("/", "-").replace(" ", "T")
            )

            def num(k, default=0):
                value = float(r.get(k, default) or default)
                if value < 0:
                    raise ValueError(f"{k} 不能为负数")
                return int(value) if k != "completion" else value

            title = str(r["title"]).strip()
            category = str(r.get("category") or classify(title))
            if num("completion") > 100:
                warnings.append(
                    {"row": i, "message": "看完比例超过 100%，请核对"}
                )
                continue
            if db.scalar(
                select(Content.id).where(
                    Content.team_id == tid,
                    Content.title == title,
                    Content.published_at.between(
                        published - timedelta(days=3),
                        published + timedelta(days=3),
                    ),
                )
            ):
                warnings.append(
                    {
                        "row": i,
                        "message": "相同标题且发布时间接近，已跳过重复行",
                    }
                )
                continue
            status = normalize_content_status(r.get("lifecycle") or "已发布")
            item = Content(
                team_id=tid,
                title=title,
                category=category,
                platform=str(r["platform"]).strip(),
                published_at=published,
                duration=num("duration"),
                views=num("views"),
                likes=num("likes"),
                comments=num("comments"),
                saves=num("saves"),
                shares=num("shares"),
                completion=num("completion"),
                total_interactions=num("likes")
                + num("comments")
                + num("saves")
                + num("shares"),
                tags_json=json.dumps(
                    re.split(r"[,，|]", str(r.get("tags", "")))
                    if r.get("tags")
                    else [],
                    ensure_ascii=False,
                ),
                source="csv",
                lifecycle=status,
                materials=str(r.get("materials") or ""),
                owner=str(r.get("owner") or ""),
                notes=str(r.get("notes") or ""),
                profile_visits=num("profile_visits"),
                follows=num("follows"),
                attendance=num("attendance"),
                tickets=num("tickets"),
                comment_text=str(r.get("comment_text") or ""),
            )
            db.add(item)
            db.flush()
            sync_tag_links(db, item, json.loads(item.tags_json))
            execute_content_rules(db, tid, item, "content.import")
            added.append(item)
        except (
            json.JSONDecodeError,
            KeyError,
            SQLAlchemyError,
            TypeError,
            ValueError,
        ) as e:
            warnings.append({"row": i, "message": str(e)})
    audit(db, tid, "import_csv", "content", detail=f"导入 {len(added)} 条")
    db.commit()
    cfg = get_config(db, tid)
    return {
        "imported": len(added),
        "warnings": warnings,
        "items": [serialize_content(x, cfg) for x in added],
    }


def classify(title: str) -> str:
    if re.search(r"后台|排练|准备|幕后|日常", title):
        return "幕后记录"
    if re.search(r"技艺|练习|身段|技巧|拆解", title):
        return "技艺展示"
    if re.search(r"科普|认识|知识|入门|语言", title):
        return "艺术科普"
    return "演出宣传"


@app.get("/api/tags")
def list_tags(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    return [
        {
            "id": t.id,
            "name": t.name,
            "color": t.color,
            "count": db.query(ContentTag)
            .filter(ContentTag.tag_id == t.id)
            .count(),
        }
        for t in db.scalars(
            select(Tag).where(Tag.team_id == tid).order_by(Tag.name)
        ).all()
    ]


@app.post("/api/tags")
def create_tag(
    data: dict[str, str],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    name = data.get("name", "").strip()
    if not name:
        raise HTTPException(422, "标签名称不能为空")
    existing = db.scalar(
        select(Tag).where(Tag.team_id == tid, Tag.name == name)
    )
    if existing:
        return {
            "id": existing.id,
            "name": existing.name,
            "color": existing.color,
        }
    t = Tag(team_id=tid, name=name, color=data.get("color", "#647383"))
    db.add(t)
    db.flush()
    audit(db, tid, "create", "tag", str(t.id), name)
    db.commit()
    return {"id": t.id, "name": t.name, "color": t.color}


@app.delete("/api/tags/{tag_id}")
def delete_tag(
    tag_id: int,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    t = db.scalar(select(Tag).where(Tag.id == tag_id, Tag.team_id == tid))
    if not t:
        raise HTTPException(404, "标签不存在")
    db.delete(t)
    db.commit()
    return {"deleted": True}


def overview_data(db: Session, tid: int) -> dict[str, Any]:
    team_or_404(db, tid)
    records = [
        c
        for c in all_contents(db, tid)
        if normalize_content_status(c.lifecycle) == "已发布"
    ]
    cfg = get_config(db, tid)
    n = len(records)
    good = sorted(records, key=lambda c: score_content(c, cfg), reverse=True)
    return {
        "total_posts": n,
        "total_assets": n,
        # Kept for existing clients; unpublished assets are backlog items, not
        # overview statistics.
        "unpublished_count": 0,
        "total_views": sum(c.views for c in records),
        "total_interactions": sum(c.total_interactions for c in records),
        "avg_completion": round(
            statistics.mean([c.completion for c in records]), 1
        )
        if records
        else 0,
        "avg_interaction_rate": round(
            statistics.mean([interaction_rate(c) for c in records]), 2
        )
        if records
        else 0,
        "platform_count": len(set(c.platform for c in records)),
        "sample_count": n,
        "min_sample": cfg.min_sample,
        "confidence_note": sample_notice(n, cfg.min_sample),
        "top_contents": [serialize_content(c, cfg) for c in good[:5]],
        "needs_work": [
            serialize_content(c, cfg) for c in list(reversed(good[-5:]))
        ],
        "lifecycle_counts": dict(
            Counter(normalize_content_status(c.lifecycle) for c in records)
        ),
        "team_name": team_or_404(db, tid).name,
    }


@app.get("/api/stats/overview")
def stats_overview(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    return overview_data(db, tid)


@app.get("/api/stats/trend")
def stats_trend(
    days: int = Query(default=90, ge=7, le=730),
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
):
    since = datetime.now() - timedelta(days=days)
    records = [
        c for c in published_contents(db, tid) if c.published_at >= since
    ]
    return {
        "days": days,
        "items": [
            {
                "date": c.published_at.date().isoformat(),
                "title": c.title,
                "platform": c.platform,
                "views": c.views,
                "interactions": c.total_interactions,
                "completion": c.completion,
                "sample_count": 1,
                "confidence_note": "单条记录",
            }
            for c in records
        ],
    }


@app.get("/api/stats/platform")
def stats_platform(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    records = published_contents(db, tid)
    cfg = get_config(db, tid)
    items = []
    for x in group_analysis(records, "platform", cfg.min_sample, cfg):
        group = [r for r in records if r.platform == x["name"]]
        items.append(
            {
                **x,
                "total_views": sum(c.views for c in group),
                "total_interactions": sum(c.total_interactions for c in group),
            }
        )
    return {
        "items": items,
        "min_sample": cfg.min_sample,
        "comparison_note": "总量显示团队录入事实；均值需达到样本门槛。平台统计口径可能不同，跨平台比较只作粗略参考。",
    }


@app.get("/api/stats/category")
def stats_category(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    records = published_contents(db, tid)
    cfg = get_config(db, tid)
    return {
        "items": group_analysis(records, "category", cfg.min_sample, cfg),
        "min_sample": cfg.min_sample,
        "score_formula": "综合表现分 = 看完比例 × 完播权重 + 互动比例 × 互动权重",
        "weights": {
            "completion": cfg.score_completion_weight,
            "interaction": cfg.score_interaction_weight,
            "by_platform": json.loads(cfg.platform_weights_json or "{}"),
        },
        "benchmark_source": "基于自身历史数据",
    }


@app.get("/api/stats/heatmap")
def stats_heatmap(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    records = published_contents(db, tid)
    cfg = get_config(db, tid)
    bins = []
    for weekday in range(7):
        for slot in ["上午", "午间", "下午", "晚间", "夜间"]:
            group = [
                c
                for c in records
                if c.published_at.weekday() == weekday
                and time_label(c.published_at.hour) == slot
            ]
            n = len(group)
            bins.append(
                {
                    "weekday": weekday,
                    "day": day_label(weekday),
                    "time_slot": slot,
                    "sample_count": n,
                    "completion": round(
                        statistics.mean(c.completion for c in group), 1
                    )
                    if n >= cfg.min_sample
                    else None,
                    "interaction_rate": round(
                        statistics.mean(interaction_rate(c) for c in group), 2
                    )
                    if n >= cfg.min_sample
                    else None,
                    "confidence_note": sample_notice(n, cfg.min_sample),
                }
            )
    return {"items": bins, "min_sample": cfg.min_sample}


@app.get("/api/stats/adoption")
def stats_adoption(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    rows = published_contents(db, tid)
    groups = {}
    for key in (True, False):
        group = [c for c in rows if c.adopted == key]
        groups["adopted" if key else "not_adopted"] = {
            "sample_count": len(group),
            "avg_completion": round(
                statistics.mean(c.completion for c in group), 1
            )
            if group
            else None,
            "avg_interaction_rate": round(
                statistics.mean(interaction_rate(c) for c in group), 2
            )
            if group
            else None,
        }
    a, b = groups["adopted"], groups["not_adopted"]
    return {
        **groups,
        "completion_difference": round(
            float(a["avg_completion"]) - float(b["avg_completion"]), 1
        )
        if a["avg_completion"] is not None and b["avg_completion"] is not None
        else None,
        "interaction_difference": round(
            float(a["avg_interaction_rate"]) - float(b["avg_interaction_rate"]),
            2,
        )
        if a["avg_interaction_rate"] is not None
        and b["avg_interaction_rate"] is not None
        else None,
        "confidence_note": sample_notice(
            int(a["sample_count"]) + int(b["sample_count"]),
            get_config(db, tid).min_sample,
        ),
    }


@app.get("/api/stats/funnel")
def stats_funnel(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    rows = published_contents(db, tid)
    return {
        "views": sum(x.views for x in rows),
        "profile_visits": sum(x.profile_visits for x in rows),
        "follows": sum(x.follows for x in rows),
        "attendance": sum(x.attendance for x in rows),
        "tickets": sum(x.tickets for x in rows),
        "sample_count": len(rows),
        "confidence_note": sample_notice(
            len(rows), get_config(db, tid).min_sample
        ),
    }


@app.get("/api/stats/diagnostics")
def stats_diagnostics(
    db: Session = Depends(get_db), tid: int = Depends(team_id)
):
    rows = published_contents(db, tid)
    cfg = get_config(db, tid)
    notices = []
    if rows:
        current_month = datetime.now().strftime("%Y-%m")
        month_count = sum(
            1 for x in rows if x.published_at.strftime("%Y-%m") == current_month
        )
        monthly = Counter(x.published_at.strftime("%Y-%m") for x in rows)
        history = statistics.mean(monthly.values()) if monthly else 0
        if month_count < history * 0.6:
            notices.append(
                {
                    "type": "cadence",
                    "level": "info",
                    "message": f"本月已记录 {month_count} 条内容，低于你的历史月均 {history:.1f} 条。",
                }
            )
        scores = [score_content(x, cfg) for x in rows]
        median = statistics.median(scores)
        for x, s in zip(rows, scores):
            if s > median + 25:
                notices.append(
                    {
                        "type": "spike",
                        "level": "good",
                        "content_id": x.id,
                        "message": f"「{x.title}」表现高于团队历史中位数，建议复盘它的开场与内容节奏。",
                    }
                )
            elif s < median - 25:
                notices.append(
                    {
                        "type": "decline",
                        "level": "watch",
                        "content_id": x.id,
                        "message": f"「{x.title}」表现明显低于团队历史中位数，可检查主题与发布时段。",
                    }
                )
    return {"items": notices, "sample_count": len(rows)}


@app.get("/api/benchmark")
def get_benchmarks(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    records = published_contents(db, tid)
    cfg = get_config(db, tid)
    result = []
    overrides = {
        x.category: x
        for x in db.scalars(
            select(Benchmark).where(
                Benchmark.team_id == tid, Benchmark.source_type == "manual"
            )
        ).all()
    }
    grouped = defaultdict(list)
    for r in records:
        grouped[r.category].append(r)
    for category, items in grouped.items():
        override = overrides.get(category)
        result.append(
            {
                "category": category,
                "sample_count": len(items),
                "completion_median": override.completion_median
                if override
                else (
                    round(statistics.median(x.completion for x in items), 1)
                    if len(items) >= cfg.min_sample
                    else None
                ),
                "interaction_median": override.interaction_median
                if override
                else (
                    round(
                        statistics.median(interaction_rate(x) for x in items), 2
                    )
                    if len(items) >= cfg.min_sample
                    else None
                ),
                "source_type": "manual" if override else "self_history",
                "source_label": "团队手动调整"
                if override
                else "基于自身历史数据",
            }
        )
    return result


@app.put("/api/benchmark")
def put_benchmark(
    data: dict[str, Any],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    category = data.get("category")
    row = db.scalar(
        select(Benchmark).where(
            Benchmark.team_id == tid, Benchmark.category == category
        )
    )
    if not row:
        row = Benchmark(
            team_id=tid, category=category, source_type="self_history"
        )
        db.add(row)
    row.completion_median = float(
        data.get("completion_median", row.completion_median)
    )
    row.interaction_median = float(
        data.get("interaction_median", row.interaction_median)
    )
    row.source_type = "manual"
    db.commit()
    return {
        "category": row.category,
        "completion_median": row.completion_median,
        "interaction_median": row.interaction_median,
        "source_type": "manual",
        "source_label": "团队手动调整",
    }


@app.get("/api/settings")
def get_settings(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    config = get_config(db, tid)
    return {
        "id": config.id,
        "name": config.name,
        "base_url": config.base_url,
        "has_key": bool(config.api_key),
        "key_hint": "••••" + config.api_key[-4:]
        if len(config.api_key) > 4
        else ("已设置" if config.api_key else ""),
        "model": config.model,
        "temperature": config.temperature,
        "request_mode": config.request_mode,
        "auth_type": config.auth_type,
        "enabled": config.enabled,
        "fallback_enabled": config.fallback_enabled,
        "score_completion_weight": config.score_completion_weight,
        "score_interaction_weight": config.score_interaction_weight,
        "min_sample": config.min_sample,
        "platform_weights": json.loads(config.platform_weights_json or "{}"),
        "configured": bool(config.base_url and config.model and config.api_key),
    }


@app.put("/api/settings")
def put_settings(
    data: AIConfigIn,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    config = get_config(db, tid)
    values = data.model_dump()
    for k, v in values.items():
        if k == "api_key":
            if v:
                config.api_key = v
        elif k == "platform_weights":
            config.platform_weights_json = json.dumps(v)
        else:
            setattr(config, k, v)
    db.commit()
    audit(db, tid, "update", "settings", str(config.id), "更新模型和分析设置")
    db.commit()
    return get_settings(db, tid)


@app.get("/api/ai/configs")
def list_ai_configs(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    rows = db.scalars(
        select(AIConfig)
        .where(AIConfig.team_id == tid)
        .order_by(AIConfig.updated_at.desc())
    ).all()
    return [
        {
            "id": x.id,
            "name": x.name,
            "model": x.model,
            "base_url": x.base_url,
            "enabled": x.enabled,
            "configured": bool(x.base_url and x.model and x.api_key),
            "key_hint": "••••" + x.api_key[-4:]
            if len(x.api_key) > 4
            else ("已设置" if x.api_key else ""),
        }
        for x in rows
    ]


@app.post("/api/ai/configs")
def create_ai_config(
    data: AIConfigIn,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    values = data.model_dump()
    key = values.pop("api_key", "")
    weights = values.pop("platform_weights", {})
    values["platform_weights_json"] = json.dumps(weights, ensure_ascii=False)
    values["team_id"] = tid
    values["api_key"] = key
    values["enabled"] = False
    row = AIConfig(**values)
    db.add(row)
    db.flush()
    audit(db, tid, "create", "ai_config", str(row.id), row.name)
    db.commit()
    return {
        "id": row.id,
        "name": row.name,
        "enabled": row.enabled,
        "configured": bool(row.base_url and row.model and row.api_key),
    }


@app.post("/api/ai/configs/{config_id}/activate")
def activate_ai_config(
    config_id: int,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    rows = db.scalars(select(AIConfig).where(AIConfig.team_id == tid)).all()
    target = next((x for x in rows if x.id == config_id), None)
    if not target:
        raise HTTPException(404, "模型配置不存在")
    for row in rows:
        row.enabled = row.id == target.id
    db.commit()
    audit(db, tid, "activate", "ai_config", str(target.id), target.name)
    db.commit()
    return {"id": target.id, "name": target.name, "enabled": True}


@app.delete("/api/ai/configs/{config_id}")
def delete_ai_config(
    config_id: int,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    row = db.scalar(
        select(AIConfig).where(
            AIConfig.id == config_id, AIConfig.team_id == tid
        )
    )
    if not row:
        raise HTTPException(404, "模型配置不存在")
    if row.enabled:
        raise HTTPException(409, "请先切换到其他配置，再删除当前配置")
    db.delete(row)
    db.commit()
    return {"deleted": True}


@app.post("/api/settings/sample-threshold")
def set_sample(
    data: SampleSetting,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    config = get_config(db, tid)
    config.min_sample = data.min_sample
    db.commit()
    return {"min_sample": config.min_sample}


@app.post("/api/ai/test")
async def ai_test(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    config = get_config(db, tid)
    if not (config.base_url and config.api_key and config.model):
        raise HTTPException(400, "请先填写模型地址、模型名称和 API Key")
    start = time.time()
    try:
        async with httpx.AsyncClient(timeout=20) as client:
            resp = await client.post(
                model_endpoint(config),
                headers=model_headers(config),
                json={
                    "model": config.model,
                    "messages": [
                        {"role": "user", "content": "只回复：连接成功"}
                    ],
                    "temperature": 0,
                    "max_tokens": 20,
                },
            )
        if resp.status_code >= 400:
            raise HTTPException(
                resp.status_code,
                f"服务商返回 HTTP {resp.status_code}：{resp.text[:500]}",
            )
        body = resp.json()
        message = (
            body.get("choices", [{}])[0].get("message", {}).get("content", "")
        )
        db.add(
            AICallLog(
                team_id=tid,
                input_summary="连接测试",
                output_summary=str(message)[:500],
                elapsed_ms=int((time.time() - start) * 1000),
                status="ok",
            )
        )
        db.commit()
        return {
            "ok": True,
            "message": "配置已生效",
            "reply": message,
            "elapsed_ms": int((time.time() - start) * 1000),
        }
    except HTTPException as e:
        db.add(
            AICallLog(
                team_id=tid,
                input_summary="连接测试",
                output_summary=str(e.detail),
                elapsed_ms=int((time.time() - start) * 1000),
                status="error",
            )
        )
        db.commit()
        raise
    except (
        IndexError,
        KeyError,
        RuntimeError,
        TypeError,
        ValueError,
        httpx.HTTPError,
        json.JSONDecodeError,
    ) as e:
        db.add(
            AICallLog(
                team_id=tid,
                input_summary="连接测试",
                output_summary=str(e)[:500],
                elapsed_ms=int((time.time() - start) * 1000),
                status="error",
            )
        )
        db.commit()
        raise HTTPException(502, f"模型连接失败：{str(e)[:300]}")


@app.post("/api/ai/recommend")
async def ai_recommend(
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    force_rule: bool = Query(default=False),
):
    del force_rule
    records = published_contents(db, tid)
    drafts = unpublished_contents(db, tid)
    config = get_config(db, tid)
    summary = dataset_summary(records, config)
    start = time.time()
    ruleset = load_rule_set_safe()
    if ruleset is None:
        raise HTTPException(503, "内容规则集缺失或无效，暂时无法生成排期建议")
    recs = build_schedule_recommendations(
        records,
        drafts,
        ruleset,
        datetime.now(),
        config.min_sample,
        db.scalars(
            select(Schedule).where(
                Schedule.team_id == tid,
                Schedule.status == "待发布",
            )
        ).all(),
    )
    source = "ruleset-v0.4"
    source_label = "项目只读内容规则集"
    error = None
    recommendation = Recommendation(
        team_id=tid,
        payload_json=json.dumps(recs, ensure_ascii=False),
        source_model=source,
        source_label=source_label,
        confidence=min(
            (x["confidence"] for x in recs),
            key=lambda x: {"low": 0, "medium": 1, "high": 2}[x],
            default="low",
        ),
        reference_count=len(records),
    )
    db.add(recommendation)
    db.add(
        AICallLog(
            team_id=tid,
            input_summary=json.dumps(summary, ensure_ascii=False)[:4000],
            output_summary=json.dumps(recs, ensure_ascii=False)[:4000],
            elapsed_ms=int((time.time() - start) * 1000),
            status="ok",
        )
    )
    audit(
        db, tid, "generate", "recommendation", recommendation.id, source_label
    )
    db.commit()
    return {
        "id": recommendation.id,
        "generated_at": recommendation.generated_at.isoformat(),
        "recommendations": recs,
        "source": source,
        "source_label": source_label,
        "reference_count": len(records),
        "confidence": recommendation.confidence,
        "confidence_note": sample_notice(len(records), config.min_sample),
        "fallback_reason": error,
        "data_summary": summary,
    }


@app.post("/api/ai/insight")
async def ai_insight(
    data: dict[str, str],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
):
    config = get_config(db, tid)
    records = published_contents(db, tid)
    question = data.get("question", "").strip()
    if not question:
        raise HTTPException(422, "请填写想了解的问题")
    if config.enabled and config.base_url and config.api_key and config.model:
        summary = dataset_summary(records, config)
        try:
            async with httpx.AsyncClient(timeout=30) as client:
                resp = await client.post(
                    model_endpoint(config),
                    headers=model_headers(config),
                    json={
                        "model": config.model,
                        "messages": [
                            {
                                "role": "user",
                                "content": f"只基于以下自有数据回答问题，样本不足需说明，不可编造：{json.dumps(summary, ensure_ascii=False)}\n问题：{question}",
                            }
                        ],
                        "temperature": config.temperature,
                    },
                )
            resp.raise_for_status()
            answer = resp.json()["choices"][0]["message"]["content"]
            return {
                "answer": answer,
                "source": "AI",
                "sample_count": len(records),
            }
        except (
            IndexError,
            KeyError,
            TypeError,
            ValueError,
            httpx.HTTPError,
            json.JSONDecodeError,
        ) as e:
            if not config.fallback_enabled:
                raise HTTPException(502, f"模型调用失败：{str(e)[:300]}")
    return {
        "answer": f"目前有 {len(records)} 条团队记录，平均看完比例 {statistics.mean([r.completion for r in records]):.1f}% 。{sample_notice(len(records), config.min_sample) or '这组数据可作为团队自身的参考。'}建议查看“效果分析”中的内容类型与发布时段对比，再安排小规模尝试。",
        "source": "规则分析（未使用大模型）",
        "sample_count": len(records),
    }


@app.get("/api/recommendations")
def list_recommendations(
    db: Session = Depends(get_db), tid: int = Depends(team_id)
):
    rows = db.scalars(
        select(Recommendation)
        .where(Recommendation.team_id == tid)
        .order_by(Recommendation.generated_at.desc())
        .limit(20)
    ).all()
    return [
        {
            "id": x.id,
            "generated_at": x.generated_at.isoformat(),
            "recommendations": json.loads(x.payload_json),
            "source": x.source_model,
            "source_label": x.source_label,
            "reference_count": x.reference_count,
            "confidence": x.confidence,
            "adopted": x.adopted,
        }
        for x in rows
    ]


@app.get("/api/schedule")
def get_schedule(
    start: date | None = None,
    end: date | None = None,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
):
    stmt = select(Schedule).where(Schedule.team_id == tid)
    if start:
        stmt = stmt.where(Schedule.planned_date >= start)
    if end:
        stmt = stmt.where(Schedule.planned_date <= end)
    rows = db.scalars(
        stmt.order_by(Schedule.planned_date, Schedule.planned_time)
    ).all()
    return [
        {
            "id": x.id,
            "recommendation_id": x.recommendation_id,
            "content_id": x.content_id,
            "planned_date": x.planned_date.isoformat(),
            "planned_time": x.planned_time,
            "title": x.title,
            "category": x.category,
            "platform": x.platform,
            "owner": x.owner,
            "reason": x.reason,
            "status": x.status,
            "prediction_views_low": x.prediction_views_low,
            "prediction_views_high": x.prediction_views_high,
            "actual_completion": x.actual_completion,
        }
        for x in rows
    ]


@app.get("/api/schedule/export")
def export_schedule(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    rows = db.scalars(
        select(Schedule)
        .where(Schedule.team_id == tid)
        .order_by(Schedule.planned_date, Schedule.planned_time)
    ).all()
    out = io.StringIO()
    writer = csv.writer(out)
    writer.writerow(
        [
            "计划日期",
            "计划时间",
            "内容主题",
            "内容类型",
            "平台",
            "负责人",
            "状态",
            "依据说明",
        ]
    )
    for x in rows:
        writer.writerow(
            [
                x.planned_date.isoformat(),
                x.planned_time,
                x.title,
                x.category,
                x.platform,
                x.owner,
                x.status,
                x.reason,
            ]
        )
    return Response(
        content="\ufeff" + out.getvalue(),
        media_type="text/csv; charset=utf-8",
        headers={
            "Content-Disposition": "attachment; filename=arts-smart-schedule.csv"
        },
    )


@app.post("/api/schedule/adopt")
def adopt(
    data: AdoptIn,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    rec = db.scalar(
        select(Recommendation).where(
            Recommendation.id == data.recommendation_id,
            Recommendation.team_id == tid,
        )
    )
    if not rec:
        raise HTTPException(404, "建议记录不存在")
    items = data.items or json.loads(rec.payload_json)
    created = []
    today = date.today()
    weekday_names = {
        "周一": 0,
        "周二": 1,
        "周三": 2,
        "周四": 3,
        "周五": 4,
        "周六": 5,
        "周日": 6,
    }
    next_monday = today + timedelta(days=(7 - today.weekday()) % 7 or 7)
    for i, item in enumerate(items):
        day = item.get("day_of_week", "周三")
        delta = (weekday_names.get(day, 2) - next_monday.weekday()) % 7
        planned_text = item.get("planned_date")
        try:
            planned = (
                date.fromisoformat(planned_text)
                if planned_text
                else next_monday + timedelta(days=delta + 7 * ((i + 1) // 3))
            )
        except (TypeError, ValueError) as exc:
            raise HTTPException(422, "建议计划日期格式无效") from exc
        if planned.weekday() != weekday_names.get(day, planned.weekday()):
            planned = next_monday + timedelta(days=delta + 7 * ((i + 1) // 3))
        time_text = item.get("time_slot", "19:30")
        try:
            datetime.strptime(time_text[:5], "%H:%M")
        except (TypeError, ValueError) as exc:
            raise HTTPException(422, "建议计划时间格式无效") from exc
        target_content_id = item.get("target_content_id")
        if target_content_id:
            target_content = db.scalar(
                select(Content).where(
                    Content.id == target_content_id,
                    Content.team_id == tid,
                )
            )
            if not target_content:
                raise HTTPException(404, "排期目标内容不存在")
            if normalize_content_status(target_content.lifecycle) != "未发布":
                raise HTTPException(409, "排期目标内容已发布，不能重复采纳")
            existing_schedule = db.scalar(
                select(Schedule.id).where(
                    Schedule.team_id == tid,
                    Schedule.content_id == target_content_id,
                    Schedule.status == "待发布",
                )
            )
            if existing_schedule:
                raise HTTPException(409, "该内容已存在待发布排期")
        schedule = Schedule(
            team_id=tid,
            recommendation_id=rec.id,
            content_id=target_content_id,
            planned_date=planned,
            planned_time=time_text[:5],
            title=item.get("title", item.get("content_type", "内容建议")),
            category=item.get("content_type", "艺术科普"),
            platform=item.get("platform", PLATFORMS[0]),
            owner=item.get("owner", ""),
            reason=item.get("reason", ""),
            prediction_views_low=item.get("prediction_views_low"),
            prediction_views_high=item.get("prediction_views_high"),
        )
        db.add(schedule)
        db.flush()
        created.append(schedule)
    rec.adopted = True
    audit(
        db,
        tid,
        "adopt",
        "recommendation",
        rec.id,
        f"采纳 {len(created)} 条建议",
    )
    db.commit()
    return {
        "created": len(created),
        "items": [
            {
                "id": x.id,
                "planned_date": x.planned_date.isoformat(),
                "planned_time": x.planned_time,
                "title": x.title,
                "category": x.category,
                "platform": x.platform,
                "status": x.status,
            }
            for x in created
        ],
    }


@app.patch("/api/schedule/{schedule_id}")
def update_schedule(
    schedule_id: str,
    data: dict[str, Any],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    item = db.scalar(
        select(Schedule).where(
            Schedule.id == schedule_id, Schedule.team_id == tid
        )
    )
    if not item:
        raise HTTPException(404, "排期不存在")
    allowed = {
        "planned_date",
        "planned_time",
        "title",
        "category",
        "platform",
        "owner",
        "status",
    }
    for k, v in data.items():
        if k in allowed:
            setattr(
                item,
                k,
                date.fromisoformat(v)
                if k == "planned_date" and isinstance(v, str)
                else v,
            )
    audit(
        db,
        tid,
        "update",
        "schedule",
        item.id,
        json.dumps(data, ensure_ascii=False),
    )
    db.commit()
    return {
        "id": item.id,
        "planned_date": item.planned_date.isoformat(),
        "planned_time": item.planned_time,
        "title": item.title,
        "status": item.status,
    }


@app.post("/api/schedule/{schedule_id}/publish")
def publish_schedule(
    schedule_id: str,
    data: PublishIn,
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    schedule = db.scalar(
        select(Schedule).where(
            Schedule.id == schedule_id, Schedule.team_id == tid
        )
    )
    if not schedule:
        raise HTTPException(404, "排期不存在")
    item = None
    if schedule.content_id:
        item = db.scalar(
            select(Content).where(
                Content.id == schedule.content_id,
                Content.team_id == tid,
            )
        )
    values = {
        "title": data.title,
        "category": data.category,
        "platform": data.platform,
        "published_at": data.published_at,
        "views": data.views,
        "likes": data.likes,
        "comments": data.comments,
        "saves": data.saves,
        "shares": data.shares,
        "completion": data.completion,
        "total_interactions": (
            data.likes + data.comments + data.saves + data.shares
        ),
        "lifecycle": "已发布",
        "profile_visits": data.profile_visits,
        "follows": data.follows,
        "attendance": data.attendance,
        "tickets": data.tickets,
        "comment_text": data.comment_text,
        "adopted": bool(schedule.recommendation_id),
        "recommendation_id": schedule.recommendation_id,
    }
    if item is None:
        item = Content(team_id=tid, duration=0, source="feedback", **values)
        db.add(item)
    else:
        for field, value in values.items():
            setattr(item, field, value)
    db.flush()
    execute_content_rules(db, tid, item, "content.update")
    schedule.content_id = item.id
    schedule.status = "已复盘"
    schedule.actual_views = data.views
    schedule.actual_likes = data.likes
    schedule.actual_comments = data.comments
    schedule.actual_saves = data.saves
    schedule.actual_shares = data.shares
    schedule.actual_completion = data.completion
    audit(
        db,
        tid,
        "publish_feedback",
        "schedule",
        schedule.id,
        f"实际播放 {data.views}，看完 {data.completion}%",
    )
    db.commit()
    predicted = (
        (schedule.prediction_views_low + schedule.prediction_views_high) / 2
        if schedule.prediction_views_low is not None
        and schedule.prediction_views_high is not None
        else None
    )
    return {
        "content": serialize_content(item, get_config(db, tid)),
        "prediction": {
            "views_midpoint": predicted,
            "actual_views": data.views,
            "views_deviation_percent": round(
                (data.views - predicted) / predicted * 100, 1
            )
            if predicted
            else None,
            "predicted_completion": None,
            "actual_completion": data.completion,
        },
    }


@app.get("/api/schedule/adoption-gains")
def adoption_gains(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    rows = [x for x in published_contents(db, tid) if x.recommendation_id]
    return {
        "items": [
            {
                "content_id": x.id,
                "title": x.title,
                "adopted": x.adopted,
                "completion": x.completion,
                "interaction_rate": round(interaction_rate(x), 2),
                "sample_count": 1,
            }
            for x in rows
        ],
        "comparison": stats_adoption(db, tid),
    }


@app.get("/api/report/export")
def report_export(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    contents = published_contents(db, tid)
    config = get_config(db, tid)
    pdf = report_pdf(db, tid, contents, config)
    return Response(
        content=pdf,
        media_type="application/pdf",
        headers={
            "Content-Disposition": "attachment; filename*=UTF-8''Artsmart-report.pdf"
        },
    )


@app.get("/api/export/anonymized")
def export_anonymized(
    db: Session = Depends(get_db), tid: int = Depends(team_id)
):
    rows = all_contents(db, tid)
    out = io.StringIO()
    writer = csv.writer(out)
    writer.writerow(
        [
            "发布时间",
            "内容类型",
            "平台",
            "状态标签",
            "播放量",
            "点赞量",
            "评论量",
            "收藏量",
            "转发量",
            "看完比例",
            "综合表现分",
        ]
    )
    config = get_config(db, tid)
    for r in rows:
        writer.writerow(
            [
                r.published_at.isoformat(),
                r.category,
                r.platform,
                normalize_content_status(r.lifecycle),
                r.views,
                r.likes,
                r.comments,
                r.saves,
                r.shares,
                r.completion,
                score_content(r, config),
            ]
        )
    return Response(
        content="\ufeff" + out.getvalue(),
        media_type="text/csv; charset=utf-8",
        headers={
            "Content-Disposition": "attachment; filename=anonymized-content.csv"
        },
    )


@app.get("/api/export/contents")
def export_contents_csv(
    db: Session = Depends(get_db), tid: int = Depends(team_id)
):
    rows = all_contents(db, tid)
    out = io.StringIO()
    writer = csv.writer(out)
    writer.writerow(
        [
            "平台",
            "发布时间",
            "标题",
            "内容类型",
            "时长",
            "播放量",
            "点赞量",
            "评论量",
            "收藏量",
            "转发量",
            "看完比例",
            "状态标签",
            "标签",
            "素材说明",
            "责任人",
            "备注",
            "主页访问",
            "新增关注",
            "到场人数",
            "售票数",
            "评论摘录",
        ]
    )
    for item in rows:
        tags = json.loads(item.tags_json or "[]")
        writer.writerow(
            [
                item.platform,
                item.published_at.isoformat(timespec="minutes"),
                item.title,
                item.category,
                item.duration,
                item.views,
                item.likes,
                item.comments,
                item.saves,
                item.shares,
                item.completion,
                normalize_content_status(item.lifecycle),
                "|".join(tags),
                item.materials,
                item.owner,
                item.notes,
                item.profile_visits,
                item.follows,
                item.attendance,
                item.tickets,
                item.comment_text,
            ]
        )
    return Response(
        content="\ufeff" + out.getvalue(),
        media_type="text/csv; charset=utf-8",
        headers={
            "Content-Disposition": "attachment; filename=arts-smart-content.csv"
        },
    )


@app.get("/api/audit")
def get_audit(
    limit: int = Query(default=100, ge=1, le=500),
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(role),
):
    rows = db.scalars(
        select(AuditLog)
        .where(AuditLog.team_id == tid)
        .order_by(AuditLog.created_at.desc())
        .limit(limit)
    ).all()
    return [
        {
            "actor": x.actor,
            "action": x.action,
            "entity_type": x.entity_type,
            "entity_id": x.entity_id,
            "created_at": x.created_at.isoformat(),
            "detail": x.detail,
        }
        for x in rows
    ]


@app.get("/api/hypotheses")
def list_hypotheses(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    return [
        {
            "id": x.id,
            "statement": x.statement,
            "result": x.result,
            "status": x.status,
            "created_at": x.created_at.isoformat(),
        }
        for x in db.scalars(
            select(Hypothesis)
            .where(Hypothesis.team_id == tid)
            .order_by(Hypothesis.created_at.desc())
        ).all()
    ]


@app.post("/api/hypotheses")
def create_hypothesis(
    data: dict[str, str],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    statement = data.get("statement", "").strip()
    if not statement:
        raise HTTPException(422, "请填写待验证的假设")
    item = Hypothesis(
        team_id=tid,
        statement=statement,
        result=data.get("result", ""),
        status=data.get("status", "待验证"),
    )
    db.add(item)
    db.commit()
    return {
        "id": item.id,
        "statement": item.statement,
        "status": item.status,
        "created_at": item.created_at.isoformat(),
    }


@app.patch("/api/hypotheses/{hypothesis_id}")
def update_hypothesis(
    hypothesis_id: str,
    data: dict[str, str],
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
    _role: str = Depends(require_editor),
):
    item = db.scalar(
        select(Hypothesis).where(
            Hypothesis.id == hypothesis_id, Hypothesis.team_id == tid
        )
    )
    if not item:
        raise HTTPException(404, "假设记录不存在")
    for k in ("statement", "result", "status"):
        if k in data:
            setattr(item, k, data[k])
    db.commit()
    return {
        "id": item.id,
        "statement": item.statement,
        "result": item.result,
        "status": item.status,
    }


@app.get("/api/handover")
def handover(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    data = overview_data(db, tid)
    records = published_contents(db, tid)
    cfg = get_config(db, tid)
    schedule = [
        {
            "planned_date": x.planned_date.isoformat(),
            "planned_time": x.planned_time,
            "title": x.title,
            "category": x.category,
            "platform": x.platform,
            "status": x.status,
        }
        for x in db.scalars(
            select(Schedule)
            .where(Schedule.team_id == tid)
            .order_by(Schedule.planned_date)
        ).all()
    ]
    todo_items = [
        {"item": "检查已采纳排期并回填实际数据", "done": False},
        {"item": "复盘表现较好的内容开场与节奏", "done": False},
    ]
    draft_count = len(unpublished_contents(db, tid))
    if draft_count:
        todo_items.insert(
            0,
            {
                "item": f"为 {draft_count} 条未发布内容确认负责人和发布时间",
                "done": False,
            },
        )
    return {
        "team_name": data["team_name"],
        "generated_at": datetime.now().isoformat(),
        "top_contents": data["top_contents"],
        "category_findings": group_analysis(
            records, "category", cfg.min_sample, cfg
        ),
        "schedules": schedule,
        "cadence_tip": "建议结合团队实际档期安排发布节奏。",
        "todo_items": todo_items,
    }


@app.get("/api/handover/export")
def handover_export(
    format: str = "markdown",
    db: Session = Depends(get_db),
    tid: int = Depends(team_id),
):
    data = handover(db, tid=tid)
    lines = [
        f"# {data['team_name']} 运营交接包",
        "",
        f"生成时间：{data['generated_at']}",
        "",
        "## 历史优质内容",
    ]
    for x in data["top_contents"]:
        lines.append(
            f"- {x['title']}｜{x['category']}｜{x['platform']}｜{x['score']} 分"
        )
    lines += ["", "## 类型复盘"]
    for x in data["category_findings"]:
        lines.append(
            f"- {x['name']}（样本 {x['sample_count']} 条）："
            + (
                f"平均看完 {x['completion']}%"
                if x["completion"] is not None
                else "样本不足"
            )
        )
    lines += ["", "## 待办"] + [
        f"- [ ] {x['item']}" for x in data["todo_items"]
    ]
    text = "\n".join(lines)
    if format == "pdf":
        return Response(
            report_pdf(
                db, tid, published_contents(db, tid), get_config(db, tid)
            ),
            media_type="application/pdf",
            headers={
                "Content-Disposition": "attachment; filename=handover.pdf"
            },
        )
    return Response(
        "\ufeff" + text,
        media_type="text/markdown; charset=utf-8",
        headers={"Content-Disposition": "attachment; filename=handover.md"},
    )


@app.get("/api")
def api_root():
    return {"name": "艺数智策 API", "docs": "/docs", "health": "/api/health"}


app.mount(
    "/", StaticFiles(directory=ROOT / "frontend", html=True), name="frontend"
)
