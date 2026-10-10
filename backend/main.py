from __future__ import annotations

import csv
import base64
import hashlib
import hmac
import io
import json
import os
import re
import secrets
import statistics
import time
import uuid
from collections import Counter, defaultdict
from datetime import date, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pandas as pd
from dotenv import load_dotenv
from fastapi import Depends, FastAPI, File, Header, HTTPException, Query, UploadFile
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, Response
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field
from sqlalchemy import Boolean, DateTime, Float, ForeignKey, Integer, String, Text, create_engine, select
from sqlalchemy.orm import DeclarativeBase, Mapped, Session, mapped_column, relationship, sessionmaker

ROOT = Path(__file__).resolve().parents[1]
load_dotenv(ROOT / ".env")
DATA_DIR = ROOT / "data"
DATA_DIR.mkdir(exist_ok=True)
APP_SECRET = os.getenv("APP_SECRET", "local-development-secret-change-before-network-use")
DATABASE_URL = os.getenv("DATABASE_URL", "sqlite:///./data/arts_smart.db")
engine = create_engine(DATABASE_URL, connect_args={"check_same_thread": False} if DATABASE_URL.startswith("sqlite") else {}, future=True)
SessionLocal = sessionmaker(bind=engine, autoflush=False, expire_on_commit=False)
MIN_SAMPLE_DEFAULT = max(2, int(os.getenv("MIN_SAMPLE_SIZE", "5")))
CATEGORIES = ["演出宣传", "技艺展示", "幕后记录", "艺术科普"]
PLATFORMS = ["短视频平台A", "短视频平台B", "内容社区C", "视频号"]


class Base(DeclarativeBase):
    pass


class Team(Base):
    __tablename__ = "team"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    name: Mapped[str] = mapped_column(String(100), default="示例艺术团队")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)


class User(Base):
    __tablename__ = "user_account"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    email: Mapped[str] = mapped_column(String(254), unique=True, index=True)
    name: Mapped[str] = mapped_column(String(100))
    role_name: Mapped[str] = mapped_column(String(20), default="editor")
    password_hash: Mapped[str] = mapped_column(String(256))
    created_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)


class UserSession(Base):
    __tablename__ = "user_session"
    id: Mapped[str] = mapped_column(String(36), primary_key=True)
    user_id: Mapped[int] = mapped_column(ForeignKey("user_account.id"), index=True)
    expires_at: Mapped[datetime] = mapped_column(DateTime)
    revoked: Mapped[bool] = mapped_column(Boolean, default=False)


class Content(Base):
    __tablename__ = "content"
    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=lambda: str(uuid.uuid4()))
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    title: Mapped[str] = mapped_column(String(300))
    category: Mapped[str] = mapped_column(String(40), default="演出宣传")
    platform: Mapped[str] = mapped_column(String(100))
    published_at: Mapped[datetime] = mapped_column(DateTime, index=True)
    duration: Mapped[int] = mapped_column(Integer, default=0)
    views: Mapped[int] = mapped_column(Integer, default=0)
    likes: Mapped[int] = mapped_column(Integer, default=0)
    comments: Mapped[int] = mapped_column(Integer, default=0)
    saves: Mapped[int] = mapped_column(Integer, default=0)
    shares: Mapped[int] = mapped_column(Integer, default=0)
    completion: Mapped[float] = mapped_column(Float, default=0)
    total_interactions: Mapped[int] = mapped_column(Integer, default=0)
    tags_json: Mapped[str] = mapped_column(Text, default="[]")
    source: Mapped[str] = mapped_column(String(30), default="manual")
    lifecycle: Mapped[str] = mapped_column(String(20), default="已发布")
    materials: Mapped[str] = mapped_column(Text, default="")
    owner: Mapped[str] = mapped_column(String(100), default="")
    notes: Mapped[str] = mapped_column(Text, default="")
    profile_visits: Mapped[int] = mapped_column(Integer, default=0)
    follows: Mapped[int] = mapped_column(Integer, default=0)
    attendance: Mapped[int] = mapped_column(Integer, default=0)
    tickets: Mapped[int] = mapped_column(Integer, default=0)
    comment_text: Mapped[str] = mapped_column(Text, default="")
    adopted: Mapped[bool] = mapped_column(Boolean, default=False)
    recommendation_id: Mapped[str | None] = mapped_column(ForeignKey("recommendation.id"), nullable=True)
    created_by: Mapped[str] = mapped_column(String(100), default="团队运营者")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)
    updated_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow, onupdate=datetime.utcnow)


class Tag(Base):
    __tablename__ = "tag"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    name: Mapped[str] = mapped_column(String(50))
    color: Mapped[str] = mapped_column(String(20), default="#647383")


class ContentTag(Base):
    __tablename__ = "content_tag"
    content_id: Mapped[str] = mapped_column(ForeignKey("content.id", ondelete="CASCADE"), primary_key=True)
    tag_id: Mapped[int] = mapped_column(ForeignKey("tag.id", ondelete="CASCADE"), primary_key=True)


class Recommendation(Base):
    __tablename__ = "recommendation"
    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=lambda: str(uuid.uuid4()))
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    generated_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)
    payload_json: Mapped[str] = mapped_column(Text)
    source_model: Mapped[str] = mapped_column(String(100), default="rule")
    source_label: Mapped[str] = mapped_column(String(120), default="规则分析（未使用大模型）")
    confidence: Mapped[str] = mapped_column(String(20), default="low")
    reference_count: Mapped[int] = mapped_column(Integer, default=0)
    adopted: Mapped[bool] = mapped_column(Boolean, default=False)


class Schedule(Base):
    __tablename__ = "schedule"
    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=lambda: str(uuid.uuid4()))
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    recommendation_id: Mapped[str | None] = mapped_column(ForeignKey("recommendation.id"), nullable=True)
    content_id: Mapped[str | None] = mapped_column(ForeignKey("content.id"), nullable=True)
    planned_date: Mapped[date] = mapped_column()
    planned_time: Mapped[str] = mapped_column(String(10), default="19:30")
    title: Mapped[str] = mapped_column(String(300))
    category: Mapped[str] = mapped_column(String(40))
    platform: Mapped[str] = mapped_column(String(100))
    reason: Mapped[str] = mapped_column(Text, default="")
    status: Mapped[str] = mapped_column(String(20), default="待发布")
    actual_views: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_likes: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_comments: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_saves: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_shares: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_completion: Mapped[float | None] = mapped_column(Float, nullable=True)
    prediction_views_low: Mapped[int | None] = mapped_column(Integer, nullable=True)
    prediction_views_high: Mapped[int | None] = mapped_column(Integer, nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)


class AIConfig(Base):
    __tablename__ = "ai_config"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    name: Mapped[str] = mapped_column(String(100), default="默认模型")
    base_url: Mapped[str] = mapped_column(Text, default="")
    api_key: Mapped[str] = mapped_column(Text, default="")
    model: Mapped[str] = mapped_column(String(150), default="")
    temperature: Mapped[float] = mapped_column(Float, default=0.4)
    request_mode: Mapped[str] = mapped_column(String(20), default="base")
    auth_type: Mapped[str] = mapped_column(String(30), default="bearer")
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    fallback_enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    score_completion_weight: Mapped[float] = mapped_column(Float, default=0.68)
    score_interaction_weight: Mapped[float] = mapped_column(Float, default=1.15)
    min_sample: Mapped[int] = mapped_column(Integer, default=MIN_SAMPLE_DEFAULT)
    platform_weights_json: Mapped[str] = mapped_column(Text, default="{}")
    updated_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow, onupdate=datetime.utcnow)


class AICallLog(Base):
    __tablename__ = "ai_call_log"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)
    input_summary: Mapped[str] = mapped_column(Text, default="")
    output_summary: Mapped[str] = mapped_column(Text, default="")
    elapsed_ms: Mapped[int] = mapped_column(Integer, default=0)
    status: Mapped[str] = mapped_column(String(30), default="ok")


class Benchmark(Base):
    __tablename__ = "benchmark"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    category: Mapped[str] = mapped_column(String(40))
    platform: Mapped[str] = mapped_column(String(100), default="")
    completion_median: Mapped[float] = mapped_column(Float, default=0)
    interaction_median: Mapped[float] = mapped_column(Float, default=0)
    source_type: Mapped[str] = mapped_column(String(30), default="self_history")


class AuditLog(Base):
    __tablename__ = "audit_log"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    actor: Mapped[str] = mapped_column(String(100), default="团队运营者")
    action: Mapped[str] = mapped_column(String(60))
    entity_type: Mapped[str] = mapped_column(String(50))
    entity_id: Mapped[str] = mapped_column(String(80), default="")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)
    detail: Mapped[str] = mapped_column(Text, default="")


class Hypothesis(Base):
    __tablename__ = "hypothesis"
    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=lambda: str(uuid.uuid4()))
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    statement: Mapped[str] = mapped_column(Text)
    result: Mapped[str] = mapped_column(Text, default="")
    status: Mapped[str] = mapped_column(String(20), default="待验证")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=datetime.utcnow)


class ContentIn(BaseModel):
    title: str = Field(min_length=1, max_length=300)
    category: str = "演出宣传"
    platform: str = Field(min_length=1, max_length=100)
    published_at: datetime
    duration: int = Field(default=0, ge=0)
    views: int = Field(default=0, ge=0)
    likes: int = Field(default=0, ge=0)
    comments: int = Field(default=0, ge=0)
    saves: int = Field(default=0, ge=0)
    shares: int = Field(default=0, ge=0)
    completion: float = Field(default=0, ge=0, le=100)
    tags: list[str] = []
    source: str = "manual"
    lifecycle: str = "已发布"
    materials: str = ""
    owner: str = ""
    notes: str = ""
    profile_visits: int = Field(default=0, ge=0)
    follows: int = Field(default=0, ge=0)
    attendance: int = Field(default=0, ge=0)
    tickets: int = Field(default=0, ge=0)
    comment_text: str = ""
    adopted: bool = False
    recommendation_id: str | None = None


