import { Button, Result } from "antd";
import { useTranslation } from "react-i18next";
import { message } from "@/utils/antdMessage";
import { OctopEmptyMascot } from "../EmptyState";
import { saveBlobAsFile } from "../../utils/saveBlobAsFile";
import {
  formatBootDiagnosticsText,
  type BootDiagnostics,
} from "../../utils/bootDiagnostics";
import styles from "./BootOfflinePanel.module.less";

interface BootOfflinePanelProps {
  /** Defaults to a full page reload. */
  onRetry?: () => void;
  diagnostics?: BootDiagnostics | null;
}

/**
 * Full-viewport panel when startup health / setup / auth probes cannot reach
 * the API (offline or backend still initializing). Prefer this over a blank
 * spinner shell. Test-phase diagnostics stay visible for installed clients.
 */
export default function BootOfflinePanel({
  onRetry,
  diagnostics,
}: BootOfflinePanelProps) {
  const { t } = useTranslation();
  const initializing = diagnostics?.phase === "initializing";

  const handleRetry = () => {
    if (onRetry) {
      onRetry();
      return;
    }
    window.location.reload();
  };

  const diagText = diagnostics ? formatBootDiagnosticsText(diagnostics) : "";

  const copyDiag = async () => {
    if (!diagText) return;
    try {
      await navigator.clipboard.writeText(diagText);
    } catch {
      /* ignore */
    }
  };

  /** client-boot.jsonl (rotated + live) as a file the user can send us. */
  const exportDiag = async () => {
    try {
      const res = await fetch("/api/client-log/export", { cache: "no-store" });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      await saveBlobAsFile(await res.blob(), "octop-client-boot.jsonl");
    } catch {
      message.error(t("errors.exportDiagnosticsFailed"));
    }
  };

  return (
    <div role="alert" className={styles.wrap}>
      <Result
        className={styles.result}
        icon={<OctopEmptyMascot />}
        title={
          initializing
            ? t("errors.startingLocalTitle")
            : t("errors.offlineTitle")
        }
        subTitle={
          initializing
            ? t("errors.startingLocalSubtitle")
            : t("errors.offlineSubtitle")
        }
        extra={
          <div className={styles.extra}>
            <Button type="primary" onClick={handleRetry}>
              {t("errors.retry")}
            </Button>
            {diagText ? (
              <div className={styles.diag}>
                <div className={styles.diagHeader}>
                  <span>{t("errors.offlineDiagnostics")}</span>
                  <span className={styles.diagActions}>
                    <Button
                      size="small"
                      type="link"
                      onClick={() => void copyDiag()}
                    >
                      {t("errors.copyDiagnostics")}
                    </Button>
                    <Button
                      size="small"
                      type="link"
                      onClick={() => void exportDiag()}
                    >
                      {t("errors.exportDiagnostics")}
                    </Button>
                  </span>
                </div>
                <pre className={styles.diagBody}>{diagText}</pre>
              </div>
            ) : null}
          </div>
        }
      />
    </div>
  );
}
