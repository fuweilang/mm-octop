import { Fragment, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Download } from "lucide-react";
import { Button, Modal, Tooltip } from "antd";
import {
  CLIENT_PLATFORM_OPTIONS,
  clientDownloadUrl,
  detectClientOs,
  fetchClientDownloadInfo,
  type ClientDownloadInfo,
  type ClientPlatform,
} from "../utils/clientDownload";
import styles from "./Sidebar.module.less";

function PlatformIcon({ platform }: { platform: ClientPlatform }) {
  if (platform === "mac-arm64") {
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <path
          fill="currentColor"
          d="M16.365 1.43c0 1.14-.493 2.27-1.177 3.08-.744.9-1.99 1.57-2.987 1.57-.12 0-.23-.02-.3-.03-.01-.06-.04-.22-.04-.39 0-1.15.572-2.27 1.206-2.98.804-.94 2.142-1.64 3.248-1.68.03.13.05.28.05.43zm4.565 15.71c-.03.07-.463 1.58-1.518 3.12-.945 1.34-1.94 2.71-3.43 2.71-1.517 0-1.9-.88-3.63-.88-1.698 0-2.302.91-3.67.91-1.377 0-2.332-1.26-3.428-2.8-1.287-1.82-2.323-4.63-2.323-7.28 0-4.28 2.797-6.55 5.552-6.55 1.448 0 2.675.95 3.6.95.865 0 2.222-1.01 3.902-1.01.613 0 2.886.06 4.374 2.19-.13.09-2.383 1.37-2.383 4.19 0 3.26 2.854 4.42 2.955 4.45z"
        />
      </svg>
    );
  }
  if (platform === "mac-x64") {
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <g
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
        >
          <rect x="6" y="6" width="12" height="12" rx="2" />
          <rect x="10" y="10" width="4" height="4" rx="1" />
          <path d="M9 3v3M12 3v3M15 3v3M9 18v3M12 18v3M15 18v3M3 9h3M3 12h3M3 15h3M18 9h3M18 12h3M18 15h3" />
        </g>
      </svg>
    );
  }
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill="currentColor"
        d="M3 5.6 10.6 4.5v6.9H3V5.6Zm0 12.8 7.6 1.1v-6.9H3v5.8ZM11.6 4.4 21 3.1v8.3h-9.4V4.4Zm0 8.4H21v8.3l-9.4-1.3v-7Z"
      />
    </svg>
  );
}

export function ClientDownloadModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [info, setInfo] = useState<ClientDownloadInfo | null>(null);
  const [started, setStarted] = useState<ClientPlatform | null>(null);

  /** 只标记不会猜错的那种：Windows 只有一种安装包；Mac 需要用户自己看芯片。 */
  const currentOptionId: ClientPlatform | null =
    detectClientOs() === "win" ? "win" : null;

  useEffect(() => {
    if (!open) {
      setStarted(null);
      return;
    }
    let cancelled = false;
    setInfo(null);
    void fetchClientDownloadInfo().then((next) => {
      if (!cancelled) setInfo(next);
    });
    return () => {
      cancelled = true;
    };
  }, [open]);

  function startDownload(platform: ClientPlatform) {
    setStarted(platform);
    window.location.assign(clientDownloadUrl(platform));
  }

  const meta = info
    ? [
        t("nav.downloadMetaVersion", { version: info.version }),
        info.date,
        info.sizeMb != null
          ? t("nav.downloadMetaSize", { size: info.sizeMb })
          : "",
      ]
        .filter(Boolean)
        .join(" · ")
    : "";

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title={t("nav.downloadClient")}
      width={460}
      centered
      footer={<Button onClick={onClose}>{t("common.close")}</Button>}
    >
      <p className={styles.downloadChoose}>{t("nav.downloadChoose")}</p>
      {meta ? <p className={styles.downloadMeta}>{meta}</p> : null}

      <div className={styles.downloadOptions}>
        {CLIENT_PLATFORM_OPTIONS.map((option) => (
          <Fragment key={option.id}>
            <div
              className={styles.downloadOption}
              role="button"
              tabIndex={0}
              onClick={() => startDownload(option.id)}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  startDownload(option.id);
                }
              }}
            >
              <span className={styles.downloadOptionIcon}>
                <PlatformIcon platform={option.id} />
              </span>
              <span className={styles.downloadOptionText}>
                <span className={styles.downloadOptionTitle}>
                  {t(option.titleKey)}
                  {option.id === currentOptionId ? (
                    <span className={styles.downloadOptionBadge}>
                      {t("nav.downloadCurrent")}
                    </span>
                  ) : null}
                </span>
                <span className={styles.downloadOptionSub}>
                  {t(option.subKey)}
                </span>
              </span>
              <span className={styles.downloadOptionAction}>
                {started === option.id ? (
                  <>
                    <Check size={14} strokeWidth={2} />
                    <span>{t("nav.downloadStarted")}</span>
                    <button
                      type="button"
                      className={styles.downloadRetry}
                      onClick={(event) => {
                        event.stopPropagation();
                        startDownload(option.id);
                      }}
                    >
                      {t("nav.downloadRetry")}
                    </button>
                  </>
                ) : (
                  <>
                    <Download size={14} strokeWidth={1.8} />
                    <span>{t("nav.downloadAction")}</span>
                  </>
                )}
              </span>
            </div>
            {option.id === "mac-x64" ? (
              <p className={styles.downloadMacHint}>
                {t("nav.downloadMacHintBefore")}
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path
                    fill="currentColor"
                    d="M16.365 1.43c0 1.14-.493 2.27-1.177 3.08-.744.9-1.99 1.57-2.987 1.57-.12 0-.23-.02-.3-.03-.01-.06-.04-.22-.04-.39 0-1.15.572-2.27 1.206-2.98.804-.94 2.142-1.64 3.248-1.68.03.13.05.28.05.43zm4.565 15.71c-.03.07-.463 1.58-1.518 3.12-.945 1.34-1.94 2.71-3.43 2.71-1.517 0-1.9-.88-3.63-.88-1.698 0-2.302.91-3.67.91-1.377 0-2.332-1.26-3.428-2.8-1.287-1.82-2.323-4.63-2.323-7.28 0-4.28 2.797-6.55 5.552-6.55 1.448 0 2.675.95 3.6.95.865 0 2.222-1.01 3.902-1.01.613 0 2.886.06 4.374 2.19-.13.09-2.383 1.37-2.383 4.19 0 3.26 2.854 4.42 2.955 4.45z"
                  />
                </svg>
                {t("nav.downloadMacHintAfter")}
              </p>
            ) : null}
          </Fragment>
        ))}
      </div>
    </Modal>
  );
}

export default function ClientDownloadLink({ compact }: { compact?: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const label = t("nav.downloadClient");

  return (
    <>
      {compact ? (
        <Tooltip title={label} placement="right" mouseEnterDelay={0.2}>
          <button
            type="button"
            className={styles.downloadButtonCompact}
            aria-label={label}
            onClick={() => setOpen(true)}
          >
            <Download size={16} strokeWidth={1.8} />
          </button>
        </Tooltip>
      ) : (
        <button
          type="button"
          className={styles.downloadButton}
          onClick={() => setOpen(true)}
        >
          <span className={styles.downloadButtonIcon}>
            <Download size={16} strokeWidth={1.8} />
          </span>
          <span className={styles.middleOfficeText}>{label}</span>
        </button>
      )}
      <ClientDownloadModal open={open} onClose={() => setOpen(false)} />
    </>
  );
}