class AIConfigIn(BaseModel):
    name: str = "默认模型"
    base_url: str = ""
    api_key: str = ""
    model: str = ""
    temperature: float = Field(default=0.4, ge=0, le=2)
    request_mode: str = "base"
    auth_type: str = "bearer"
    enabled: bool = True
    fallback_enabled: bool = True
    score_completion_weight: float = Field(default=0.68, ge=0, le=1)
    score_interaction_weight: float = Field(default=1.15, ge=0, le=3)
    min_sample: int = Field(default=MIN_SAMPLE_DEFAULT, ge=2, le=1000)
    platform_weights: dict[str, dict[str, float]] = {}


class TeamIn(BaseModel):
    name: str = Field(min_length=1, max_length=100)


class AdoptIn(BaseModel):
    recommendation_id: str
    items: list[dict[str, Any]]


class PublishIn(BaseModel):
    title: str
    category: str
    platform: str
    published_at: datetime
    views: int = Field(ge=0)
    likes: int = Field(ge=0)
    comments: int = Field(default=0, ge=0)
    saves: int = Field(default=0, ge=0)
    shares: int = Field(default=0, ge=0)
    completion: float = Field(ge=0, le=100)
    profile_visits: int = Field(default=0, ge=0)
    follows: int = Field(default=0, ge=0)
    attendance: int = Field(default=0, ge=0)
    tickets: int = Field(default=0, ge=0)
    comment_text: str = ""


class SampleSetting(BaseModel):
    min_sample: int = Field(ge=2, le=1000)


class AuthSetup(BaseModel):
    email: str = Field(min_length=5, max_length=254)
    password: str = Field(min_length=10, max_length=200)
    name: str = Field(min_length=1, max_length=100)
    team_name: str = Field(min_length=1, max_length=100)


class LoginIn(BaseModel):
    email: str
    password: str


class UserInvite(BaseModel):
    email: str
    password: str = Field(min_length=10, max_length=200)
    name: str
    role: str = "editor"


def get_db():
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()


def password_hash(password: str, salt: bytes | None = None) -> str:
    salt = salt or secrets.token_bytes(16)
    digest = hashlib.pbkdf2_hmac("sha256", password.encode(), salt, 310_000)
    return f"pbkdf2_sha256${base64.urlsafe_b64encode(salt).decode()}${base64.urlsafe_b64encode(digest).decode()}"


def verify_password(password: str, encoded: str) -> bool:
    try:
        algorithm, salt, expected = encoded.split("$")
        if algorithm != "pbkdf2_sha256": return False
        return hmac.compare_digest(password_hash(password, base64.urlsafe_b64decode(salt.encode())).split("$")[-1], expected)
    except Exception: return False


def issue_token(db: Session, user: User) -> str:
    sid = str(uuid.uuid4()); expires = datetime.utcnow() + timedelta(days=14)
    db.add(UserSession(id=sid, user_id=user.id, expires_at=expires)); db.flush()
    payload = base64.urlsafe_b64encode(json.dumps({"sid":sid,"sub":user.id,"tid":user.team_id,"exp":int(expires.timestamp())},separators=(",",":")).encode()).decode().rstrip("=")
    signature = hmac.new(APP_SECRET.encode(), payload.encode(), hashlib.sha256).hexdigest()
    return payload + "." + signature


def auth_claims(authorization: str = Header(default=""), db: Session = Depends(get_db)) -> dict[str, Any]:
    if not authorization.startswith("Bearer "): raise HTTPException(401,"请先登录后继续")
    token=authorization[7:].strip()
    try:
        payload,signature=token.split(".",1);expected=hmac.new(APP_SECRET.encode(),payload.encode(),hashlib.sha256).hexdigest()
        if not hmac.compare_digest(signature,expected):raise ValueError("签名无效")
        decoded=json.loads(base64.urlsafe_b64decode(payload+"="*((4-len(payload)%4)%4)))
        if int(decoded["exp"])<int(datetime.utcnow().timestamp()):raise ValueError("登录已过期")
        session=db.get(UserSession,decoded["sid"]);user=db.get(User,decoded["sub"])
        if not session or session.revoked or not user or session.expires_at<datetime.utcnow():raise ValueError("登录状态已失效")
        return {"user_id":user.id,"team_id":user.team_id,"role":user.role_name,"email":user.email,"name":user.name,"session_id":session.id}
    except HTTPException:raise
    except Exception:raise HTTPException(401,"登录状态无效或已过期")


def team_id(claims: dict[str,Any] = Depends(auth_claims), x_team_id: int | None = Header(default=None, alias="X-Team-Id")) -> int:
    if x_team_id is not None and x_team_id != claims["team_id"]:raise HTTPException(403,"当前账号无权访问该团队")
    return claims["team_id"]


def role(claims: dict[str,Any] = Depends(auth_claims)) -> str:
    return claims["role"]


def require_editor(user_role: str = Depends(role)):
    if user_role == "viewer":
        raise HTTPException(403, "查看者账号不能修改内容")
    return user_role


def team_or_404(db: Session, tid: int) -> Team:
    team = db.get(Team, tid)
    if not team:
        raise HTTPException(404, "团队不存在")
    return team


def get_config(db: Session, tid: int) -> AIConfig:
    config = db.scalar(select(AIConfig).where(AIConfig.team_id == tid, AIConfig.enabled.is_(True)))
    if config:
        return config
    config = AIConfig(team_id=tid, min_sample=MIN_SAMPLE_DEFAULT, api_key=os.getenv("AI_API_KEY", ""), base_url=os.getenv("AI_BASE_URL", ""), model=os.getenv("AI_MODEL", ""))
    db.add(config)
    db.flush()
    return config


def score_content(c: Content, config: AIConfig) -> float:
    interaction_rate = (c.likes + c.comments + c.saves + c.shares) / c.views if c.views else 0
    templates=json.loads(config.platform_weights_json or "{}")
    weights=templates.get(c.platform,{})
    completion_weight=float(weights.get("completion",config.score_completion_weight))
    interaction_weight=float(weights.get("interaction",config.score_interaction_weight))
    return round(min(100, c.completion * completion_weight + min(interaction_rate * 100, 28) * interaction_weight), 1)


def serialize_content(c: Content, config: AIConfig | None = None) -> dict[str, Any]:
    try:
        tags = json.loads(c.tags_json or "[]")
    except Exception:
        tags = []
    return {"id": c.id, "title": c.title, "category": c.category, "platform": c.platform,
            "published_at": c.published_at.isoformat(), "date": c.published_at.isoformat(timespec="minutes"),
            "duration": c.duration, "views": c.views, "likes": c.likes, "comments": c.comments,
            "saves": c.saves, "shares": c.shares, "total_interactions": c.total_interactions,
            "completion": c.completion, "tags": tags, "source": c.source, "lifecycle": c.lifecycle,
            "materials": c.materials, "owner": c.owner, "notes": c.notes, "profile_visits": c.profile_visits,
            "follows": c.follows, "attendance": c.attendance, "tickets": c.tickets, "comment_text": c.comment_text,
            "adopted": c.adopted, "recommendation_id": c.recommendation_id,
            "score": score_content(c, config) if config else None, "created_by": c.created_by,
            "created_at": c.created_at.isoformat()}


def audit(db: Session, tid: int, action: str, entity: str, entity_id: str = "", detail: str = "", actor: str = "团队运营者"):
    db.add(AuditLog(team_id=tid, actor=actor, action=action, entity_type=entity, entity_id=entity_id, detail=detail[:2000]))


def record_from_input(data: ContentIn, tid: int, user: str = "团队运营者") -> Content:
    values = data.model_dump()
    tags = values.pop("tags", [])
    values["tags_json"] = json.dumps(tags, ensure_ascii=False)
    values["total_interactions"] = sum(values.get(k, 0) for k in ("likes", "comments", "saves", "shares"))
    values["team_id"] = tid
    values["created_by"] = user
    return Content(**values)


def sync_tag_links(db: Session, item: Content, names: list[str]):
    db.query(ContentTag).filter(ContentTag.content_id == item.id).delete(synchronize_session=False)
    for name in dict.fromkeys(x.strip() for x in names if x.strip()):
        tag = db.scalar(select(Tag).where(Tag.team_id == item.team_id, Tag.name == name))
        if not tag:
            tag = Tag(team_id=item.team_id, name=name)
            db.add(tag); db.flush()
        db.add(ContentTag(content_id=item.id, tag_id=tag.id))


def all_contents(db: Session, tid: int) -> list[Content]:
    return list(db.scalars(select(Content).where(Content.team_id == tid).order_by(Content.published_at)).all())


def sample_notice(n: int, threshold: int) -> str:
    if n < threshold:
        return "样本不足"
    return "样本较少，仅供参考" if n <= threshold * 2 else ""


def interaction_rate(c: Content) -> float:
    return (c.likes + c.comments + c.saves + c.shares) / c.views * 100 if c.views else 0


def content_frame(records: list[Content], config: AIConfig | None = None) -> pd.DataFrame:
    rows = []
    for c in records:
        rows.append({"id": c.id, "title": c.title, "category": c.category, "platform": c.platform,
                     "published_at": c.published_at, "views": c.views, "completion": c.completion,
                     "interaction_rate": interaction_rate(c), "interactions": c.total_interactions,
                     "weekday": c.published_at.weekday(), "hour": c.published_at.hour,
                     "score": score_content(c,config) if config else round(c.completion * .68 + min(interaction_rate(c), 28) * 1.15, 1),
                     "profile_visits": c.profile_visits, "follows": c.follows,
                     "attendance": c.attendance, "tickets": c.tickets})
    return pd.DataFrame(rows)


