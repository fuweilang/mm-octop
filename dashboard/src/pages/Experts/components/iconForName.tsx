/**
 * iconForName — map an expert's ``icon_name`` (a free-form string from the
 * manifest, e.g. ``"cpu"``, ``"trending-up"``, ``"baby"``) to a lucide-react
 * icon node. Returns a sensible default (``Layers``) when the key is missing
 * or unknown so cards never render blank.
 *
 * Ported from finnie's SceneTabContent + Personalization page so the octop
 * Experts page presents the same colourful, themed iconography.
 */

import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import {
  Sparkles,
  Globe,
  BookOpen,
  User,
  Rocket,
  Fingerprint,
  FileText,
  Video,
  Palette,
  TrendingUp,
  PenTool,
  CandlestickChart,
  Home,
  Baby,
  MessageSquare,
  Cpu,
  Server,
  Wrench,
  Heart,
  Layers,
  Mail,
  Zap,
  Terminal,
  ListTodo,
  Presentation,
  Activity,
  HardDrive,
  Bell,
  BarChart3,
  Network,
  ShieldCheck,
  RefreshCw,
  Utensils,
  Coffee,
  ShoppingBag,
  Users,
  Cloudy,
  Cloud,
  CloudCog,
  Laptop,
  Monitor,
  Wifi,
  RadioTower,
  Satellite,
  MapPin,
  Puzzle,
  Shield,
  BookOpenCheck,
  Briefcase,
  Code2,
} from "lucide-react";
import { useAuthImageSrc } from "../../../hooks/useAuthImageSrc";
import { needsAuthBlobFetch } from "../../../utils/toolMediaBlocks";

const ICON_STROKE = 1.75;

const iconMap: Record<string, (size: number) => ReactNode> = {
  sparkles: (size) => <Sparkles size={size} strokeWidth={ICON_STROKE} />,
  globe: (size) => <Globe size={size} strokeWidth={ICON_STROKE} />,
  "book-open": (size) => <BookOpen size={size} strokeWidth={ICON_STROKE} />,
  "book-open-check": (size) => (
    <BookOpenCheck size={size} strokeWidth={ICON_STROKE} />
  ),
  user: (size) => <User size={size} strokeWidth={ICON_STROKE} />,
  users: (size) => <Users size={size} strokeWidth={ICON_STROKE} />,
  rocket: (size) => <Rocket size={size} strokeWidth={ICON_STROKE} />,
  fingerprint: (size) => <Fingerprint size={size} strokeWidth={ICON_STROKE} />,
  "file-text": (size) => <FileText size={size} strokeWidth={ICON_STROKE} />,
  video: (size) => <Video size={size} strokeWidth={ICON_STROKE} />,
  palette: (size) => <Palette size={size} strokeWidth={ICON_STROKE} />,
  "trending-up": (size) => <TrendingUp size={size} strokeWidth={ICON_STROKE} />,
  "pen-tool": (size) => <PenTool size={size} strokeWidth={ICON_STROKE} />,
  "candlestick-chart": (size) => (
    <CandlestickChart size={size} strokeWidth={ICON_STROKE} />
  ),
  home: (size) => <Home size={size} strokeWidth={ICON_STROKE} />,
  baby: (size) => <Baby size={size} strokeWidth={ICON_STROKE} />,
  "message-square": (size) => (
    <MessageSquare size={size} strokeWidth={ICON_STROKE} />
  ),
  cpu: (size) => <Cpu size={size} strokeWidth={ICON_STROKE} />,
  code: (size) => <Code2 size={size} strokeWidth={ICON_STROKE} />,
  server: (size) => <Server size={size} strokeWidth={ICON_STROKE} />,
  wrench: (size) => <Wrench size={size} strokeWidth={ICON_STROKE} />,
  heart: (size) => <Heart size={size} strokeWidth={ICON_STROKE} />,
  mail: (size) => <Mail size={size} strokeWidth={ICON_STROKE} />,
  zap: (size) => <Zap size={size} strokeWidth={ICON_STROKE} />,
  terminal: (size) => <Terminal size={size} strokeWidth={ICON_STROKE} />,
  "list-todo": (size) => <ListTodo size={size} strokeWidth={ICON_STROKE} />,
  presentation: (size) => <Presentation size={size} strokeWidth={ICON_STROKE} />,
  activity: (size) => <Activity size={size} strokeWidth={ICON_STROKE} />,
  "hard-drive": (size) => <HardDrive size={size} strokeWidth={ICON_STROKE} />,
  "bar-chart-3": (size) => <BarChart3 size={size} strokeWidth={ICON_STROKE} />,
  network: (size) => <Network size={size} strokeWidth={ICON_STROKE} />,
  shield: (size) => <Shield size={size} strokeWidth={ICON_STROKE} />,
  "shield-check": (size) => <ShieldCheck size={size} strokeWidth={ICON_STROKE} />,
  puzzle: (size) => <Puzzle size={size} strokeWidth={ICON_STROKE} />,
  briefcase: (size) => <Briefcase size={size} strokeWidth={ICON_STROKE} />,
  "refresh-cw": (size) => <RefreshCw size={size} strokeWidth={ICON_STROKE} />,
  utensils: (size) => <Utensils size={size} strokeWidth={ICON_STROKE} />,
  bell: (size) => <Bell size={size} strokeWidth={ICON_STROKE} />,
  coffee: (size) => <Coffee size={size} strokeWidth={ICON_STROKE} />,
  "shopping-bag": (size) => <ShoppingBag size={size} strokeWidth={ICON_STROKE} />,
  cloudy: (size) => <Cloudy size={size} strokeWidth={ICON_STROKE} />,
  cloud: (size) => <Cloud size={size} strokeWidth={ICON_STROKE} />,
  "cloud-cog": (size) => <CloudCog size={size} strokeWidth={ICON_STROKE} />,
  laptop: (size) => <Laptop size={size} strokeWidth={ICON_STROKE} />,
  monitor: (size) => <Monitor size={size} strokeWidth={ICON_STROKE} />,
  wifi: (size) => <Wifi size={size} strokeWidth={ICON_STROKE} />,
  "radio-tower": (size) => <RadioTower size={size} strokeWidth={ICON_STROKE} />,
  satellite: (size) => <Satellite size={size} strokeWidth={ICON_STROKE} />,
  "map-pin": (size) => <MapPin size={size} strokeWidth={ICON_STROKE} />,
};

