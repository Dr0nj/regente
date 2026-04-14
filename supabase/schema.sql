-- Regente — Supabase Schema
-- Run this in the Supabase SQL Editor (https://app.supabase.com → SQL Editor)

create table if not exists public.workflows (
  id          uuid primary key default gen_random_uuid(),
  name        text not null,
  description text,
  nodes       jsonb not null default '[]'::jsonb,
  edges       jsonb not null default '[]'::jsonb,
  owner_id    uuid references auth.users(id) on delete set null,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);

-- Auto-update updated_at on row change
create or replace function public.handle_updated_at()
returns trigger as $$
begin
  new.updated_at = now();
  return new;
end;
$$ language plpgsql;

create trigger on_workflow_updated
  before update on public.workflows
  for each row execute procedure public.handle_updated_at();

-- Row Level Security (optional — enable when auth is added)
-- alter table public.workflows enable row level security;
-- create policy "Users can CRUD own workflows"
--   on public.workflows for all
--   using (auth.uid() = owner_id)
--   with check (auth.uid() = owner_id);

-- Index for faster listing
create index if not exists idx_workflows_updated on public.workflows(updated_at desc);