def sample_aggregate(records: list[Content], metric: str, threshold: int) -> dict[str, Any]:
    n = len(records)
    return {"sample_count": n, "value": round(float(pd.Series([getattr(c, metric) for c in records]).mean()), 2) if n >= threshold else None,
            "confidence_note": sample_notice(n, threshold)}


def day_label(i: int) -> str:
    return ["周一", "周二", "周三", "周四", "周五", "周六", "周日"][i % 7]


def time_label(hour: int) -> str:
    if 5 <= hour < 11: return "上午"
    if 11 <= hour < 14: return "午间"
    if 14 <= hour < 18: return "下午"
    if 18 <= hour < 22: return "晚间"
    return "夜间"


def group_analysis(records: list[Content], key, threshold: int, config: AIConfig | None = None) -> list[dict[str, Any]]:
    frame = content_frame(records,config)
    if frame.empty:
        return []
    groups = []
    for name, group in frame.groupby(key, dropna=False):
        n = int(len(group))
        adequate = n >= threshold
        groups.append({"name": str(name), "sample_count": n,
                       "views": round(float(group.views.mean()), 1) if adequate else None,
                       "completion": round(float(group.completion.mean()), 1) if adequate else None,
                       "interaction_rate": round(float(group.interaction_rate.mean()), 2) if adequate else None,
                       "score": round(float(group.score.mean()), 1) if adequate else None,
                       "confidence_note": sample_notice(n, threshold)})
    return sorted(groups, key=lambda x: (x["score"] is not None, x["score"] or 0), reverse=True)


def model_endpoint(config: AIConfig) -> str:
    raw = config.base_url.strip().rstrip("/")
    if not raw:
        raise ValueError("尚未填写模型接口地址")
    if config.request_mode == "full":
        return raw
    return raw if raw.endswith("/chat/completions") else raw + "/chat/completions"


def model_headers(config: AIConfig) -> dict[str, str]:
    if config.auth_type == "api-key":
        return {"api-key": config.api_key, "Content-Type": "application/json"}
    if config.auth_type.startswith("custom:"):
        return {config.auth_type.split(":", 1)[1]: config.api_key, "Content-Type": "application/json"}
    return {"Authorization": f"Bearer {config.api_key}", "Content-Type": "application/json"}


def parse_model_json(text: str) -> dict[str, Any]:
    text = text.strip()
    text = re.sub(r"^```(?:json)?\s*|\s*```$", "", text)
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end <= start:
        raise ValueError("模型没有返回有效 JSON")
    data = json.loads(text[start:end + 1])
    if isinstance(data, list): data = {"recommendations": data}
    recs = data.get("recommendations", data.get("suggestions", []))
    if not isinstance(recs, list) or not recs:
        raise ValueError("模型返回结果中没有建议列表")
    normalized = []
    for rec in recs[:3]:
        normalized.append({"content_type": rec.get("content_type", rec.get("category", "艺术科普")),
                           "platform": rec.get("platform", PLATFORMS[0]),
                           "day_of_week": rec.get("day_of_week", rec.get("day", "周三")),
                           "time_slot": rec.get("time_slot", rec.get("time", "19:30")),
                           "title": rec.get("title", rec.get("topic", "围绕团队经验创作一条内容")),
                           "reason": rec.get("reason", "建议仅供参考，请结合团队实际安排。"),
                           "reference_sample_count": int(rec.get("reference_sample_count", 0) or 0),
                           "confidence": rec.get("confidence", "low") if rec.get("confidence") in {"high", "medium", "low"} else "low"})
    return {"recommendations": normalized}


def dataset_summary(records: list[Content], config: AIConfig) -> dict[str, Any]:
    threshold = config.min_sample
    by_category = group_analysis(records, "category", threshold,config)
    by_platform = group_analysis(records, "platform", threshold,config)
    combos: dict[tuple[str, int, int], list[Content]] = defaultdict(list)
    for c in records:
        combos[(c.category, c.published_at.weekday(), c.published_at.hour)].append(c)
    best = []
    for (category, weekday, hour), group in combos.items():
        if len(group) >= threshold:
            best.append({"category": category, "day": day_label(weekday), "hour": hour,
                         "completion_mean": round(statistics.mean(c.completion for c in group), 1),
                         "interaction_mean": round(statistics.mean(interaction_rate(c) for c in group), 2),
                         "sample_count": len(group)})
    best.sort(key=lambda x: x["completion_mean"] + min(x["interaction_mean"], 28), reverse=True)
    records_summary = [{"category": c.category, "tags": json.loads(c.tags_json or "[]"), "platform": c.platform,
                        "weekday": day_label(c.published_at.weekday()), "time_slot": time_label(c.published_at.hour),
                        "views": c.views, "completion": c.completion, "score": score_content(c,config),
                        "interactions": c.total_interactions} for c in records[-200:]]
    return {"sample_total": len(records), "minimum_sample_for_patterns": threshold,
            "category_aggregates": by_category, "platform_aggregates": by_platform,
            "best_time_combinations": best[:10], "records": records_summary}


def rule_recommendations(records: list[Content], config: AIConfig) -> list[dict[str, Any]]:
    if not records:
        return [{"content_type": "艺术科普", "platform": PLATFORMS[0], "day_of_week": "周三", "time_slot": "19:30",
                 "title": "用一分钟讲清一项艺术知识", "reason": "团队还没有历史记录。这是一条通用起步建议，请按团队实际情况调整。",
                 "reference_sample_count": 0, "confidence": "low"}]
    # Rank actual records to generate suggestions from the team's own best observed examples.
    ranked = sorted(records, key=lambda c: score_content(c, config), reverse=True)
    distinct: list[Content] = []
    seen: set[tuple[str, str]] = set()
    for c in ranked:
        k = (c.category, c.platform)
        if k not in seen:
            distinct.append(c); seen.add(k)
    while len(distinct) < min(3, len(ranked)):
        candidate = next((c for c in ranked if c not in distinct), None)
        if not candidate: break
        distinct.append(candidate)
    patterns = defaultdict(list)
    for c in records:
        patterns[(c.category, c.platform, c.published_at.weekday(), time_label(c.published_at.hour))].append(c)
    recs=[]
    for i,c in enumerate((distinct or ranked)[:3]):
        matching=patterns[(c.category,c.platform,c.published_at.weekday(),time_label(c.published_at.hour))]
        group=matching if len(matching)>=config.min_sample else [c]
        n=len(group); confidence="high" if n>=config.min_sample*2 else "medium" if n>=config.min_sample else "low"
        mean_completion=statistics.mean(x.completion for x in group)
        mean_inter=statistics.mean(interaction_rate(x) for x in group)
        reason=(f"团队已有 {n} 条「{c.category}」相关记录，平均看完比例 {mean_completion:.0f}%，互动比例 {mean_inter:.1f}%。"
                f"建议参考 {day_label(c.published_at.weekday())}{time_label(c.published_at.hour)} 的发布时段，沿用内容方向并尝试新的主题。")
        if n<config.min_sample: reason+=f"当前样本未达到 {config.min_sample} 条，规律尚不稳定，仅供参考。"
        recs.append({"content_type":c.category,"platform":c.platform,"day_of_week":day_label(c.published_at.weekday()),
                     "time_slot":f"{c.published_at.hour:02d}:30","title":f"延展主题｜{c.title.split('｜')[-1]}",
                     "reason":reason,"reference_sample_count":n,"confidence":confidence,
                     "prediction_views_low":max(0,round(c.views*.7)),"prediction_views_high":round(c.views*1.3)})
    return recs


