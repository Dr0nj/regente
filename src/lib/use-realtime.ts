/**
 * Realtime sync hook — Phase 6
 *
 * Subscribes to Supabase Realtime changes on the workflows table.
 * When another user/tab saves a workflow, this hook triggers a reload.
 */

import { useEffect, useRef, useCallback } from "react";
import { supabase, isSupabaseConfigured } from "@/lib/supabase";

type RealtimeEvent = "INSERT" | "UPDATE" | "DELETE";

interface UseRealtimeOptions {
  table: string;
  /** Only listen for changes matching this filter (e.g. "id=eq.abc") */
  filter?: string;
  /** Callback when a change is detected */
  onInsert?: (payload: Record<string, unknown>) => void;
  onUpdate?: (payload: Record<string, unknown>) => void;
  onDelete?: (payload: Record<string, unknown>) => void;
  /** Master enable flag */
  enabled?: boolean;
}

export function useSupabaseRealtime({
  table,
  filter,
  onInsert,
  onUpdate,
  onDelete,
  enabled = true,
}: UseRealtimeOptions) {
  const channelRef = useRef<ReturnType<typeof supabase.channel> | null>(null);

  // Store callbacks in refs to avoid resubscribing on every render
  const onInsertRef = useRef(onInsert);
  const onUpdateRef = useRef(onUpdate);
  const onDeleteRef = useRef(onDelete);
  onInsertRef.current = onInsert;
  onUpdateRef.current = onUpdate;
  onDeleteRef.current = onDelete;

  useEffect(() => {
    if (!isSupabaseConfigured || !enabled) return;

    const channelName = `realtime-${table}-${filter ?? "all"}`;
    const channel = supabase
      .channel(channelName)
      .on(
        "postgres_changes" as never,
        {
          event: "*",
          schema: "public",
          table,
          ...(filter ? { filter } : {}),
        },
        (payload: { eventType: RealtimeEvent; new: Record<string, unknown>; old: Record<string, unknown> }) => {
          if (payload.eventType === "INSERT") onInsertRef.current?.(payload.new);
          else if (payload.eventType === "UPDATE") onUpdateRef.current?.(payload.new);
          else if (payload.eventType === "DELETE") onDeleteRef.current?.(payload.old);
        }
      )
      .subscribe();

    channelRef.current = channel;

    return () => {
      supabase.removeChannel(channel);
      channelRef.current = null;
    };
  }, [table, filter, enabled]);
}

/**
 * Presence hook — tracks user presence on a workflow.
 */
export function usePresence(workflowId: string | null, userId: string | null) {
  const channelRef = useRef<ReturnType<typeof supabase.channel> | null>(null);

  const updatePresence = useCallback(
    (data: { cursor_x?: number; cursor_y?: number; selected_node?: string | null }) => {
      if (!channelRef.current) return;
      channelRef.current.send({
        type: "broadcast",
        event: "presence",
        payload: { userId, ...data },
      });
    },
    [userId]
  );

  useEffect(() => {
    if (!isSupabaseConfigured || !workflowId || !userId) return;

    const channel = supabase
      .channel(`presence-${workflowId}`)
      .subscribe();

    channelRef.current = channel;

    return () => {
      supabase.removeChannel(channel);
      channelRef.current = null;
    };
  }, [workflowId, userId]);

  return { updatePresence };
}
