import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Building2, Check, ChevronDown } from "lucide-react";
import { Dropdown, message } from "antd";
import type { MenuProps } from "antd";
import { useCurrentUser } from "../hooks/useCurrentUser";
import styles from "./ProductSwitcher.module.less";

/** 品牌旁的产品切换器：当前在「猛犸智能体」，可切到「猛犸中台」。 */
export default function ProductSwitcher() {
  const { t } = useTranslation();
  const user = useCurrentUser();
  const [switching, setSwitching] = useState(false);

  /** 控制台得先有企业身份（SSO 登录时写入交接 token），否则切过去只会 403。 */
  const canSwitch = Boolean(user?.sso_linked);
  const agentName = t("nav.productAgent");
  const consoleName = t("nav.productConsole");

  async function openConsole() {
    if (!canSwitch || switching) return;
    setSwitching(true);
    try {
      const { openEnterpriseConsoleFromClient } = await import(
        "../utils/mammothSso"
      );
      window.open(
        await openEnterpriseConsoleFromClient(),
        "_blank",
        "noopener",
      );
    } catch {
      message.error(t("errors.consoleHandoffFailed"));
    } finally {
      setSwitching(false);
    }
  }

  const items: MenuProps["items"] = [
    {
      key: "console",
      icon: <Building2 size={16} strokeWidth={1.8} />,
      disabled: !canSwitch || switching,
      label: (
        <span className={styles.itemLabel}>
          <span>{consoleName}</span>
          {!canSwitch ? (
            <span className={styles.itemHint}>
              {t("nav.switchProductDisabled")}
            </span>
          ) : null}
        </span>
      ),
    },
    {
      key: "agent",
      icon: <Check size={16} strokeWidth={1.8} />,
      disabled: true,
      label: (
        <span className={styles.itemLabel}>
          <span className={styles.itemCurrent}>{agentName}</span>
        </span>
      ),
    },
  ];

  return (
    <Dropdown
      menu={{
        items,
        onClick: ({ key }) => {
          if (key === "console") void openConsole();
        },
      }}
      trigger={["click"]}
      placement="bottomLeft"
    >
      <button
        type="button"
        className={styles.trigger}
        aria-label={t("nav.switchProduct")}
        title={`${t("nav.switchProduct")} · ${t("nav.switchProductCurrent", {
          name: agentName,
        })}`}
      >
        <Building2 size={14} strokeWidth={1.8} />
        <ChevronDown size={12} strokeWidth={2} />
      </button>
    </Dropdown>
  );
}