export const EXPERT_ICON_NAMES = Object.keys(iconMap);

export function iconForName(
  name: string | null | undefined,
  size = 22,
): ReactNode {
  if (!name) return <Layers size={size} strokeWidth={ICON_STROKE} />;
  const fn = iconMap[name];
  return fn ? fn(size) : <Layers size={size} strokeWidth={ICON_STROKE} />;
}

/** Prefer a remote ``icon_url`` (SkillHub / market) or uploaded avatar over Lucide. */
export function ExpertIcon({
  iconUrl,
  iconName,
  size = 22,
  className,
}: {
  iconUrl?: string | null;
  iconName?: string | null;
  size?: number;
  className?: string;
}): ReactNode {
  const url = resolveExpertAvatarUrl(iconUrl);
  if (url) {
    return (
      <ExpertIconImage
        url={url}
        iconName={iconName}
        size={size}
        className={className}
      />
    );
  }
  return iconForName(iconName, size);
}

/**
 * Encode the agent-id segment in ``/api/agents/{id}/…`` URLs.
 * Bridge shadow ids contain ``:`` and must match chat WS / threads encoding.
 */
export function normalizeAgentScopedApiUrl(url: string): string {
  const trimmed = url.trim();
  if (!trimmed) return trimmed;
  try {
    const abs = trimmed.startsWith("http")
      ? new URL(trimmed)
      : new URL(trimmed, "http://octop.local");
    const match = abs.pathname.match(/^(\/api\/agents\/)([^/]+)(\/.*)?$/);
    if (!match) return trimmed;
    let agentId = match[2];
    try {
      agentId = decodeURIComponent(agentId);
    } catch {
      /* keep raw */
    }
    const path = `${match[1]}${encodeURIComponent(agentId)}${match[3] || ""}`;
    if (trimmed.startsWith("http")) {
      abs.pathname = path;
      return abs.toString();
    }
    return `${path}${abs.search}${abs.hash}`;
  } catch {
    return trimmed;
  }
}

