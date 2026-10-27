import { Button, Result, Spin, message } from "antd";
import { Settings } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { useAgent } from "../../../context/AgentContext";
import type { OctopAgent } from "../../../context/AgentContext";
import { request } from "../../../api/request";
import { apiErrorMessage } from "../../../utils/apiError";
import {
  formatAgentError,
  isAgentModelConfigError,
} from "../../../utils/agentError";
import styles from "../index.module.less";

interface AgentNotReadyScreenProps {
  agent: OctopAgent | null;
  noAgents?: boolean;
  loading?: boolean;
}

export default function AgentNotReadyScreen({
  agent,
  noAgents = false,
  loading = false,
}: AgentNotReadyScreenProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { refresh } = useAgent();
  const [bootstrapCreating, setBootstrapCreating] = useState(false);
  const [bootstrapFailed, setBootstrapFailed] = useState(false);
  const bootstrapStarted = useRef(false);

  /**
   * MAMMOTH-PATCH: 零专家冷启动——无 agent 时自动创建默认专家(general-assistant)并直接进会话，替换上游"去专家页挑选"空态 C类-功能侵入
   * Zero-expert entry: create the bundled default expert (general-assistant)
   * with template defaults, then jump straight into its chat. Users who never
   * picked an expert should not have to browse the expert list first.
   */
  const startDefaultChat = useCallback(async () => {
    setBootstrapFailed(false);
    setBootstrapCreating(true);
    try {
      const expert = await request<{ id: string }>("/experts/default");
      const created = await request<{ agent_id: string }>(
        `/agents/from-expert/${encodeURIComponent(expert.id)}`,
        { method: "POST", body: JSON.stringify({}) },
      );
      await refresh({ force: true });
      navigate(`/chat/${encodeURIComponent(created.agent_id)}`);
    } catch (err: unknown) {
      setBootstrapFailed(true);
      message.error(apiErrorMessage(err, t("experts.createFailed"), t));
    } finally {
      setBootstrapCreating(false);
    }
  }, [navigate, refresh, t]);

  useEffect(() => {
    if (!noAgents || loading || bootstrapStarted.current) return;
    bootstrapStarted.current = true;
    void startDefaultChat();
  }, [noAgents, loading, startDefaultChat]);

  if (loading) {
    return (
      <div className={styles.agentNotReady}>
        <Spin />
      </div>
    );
  }

  if (noAgents) {
    if (!bootstrapFailed) {
      return (
        <div className={styles.agentNotReady}>
          <Spin tip={t("chat.agentStarting")} />
        </div>
      );
    }
    return (
      <div className={styles.agentNotReady}>
        <Result
          status="warning"
          title={t("experts.createFailed")}
          extra={
            <>
              <Button
                type="primary"
                loading={bootstrapCreating}
                onClick={() => void startDefaultChat()}
              >
                {t("chat.startChatting")}
              </Button>
              <Button
                disabled={bootstrapCreating}
                onClick={() => navigate("/experts")}
              >
                {t("chat.createExpert")}
              </Button>
            </>
          }
        />
      </div>
    );
  }

  if (!agent) {
    return (
      <div className={styles.agentNotReady}>
        <Result status="info" title={t("chat.pickAgent")} />
      </div>
    );
  }

  const state = agent.state;
  const errorText = formatAgentError(agent.last_error, t);
  const isModelError = isAgentModelConfigError(agent.last_error);
  const disconnected = Boolean(agent.bridge && agent.bridge_disconnected);

  let title = t("chat.agentNotRunning");
  let subTitle = t("chat.agentNotRunningHint");

  if (disconnected) {
    const inbound = Boolean(agent.bridge_inbound);
    title = t("chat.remoteExpert.disconnectedTitle");
    subTitle = inbound
      ? agent.bridge_connection_name
        ? t("chat.remoteExpert.waitForPeerNamed", {
            name: agent.bridge_connection_name,
          })
        : t("chat.remoteExpert.waitForPeer")
      : agent.bridge_connection_name
      ? t("chat.remoteExpert.disconnectedHintNamed", {
          name: agent.bridge_connection_name,
        })
      : t("chat.remoteExpert.disconnectedHint");
  } else if (state === "failed") {
    title = t("chat.agentFailed");
    subTitle = errorText || t("chat.agentFailedHint");
  } else if (state === "stopped" || state === "created") {
    title = t("chat.agentNotRunning");
    subTitle = t("chat.agentNotRunningHint");
  } else if (state === "starting" || state === "stopping") {
    title = t("chat.agentStarting");
    subTitle = t("chat.agentStartingHint");
  }

  return (
    <div className={styles.agentNotReady}>
      <Result
        status={
          disconnected
            ? "warning"
            : state === "failed"
            ? "error"
            : state === "stopped" || state === "created"
            ? "warning"
            : "info"
        }
        title={title}
        subTitle={subTitle}
        extra={
          disconnected ? (
            <Button type="primary" onClick={() => navigate("/bridge")}>
              {t(
                agent.bridge_inbound
                  ? "chat.remoteExpert.openBridge"
                  : "chat.remoteExpert.reconnect",
              )}
            </Button>
          ) : isModelError ? (
            <Button
              type="primary"
              icon={<Settings size={14} />}
              onClick={() => navigate("/admin/models")}
            >
              {t("modelConfig.configureButton")}
            </Button>
          ) : (
            <Button type="primary" onClick={() => navigate("/experts")}>
              {t("chat.goToExperts")}
            </Button>
          )
        }
      />
    </div>
  );
}
