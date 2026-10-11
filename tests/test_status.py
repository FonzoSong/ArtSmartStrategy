# SPDX-License-Identifier: Apache-2.0
"""Tests for content status compatibility and normalization."""

import pytest
from backend.status import normalize_content_status


@pytest.mark.parametrize("value", ["已发布", "已复盘", "published", "发布"])
def test_normalize_published_statuses(value: str) -> None:
    """Historical published statuses normalize to the published state."""
    assert normalize_content_status(value) == "已发布"


@pytest.mark.parametrize(
    "value",
    ["未发布", "创意", "待拍", "待剪", "待审", "draft", "unpublished"],
)
def test_normalize_unpublished_statuses(value: str) -> None:
    """Historical draft statuses normalize to the unpublished state."""
    assert normalize_content_status(value) == "未发布"


@pytest.mark.parametrize("value", ["未知状态", 42])
def test_unknown_statuses_default_to_unpublished(value: object) -> None:
    """Unknown non-empty values default to unpublished."""
    assert normalize_content_status(value) == "未发布"


@pytest.mark.parametrize("value", [None, ""])
def test_empty_statuses_keep_the_published_default(value: object) -> None:
    """Missing statuses retain the existing published import default."""
    assert normalize_content_status(value) == "已发布"
