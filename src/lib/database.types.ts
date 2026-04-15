/**
 * Supabase Database types for Regente.
 *
 * These match the SQL schema in supabase/schema.sql.
 * Regenerate with: npx supabase gen types typescript --local > src/lib/database.types.ts
 */

export interface Database {
  public: {
    Tables: {
      workflows: {
        Row: {
          id: string;
          name: string;
          description: string | null;
          nodes: WorkflowNode[];
          edges: WorkflowEdge[];
          created_at: string;
          updated_at: string;
          owner_id: string | null;
        };
        Insert: Omit<Database["public"]["Tables"]["workflows"]["Row"], "id" | "created_at" | "updated_at"> & {
          id?: string;
          created_at?: string;
          updated_at?: string;
        };
        Update: Partial<Omit<Database["public"]["Tables"]["workflows"]["Row"], "id" | "created_at" | "updated_at">>;
        Relationships: [];
      };
      workflow_versions: {
        Row: {
          id: string;
          workflow_id: string;
          version: number;
          label: string;
          nodes: WorkflowNode[];
          edges: WorkflowEdge[];
          created_at: string;
        };
        Insert: Omit<Database["public"]["Tables"]["workflow_versions"]["Row"], "id" | "created_at"> & {
          id?: string;
          created_at?: string;
        };
        Update: Partial<Omit<Database["public"]["Tables"]["workflow_versions"]["Row"], "id" | "created_at">>;
        Relationships: [];
      };
      profiles: {
        Row: {
          id: string;
          email: string | null;
          display_name: string | null;
          avatar_url: string | null;
          updated_at: string;
        };
        Insert: Omit<Database["public"]["Tables"]["profiles"]["Row"], "updated_at"> & {
          updated_at?: string;
        };
        Update: Partial<Omit<Database["public"]["Tables"]["profiles"]["Row"], "id">>;
        Relationships: [];
      };
      presence: {
        Row: {
          id: string;
          user_id: string;
          workflow_id: string;
          cursor_x: number | null;
          cursor_y: number | null;
          selected_node: string | null;
          last_seen: string;
        };
        Insert: Omit<Database["public"]["Tables"]["presence"]["Row"], "id" | "last_seen"> & {
          id?: string;
          last_seen?: string;
        };
        Update: Partial<Omit<Database["public"]["Tables"]["presence"]["Row"], "id">>;
        Relationships: [];
      };
    };
    Views: Record<string, never>;
    Functions: Record<string, never>;
    Enums: Record<string, never>;
    CompositeTypes: Record<string, never>;
  };
}

/** Serialized node for storage */
export interface WorkflowNode {
  id: string;
  type: string;
  position: { x: number; y: number };
  data: {
    label: string;
    jobType: string;
    status: string;
    team?: string;
    lastRun?: string;
    schedule?: string;
    timeout?: number;
    retries?: number;
    variables?: { key: string; value: string }[];
  };
}

/** Serialized edge for storage */
export interface WorkflowEdge {
  id: string;
  source: string;
  target: string;
  animated?: boolean;
  label?: string;
  data?: Record<string, unknown>;
}
