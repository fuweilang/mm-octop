import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button, Result, Spin } from "antd";
import { setAuthToken } from "../../api";
import { authApi } from "../../api/modules/auth";
import { refreshServerLabels } from "../../i18n";
import { applyUserLocale } from "../../utils/locale";
import {
  ensurePlatformOrigin,
  MammothLoginUnreachableError,
  shouldUseLocalLogin,
  startMammothSession,
} from "../../utils/mammothSso";
import styles from "./DesktopLogin.module.less";

type Mode = "loading" | "form" | "redirect" | "unreachable";

function errorText(error: unknown, registering: boolean): string {
  const text = error instanceof Error ? error.message : "";
  if (text.includes("phone_taken") || text.includes("Request failed: 409")) {
    return "手机号已注册";
  }
  if (text.includes("bad_credentials") || text.includes("Request failed: 400")) {
    return registering ? "注册失败，请检查填写内容" : "手机号或密码不正确";
  }
  if (text.includes("unreachable") || text.includes("Request failed: 502")) {
    return "登录服务没有响应，请稍后再试";
  }
  return registering ? "注册没有完成，请重试" : "登录没有完成，请重试";
}

export default function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [mode, setMode] = useState<Mode>("loading");
  const [origin, setOrigin] = useState("");
  const [accountMode, setAccountMode] = useState<"password" | "register">("password");
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [enterpriseName, setEnterpriseName] = useState("");
  const [password, setPassword] = useState("");
  const [agree, setAgree] = useState(true);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const registering = accountMode === "register";

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const platform = await ensurePlatformOrigin();
      if (cancelled) return;
      if (!shouldUseLocalLogin(platform, window.location.origin)) {
        setMode("redirect");
        try {
          await startMammothSession();
        } catch (caught) {
          if (cancelled) return;
          setOrigin(caught instanceof MammothLoginUnreachableError ? caught.origin : "");
          setMode("unreachable");
        }
        return;
      }
      setMode("form");
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (submitting || !agree) return;
    const cleanedPhone = phone.replace(/[^\d]/g, "").slice(0, 11);
    if (!/^1\d{10}$/.test(cleanedPhone)) {
      setError("请输入正确的手机号");
      return;
    }
    if (registering && !name.trim()) {
      setError("请输入姓名");
      return;
    }
    if (registering && !enterpriseName.trim()) {
      setError("请输入企业/主体名称");
      return;
    }
    setError("");
    setSubmitting(true);
    try {
      const session = registering
        ? await authApi.desktopRegister({
            name: name.trim(),
            phone: cleanedPhone,
            password,
            enterpriseName: enterpriseName.trim(),
          })
        : await authApi.desktopLogin(cleanedPhone, password);
      setAuthToken(session.access_token, true);
      await applyUserLocale(session.user.locale);
      void refreshServerLabels(session.user.locale);
      navigate("/chat", { replace: true });
    } catch (caught) {
      setError(errorText(caught, registering));
    } finally {
      setSubmitting(false);
    }
  }

  if (mode === "form") {
    return (
      <div className={styles.page}>
        <form
          className={`${styles.card} ${registering ? styles.cardRegister : ""}`}
          onSubmit={(event) => void onSubmit(event)}
        >
          <div className={styles.head}>
            <img className={styles.logo} src="/mengma-logo.png" alt="" width={72} height={72} />
            <h2>猛犸智能体</h2>
            <p>{registering ? "注册后立即开通企业空间" : "企业用户使用手机号和密码进入系统"}</p>
          </div>
          <div className={styles.switch} role="tablist">
            <button
              type="button"
              className={accountMode === "password" ? styles.switchOn : ""}
              onClick={() => setAccountMode("password")}
            >
              密码登录
            </button>
            <button
              type="button"
              className={accountMode === "register" ? styles.switchOn : ""}
              onClick={() => setAccountMode("register")}
            >
              注册
            </button>
          </div>
          {registering ? (
            <label className={styles.field}>
              <span>姓名</span>
              <input
                value={name}
                maxLength={40}
                placeholder="请输入姓名"
                onChange={(event) => setName(event.target.value)}
              />
            </label>
          ) : null}
          <label className={styles.field}>
            <span>手机号</span>
            <input
              name="phone"
              type="tel"
              autoComplete="username"
              maxLength={11}
              placeholder="请输入手机号"
              value={phone}
              onChange={(event) => setPhone(event.target.value.replace(/[^\d]/g, "").slice(0, 11))}
            />
          </label>
          {registering ? (
            <label className={styles.field}>
              <span>企业/主体名称</span>
              <input
                value={enterpriseName}
                maxLength={160}
                placeholder="请输入企业或经营主体名称"
                onChange={(event) => setEnterpriseName(event.target.value)}
              />
            </label>
          ) : null}
          <label className={styles.field}>
            <span>{registering ? "设置密码" : "密码"}</span>
            <input
              name="password"
              type="password"
              autoComplete={registering ? "new-password" : "current-password"}
              placeholder={registering ? "请设置密码" : "请输入密码"}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          <label className={styles.agreement}>
            <input type="checkbox" checked={agree} onChange={(event) => setAgree(event.target.checked)} />
            <span>
              我已阅读并同意 <span>用户协议</span> 和 <span>隐私政策</span>
            </span>
          </label>
          {error ? <p className={styles.error}>{error}</p> : null}
          <button className={styles.submit} type="submit" disabled={submitting || !agree}>
            {registering ? "注册并进入系统" : "登录"}
          </button>
        </form>
      </div>
    );
  }

  if (mode === "unreachable") {
    return (
      <div className={styles.page}>
        <Result
          status="warning"
          title={t("errors.loginUnreachableTitle")}
          subTitle={t("errors.loginUnreachableSubtitle", {
            origin: origin || t("errors.loginUnreachableUnknown"),
          })}
          extra={
            <Button type="primary" onClick={() => window.location.reload()}>
              {t("errors.retry")}
            </Button>
          }
        />
      </div>
    );
  }

  return (
    <div className={styles.page}>
      <Spin size="large" />
    </div>
  );
}
