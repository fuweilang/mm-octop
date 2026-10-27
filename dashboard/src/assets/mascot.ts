/**
 * 猛犸 logo. Replaces the upstream Octop mascot in empty states and connecting screens.
 */
export const MENGMA_LOGO_SRC = `${import.meta.env.BASE_URL}mengma-logo.png`;

/** @deprecated Use ``MENGMA_LOGO_SRC``. Kept so existing empty-state callers stay valid. */
export const OCTOP_EMPTY_MASCOT_SRC = MENGMA_LOGO_SRC;
