import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Tooltip } from "antd";
import { useAppVersion } from "../../hooks/useAppVersion";
import { useUpdateStatus } from "../../hooks/useUpdateStatus";
import { useCurrentUser } from "../../hooks/useCurrentUser";
import { userCan } from "../../utils/permissions";
import styles from "./index.module.less";

interface CurrentVersionBadgeProps {
  isMobile?: boolean;
}

export default function CurrentVersionBadge({
  isMobile,
}: CurrentVersionBadgeProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const user = useCurrentUser();
  const { status } = useUpdateStatus();
  const appVersion = useAppVersion();

  // 显示版本一律优先 /api/version（镜像内版本）；update status 仅作兜底。
  const version = appVersion ?? status?.current_version;
  if (!version) return null;

  const canUpdate = userCan(user, "update");
  const tooltip = canUpdate
    ? t("header.currentVersionAdmin", { version })
    : t("header.currentVersion", { version });
  const label = `v${version}`;

  if (canUpdate) {
    return (
      <Tooltip title={tooltip} mouseEnterDelay={0.35}>
        <button
          type="button"
          className={`${styles.versionBadge} ${
            isMobile ? styles.versionBadgeMobile : ""
          } ${styles.versionBadgeClickable}`}
          onClick={() => navigate("/admin/advanced?tab=updates")}
          aria-label={tooltip}
        >
          {label}
        </button>
      </Tooltip>
    );
  }

  return (
    <Tooltip title={tooltip} mouseEnterDelay={0.35}>
      <span
        className={`${styles.versionBadge} ${
          isMobile ? styles.versionBadgeMobile : ""
        }`}
        aria-label={tooltip}
      >
        {label}
      </span>
    </Tooltip>
  );
}
