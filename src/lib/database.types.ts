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
  };
}

/** Serialized edge for storage */
export interface WorkflowEdge {
  id: string;
  source: string;
  target: string;
  animated?: boolean;
}
