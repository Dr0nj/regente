/**
 * NotificationSettings — Phase 12
 *
 * Floating panel for configuring external notification channels
 * (Slack, Discord, generic webhook). Supports add/edit/remove/test.
 */

import { useState, useCallback } from "react";
import {
  X,
  Plus,
  Trash2,
  Send,
  Check,
  AlertCircle,
  Loader2,
  Settings2,
  ToggleLeft,
  ToggleRight,
} from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { Button } from "@/components/ui/button";
import {
  type ChannelConfig,
  type ChannelType,
  CHANNEL_TYPE_META,
  dispatchToChannel,
  createTestEvent,
} from "@/lib/notification-channels";
import {
  getNotificationChannels,
  saveNotificationChannels,
  generateChannelId,
} from "@/lib/notification-settings";
import { cn } from "@/lib/utils";

interface NotificationSettingsProps {
  onClose: () => void;
}

type TestStatus = "idle" | "sending" | "success" | "error";

const EMPTY_CHANNEL: Omit<ChannelConfig, "id"> = {
  type: "slack",
  name: "",
  enabled: true,
  webhookUrl: "",
  messageTemplate: "",
  rateLimitMs: 60_000,
};

export default function NotificationSettings({ onClose }: NotificationSettingsProps) {
  const [channels, setChannels] = useState<ChannelConfig[]>(() => getNotificationChannels());
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState<Omit<ChannelConfig, "id">>(EMPTY_CHANNEL);
  const [showAddForm, setShowAddForm] = useState(false);
  const [testStatuses, setTestStatuses] = useState<Record<string, TestStatus>>({});

  const persist = useCallback((updated: ChannelConfig[]) => {
    setChannels(updated);
    saveNotificationChannels(updated);
  }, []);

  const handleAdd = useCallback(() => {
    if (!draft.name.trim() || !draft.webhookUrl.trim()) return;
    const newChannel: ChannelConfig = {
      ...draft,
      id: generateChannelId(),
      name: draft.name.trim(),
      webhookUrl: draft.webhookUrl.trim(),
      messageTemplate: draft.messageTemplate?.trim() || undefined,
    };
    persist([...channels, newChannel]);
    setDraft(EMPTY_CHANNEL);
    setShowAddForm(false);
  }, [draft, channels, persist]);

  const handleUpdate = useCallback(() => {
    if (!editingId) return;
    const updated = channels.map((c) =>
      c.id === editingId
        ? {
            ...c,
            ...draft,
            name: draft.name.trim(),
            webhookUrl: draft.webhookUrl.trim(),
            messageTemplate: draft.messageTemplate?.trim() || undefined,
          }
        : c,
    );
    persist(updated);
    setEditingId(null);
    setDraft(EMPTY_CHANNEL);
  }, [editingId, draft, channels, persist]);

  const handleRemove = useCallback(
    (id: string) => {
      persist(channels.filter((c) => c.id !== id));
      if (editingId === id) {
        setEditingId(null);
        setDraft(EMPTY_CHANNEL);
      }
    },
    [channels, editingId, persist],
  );

  const handleToggle = useCallback(
    (id: string) => {
      persist(channels.map((c) => (c.id === id ? { ...c, enabled: !c.enabled } : c)));
    },
    [channels, persist],
  );

  const handleEdit = useCallback((ch: ChannelConfig) => {
    setEditingId(ch.id);
    setDraft({
      type: ch.type,
      name: ch.name,
      enabled: ch.enabled,
      webhookUrl: ch.webhookUrl,
      messageTemplate: ch.messageTemplate ?? "",
      rateLimitMs: ch.rateLimitMs,
    });
    setShowAddForm(false);
  }, []);

  const handleTest = useCallback(
    async (ch: ChannelConfig) => {
      setTestStatuses((prev) => ({ ...prev, [ch.id]: "sending" }));
      try {
        const result = await dispatchToChannel(
          { ...ch, rateLimitMs: 0 }, // bypass rate limit for test
          createTestEvent(),
        );
        setTestStatuses((prev) => ({
          ...prev,
          [ch.id]: result?.success ? "success" : "error",
        }));
      } catch {
        setTestStatuses((prev) => ({ ...prev, [ch.id]: "error" }));
      }
      // Reset after 3s
      setTimeout(() => {
        setTestStatuses((prev) => ({ ...prev, [ch.id]: "idle" }));
      }, 3000);
    },
    [],
  );

  const cancelForm = useCallback(() => {
    setShowAddForm(false);
    setEditingId(null);
    setDraft(EMPTY_CHANNEL);
  }, []);

  return (
    <motion.div
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 10 }}
      className="absolute right-4 top-16 z-50 w-[420px] max-h-[calc(100vh-120px)] overflow-auto rounded-xl border border-white/[0.06] bg-bg-surface/95 backdrop-blur-2xl shadow-2xl"
    >
      {/* Header */}
      <div className="sticky top-0 z-10 flex items-center justify-between border-b border-white/[0.06] bg-bg-surface/95 backdrop-blur-xl px-4 py-3">
        <div className="flex items-center gap-2">
          <Settings2 className="h-4 w-4 text-violet-400" />
          <h3 className="text-sm font-semibold text-text-primary">
            Notification Channels
          </h3>
          <span className="rounded-full bg-white/[0.06] px-2 py-0.5 text-[10px] text-text-muted">
            {channels.filter((c) => c.enabled).length}/{channels.length} active
          </span>
        </div>
        <Button variant="ghost" size="icon-sm" onClick={onClose}>
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="p-4 space-y-3">
        {/* Channel list */}
        {channels.length === 0 && !showAddForm && (
          <div className="text-center py-8 text-text-muted text-xs">
            No notification channels configured.
            <br />
            Add a channel to receive alerts via Slack, Discord, or webhooks.
          </div>
        )}

        <AnimatePresence mode="popLayout">
          {channels.map((ch) => (
            <motion.div
              key={ch.id}
              layout
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              className={cn(
                "rounded-lg border p-3 space-y-2 transition-colors",
                ch.enabled
                  ? "border-white/[0.08] bg-white/[0.02]"
                  : "border-white/[0.04] bg-white/[0.01] opacity-60",
              )}
            >
              {editingId === ch.id ? (
                /* Inline edit form */
                <ChannelForm
                  draft={draft}
                  setDraft={setDraft}
                  onSubmit={handleUpdate}
                  onCancel={cancelForm}
                  submitLabel="Save"
                />
              ) : (
                /* Display mode */
                <>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <span className={cn("text-xs font-bold uppercase", CHANNEL_TYPE_META[ch.type].color)}>
                        {CHANNEL_TYPE_META[ch.type].label}
                      </span>
                      <span className="text-sm font-medium text-text-primary">{ch.name}</span>
                    </div>
                    <div className="flex items-center gap-1">
                      <TestButton status={testStatuses[ch.id] ?? "idle"} onClick={() => handleTest(ch)} />
                      <Button variant="ghost" size="icon-sm" onClick={() => handleToggle(ch.id)} title={ch.enabled ? "Disable" : "Enable"}>
                        {ch.enabled ? (
                          <ToggleRight className="h-4 w-4 text-emerald-400" />
                        ) : (
                          <ToggleLeft className="h-4 w-4 text-text-muted" />
                        )}
                      </Button>
                      <Button variant="ghost" size="icon-sm" onClick={() => handleEdit(ch)} title="Edit">
                        <Settings2 className="h-3 w-3" />
                      </Button>
                      <Button variant="ghost" size="icon-sm" onClick={() => handleRemove(ch.id)} title="Remove" className="hover:text-red-400">
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </div>
                  </div>
                  <div className="text-[11px] text-text-muted truncate font-mono">
                    {maskUrl(ch.webhookUrl)}
                  </div>
                  {ch.messageTemplate && (
                    <div className="text-[10px] text-text-muted/60 truncate">
                      Template: {ch.messageTemplate}
                    </div>
                  )}
                </>
              )}
            </motion.div>
          ))}
        </AnimatePresence>

        {/* Add form */}
        {showAddForm && (
          <motion.div
            initial={{ opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            className="rounded-lg border border-violet-500/20 bg-violet-500/5 p-3"
          >
            <ChannelForm
              draft={draft}
              setDraft={setDraft}
              onSubmit={handleAdd}
              onCancel={cancelForm}
              submitLabel="Add Channel"
            />
          </motion.div>
        )}

        {/* Add button */}
        {!showAddForm && !editingId && (
          <Button
            variant="ghost"
            size="sm"
            className="w-full gap-1.5 text-violet-400 hover:text-violet-300 border border-dashed border-white/[0.06] hover:border-violet-500/30"
            onClick={() => {
              setDraft(EMPTY_CHANNEL);
              setShowAddForm(true);
            }}
          >
            <Plus className="h-3.5 w-3.5" />
            Add Channel
          </Button>
        )}

        {/* Template help */}
        <div className="rounded-lg bg-white/[0.02] border border-white/[0.04] p-3 space-y-1">
          <p className="text-[10px] font-semibold text-text-muted uppercase tracking-wider">
            Template Variables
          </p>
          <p className="text-[10px] text-text-muted/70 leading-relaxed font-mono">
            {"{{severity}} {{ruleName}} {{message}} {{workflowName}} {{workflowId}} {{timestamp}}"}
          </p>
        </div>
      </div>
    </motion.div>
  );
}

