"""Product brand prompt.

The runtime package is called ``octop`` — that name leaks into the agent's
context (workspace paths such as ``~/.octop/...``, tool names such as
``octop-memory``, the Python package name). Models then volunteer it when the
user asks "你是谁 / 你跑在哪", which is wrong: the user-facing product name is
``猛犸AI``. The workspace files (SOUL.md / AGENTS.md) are user-maintained and
cannot be edited retroactively, so the naming rule is injected here as the
prefix of every agent system prompt.
"""

from __future__ import annotations

PRODUCT_NAME = "猛犸AI"
PRODUCT_NAME_SHORT = "猛犸"

BRAND_PROMPT = (
    f"## 产品身份\n"
    f"你运行在「{PRODUCT_NAME}」平台上（口语可简称「{PRODUCT_NAME_SHORT}」）。"
    f"当用户问你是谁、你跑在哪、由谁提供时，一律回答「{PRODUCT_NAME}」，"
    f"不要说出内部代号、包名、工具名或安装路径"
    f"（例如 octop / Octop / octop_harness / octop-memory / .octop）。"
    f"被问到实现细节时，只说「由运行环境提供」，不要列举内部实现名。"
)


def with_brand(system_prompt: str | None) -> str:
    """Return ``system_prompt`` prefixed with the product brand block."""
    base = (system_prompt or "").strip()
    return f"{BRAND_PROMPT}\n\n{base}" if base else BRAND_PROMPT
