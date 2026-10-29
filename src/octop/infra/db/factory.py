"""Open a database pool from process configuration."""

from __future__ import annotations

from pathlib import Path

from octop.config import OctopConfig, database_env_configured, is_desktop_shell
from octop.infra.db.pool import DatabasePool, PostgresPool, SqlitePool
from octop.infra.utils.paths import PathLayout


def resolve_sqlite_db_path(config: OctopConfig, paths: PathLayout) -> Path:
    """Return the SQLite file path that ``open_database`` would use for sqlite."""
    db_cfg = config.database
    if config.database_in_file or database_env_configured():
        return db_cfg.resolve_sqlite_path(paths.root)
    return paths.db


def should_defer_control_plane_db(config: OctopConfig, paths: PathLayout) -> bool:
    """True when first-run setup should delay opening the control-plane DB.

    Defer only for greenfield SQLite installs with no existing DB file and no
    ``OCTOP_DATABASE_*`` / PostgreSQL configuration. Password verification and
    early wizard steps then run without a pool.

    Desktop never defers: the installed client has no local setup wizard, so a
    deferred DB left ``/api/auth/oauth/status`` at 503 forever and the window
    stuck on 「正在启动本地服务」. Let SQLite be created on first boot instead.
    """
    if config.database.is_postgresql:
        return False
    if database_env_configured():
        return False
    if is_desktop_shell():
        return False
    return not resolve_sqlite_db_path(config, paths).exists()


def open_database(config: OctopConfig, paths: PathLayout) -> DatabasePool:
    """Return a DB pool for the configured driver.

    When ``config.json`` has no ``database`` section and no ``OCTOP_DATABASE_*``
    env overrides are set, falls back to ``paths.db`` (legacy layout).
    """
    db_cfg = config.database
    if db_cfg.is_postgresql:
        return PostgresPool(db_cfg.postgresql_conninfo())

    return SqlitePool(resolve_sqlite_db_path(config, paths))