// MAMMOTH-PATCH: /experts/ 头像 URL 前缀 BASE_URL（经网关部署时仪表盘不在根路径），连带 resolveExpertAvatarUrl 的 /experts/ 分支 C类-功能侵入
function withDashboardBase(path: string): string {
  const base = String(import.meta.env.BASE_URL || "/").replace(/\/$/, "");
  if (!base) return path;
  if (path === base || path.startsWith(`${base}/`)) return path;
  return `${base}${path}`;
}

/** Trust the API ``icon_url``; do not invent scene portraits on the client. */
export function resolveExpertAvatarUrl(iconUrl?: string | null): string | null {
  const url = iconUrl?.trim() || "";
  if (!url) return null;
  if (url.startsWith("/api/agents/")) {
    return normalizeAgentScopedApiUrl(url);
  }
  if (url.startsWith("/experts/")) {
    return withDashboardBase(url);
  }
  return url;
}

function ExpertIconImage({
  url,
  iconName,
  size,
  className,
}: {
  url: string;
  iconName?: string | null;
  size: number;
  className?: string;
}) {
  const { src, loadState } = useAuthImageSrc(url);
  const [imgFailed, setImgFailed] = useState(false);
  useEffect(() => {
    setImgFailed(false);
  }, [url]);
  if (!src || loadState !== "ready" || imgFailed) {
    return iconForName(iconName, Math.min(size, 20));
  }
  const cover =
    needsAuthBlobFetch(url) ||
    url.startsWith("blob:") ||
    url.includes("/experts/avatars/");
  return (
    <img
      src={src}
      alt=""
      className={className}
      onError={() => setImgFailed(true)}
      style={{
        width: size,
        height: size,
        objectFit: cover ? "cover" : "contain",
        display: "block",
        borderRadius: "inherit",
      }}
    />
  );
}

/**
 * Per-file icon + colour metadata, keyed by canonical filenames (SOUL.md,
 * IDENTITY.md, …). Mirrors finnie's ``CARD_META`` so the file list shows
 * the same colour-coded badges users already recognise from the
 * Personalization page.
 *
 * Unknown filenames render a neutral ``FileText`` badge.
 */
export interface FileMeta {
  icon: ReactNode;
  color: string;
  label: string;
}

type Translator = (key: string) => string;

const FILE_META_MAP: Record<
  string,
  { icon: ReactNode; color: string; labelKey: string }
> = {
  "SOUL.md": {
    icon: <Sparkles size={20} />,
    color: "#7c3aed",
    labelKey: "experts.fileLabel.soul",
  },
  "IDENTITY.md": {
    icon: <Fingerprint size={20} />,
    color: "#db2777",
    labelKey: "experts.fileLabel.identity",
  },
  "USER.md": {
    icon: <User size={20} />,
    color: "#2563eb",
    labelKey: "experts.fileLabel.user",
  },
  "AGENTS.md": {
    icon: <BookOpen size={20} />,
    color: "#059669",
    labelKey: "experts.fileLabel.agents",
  },
  "TOOLS.md": {
    icon: <Wrench size={20} />,
    color: "#f59e0b",
    labelKey: "experts.fileLabel.tools",
  },
  "BOOTSTRAP.md": {
    icon: <Rocket size={20} />,
    color: "#d97706",
    labelKey: "experts.fileLabel.bootstrap",
  },
  "HEARTBEAT.md": {
    icon: <Heart size={20} />,
    color: "#ef4444",
    labelKey: "experts.fileLabel.heartbeat",
  },
  "SKILL.md": {
    icon: <Zap size={20} />,
    color: "#8b5cf6",
    labelKey: "experts.fileLabel.skill",
  },
  "MEMORY.md": {
    icon: <Layers size={20} />,
    color: "#0891b2",
    labelKey: "experts.fileLabel.memory",
  },
  "PROACTIVE.md": {
    icon: <Bell size={20} />,
    color: "#f97316",
    labelKey: "experts.fileLabel.proactive",
  },
};

export function metaForFile(
  path: string,
  t: Translator,
  fallbackColor?: string | null,
): FileMeta {
  const hit = FILE_META_MAP[path];
  if (hit) return { icon: hit.icon, color: hit.color, label: t(hit.labelKey) };
  return {
    icon: <FileText size={20} />,
    color: fallbackColor || "#6366f1",
    label: path.replace(/\.md$/i, ""),
  };
}
