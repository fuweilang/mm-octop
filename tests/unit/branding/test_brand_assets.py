"""品牌资产守卫：仓库里不允许再出现上游 Octop 的 logo / 吉祥物图。

源图与导出脚本在 mmPlatform 的 ``ground/brand/``：
先跑 ``ground/brand/export.sh``，把 ``ground/brand/out/`` 复制到
``dashboard/public/``，再同步下面的 sha256。
"""

from __future__ import annotations

import hashlib
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[3]
PUBLIC = REPO_ROOT / "dashboard" / "public"
DASHBOARD_SRC = REPO_ROOT / "dashboard" / "src"
DESKTOP_ASSETS = REPO_ROOT / "desktop" / "src" / "assets"

# 由 ground/brand/mengma-mark.png 导出，禁止换成别的图。
BRAND_SHA256 = {
    "brand-mark.png": "eb04a6ebcb34457eb6fa8bc7f6b5c4b53c0c2aee1e7c3e4fd6ee64a277c7cd96",
    "logo-icon.png": "51aaa9bd3026c8c240e04a24c5bc06c37f647ea41a4a40907f44dd8ce0c257de",
    "pwa-192.png": "2b7c82d52765b686fe5b8850878638d71a67a4657c202d6aa7639a14e9ac788a",
    "pwa-512.png": "51aaa9bd3026c8c240e04a24c5bc06c37f647ea41a4a40907f44dd8ce0c257de",
    "apple-touch-icon.png": "acedaaeac04e704fe1a541ecd7b1a04deb77341f3531ff19ca1d860aa7b0bdf3",
    "favicon.png": "1d387ee3d62fe5defc4190a7ba0d08543de44a2974d2ccb87c0dc3a3fbc00ebb",
    "favicon.ico": "06270c637f5e8401a824dc56702e4143277102dbab493cf5830659e2ab286661",
}

# 上游 Octop 品牌资源常见的文件名特征。
FORBIDDEN_NAME_PARTS = (
    "octop",
    "upstream",
    "logo_vertical",
    "logo_horizontal",
    "mengma-logo2",
    "favico.svg",
)


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _offenders(directory: Path) -> list[str]:
    return sorted(
        path.name
        for path in directory.iterdir()
        if path.is_file() and any(part in path.name.lower() for part in FORBIDDEN_NAME_PARTS)
    )


@pytest.mark.parametrize("name,expected", BRAND_SHA256.items())
def test_public_brand_assets_come_from_brand_source(name: str, expected: str) -> None:
    path = PUBLIC / name
    assert path.is_file(), f"缺少品牌图 {name}"
    assert _sha256(path) == expected, (
        f"{name} 与 ground/brand 的导出版本不一致；请重新导出而不是换一张图"
    )


def test_dashboard_public_has_no_upstream_octop_assets() -> None:
    assert _offenders(PUBLIC) == []


def test_desktop_assets_have_no_upstream_octop_assets() -> None:
    assert _offenders(DESKTOP_ASSETS) == []


def test_sidebar_reuses_the_brand_mark_for_both_states() -> None:
    sidebar = (DASHBOARD_SRC / "layouts" / "Sidebar.tsx").read_text(encoding="utf-8")

    assert "brand-mark.png" in sidebar
    # 收起态曾经回落到 pwa-192.png（上游红色 logo），别再回来。
    assert "pwa-192.png" not in sidebar
    assert "mengma-logo.png" not in sidebar


def test_offline_page_uses_the_brand_mark() -> None:
    offline = (PUBLIC / "offline.html").read_text(encoding="utf-8")

    assert "brand-mark.png" in offline
    assert "logo_vertical" not in offline