def report_pdf(db: Session, tid: int, records: list[Content], config: AIConfig) -> bytes:
    from reportlab.lib import colors
    from reportlab.lib.pagesizes import A4
    from reportlab.pdfbase import pdfmetrics
    from reportlab.pdfbase.ttfonts import TTFont
    from reportlab.pdfgen import canvas
    font_candidates = ["/usr/share/fonts/google-droid-sans-fonts/DroidSansFallbackFull.ttf", "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc", "/System/Library/Fonts/PingFang.ttc"]
    font = next((x for x in font_candidates if Path(x).exists()), None)
    if font:
        try: pdfmetrics.registerFont(TTFont("CJK", font, subfontIndex=0)); font_name="CJK"
        except Exception: font_name="Helvetica"
    else: font_name="Helvetica"
    out=io.BytesIO(); c=canvas.Canvas(out,pagesize=A4); width,height=A4
    c.setFillColor(colors.HexColor("#eee9dc")); c.rect(0,0,width,height,fill=1,stroke=0)
    c.setFillColor(colors.HexColor("#302b24")); c.setFont(font_name,20); c.drawString(42,height-54,"艺数智策 运营复盘")
    c.setFont(font_name,9); c.setFillColor(colors.HexColor("#81796d")); c.drawString(42,height-74,f"生成时间 {datetime.now().strftime('%Y-%m-%d %H:%M')} · 仅基于团队主动录入的数据")
    y=height-112; total=len(records); views=sum(x.views for x in records); inter=sum(x.total_interactions for x in records)
    completion=statistics.mean([x.completion for x in records]) if records else 0
    c.setFont(font_name,11); c.setFillColor(colors.HexColor("#3d4b5c")); c.drawString(42,y,"传播表现概览")
    c.setFillColor(colors.HexColor("#302b24")); c.setFont(font_name,10)
    c.drawString(42,y-23,f"发布记录：{total} 条     总播放：{views:,}     总互动：{inter:,}     平均看完比例：{completion:.1f}%")
    y-=65; c.setFillColor(colors.HexColor("#3d4b5c")); c.setFont(font_name,11); c.drawString(42,y,"内容类型表现（样本不足时不展示均值）")
    groups=group_analysis(records,"category",config.min_sample,config); y-=20
    c.setFont(font_name,9)
    for g in groups:
        value=f"平均看完 {g['completion']}% · 平均播放 {g['views']:,.0f}" if g['sample_count']>=config.min_sample else "样本不足，继续积累记录"
        c.setFillColor(colors.HexColor("#302b24"));c.drawString(46,y,f"{g['name']}  ·  {g['sample_count']} 条  ·  {value}")
        c.setFillColor(colors.HexColor("#d7d0c2")); c.rect(300,y-2,180,8,fill=1,stroke=0)
        if g['score'] is not None:
            c.setFillColor(colors.HexColor("#647383")); c.rect(300,y-2,180*min(1,g['score']/100),8,fill=1,stroke=0)
        y-=22
    y-=12;c.setFillColor(colors.HexColor("#3d4b5c"));c.setFont(font_name,11);c.drawString(42,y,"优质内容")
    y-=20;c.setFont(font_name,9);ranked=sorted(records,key=lambda x:score_content(x,config),reverse=True)[:5]
    for item in ranked:
        text=f"{item.title[:36]} · {item.category} · {item.platform} · {score_content(item,config):.0f}分"
        c.setFillColor(colors.HexColor("#302b24"));c.drawString(46,y,text);y-=19
    rec=rule_recommendations(records,config)
    y-=10;c.setFillColor(colors.HexColor("#a84f2c"));c.setFont(font_name,11);c.drawString(42,y,"下周发布参考")
    y-=20;c.setFont(font_name,9);c.setFillColor(colors.HexColor("#302b24"))
    for r in rec:
        if y<95: c.showPage();y=height-55;c.setFont(font_name,9)
        c.drawString(46,y,f"{r['day_of_week']} {r['time_slot']} · {r['content_type']} · {r['platform']} · {r['confidence']}信心")
        y-=14;c.setFillColor(colors.HexColor("#81796d"));c.drawString(54,y,r['reason'][:70]);c.setFillColor(colors.HexColor("#302b24"));y-=20
    c.setFont(font_name,8);c.setFillColor(colors.HexColor("#81796d"));c.drawString(42,35,"综合表现分根据看完比例和互动表现计算。小样本结论仅供参考，不代表行业均值。")
    c.save();return out.getvalue()


app = FastAPI(title="艺数智策 API", version="1.0.0", description="面向艺术团队的自有数据复盘系统")
app.add_middleware(CORSMiddleware, allow_origins=["http://localhost:8000", "http://127.0.0.1:8000"], allow_methods=["*"], allow_headers=["*"])


@app.on_event("startup")
def startup():
    Base.metadata.create_all(engine)
    with SessionLocal() as db:
        if not db.get(Team, 1):
            db.add(Team(id=1, name="示例艺术团队")); db.commit()
        if os.getenv("SEED_ON_START", "true").lower() == "true" and not db.scalar(select(Content.id).where(Content.team_id == 1)):
            seed = [
                ("短视频平台A",18,"18:30","后台揭秘｜开演前的九十分钟","幕后记录",48,3280,246,38,71,52,68,"后台,准备"),
                ("短视频平台B",12,"20:00","技艺拆解｜台上一分钟是怎样炼成的","技艺展示",62,5120,391,64,120,88,74,"技艺,练习"),
                ("内容社区C",5,"12:30","一分钟认识传统艺术的舞台语言","艺术科普",56,2740,173,29,56,31,61,"科普,入门")]
            now=datetime.now()
            for p,ago,h,title,cat,dur,views,likes,comments,saves,shares,completion,tags in seed:
                published=now-timedelta(days=ago); published=published.replace(hour=int(h[:2]),minute=int(h[-2:]),second=0,microsecond=0)
                db.add(Content(team_id=1,title=title,category=cat,platform=p,published_at=published,duration=dur,views=views,likes=likes,comments=comments,saves=saves,shares=shares,completion=completion,total_interactions=likes+comments+saves+shares,tags_json=json.dumps(tags.split(","),ensure_ascii=False),source="seed",lifecycle="已复盘",comment_text="观众反馈积极，内容清晰"))
            db.commit()


