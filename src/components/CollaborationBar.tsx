/**
 * CollaborationBar — Phase 6
 *
 * Shows a small bar of colored avatars for users currently viewing the same
 * workflow (presence). Visible only when Supabase is configured.
 */

import { motion, AnimatePresence } from "framer-motion";

export interface PresenceUser {
  userId: string;
  displayName: string;
  color: string;
}

const COLORS = [
  "#ef4444",
  "#f59e0b",
  "#10b981",
  "#3b82f6",
  "#8b5cf6",
  "#ec4899",
  "#06b6d4",
  "#f97316",
];

export function assignColor(userId: string): string {
  let hash = 0;
  for (let i = 0; i < userId.length; i++) {
    hash = (hash * 31 + userId.charCodeAt(i)) | 0;
  }
  return COLORS[Math.abs(hash) % COLORS.length];
}

function initials(name: string): string {
  return name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

interface Props {
  users: PresenceUser[];
}

export default function CollaborationBar({ users }: Props) {
  if (users.length === 0) return null;

  return (
    <div className="flex items-center gap-1">
      <AnimatePresence>
        {users.map((u) => (
          <motion.div
            key={u.userId}
            initial={{ scale: 0, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            exit={{ scale: 0, opacity: 0 }}
            transition={{ type: "spring", stiffness: 500, damping: 25 }}
            title={u.displayName}
            className="flex h-7 w-7 items-center justify-center rounded-full text-[10px] font-bold text-white ring-2 ring-gray-900"
            style={{ backgroundColor: u.color }}
          >
            {initials(u.displayName)}
          </motion.div>
        ))}
      </AnimatePresence>
      <span className="ml-1 text-xs text-gray-400">
        {users.length} online
      </span>
    </div>
  );
}
