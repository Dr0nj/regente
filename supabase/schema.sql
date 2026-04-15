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

-- Row Level Security
alter table public.workflows enable row level security;

-- Authenticated users can see their own workflows
create policy "Users can read own workflows"
  on public.workflows for select
  using (auth.uid() = owner_id);

-- Authenticated users can insert workflows as their own
create policy "Users can insert own workflows"
  on public.workflows for insert
  with check (auth.uid() = owner_id);

-- Authenticated users can update their own workflows
create policy "Users can update own workflows"
  on public.workflows for update
  using (auth.uid() = owner_id)
  with check (auth.uid() = owner_id);

-- Authenticated users can delete their own workflows
create policy "Users can delete own workflows"
  on public.workflows for delete
  using (auth.uid() = owner_id);

-- Allow anon/public read for demo mode (optional — remove in production)
create policy "Public can read all workflows"
  on public.workflows for select
  using (true);

-- Index for faster listing
create index if not exists idx_workflows_updated on public.workflows(updated_at desc);
create index if not exists idx_workflows_owner on public.workflows(owner_id);

-- Enable Realtime for workflows table
alter publication supabase_realtime add table public.workflows;

-- Workflow versions table
create table if not exists public.workflow_versions (
  id          uuid primary key default gen_random_uuid(),
  workflow_id uuid not null references public.workflows(id) on delete cascade,
  version     integer not null,
  label       text not null default 'Manual save',
  nodes       jsonb not null default '[]'::jsonb,
  edges       jsonb not null default '[]'::jsonb,
  created_at  timestamptz not null default now(),
  unique(workflow_id, version)
);

alter table public.workflow_versions enable row level security;

create policy "Users can read own workflow versions"
  on public.workflow_versions for select
  using (
    exists (
      select 1 from public.workflows w
      where w.id = workflow_id and w.owner_id = auth.uid()
    )
  );

create policy "Users can insert own workflow versions"
  on public.workflow_versions for insert
  with check (
    exists (
      select 1 from public.workflows w
      where w.id = workflow_id and w.owner_id = auth.uid()
    )
  );

create policy "Public can read all versions"
  on public.workflow_versions for select
  using (true);

create index if not exists idx_versions_workflow on public.workflow_versions(workflow_id, version desc);

-- User profiles (for collaboration indicators)
create table if not exists public.profiles (
  id          uuid primary key references auth.users(id) on delete cascade,
  email       text,
  display_name text,
  avatar_url  text,
  updated_at  timestamptz not null default now()
);

alter table public.profiles enable row level security;

create policy "Anyone can read profiles"
  on public.profiles for select
  using (true);

create policy "Users can update own profile"
  on public.profiles for update
  using (auth.uid() = id)
  with check (auth.uid() = id);

-- Auto-create profile on signup
create or replace function public.handle_new_user()
returns trigger as $$
begin
  insert into public.profiles (id, email, display_name)
  values (
    new.id,
    new.email,
    coalesce(new.raw_user_meta_data->>'display_name', split_part(new.email, '@', 1))
  );
  return new;
end;
$$ language plpgsql security definer;

create trigger on_auth_user_created
  after insert on auth.users
  for each row execute procedure public.handle_new_user();

-- Presence tracking (for collaboration)
create table if not exists public.presence (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references auth.users(id) on delete cascade,
  workflow_id uuid not null references public.workflows(id) on delete cascade,
  cursor_x    float,
  cursor_y    float,
  selected_node text,
  last_seen   timestamptz not null default now(),
  unique(user_id, workflow_id)
);

alter table public.presence enable row level security;

create policy "Anyone can read presence"
  on public.presence for select using (true);

create policy "Users can upsert own presence"
  on public.presence for insert
  with check (auth.uid() = user_id);

create policy "Users can update own presence"
  on public.presence for update
  using (auth.uid() = user_id);

create policy "Users can delete own presence"
  on public.presence for delete
  using (auth.uid() = user_id);

alter publication supabase_realtime add table public.presence;
