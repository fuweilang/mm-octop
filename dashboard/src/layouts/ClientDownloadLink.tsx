import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, Download } from "lucide-react";
import { Dropdown } from "antd";
import type { MenuProps } from "antd";
import {
  CLIENT_PLATFORM_IDS,
  clientDownloadUrl,
  detectClientPlatform,
  type ClientPlatform,
} from "../utils/clientDownload";
import styles from "./Sidebar.module.less";

const PLATFORM_LABEL_KEY: Record<ClientPlatform, string> = {
  "mac-arm64": "nav.downloadMacArm",
  "mac-x64": "nav.downloadMacIntel",
  win: "nav.downloadWindows",
};

export default function ClientDownloadLink({ compact }: { compact?: boolean }) {
  const { t } = useTranslation();
  const primary = detectClientPlatform();

  const menu = useMemo<MenuProps>(() => {
    return {
      items: CLIENT_PLATFORM_IDS.map((id) => ({
        key: id,
        label: (
          <span className={styles.downloadMenuItem}>
            {t(PLATFORM_LABEL_KEY[id])}
          </span>
        ),
      })),
      onClick: ({ key }) => {
        window.location.assign(clientDownloadUrl(key as ClientPlatform));
      },
    };
  }, [t]);

  if (compact) {
    return (
      <Dropdown menu={menu} trigger={["click"]} placement="topRight">
        <button
          type="button"
          className={styles.middleOfficeCollapsed}
          aria-label={t("nav.downloadClient")}
          title={t("nav.downloadClient")}
        >
          <Download size={16} strokeWidth={1.8} />
        </button>
      </Dropdown>
    );
  }

  return (
    <div className={styles.downloadSplit}>
      <a className={styles.middleOfficeLink} href={clientDownloadUrl(primary)}>
        <span className={styles.middleOfficeIcon}>
          <Download size={16} strokeWidth={1.8} />
        </span>
        <span className={styles.middleOfficeText}>{t("nav.downloadClient")}</span>
      </a>
      <Dropdown menu={menu} trigger={["click"]} placement="topRight">
        <button
          type="button"
          className={styles.downloadMore}
          title={t("nav.downloadOther")}
          aria-label={t("nav.downloadOther")}
        >
          <ChevronDown size={14} strokeWidth={2} />
        </button>
      </Dropdown>
    </div>
  );
}
