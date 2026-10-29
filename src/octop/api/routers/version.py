"""Product version endpoint.

Single source of truth for every surface that shows a version number
(dashboard badge, console, admin). Reads the version baked into the image
(``pyproject`` / package metadata) and never an env override, so a stale
``MAMMOTH_APP_VERSION`` in the server ``.env`` can no longer drift.
"""

from __future__ import annotations

from fastapi import APIRouter
from pydantic import BaseModel

from octop.infra.setup.self_update import get_product_version

router = APIRouter(prefix="/version", tags=["version"])


class VersionInfo(BaseModel):
    product: str
    version: str


@router.get("", response_model=VersionInfo)
def read_version() -> VersionInfo:
    return VersionInfo(product="mammoth", version=get_product_version())