@app.get("/api/health")
def health(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    team_or_404(db, tid)
    return {"status": "ok", "database": "connected", "team_id": tid, "version": app.version}


@app.get("/api/auth/status")
def auth_status(db:Session=Depends(get_db)):
    return {"setup_required":db.scalar(select(User.id).limit(1)) is None}


def auth_result(db:Session,user:User)->dict[str,Any]:
    token=issue_token(db,user);team=db.get(Team,user.team_id);db.commit()
    return {"access_token":token,"token_type":"bearer","expires_in":1209600,"user":{"id":user.id,"email":user.email,"name":user.name,"role":user.role_name,"team_id":user.team_id,"team_name":team.name if team else ""}}


@app.post("/api/auth/setup")
def setup_account(data:AuthSetup,db:Session=Depends(get_db)):
    if db.scalar(select(User.id).limit(1)) is not None:raise HTTPException(409,"管理员账号已经创建，请使用登录")
    team=db.get(Team,1)
    if not team:team=Team(id=1,name=data.team_name);db.add(team)
    else:team.name=data.team_name
    if "@" not in data.email:raise HTTPException(422,"请输入有效邮箱地址")
    user=User(team_id=1,email=data.email.strip().lower(),name=data.name.strip(),role_name="owner",password_hash=password_hash(data.password));db.add(user);db.flush()
    audit(db,1,"setup","user",str(user.id),"创建团队管理员",actor=user.name)
    return auth_result(db,user)


@app.post("/api/auth/register-team")
def register_team(data:AuthSetup,db:Session=Depends(get_db)):
    if "@" not in data.email:raise HTTPException(422,"请输入有效邮箱地址")
    if db.scalar(select(User).where(User.email==data.email.strip().lower())):raise HTTPException(409,"该邮箱已经注册")
    team=Team(name=data.team_name.strip());db.add(team);db.flush()
    user=User(team_id=team.id,email=data.email.strip().lower(),name=data.name.strip(),role_name="owner",password_hash=password_hash(data.password));db.add(user);db.flush()
    audit(db,team.id,"register_team","team",str(team.id),"创建新团队",actor=user.name)
    return auth_result(db,user)


@app.post("/api/auth/login")
def login(data:LoginIn,db:Session=Depends(get_db)):
    user=db.scalar(select(User).where(User.email==data.email.strip().lower()))
    if not user or not verify_password(data.password,user.password_hash):raise HTTPException(401,"邮箱或密码不正确")
    return auth_result(db,user)


@app.get("/api/auth/me")
def current_account(claims:dict[str,Any]=Depends(auth_claims),db:Session=Depends(get_db)):
    team=db.get(Team,claims["team_id"])
    return {"id":claims["user_id"],"email":claims["email"],"name":claims["name"],"role":claims["role"],"team_id":claims["team_id"],"team_name":team.name if team else ""}


@app.post("/api/auth/logout")
def logout(claims:dict[str,Any]=Depends(auth_claims),db:Session=Depends(get_db)):
    session=db.get(UserSession,claims["session_id"]);session.revoked=True;db.commit();return {"logged_out":True}


@app.get("/api/users")
def list_users(db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    return [{"id":u.id,"email":u.email,"name":u.name,"role":u.role_name,"created_at":u.created_at.isoformat()} for u in db.scalars(select(User).where(User.team_id==tid).order_by(User.created_at)).all()]


@app.post("/api/users")
def add_user(data:UserInvite,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(role)):
    if _role!="owner":raise HTTPException(403,"只有团队负责人可以管理成员")
    email=data.email.strip().lower()
    if "@" not in email:raise HTTPException(422,"请输入有效邮箱地址")
    if data.role not in {"editor","viewer"}:raise HTTPException(422,"成员角色只能是录入者或查看者")
    if db.scalar(select(User).where(User.email==email)):raise HTTPException(409,"该邮箱已经注册")
    user=User(team_id=tid,email=email,name=data.name.strip(),role_name=data.role,password_hash=password_hash(data.password));db.add(user);db.flush()
    audit(db,tid,"invite","user",str(user.id),f"新增成员 {email} / {data.role}");db.commit()
    return {"id":user.id,"email":user.email,"name":user.name,"role":user.role_name}


@app.get("/api/teams")
def teams(db: Session = Depends(get_db),tid:int=Depends(team_id)):
    team=team_or_404(db,tid);return [{"id":team.id,"name":team.name}]


@app.post("/api/teams")
def create_team(data: TeamIn, db: Session = Depends(get_db), _role: str = Depends(require_editor)):
    team = Team(name=data.name); db.add(team); db.flush(); audit(db,team.id,"create","team",str(team.id)); db.commit()
    return {"id":team.id,"name":team.name}


@app.get("/api/contents")
def list_contents(db: Session = Depends(get_db), tid: int = Depends(team_id)):
    team_or_404(db, tid)
    cfg=get_config(db,tid)
    return {"items":[serialize_content(c,cfg) for c in all_contents(db,tid)],"count":db.scalar(select(Content.id).where(Content.team_id==tid).count()) if False else len(all_contents(db,tid))}


@app.post("/api/contents")
def create_content(data: ContentIn, db: Session = Depends(get_db), tid: int = Depends(team_id), _role: str = Depends(require_editor)):
    team_or_404(db,tid)
    if data.completion > 100: raise HTTPException(422,"看完比例不能超过 100%")
    near = db.scalars(select(Content).where(Content.team_id==tid,Content.title==data.title)).all()
    duplicate = any(abs((x.published_at-data.published_at).total_seconds())<3*86400 for x in near)
    item=record_from_input(data,tid);db.add(item);db.flush();sync_tag_links(db,item,data.tags);audit(db,tid,"create","content",item.id,data.title);db.commit()
    return {"item":serialize_content(item,get_config(db,tid)),"duplicate_warning":"发现标题相同且发布时间接近的记录，请确认是否重复。" if duplicate else None}


@app.get("/api/contents/{content_id}")
def get_content(content_id: str,db: Session=Depends(get_db),tid:int=Depends(team_id)):
    item=db.scalar(select(Content).where(Content.id==content_id,Content.team_id==tid))
    if not item:raise HTTPException(404,"内容记录不存在")
    return serialize_content(item,get_config(db,tid))


@app.patch("/api/contents/{content_id}")
def update_content(content_id:str,data:dict[str,Any],db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    item=db.scalar(select(Content).where(Content.id==content_id,Content.team_id==tid))
    if not item:raise HTTPException(404,"内容记录不存在")
    allowed=set(ContentIn.model_fields)-{"tags"}
    for k,v in data.items():
        if k not in allowed and k!="tags":continue
        if k=="published_at" and isinstance(v,str):v=datetime.fromisoformat(v.replace("Z","+00:00")).replace(tzinfo=None)
        if k=="tags":item.tags_json=json.dumps(v,ensure_ascii=False);sync_tag_links(db,item,v)
        else:setattr(item,k,v)
    item.total_interactions=item.likes+item.comments+item.saves+item.shares
    audit(db,tid,"update","content",item.id,json.dumps(data,ensure_ascii=False));db.commit()
    return serialize_content(item,get_config(db,tid))


@app.delete("/api/contents/{content_id}")
def delete_content(content_id:str,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    item=db.scalar(select(Content).where(Content.id==content_id,Content.team_id==tid))
    if not item:raise HTTPException(404,"内容记录不存在")
    audit(db,tid,"delete","content",item.id,item.title);db.delete(item);db.commit();return {"deleted":True}


@app.post("/api/contents/import-csv")
async def import_csv(file:UploadFile=File(...),db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    raw=await file.read()
    try:text=raw.decode("utf-8-sig")
    except UnicodeDecodeError:text=raw.decode("gb18030",errors="replace")
    rows=list(csv.DictReader(io.StringIO(text)))
    aliases={"平台":"platform","发布平台":"platform","发布时间":"published_at","标题":"title","标题文案":"title","内容类型":"category","时长":"duration","视频时长":"duration","播放量":"views","点赞量":"likes","评论量":"comments","收藏量":"saves","转发量":"shares","看完比例":"completion","完播率":"completion","标签":"tags"}
    mapped=[]
    for row in rows:
        normalized={aliases.get(k.strip(),k.strip()):v for k,v in row.items() if k}
        mapped.append(normalized)
    required={"platform","published_at","title","views","likes"}
    missing=sorted(required-set().union(*(set(r) for r in mapped))) if mapped else ["platform","published_at","title","views","likes"]
    if missing:raise HTTPException(422,{"message":"CSV 缺少必要字段","missing":missing,"template":"平台,发布时间,标题,播放量,点赞量"})
    added=[];warnings=[]
    for i,r in enumerate(mapped,2):
        try:
            published=datetime.fromisoformat(str(r["published_at"]).replace("/","-").replace(" ","T"))
            def num(k,default=0):
                value=float(r.get(k,default) or default)
                if value<0:raise ValueError(f"{k} 不能为负数")
                return int(value) if k!="completion" else value
            title=str(r["title"]).strip();category=str(r.get("category") or classify(title))
            if num("completion")>100: warnings.append({"row":i,"message":"看完比例超过 100%，请核对"});continue
            if db.scalar(select(Content.id).where(Content.team_id==tid,Content.title==title,Content.published_at.between(published-timedelta(days=3),published+timedelta(days=3)))):
                warnings.append({"row":i,"message":"相同标题且发布时间接近，已跳过重复行"});continue
            item=Content(team_id=tid,title=title,category=category,platform=str(r["platform"]).strip(),published_at=published,
                duration=num("duration"),views=num("views"),likes=num("likes"),comments=num("comments"),saves=num("saves"),shares=num("shares"),
                completion=num("completion"),total_interactions=num("likes")+num("comments")+num("saves")+num("shares"),
                tags_json=json.dumps(re.split(r"[,，|]",str(r.get("tags", ""))) if r.get("tags") else [],ensure_ascii=False),source="csv",lifecycle="已发布")
            db.add(item);db.flush();sync_tag_links(db,item,json.loads(item.tags_json));added.append(item)
        except Exception as e:warnings.append({"row":i,"message":str(e)})
    audit(db,tid,"import_csv","content",detail=f"导入 {len(added)} 条")
    db.commit()
    cfg=get_config(db,tid)
    return {"imported":len(added),"warnings":warnings,"items":[serialize_content(x,cfg) for x in added]}


def classify(title:str)->str:
    if re.search(r"后台|排练|准备|幕后|日常",title):return "幕后记录"
    if re.search(r"技艺|练习|身段|技巧|拆解",title):return "技艺展示"
    if re.search(r"科普|认识|知识|入门|语言",title):return "艺术科普"
    return "演出宣传"


@app.post("/api/contents/import-image")
async def import_image(file:UploadFile=File(...),tid:int=Depends(team_id)):
    content=await file.read()
    if len(content)>12*1024*1024:raise HTTPException(413,"图片不能超过 12 MB")
    try:
        from PIL import Image
        image=Image.open(io.BytesIO(content)).convert("RGB")
        import pytesseract
        text=pytesseract.image_to_string(image,lang=os.getenv("OCR_LANG","chi_sim+eng"))
    except ImportError:raise HTTPException(503,"OCR 组件未安装，请在 Docker 中运行或安装 Tesseract 与中文语言包")
    except Exception as e:raise HTTPException(422,f"截图识别失败：{str(e)[:220]}。请上传清晰截图，或改用手动录入。")
    numbers=re.findall(r"(?<!\d)\d[\d,]*(?:\.\d+)?%?",text)
    title=next((line.strip() for line in text.splitlines() if len(line.strip())>5),"")
    draft={"title":title,"platform":"","published_at":datetime.now().replace(second=0,microsecond=0).isoformat(timespec="minutes"),"category":classify(title) if title else "演出宣传",
           "views":0,"likes":0,"comments":0,"saves":0,"shares":0,"completion":0,"raw_text":text,"fields_needing_review":["platform","title","views","likes","completion"]}
    labels={"播放量":"views","播放":"views","点赞":"likes","评论":"comments","收藏":"saves","转发":"shares","完播率":"completion","看完比例":"completion"}
    for line in text.splitlines():
        for label,key in labels.items():
            if label in line:
                m=re.search(r"\d[\d,]*(?:\.\d+)?%?",line)
                if m:
                    val=float(m.group().replace(",","").replace("%",""))
                    draft[key]=val if key=="completion" else int(val)
                    if key in draft["fields_needing_review"]:draft["fields_needing_review"].remove(key)
    if not text.strip():draft["fields_needing_review"]=["title","platform","views","likes","completion"]
    return {"draft":draft,"recognized_text":text,"requires_confirmation":True,"message":"请逐项核对识别字段，确认后才会保存。"}


@app.post("/api/contents/confirm")
def confirm_ocr(data:ContentIn,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    item=record_from_input(data,tid);item.source="ocr";db.add(item);db.flush();sync_tag_links(db,item,data.tags);audit(db,tid,"confirm_ocr","content",item.id,item.title);db.commit()
    return serialize_content(item,get_config(db,tid))


@app.get("/api/tags")
def list_tags(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    return [{"id":t.id,"name":t.name,"color":t.color,"count":db.query(ContentTag).filter(ContentTag.tag_id==t.id).count()} for t in db.scalars(select(Tag).where(Tag.team_id==tid).order_by(Tag.name)).all()]


@app.post("/api/tags")
def create_tag(data:dict[str,str],db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    name=data.get("name","").strip()
    if not name:raise HTTPException(422,"标签名称不能为空")
    existing=db.scalar(select(Tag).where(Tag.team_id==tid,Tag.name==name))
    if existing:return {"id":existing.id,"name":existing.name,"color":existing.color}
    t=Tag(team_id=tid,name=name,color=data.get("color","#647383"));db.add(t);db.flush();audit(db,tid,"create","tag",str(t.id),name);db.commit();return {"id":t.id,"name":t.name,"color":t.color}


@app.delete("/api/tags/{tag_id}")
def delete_tag(tag_id:int,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    t=db.scalar(select(Tag).where(Tag.id==tag_id,Tag.team_id==tid))
    if not t:raise HTTPException(404,"标签不存在")
    db.delete(t);db.commit();return {"deleted":True}


def overview_data(db:Session,tid:int)->dict[str,Any]:
    team_or_404(db,tid);records=all_contents(db,tid);cfg=get_config(db,tid);n=len(records)
    good=sorted(records,key=lambda c:score_content(c,cfg),reverse=True)
    return {"total_posts":n,"total_views":sum(c.views for c in records),"total_interactions":sum(c.total_interactions for c in records),
            "avg_completion":round(statistics.mean([c.completion for c in records]),1) if records else 0,
            "avg_interaction_rate":round(statistics.mean([interaction_rate(c) for c in records]),2) if records else 0,
            "platform_count":len(set(c.platform for c in records)),"sample_count":n,"min_sample":cfg.min_sample,
            "confidence_note":sample_notice(n,cfg.min_sample),"top_contents":[serialize_content(c,cfg) for c in good[:5]],
            "needs_work":[serialize_content(c,cfg) for c in list(reversed(good[-5:]))],
            "lifecycle_counts":dict(Counter(c.lifecycle for c in records)),"team_name":db.get(Team,tid).name}


@app.get("/api/stats/overview")
def stats_overview(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    return overview_data(db,tid)


@app.get("/api/stats/trend")
def stats_trend(days:int=Query(default=90,ge=7,le=730),db:Session=Depends(get_db),tid:int=Depends(team_id)):
    since=datetime.now()-timedelta(days=days);records=[c for c in all_contents(db,tid) if c.published_at>=since]
    return {"days":days,"items":[{"date":c.published_at.date().isoformat(),"title":c.title,"platform":c.platform,"views":c.views,"interactions":c.total_interactions,"completion":c.completion,"sample_count":1,"confidence_note":"单条记录"} for c in records]}


@app.get("/api/stats/platform")
def stats_platform(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    records=all_contents(db,tid);cfg=get_config(db,tid);items=[]
    for x in group_analysis(records,"platform",cfg.min_sample,cfg):
        group=[r for r in records if r.platform==x["name"]]
        items.append({**x,"total_views":sum(c.views for c in group) if len(group)>=cfg.min_sample else None,"total_interactions":sum(c.total_interactions for c in group) if len(group)>=cfg.min_sample else None})
    return {"items":items,"min_sample":cfg.min_sample,"comparison_note":"平台统计口径可能不同，跨平台比较只作粗略参考。"}


@app.get("/api/stats/category")
def stats_category(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    records=all_contents(db,tid);cfg=get_config(db,tid);return {"items":group_analysis(records,"category",cfg.min_sample,cfg),"min_sample":cfg.min_sample,"score_formula":"综合表现分 = 看完比例 × 完播权重 + 互动比例 × 互动权重","weights":{"completion":cfg.score_completion_weight,"interaction":cfg.score_interaction_weight,"by_platform":json.loads(cfg.platform_weights_json or "{}")},"benchmark_source":"基于自身历史数据"}


@app.get("/api/stats/heatmap")
def stats_heatmap(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    records=all_contents(db,tid);cfg=get_config(db,tid);bins=[]
    for weekday in range(7):
        for slot in ["上午","午间","下午","晚间","夜间"]:
            group=[c for c in records if c.published_at.weekday()==weekday and time_label(c.published_at.hour)==slot];n=len(group)
            bins.append({"weekday":weekday,"day":day_label(weekday),"time_slot":slot,"sample_count":n,
                         "completion":round(statistics.mean(c.completion for c in group),1) if n>=cfg.min_sample else None,
                         "interaction_rate":round(statistics.mean(interaction_rate(c) for c in group),2) if n>=cfg.min_sample else None,
                         "confidence_note":sample_notice(n,cfg.min_sample)})
    return {"items":bins,"min_sample":cfg.min_sample}


@app.get("/api/stats/adoption")
def stats_adoption(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    rows=all_contents(db,tid); groups={}
    for key in (True,False):
        group=[c for c in rows if c.adopted==key];groups["adopted" if key else "not_adopted"]={"sample_count":len(group),"avg_completion":round(statistics.mean(c.completion for c in group),1) if group else None,"avg_interaction_rate":round(statistics.mean(interaction_rate(c) for c in group),2) if group else None}
    a,b=groups["adopted"],groups["not_adopted"]
    return {**groups,"completion_difference":round(a["avg_completion"]-b["avg_completion"],1) if a["avg_completion"] is not None and b["avg_completion"] is not None else None,
            "interaction_difference":round(a["avg_interaction_rate"]-b["avg_interaction_rate"],2) if a["avg_interaction_rate"] is not None and b["avg_interaction_rate"] is not None else None,
            "confidence_note":sample_notice(a["sample_count"]+b["sample_count"],get_config(db,tid).min_sample)}


@app.get("/api/stats/funnel")
def stats_funnel(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    rows=all_contents(db,tid)
    return {"views":sum(x.views for x in rows),"profile_visits":sum(x.profile_visits for x in rows),"follows":sum(x.follows for x in rows),"attendance":sum(x.attendance for x in rows),"tickets":sum(x.tickets for x in rows),"sample_count":len(rows),"confidence_note":sample_notice(len(rows),get_config(db,tid).min_sample)}


@app.get("/api/stats/diagnostics")
def stats_diagnostics(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    rows=all_contents(db,tid);cfg=get_config(db,tid);notices=[]
    if rows:
        current_month=datetime.now().strftime("%Y-%m");month_count=sum(1 for x in rows if x.published_at.strftime("%Y-%m")==current_month)
        monthly=Counter(x.published_at.strftime("%Y-%m") for x in rows);history=statistics.mean(monthly.values()) if monthly else 0
        if month_count<history*.6:notices.append({"type":"cadence","level":"info","message":f"本月已记录 {month_count} 条内容，低于你的历史月均 {history:.1f} 条。"})
        scores=[score_content(x,cfg) for x in rows];median=statistics.median(scores)
        for x,s in zip(rows,scores):
            if s>median+25:notices.append({"type":"spike","level":"good","content_id":x.id,"message":f"「{x.title}」表现高于团队历史中位数，建议复盘它的开场与内容节奏。"})
            elif s<median-25:notices.append({"type":"decline","level":"watch","content_id":x.id,"message":f"「{x.title}」表现明显低于团队历史中位数，可检查主题与发布时段。"})
    return {"items":notices,"sample_count":len(rows)}


@app.get("/api/benchmark")
def get_benchmarks(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    records=all_contents(db,tid);cfg=get_config(db,tid);result=[]
    overrides={x.category:x for x in db.scalars(select(Benchmark).where(Benchmark.team_id==tid,Benchmark.source_type=="manual")).all()}
    grouped=defaultdict(list)
    for r in records:grouped[r.category].append(r)
    for category,items in grouped.items():
        override=overrides.get(category)
        result.append({"category":category,"sample_count":len(items),"completion_median":override.completion_median if override else (round(statistics.median(x.completion for x in items),1) if len(items)>=cfg.min_sample else None),
                       "interaction_median":override.interaction_median if override else (round(statistics.median(interaction_rate(x) for x in items),2) if len(items)>=cfg.min_sample else None),"source_type":"manual" if override else "self_history","source_label":"团队手动调整" if override else "基于自身历史数据"})
    return result


@app.put("/api/benchmark")
def put_benchmark(data:dict[str,Any],db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    category=data.get("category");row=db.scalar(select(Benchmark).where(Benchmark.team_id==tid,Benchmark.category==category))
    if not row:row=Benchmark(team_id=tid,category=category,source_type="self_history");db.add(row)
    row.completion_median=float(data.get("completion_median",row.completion_median));row.interaction_median=float(data.get("interaction_median",row.interaction_median));row.source_type="manual";db.commit()
    return {"category":row.category,"completion_median":row.completion_median,"interaction_median":row.interaction_median,"source_type":"manual","source_label":"团队手动调整"}


@app.get("/api/settings")
def get_settings(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    config=get_config(db,tid)
    return {"id":config.id,"name":config.name,"base_url":config.base_url,"has_key":bool(config.api_key),"key_hint":"••••"+config.api_key[-4:] if len(config.api_key)>4 else ("已设置" if config.api_key else ""),"model":config.model,
            "temperature":config.temperature,"request_mode":config.request_mode,"auth_type":config.auth_type,"enabled":config.enabled,"fallback_enabled":config.fallback_enabled,
            "score_completion_weight":config.score_completion_weight,"score_interaction_weight":config.score_interaction_weight,"min_sample":config.min_sample,"platform_weights":json.loads(config.platform_weights_json or "{}"),"configured":bool(config.base_url and config.model and config.api_key)}


@app.put("/api/settings")
def put_settings(data:AIConfigIn,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    config=get_config(db,tid);values=data.model_dump()
    for k,v in values.items():
        if k=="api_key":
            if v:config.api_key=v
        elif k=="platform_weights":config.platform_weights_json=json.dumps(v)
        else:setattr(config,k,v)
    db.commit();audit(db,tid,"update","settings",str(config.id),"更新模型和分析设置");db.commit()
    return get_settings(db,tid)


@app.get("/api/ai/configs")
def list_ai_configs(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    rows=db.scalars(select(AIConfig).where(AIConfig.team_id==tid).order_by(AIConfig.updated_at.desc())).all()
    return [{"id":x.id,"name":x.name,"model":x.model,"base_url":x.base_url,"enabled":x.enabled,"configured":bool(x.base_url and x.model and x.api_key),"key_hint":"••••"+x.api_key[-4:] if len(x.api_key)>4 else ("已设置" if x.api_key else "")} for x in rows]


@app.post("/api/ai/configs")
def create_ai_config(data:AIConfigIn,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    values=data.model_dump();key=values.pop("api_key","");weights=values.pop("platform_weights",{})
    values["platform_weights_json"]=json.dumps(weights,ensure_ascii=False);values["team_id"]=tid;values["api_key"]=key;values["enabled"]=False
    row=AIConfig(**values);db.add(row);db.flush();audit(db,tid,"create","ai_config",str(row.id),row.name);db.commit()
    return {"id":row.id,"name":row.name,"enabled":row.enabled,"configured":bool(row.base_url and row.model and row.api_key)}


@app.post("/api/ai/configs/{config_id}/activate")
def activate_ai_config(config_id:int,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    rows=db.scalars(select(AIConfig).where(AIConfig.team_id==tid)).all();target=next((x for x in rows if x.id==config_id),None)
    if not target:raise HTTPException(404,"模型配置不存在")
    for row in rows:row.enabled=row.id==target.id
    db.commit();audit(db,tid,"activate","ai_config",str(target.id),target.name);db.commit()
    return {"id":target.id,"name":target.name,"enabled":True}


@app.delete("/api/ai/configs/{config_id}")
def delete_ai_config(config_id:int,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    row=db.scalar(select(AIConfig).where(AIConfig.id==config_id,AIConfig.team_id==tid))
    if not row:raise HTTPException(404,"模型配置不存在")
    if row.enabled:raise HTTPException(409,"请先切换到其他配置，再删除当前配置")
    db.delete(row);db.commit();return {"deleted":True}


@app.post("/api/settings/sample-threshold")
def set_sample(data:SampleSetting,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    config=get_config(db,tid);config.min_sample=data.min_sample;db.commit();return {"min_sample":config.min_sample}


@app.post("/api/ai/test")
async def ai_test(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    config=get_config(db,tid)
    if not (config.base_url and config.api_key and config.model):raise HTTPException(400,"请先填写模型地址、模型名称和 API Key")
    start=time.time()
    try:
        async with httpx.AsyncClient(timeout=20) as client:
            resp=await client.post(model_endpoint(config),headers=model_headers(config),json={"model":config.model,"messages":[{"role":"user","content":"只回复：连接成功"}],"temperature":0,"max_tokens":20})
        if resp.status_code>=400:raise HTTPException(resp.status_code,f"服务商返回 HTTP {resp.status_code}：{resp.text[:500]}")
        body=resp.json();message=body.get("choices",[{}])[0].get("message",{}).get("content","")
        db.add(AICallLog(team_id=tid,input_summary="连接测试",output_summary=str(message)[:500],elapsed_ms=int((time.time()-start)*1000),status="ok"));db.commit()
        return {"ok":True,"message":"配置已生效","reply":message,"elapsed_ms":int((time.time()-start)*1000)}
    except HTTPException as e:
        db.add(AICallLog(team_id=tid,input_summary="连接测试",output_summary=str(e.detail),elapsed_ms=int((time.time()-start)*1000),status="error"));db.commit();raise
    except Exception as e:
        db.add(AICallLog(team_id=tid,input_summary="连接测试",output_summary=str(e)[:500],elapsed_ms=int((time.time()-start)*1000),status="error"));db.commit();raise HTTPException(502,f"模型连接失败：{str(e)[:300]}")


@app.post("/api/ai/recommend")
async def ai_recommend(db:Session=Depends(get_db),tid:int=Depends(team_id),force_rule:bool=Query(default=False)):
    records=all_contents(db,tid);config=get_config(db,tid);summary=dataset_summary(records,config);start=time.time();recs=None;source="rule";source_label="规则分析（未使用大模型）";error=None
    if not force_rule and config.enabled and config.base_url and config.api_key and config.model:
        prompt=("角色：你是艺术团队内容运营分析助手。只根据下方团队自己的历史发布数据，给出下周三条可执行排期建议。样本小，未达到 minimum_sample_for_patterns 的分组不得声称规律；不得编造结论。请用容易执行的话说明依据，给出主题标题。只返回 JSON，对象结构为 {\"recommendations\":[{\"content_type\":\"内容类型\",\"platform\":\"平台\",\"day_of_week\":\"周几\",\"time_slot\":\"HH:MM\",\"title\":\"建议主题\",\"reason\":\"通俗依据\",\"reference_sample_count\":0,\"confidence\":\"low|medium|high\"}]}。数据摘要：\n"+json.dumps(summary,ensure_ascii=False))
        try:
            async with httpx.AsyncClient(timeout=45) as client:
                resp=await client.post(model_endpoint(config),headers=model_headers(config),json={"model":config.model,"messages":[{"role":"user","content":prompt}],"temperature":config.temperature,"response_format":{"type":"json_object"}})
            if resp.status_code>=400:raise RuntimeError(f"服务商 HTTP {resp.status_code}: {resp.text[:400]}")
            body=resp.json();txt=body.get("choices",[{}])[0].get("message",{}).get("content","");recs=parse_model_json(txt)["recommendations"];source=config.model;source_label=f"AI 建议 · {config.model}"
            for r in recs:
                r["prediction_views_low"]=None;r["prediction_views_high"]=None
        except Exception as e:error=str(e)[:500]
    if recs is None:
        if config.base_url and config.model and config.api_key and not config.fallback_enabled and not force_rule:
            db.add(AICallLog(team_id=tid,input_summary=json.dumps(summary,ensure_ascii=False)[:4000],output_summary=error or "调用失败",elapsed_ms=int((time.time()-start)*1000),status="error"));db.commit()
            raise HTTPException(502,f"模型调用失败且已关闭自动降级：{error}")
        recs=rule_recommendations(records,config)
    recommendation=Recommendation(team_id=tid,payload_json=json.dumps(recs,ensure_ascii=False),source_model=source,source_label=source_label,confidence=min((x["confidence"] for x in recs),key=lambda x:{"low":0,"medium":1,"high":2}[x]),reference_count=len(records))
    db.add(recommendation);db.add(AICallLog(team_id=tid,input_summary=json.dumps(summary,ensure_ascii=False)[:4000],output_summary=json.dumps(recs,ensure_ascii=False)[:4000],elapsed_ms=int((time.time()-start)*1000),status="fallback" if error else "ok"));audit(db,tid,"generate","recommendation",recommendation.id,source_label);db.commit()
    return {"id":recommendation.id,"generated_at":recommendation.generated_at.isoformat(),"recommendations":recs,"source":source,"source_label":source_label,"reference_count":len(records),"confidence":recommendation.confidence,"confidence_note":sample_notice(len(records),config.min_sample),"fallback_reason":error,"data_summary":summary}


@app.post("/api/ai/insight")
async def ai_insight(data:dict[str,str],db:Session=Depends(get_db),tid:int=Depends(team_id)):
    config=get_config(db,tid);records=all_contents(db,tid);question=data.get("question","").strip()
    if not question:raise HTTPException(422,"请填写想了解的问题")
    if config.enabled and config.base_url and config.api_key and config.model:
        summary=dataset_summary(records,config)
        try:
            async with httpx.AsyncClient(timeout=30) as client:
                resp=await client.post(model_endpoint(config),headers=model_headers(config),json={"model":config.model,"messages":[{"role":"user","content":f"只基于以下自有数据回答问题，样本不足需说明，不可编造：{json.dumps(summary,ensure_ascii=False)}\n问题：{question}"}],"temperature":config.temperature})
            resp.raise_for_status();answer=resp.json()["choices"][0]["message"]["content"];return {"answer":answer,"source":"AI","sample_count":len(records)}
        except Exception as e:
            if not config.fallback_enabled:raise HTTPException(502,f"模型调用失败：{str(e)[:300]}")
    return {"answer":f"目前有 {len(records)} 条团队记录，平均看完比例 {statistics.mean([r.completion for r in records]):.1f}% 。{sample_notice(len(records),config.min_sample) or '这组数据可作为团队自身的参考。'}建议查看“效果分析”中的内容类型与发布时段对比，再安排小规模尝试。","source":"规则分析（未使用大模型）","sample_count":len(records)}


@app.get("/api/recommendations")
def list_recommendations(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    rows=db.scalars(select(Recommendation).where(Recommendation.team_id==tid).order_by(Recommendation.generated_at.desc()).limit(20)).all()
    return [{"id":x.id,"generated_at":x.generated_at.isoformat(),"recommendations":json.loads(x.payload_json),"source":x.source_model,"source_label":x.source_label,"reference_count":x.reference_count,"confidence":x.confidence,"adopted":x.adopted} for x in rows]


@app.get("/api/schedule")
def get_schedule(start:date|None=None,end:date|None=None,db:Session=Depends(get_db),tid:int=Depends(team_id)):
    stmt=select(Schedule).where(Schedule.team_id==tid)
    if start:stmt=stmt.where(Schedule.planned_date>=start)
    if end:stmt=stmt.where(Schedule.planned_date<=end)
    rows=db.scalars(stmt.order_by(Schedule.planned_date,Schedule.planned_time)).all()
    return [{"id":x.id,"recommendation_id":x.recommendation_id,"content_id":x.content_id,"planned_date":x.planned_date.isoformat(),"planned_time":x.planned_time,"title":x.title,"category":x.category,"platform":x.platform,"reason":x.reason,"status":x.status,"prediction_views_low":x.prediction_views_low,"prediction_views_high":x.prediction_views_high,"actual_completion":x.actual_completion} for x in rows]


@app.get("/api/schedule/export")
def export_schedule(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    rows=db.scalars(select(Schedule).where(Schedule.team_id==tid).order_by(Schedule.planned_date,Schedule.planned_time)).all()
    out=io.StringIO();writer=csv.writer(out);writer.writerow(["计划日期","计划时间","内容主题","内容类型","平台","状态","依据说明"])
    for x in rows:writer.writerow([x.planned_date.isoformat(),x.planned_time,x.title,x.category,x.platform,x.status,x.reason])
    return Response(content="\ufeff"+out.getvalue(),media_type="text/csv; charset=utf-8",headers={"Content-Disposition":"attachment; filename=arts-smart-schedule.csv"})


@app.post("/api/schedule/adopt")
def adopt(data:AdoptIn,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    rec=db.scalar(select(Recommendation).where(Recommendation.id==data.recommendation_id,Recommendation.team_id==tid))
    if not rec:raise HTTPException(404,"建议记录不存在")
    items=data.items or json.loads(rec.payload_json);created=[];today=date.today();weekday_names={"周一":0,"周二":1,"周三":2,"周四":3,"周五":4,"周六":5,"周日":6}
    next_monday=today+timedelta(days=(7-today.weekday())%7 or 7)
    for i,item in enumerate(items):
        day=item.get("day_of_week","周三");delta=(weekday_names.get(day,2)-next_monday.weekday())%7
        planned=next_monday+timedelta(days=delta+7*((i+1)//3))
        time_text=item.get("time_slot","19:30")
        schedule=Schedule(team_id=tid,recommendation_id=rec.id,planned_date=planned,planned_time=time_text[:5],title=item.get("title",item.get("content_type","内容建议")),category=item.get("content_type","艺术科普"),platform=item.get("platform",PLATFORMS[0]),reason=item.get("reason",""),prediction_views_low=item.get("prediction_views_low"),prediction_views_high=item.get("prediction_views_high"))
        db.add(schedule);db.flush();created.append(schedule)
    rec.adopted=True;audit(db,tid,"adopt","recommendation",rec.id,f"采纳 {len(created)} 条建议");db.commit()
    return {"created":len(created),"items":[{"id":x.id,"planned_date":x.planned_date.isoformat(),"planned_time":x.planned_time,"title":x.title,"category":x.category,"platform":x.platform,"status":x.status} for x in created]}


@app.patch("/api/schedule/{schedule_id}")
def update_schedule(schedule_id:str,data:dict[str,Any],db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    item=db.scalar(select(Schedule).where(Schedule.id==schedule_id,Schedule.team_id==tid))
    if not item:raise HTTPException(404,"排期不存在")
    allowed={"planned_date","planned_time","title","category","platform","status"}
    for k,v in data.items():
        if k in allowed:setattr(item,k,date.fromisoformat(v) if k=="planned_date" and isinstance(v,str) else v)
    audit(db,tid,"update","schedule",item.id,json.dumps(data,ensure_ascii=False));db.commit();return {"id":item.id,"planned_date":item.planned_date.isoformat(),"planned_time":item.planned_time,"title":item.title,"status":item.status}


@app.post("/api/schedule/{schedule_id}/publish")
def publish_schedule(schedule_id:str,data:PublishIn,db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    schedule=db.scalar(select(Schedule).where(Schedule.id==schedule_id,Schedule.team_id==tid))
    if not schedule:raise HTTPException(404,"排期不存在")
    item=Content(team_id=tid,title=data.title,category=data.category,platform=data.platform,published_at=data.published_at,duration=0,views=data.views,likes=data.likes,comments=data.comments,saves=data.saves,shares=data.shares,completion=data.completion,total_interactions=data.likes+data.comments+data.saves+data.shares,source="feedback",lifecycle="已复盘",profile_visits=data.profile_visits,follows=data.follows,attendance=data.attendance,tickets=data.tickets,comment_text=data.comment_text,adopted=bool(schedule.recommendation_id),recommendation_id=schedule.recommendation_id)
    db.add(item);db.flush();schedule.content_id=item.id;schedule.status="已复盘";schedule.actual_views=data.views;schedule.actual_likes=data.likes;schedule.actual_comments=data.comments;schedule.actual_saves=data.saves;schedule.actual_shares=data.shares;schedule.actual_completion=data.completion
    audit(db,tid,"publish_feedback","schedule",schedule.id,f"实际播放 {data.views}，看完 {data.completion}%");db.commit()
    predicted=(schedule.prediction_views_low+schedule.prediction_views_high)/2 if schedule.prediction_views_low is not None and schedule.prediction_views_high is not None else None
    return {"content":serialize_content(item,get_config(db,tid)),"prediction":{"views_midpoint":predicted,"actual_views":data.views,"views_deviation_percent":round((data.views-predicted)/predicted*100,1) if predicted else None,"predicted_completion":None,"actual_completion":data.completion}}


@app.get("/api/schedule/adoption-gains")
def adoption_gains(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    rows=[x for x in all_contents(db,tid) if x.recommendation_id]
    return {"items":[{"content_id":x.id,"title":x.title,"adopted":x.adopted,"completion":x.completion,"interaction_rate":round(interaction_rate(x),2),"sample_count":1} for x in rows],"comparison":stats_adoption(db,tid)}


@app.get("/api/report/export")
def report_export(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    contents=all_contents(db,tid);config=get_config(db,tid)
    pdf=report_pdf(db,tid,contents,config)
    return Response(content=pdf,media_type="application/pdf",headers={"Content-Disposition":"attachment; filename*=UTF-8''Artsmart-report.pdf"})


@app.get("/api/export/anonymized")
def export_anonymized(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    rows=all_contents(db,tid);out=io.StringIO();writer=csv.writer(out);writer.writerow(["发布时间","内容类型","平台","播放量","点赞量","评论量","收藏量","转发量","看完比例","综合表现分"]);config=get_config(db,tid)
    for r in rows:writer.writerow([r.published_at.isoformat(),r.category,r.platform,r.views,r.likes,r.comments,r.saves,r.shares,r.completion,score_content(r,config)])
    return Response(content="\ufeff"+out.getvalue(),media_type="text/csv; charset=utf-8",headers={"Content-Disposition":"attachment; filename=anonymized-content.csv"})


@app.get("/api/audit")
def get_audit(limit:int=Query(default=100,ge=1,le=500),db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(role)):
    rows=db.scalars(select(AuditLog).where(AuditLog.team_id==tid).order_by(AuditLog.created_at.desc()).limit(limit)).all()
    return [{"actor":x.actor,"action":x.action,"entity_type":x.entity_type,"entity_id":x.entity_id,"created_at":x.created_at.isoformat(),"detail":x.detail} for x in rows]


@app.get("/api/hypotheses")
def list_hypotheses(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    return [{"id":x.id,"statement":x.statement,"result":x.result,"status":x.status,"created_at":x.created_at.isoformat()} for x in db.scalars(select(Hypothesis).where(Hypothesis.team_id==tid).order_by(Hypothesis.created_at.desc())).all()]


@app.post("/api/hypotheses")
def create_hypothesis(data:dict[str,str],db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    statement=data.get("statement","").strip()
    if not statement:raise HTTPException(422,"请填写待验证的假设")
    item=Hypothesis(team_id=tid,statement=statement,result=data.get("result", ""),status=data.get("status","待验证"));db.add(item);db.commit()
    return {"id":item.id,"statement":item.statement,"status":item.status,"created_at":item.created_at.isoformat()}


@app.patch("/api/hypotheses/{hypothesis_id}")
def update_hypothesis(hypothesis_id:str,data:dict[str,str],db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    item=db.scalar(select(Hypothesis).where(Hypothesis.id==hypothesis_id,Hypothesis.team_id==tid))
    if not item:raise HTTPException(404,"假设记录不存在")
    for k in ("statement","result","status"):
        if k in data:setattr(item,k,data[k])
    db.commit();return {"id":item.id,"statement":item.statement,"result":item.result,"status":item.status}


@app.get("/api/handover")
def handover(db:Session=Depends(get_db),tid:int=Depends(team_id)):
    data=overview_data(db,tid);records=all_contents(db,tid);cfg=get_config(db,tid)
    schedule=[{"planned_date":x.planned_date.isoformat(),"planned_time":x.planned_time,"title":x.title,"category":x.category,"platform":x.platform,"status":x.status} for x in db.scalars(select(Schedule).where(Schedule.team_id==tid).order_by(Schedule.planned_date)).all()]
    return {"team_name":data["team_name"],"generated_at":datetime.now().isoformat(),"top_contents":data["top_contents"],"category_findings":group_analysis(records,"category",cfg.min_sample,cfg),"schedules":schedule,"cadence_tip":"建议结合团队实际档期安排发布节奏。","todo_items":[{"item":"检查已采纳排期并回填实际数据","done":False},{"item":"复盘表现较好的内容开场与节奏","done":False}]}


@app.get("/api/handover/export")
def handover_export(format:str="markdown",db:Session=Depends(get_db),tid:int=Depends(team_id)):
    data=handover(db,tid=tid);lines=[f"# {data['team_name']} 运营交接包","",f"生成时间：{data['generated_at']}","","## 历史优质内容"]
    for x in data["top_contents"]:lines.append(f"- {x['title']}｜{x['category']}｜{x['platform']}｜{x['score']} 分")
    lines += ["","## 类型复盘"]
    for x in data["category_findings"]:lines.append(f"- {x['name']}（样本 {x['sample_count']} 条）："+(f"平均看完 {x['completion']}%" if x['completion'] is not None else "样本不足"))
    lines += ["","## 待办"]+[f"- [ ] {x['item']}" for x in data["todo_items"]]
    text="\n".join(lines)
    if format=="pdf":return Response(report_pdf(db,tid,all_contents(db,tid),get_config(db,tid)),media_type="application/pdf",headers={"Content-Disposition":"attachment; filename=handover.pdf"})
    return Response("\ufeff"+text,media_type="text/markdown; charset=utf-8",headers={"Content-Disposition":"attachment; filename=handover.md"})


@app.post("/api/seed/reset")
def reset_seed(db:Session=Depends(get_db),tid:int=Depends(team_id),_role:str=Depends(require_editor)):
    for item in all_contents(db,tid):db.delete(item)
    db.commit();audit(db,tid,"clear","content",detail="清空团队记录");db.commit()
    return {"deleted":True,"count":0}


@app.get("/api")
def api_root():return {"name":"艺数智策 API","docs":"/docs","health":"/api/health"}


app.mount("/", StaticFiles(directory=ROOT / "frontend", html=True), name="frontend")