/* ── Sub-components ── */

interface ChannelFormProps {
  draft: Omit<ChannelConfig, "id">;
  setDraft: (fn: Omit<ChannelConfig, "id"> | ((prev: Omit<ChannelConfig, "id">) => Omit<ChannelConfig, "id">)) => void;
  onSubmit: () => void;
  onCancel: () => void;
  submitLabel: string;
}

function ChannelForm({ draft, setDraft, onSubmit, onCancel, submitLabel }: ChannelFormProps) {
  return (
    <div className="space-y-2.5">
      {/* Type select */}
      <div className="flex gap-1.5">
        {(Object.keys(CHANNEL_TYPE_META) as ChannelType[]).map((type) => (
          <button
            key={type}
            onClick={() => setDraft((d) => ({ ...d, type }))}
            className={cn(
              "flex-1 rounded-md px-2 py-1.5 text-[11px] font-semibold transition-all border",
              draft.type === type
                ? "bg-white/[0.06] border-white/[0.12] text-text-primary"
                : "border-transparent text-text-muted hover:text-text-secondary hover:bg-white/[0.03]",
            )}
          >
            {CHANNEL_TYPE_META[type].label}
          </button>
        ))}
      </div>

      {/* Name */}
      <input
        type="text"
        value={draft.name}
        onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
        placeholder="Channel name"
        className="w-full rounded-md border border-white/[0.06] bg-white/[0.03] px-3 py-1.5 text-xs text-text-primary placeholder:text-text-muted/50 outline-none focus:border-violet-500/40 focus:ring-1 focus:ring-violet-500/20"
      />

      {/* Webhook URL */}
      <input
        type="url"
        value={draft.webhookUrl}
        onChange={(e) => setDraft((d) => ({ ...d, webhookUrl: e.target.value }))}
        placeholder={CHANNEL_TYPE_META[draft.type].placeholder}
        className="w-full rounded-md border border-white/[0.06] bg-white/[0.03] px-3 py-1.5 text-xs text-text-primary placeholder:text-text-muted/50 outline-none focus:border-violet-500/40 focus:ring-1 focus:ring-violet-500/20 font-mono"
      />

      {/* Message template (optional) */}
      <textarea
        value={draft.messageTemplate ?? ""}
        onChange={(e) => setDraft((d) => ({ ...d, messageTemplate: e.target.value }))}
        placeholder="Custom message template (optional) — uses {{variables}}"
        rows={2}
        className="w-full rounded-md border border-white/[0.06] bg-white/[0.03] px-3 py-1.5 text-xs text-text-primary placeholder:text-text-muted/50 outline-none focus:border-violet-500/40 focus:ring-1 focus:ring-violet-500/20 resize-none font-mono"
      />

      {/* Rate limit */}
      <div className="flex items-center gap-2">
        <label className="text-[10px] text-text-muted whitespace-nowrap">Rate limit:</label>
        <select
          value={draft.rateLimitMs}
          onChange={(e) => setDraft((d) => ({ ...d, rateLimitMs: Number(e.target.value) }))}
          className="flex-1 rounded-md border border-white/[0.06] bg-white/[0.03] px-2 py-1 text-[11px] text-text-primary outline-none"
        >
          <option value={0}>No limit</option>
          <option value={30000}>30 seconds</option>
          <option value={60000}>1 minute</option>
          <option value={300000}>5 minutes</option>
          <option value={600000}>10 minutes</option>
        </select>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2 pt-1">
        <Button size="sm" className="flex-1 gap-1.5" onClick={onSubmit} disabled={!draft.name.trim() || !draft.webhookUrl.trim()}>
          <Check className="h-3 w-3" />
          {submitLabel}
        </Button>
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </div>
  );
}

function TestButton({ status, onClick }: { status: TestStatus; onClick: () => void }) {
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      onClick={onClick}
      disabled={status === "sending"}
      title="Send test notification"
      className={cn(
        status === "success" && "text-emerald-400",
        status === "error" && "text-red-400",
      )}
    >
      {status === "sending" && <Loader2 className="h-3 w-3 animate-spin" />}
      {status === "success" && <Check className="h-3 w-3" />}
      {status === "error" && <AlertCircle className="h-3 w-3" />}
      {status === "idle" && <Send className="h-3 w-3" />}
    </Button>
  );
}

/** Mask webhook URL for display (show first+last parts) */
function maskUrl(url: string): string {
  try {
    const u = new URL(url);
    const path = u.pathname;
    if (path.length > 20) {
      return `${u.origin}/${path.slice(1, 8)}...${path.slice(-8)}`;
    }
    return url;
  } catch {
    return url.length > 40 ? `${url.slice(0, 20)}...${url.slice(-10)}` : url;
  }
}
