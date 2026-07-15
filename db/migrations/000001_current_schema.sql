-- +goose Up
-- Generated from an empty PostgreSQL database after applying the pre-baseline
-- migration history. Do not place business seed data in this file.
--
-- PostgreSQL database dump
--


-- Dumped from database version 16.13 (Debian 16.13-1.pgdg13+1)
-- Dumped by pg_dump version 16.13 (Debian 16.13-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: btree_gist; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;


--
-- Name: EXTENSION btree_gist; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION btree_gist IS 'support for indexing common datatypes in GiST';


--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


--
-- Name: set_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

-- +goose StatementBegin
CREATE FUNCTION public.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$;
-- +goose StatementEnd


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: adaptation_plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.adaptation_plans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    source_id uuid,
    script_id uuid,
    title text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    target_format text DEFAULT 'short_video'::text NOT NULL,
    target_duration_seconds integer,
    max_shots integer,
    selected_event_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    structure jsonb DEFAULT '{}'::jsonb NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    prompt_version_id uuid,
    prompt_hash text,
    review_status text DEFAULT 'pending'::text NOT NULL,
    manual_override boolean DEFAULT false NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    edited_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone
);


--
-- Name: agent_approvals; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_approvals (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    step_id uuid,
    approval_type text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    requested_payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    decision_payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    decided_by uuid,
    decided_at timestamp with time zone,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT agent_approvals_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'expired'::text, 'cancelled'::text])))
);


--
-- Name: agent_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    session_id uuid NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT agent_messages_role_check CHECK ((role = ANY (ARRAY['user'::text, 'assistant'::text, 'system'::text, 'tool'::text])))
);


--
-- Name: agent_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    session_id uuid,
    agent_type text NOT NULL,
    task_type text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    input jsonb DEFAULT '{}'::jsonb NOT NULL,
    output jsonb DEFAULT '{}'::jsonb NOT NULL,
    provider_call_id uuid,
    prompt_version_id uuid,
    prompt_hash text,
    error_code text,
    error_message text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    task_id uuid,
    step_id uuid,
    CONSTRAINT agent_runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text])))
);


--
-- Name: agent_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    agent_type text NOT NULL,
    title text,
    status text DEFAULT 'active'::text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT agent_sessions_agent_type_check CHECK ((agent_type = ANY (ARRAY['script_agent'::text, 'asset_agent'::text, 'storyboard_agent'::text, 'shot_asset_agent'::text, 'project_agent'::text]))),
    CONSTRAINT agent_sessions_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text])))
);


--
-- Name: agent_steps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_steps (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    step_index integer NOT NULL,
    tool_name text NOT NULL,
    risk text NOT NULL,
    permission text,
    status text DEFAULT 'planned'::text NOT NULL,
    requires_approval boolean DEFAULT false NOT NULL,
    input jsonb DEFAULT '{}'::jsonb NOT NULL,
    dry_run_output jsonb DEFAULT '{}'::jsonb NOT NULL,
    supervisor_decision jsonb DEFAULT '{}'::jsonb NOT NULL,
    output jsonb DEFAULT '{}'::jsonb NOT NULL,
    verifier_output jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_code text,
    error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    CONSTRAINT agent_steps_risk_check CHECK ((risk = ANY (ARRAY['read'::text, 'draft'::text, 'write'::text, 'workflow'::text, 'costed'::text, 'destructive'::text, 'admin'::text]))),
    CONSTRAINT agent_steps_status_check CHECK ((status = ANY (ARRAY['planned'::text, 'waiting_approval'::text, 'approved'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'blocked'::text, 'skipped'::text, 'cancelled'::text])))
);


--
-- Name: agent_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_tasks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    session_id uuid,
    agent_type text DEFAULT 'project_agent'::text NOT NULL,
    user_goal text NOT NULL,
    mode text DEFAULT 'supervised'::text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    temporal_workflow_id text,
    constraints jsonb DEFAULT '{}'::jsonb NOT NULL,
    plan jsonb DEFAULT '{}'::jsonb NOT NULL,
    summary jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_code text,
    error_message text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    CONSTRAINT agent_tasks_mode_check CHECK ((mode = ANY (ARRAY['plan_only'::text, 'supervised'::text, 'auto_low_risk'::text]))),
    CONSTRAINT agent_tasks_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'planning'::text, 'waiting_approval'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'blocked'::text, 'cancelled'::text])))
);


--
-- Name: artifacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.artifacts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid,
    workflow_run_id uuid,
    node_run_id uuid,
    type text NOT NULL,
    storage_key text,
    mime_type text,
    content_hash text,
    prompt_hash text,
    model_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: asset_references; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.asset_references (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    asset_id uuid NOT NULL,
    reference_type text DEFAULT 'generated'::text NOT NULL,
    title text,
    description text,
    artifact_id uuid,
    media_file_id uuid,
    storage_key text,
    preview_url text,
    prompt text,
    prompt_version_id uuid,
    prompt_hash text,
    is_primary boolean DEFAULT false NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT asset_references_reference_type_check CHECK ((reference_type = ANY (ARRAY['generated'::text, 'uploaded'::text, 'derived'::text, 'selected'::text]))),
    CONSTRAINT asset_references_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'archived'::text, 'failed'::text])))
);


--
-- Name: asset_relations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.asset_relations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    source_asset_id uuid NOT NULL,
    target_asset_id uuid NOT NULL,
    relation_type text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: asset_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.asset_versions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    asset_id uuid NOT NULL,
    version integer NOT NULL,
    description text NOT NULL,
    base_prompt text,
    visual_traits jsonb DEFAULT '{}'::jsonb NOT NULL,
    reference_artifact_id uuid,
    reference_media_file_id uuid,
    reference_storage_key text,
    prompt_version_id uuid,
    prompt_hash text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: assets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.assets (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    asset_type text NOT NULL,
    name text NOT NULL,
    description text,
    current_artifact_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audio_mix_clips; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audio_mix_clips (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    audio_mix_version_id uuid NOT NULL,
    track_kind text NOT NULL,
    source_kind text NOT NULL,
    timing_unit_id uuid,
    tts_audio_clip_id uuid,
    video_render_segment_id uuid,
    artifact_id uuid,
    media_file_id uuid,
    storage_key text,
    ordinal integer NOT NULL,
    start_tick bigint NOT NULL,
    end_tick bigint NOT NULL,
    trim_start_tick bigint DEFAULT 0 NOT NULL,
    trim_end_tick bigint,
    gain_db numeric DEFAULT 0 NOT NULL,
    fade_in_ticks bigint DEFAULT 0 NOT NULL,
    fade_out_ticks bigint DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT audio_mix_clips_check CHECK ((end_tick > start_tick)),
    CONSTRAINT audio_mix_clips_check1 CHECK (((trim_end_tick IS NULL) OR (trim_end_tick > trim_start_tick))),
    CONSTRAINT audio_mix_clips_fade_in_ticks_check CHECK ((fade_in_ticks >= 0)),
    CONSTRAINT audio_mix_clips_fade_out_ticks_check CHECK ((fade_out_ticks >= 0)),
    CONSTRAINT audio_mix_clips_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT audio_mix_clips_ordinal_check CHECK ((ordinal >= 0)),
    CONSTRAINT audio_mix_clips_source_kind_check CHECK ((source_kind = ANY (ARRAY['tts_clip'::text, 'render_segment_audio'::text, 'artifact'::text, 'generated_silence'::text]))),
    CONSTRAINT audio_mix_clips_start_tick_check CHECK ((start_tick >= 0)),
    CONSTRAINT audio_mix_clips_track_kind_check CHECK ((track_kind = ANY (ARRAY['dialogue'::text, 'voiceover'::text, 'narration'::text, 'ambience'::text, 'sfx'::text, 'music'::text, 'native'::text, 'silence'::text]))),
    CONSTRAINT audio_mix_clips_trim_start_tick_check CHECK ((trim_start_tick >= 0))
);


--
-- Name: audio_mix_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audio_mix_versions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_episode_id uuid,
    storyboard_plan_id uuid,
    timing_analysis_id uuid,
    workflow_run_id uuid,
    revision integer NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    active boolean DEFAULT false NOT NULL,
    audio_strategy text NOT NULL,
    timeline_timebase bigint NOT NULL,
    duration_ticks bigint,
    sample_rate integer DEFAULT 48000 NOT NULL,
    channel_count integer DEFAULT 2 NOT NULL,
    artifact_id uuid,
    media_file_id uuid,
    storage_key text,
    mime_type text,
    production_readiness text DEFAULT 'blocked'::text NOT NULL,
    track_summary jsonb DEFAULT '{}'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    audio_configuration_revision integer DEFAULT 1 NOT NULL,
    CONSTRAINT audio_mix_versions_audio_configuration_revision_positive CHECK ((audio_configuration_revision > 0)),
    CONSTRAINT audio_mix_versions_audio_strategy_check CHECK ((audio_strategy = ANY (ARRAY['native_av'::text, 'hybrid'::text, 'tts_postdub'::text]))),
    CONSTRAINT audio_mix_versions_channel_count_check CHECK ((channel_count > 0)),
    CONSTRAINT audio_mix_versions_check CHECK (((NOT active) OR ((status = 'ready'::text) AND (production_readiness = 'ready'::text)))),
    CONSTRAINT audio_mix_versions_duration_ticks_check CHECK (((duration_ticks IS NULL) OR (duration_ticks > 0))),
    CONSTRAINT audio_mix_versions_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT audio_mix_versions_production_readiness_check CHECK ((production_readiness = ANY (ARRAY['ready'::text, 'preview_only'::text, 'partial'::text, 'blocked'::text]))),
    CONSTRAINT audio_mix_versions_revision_check CHECK ((revision > 0)),
    CONSTRAINT audio_mix_versions_sample_rate_check CHECK ((sample_rate > 0)),
    CONSTRAINT audio_mix_versions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'mixing'::text, 'ready'::text, 'failed'::text, 'stale'::text, 'archived'::text]))),
    CONSTRAINT audio_mix_versions_timeline_timebase_check CHECK ((timeline_timebase > 0)),
    CONSTRAINT audio_mix_versions_track_summary_check CHECK ((jsonb_typeof(track_summary) = 'object'::text))
);


--
-- Name: audit_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_logs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    actor_user_id uuid,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    ip_address text,
    user_agent text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: auth_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    organization_id uuid,
    refresh_token_hash text NOT NULL,
    user_agent text,
    ip_address text,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: canonical_assets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.canonical_assets (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    asset_type text NOT NULL,
    name text NOT NULL,
    description text NOT NULL,
    base_prompt text,
    visual_traits jsonb DEFAULT '{}'::jsonb NOT NULL,
    reference_artifact_id uuid,
    reference_media_file_id uuid,
    reference_storage_key text,
    status text DEFAULT 'draft'::text NOT NULL,
    source_script_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    review_status text DEFAULT 'pending'::text NOT NULL,
    manual_override boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    edited_by uuid,
    edited_at timestamp with time zone,
    profile jsonb DEFAULT '{}'::jsonb NOT NULL,
    negative_prompt text,
    consistency_prompt text,
    primary_reference_artifact_id uuid,
    primary_reference_media_file_id uuid,
    primary_reference_storage_key text,
    lock_reference boolean DEFAULT false NOT NULL,
    CONSTRAINT canonical_assets_asset_type_check CHECK ((asset_type = ANY (ARRAY['character'::text, 'scene'::text, 'prop'::text]))),
    CONSTRAINT canonical_assets_review_status_check CHECK ((review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'needs_edit'::text]))),
    CONSTRAINT canonical_assets_stale_state_check CHECK ((stale_state = ANY (ARRAY['fresh'::text, 'upstream_changed'::text, 'needs_regeneration'::text]))),
    CONSTRAINT canonical_assets_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'prompt_ready'::text, 'image_running'::text, 'image_succeeded'::text, 'image_failed'::text, 'archived'::text])))
);


--
-- Name: character_voice_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.character_voice_profiles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    canonical_asset_id uuid,
    character_name text NOT NULL,
    display_name text NOT NULL,
    language text DEFAULT 'zh-CN'::text NOT NULL,
    model_profile_key text DEFAULT 'tts_generation_default'::text NOT NULL,
    provider_model_id uuid,
    voice_key text NOT NULL,
    instructions text,
    reference_artifact_id uuid,
    reference_media_file_id uuid,
    parameters jsonb DEFAULT '{}'::jsonb NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT character_voice_profiles_character_name_check CHECK ((btrim(character_name) <> ''::text)),
    CONSTRAINT character_voice_profiles_display_name_check CHECK ((btrim(display_name) <> ''::text)),
    CONSTRAINT character_voice_profiles_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT character_voice_profiles_parameters_check CHECK ((jsonb_typeof(parameters) = 'object'::text)),
    CONSTRAINT character_voice_profiles_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text]))),
    CONSTRAINT character_voice_profiles_voice_key_check CHECK ((btrim(voice_key) <> ''::text))
);


--
-- Name: cost_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cost_records (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid,
    workflow_run_id uuid,
    node_run_id uuid,
    provider_call_id uuid,
    provider_model_id uuid,
    credential_id uuid,
    model_profile_id uuid,
    cost_type text NOT NULL,
    amount numeric(18,8) NOT NULL,
    currency text DEFAULT 'USD'::text NOT NULL,
    unit text,
    quantity numeric(18,6),
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: episode_continuity_blueprints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.episode_continuity_blueprints (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_id uuid NOT NULL,
    script_version_id uuid NOT NULL,
    script_episode_id uuid NOT NULL,
    timing_analysis_id uuid NOT NULL,
    revision integer NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    blueprint jsonb DEFAULT '{}'::jsonb NOT NULL,
    dependencies jsonb DEFAULT '[]'::jsonb NOT NULL,
    serial_groups jsonb DEFAULT '[]'::jsonb NOT NULL,
    parallel_groups jsonb DEFAULT '[]'::jsonb NOT NULL,
    prompt_version_id uuid,
    prompt_hash text,
    provider_call_id uuid,
    model_id uuid,
    error_code text,
    error_message text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT episode_continuity_blueprints_blueprint_check CHECK ((jsonb_typeof(blueprint) = 'object'::text)),
    CONSTRAINT episode_continuity_blueprints_dependencies_check CHECK ((jsonb_typeof(dependencies) = 'array'::text)),
    CONSTRAINT episode_continuity_blueprints_parallel_groups_check CHECK ((jsonb_typeof(parallel_groups) = 'array'::text)),
    CONSTRAINT episode_continuity_blueprints_revision_check CHECK ((revision > 0)),
    CONSTRAINT episode_continuity_blueprints_serial_groups_check CHECK ((jsonb_typeof(serial_groups) = 'array'::text)),
    CONSTRAINT episode_continuity_blueprints_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'analyzing'::text, 'ready'::text, 'failed'::text, 'archived'::text])))
);


--
-- Name: event_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.event_outbox (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    CONSTRAINT event_outbox_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'publishing'::text, 'published'::text, 'failed'::text])))
);


--
-- Name: final_video_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.final_video_versions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    timeline_id uuid NOT NULL,
    workflow_run_id uuid,
    version integer NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    artifact_id uuid,
    media_file_id uuid,
    storage_key text,
    resolution text DEFAULT '720p'::text NOT NULL,
    aspect_ratio text DEFAULT '16:9'::text NOT NULL,
    compose_settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    duration_ticks bigint,
    native_audio_status text DEFAULT 'not_requested'::text NOT NULL,
    production_readiness text DEFAULT 'ready'::text NOT NULL,
    audio_mix_version_id uuid,
    CONSTRAINT final_video_versions_duration_ticks_positive CHECK (((duration_ticks IS NULL) OR (duration_ticks > 0))),
    CONSTRAINT final_video_versions_native_audio_status_check CHECK ((native_audio_status = ANY (ARRAY['not_requested'::text, 'native_audio_unavailable'::text, 'audio_unverified'::text, 'audio_verified'::text, 'needs_audio_retry'::text]))),
    CONSTRAINT final_video_versions_production_readiness_check CHECK ((production_readiness = ANY (ARRAY['ready'::text, 'preview_only'::text, 'partial'::text, 'blocked'::text]))),
    CONSTRAINT final_video_versions_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'active'::text, 'archived'::text, 'failed'::text])))
);


--
-- Name: idempotency_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.idempotency_keys (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    key text NOT NULL,
    scope text NOT NULL,
    request_hash text NOT NULL,
    response_snapshot jsonb,
    status text DEFAULT 'processing'::text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT idempotency_keys_status_check CHECK ((status = ANY (ARRAY['processing'::text, 'succeeded'::text, 'failed'::text])))
);


--
-- Name: media_files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_files (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid,
    artifact_id uuid,
    storage_key text NOT NULL,
    mime_type text NOT NULL,
    byte_size bigint,
    width integer,
    height integer,
    duration_seconds numeric,
    checksum text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    frame_rate_numerator bigint,
    frame_rate_denominator bigint,
    frame_count bigint,
    video_stream_count integer,
    audio_stream_count integer,
    media_probe jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT media_files_frame_count_non_negative CHECK (((frame_count IS NULL) OR (frame_count >= 0))),
    CONSTRAINT media_files_frame_rate_positive CHECK ((((frame_rate_numerator IS NULL) AND (frame_rate_denominator IS NULL)) OR ((frame_rate_numerator > 0) AND (frame_rate_denominator > 0)))),
    CONSTRAINT media_files_stream_counts_non_negative CHECK ((((video_stream_count IS NULL) OR (video_stream_count >= 0)) AND ((audio_stream_count IS NULL) OR (audio_stream_count >= 0))))
);


--
-- Name: media_variants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_variants (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    media_file_id uuid NOT NULL,
    variant_type text NOT NULL,
    storage_key text NOT NULL,
    mime_type text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: model_profile_bindings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.model_profile_bindings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    model_profile_id uuid NOT NULL,
    provider_model_id uuid NOT NULL,
    priority integer DEFAULT 100 NOT NULL,
    weight integer DEFAULT 100 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: model_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.model_profiles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    profile_key text NOT NULL,
    name text NOT NULL,
    purpose text NOT NULL,
    routing_strategy text DEFAULT 'priority_with_fallback'::text NOT NULL,
    fallback_strategy jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT model_profiles_routing_strategy_check CHECK ((routing_strategy = ANY (ARRAY['priority'::text, 'priority_with_fallback'::text, 'weighted'::text, 'cost_optimized'::text, 'latency_optimized'::text])))
);


--
-- Name: native_audio_reviews; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.native_audio_reviews (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    video_render_plan_id uuid NOT NULL,
    video_render_segment_id uuid NOT NULL,
    workflow_run_id uuid,
    node_run_id uuid,
    provider_call_id uuid,
    provider_model_id uuid,
    revision integer NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    expected_dialogue jsonb DEFAULT '[]'::jsonb NOT NULL,
    transcript text,
    language text,
    alignment jsonb DEFAULT '[]'::jsonb NOT NULL,
    dialogue_coverage numeric(5,4),
    text_accuracy numeric(5,4),
    timing_accuracy numeric(5,4),
    speaker_turn_accuracy numeric(5,4),
    error_code text,
    error_message text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    reviewed_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    audio_configuration_revision integer DEFAULT 1 NOT NULL,
    CONSTRAINT native_audio_reviews_alignment_check CHECK ((jsonb_typeof(alignment) = 'array'::text)),
    CONSTRAINT native_audio_reviews_audio_configuration_revision_positive CHECK ((audio_configuration_revision > 0)),
    CONSTRAINT native_audio_reviews_dialogue_coverage_check CHECK (((dialogue_coverage IS NULL) OR ((dialogue_coverage >= (0)::numeric) AND (dialogue_coverage <= (1)::numeric)))),
    CONSTRAINT native_audio_reviews_expected_dialogue_check CHECK ((jsonb_typeof(expected_dialogue) = 'array'::text)),
    CONSTRAINT native_audio_reviews_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT native_audio_reviews_revision_check CHECK ((revision > 0)),
    CONSTRAINT native_audio_reviews_speaker_turn_accuracy_check CHECK (((speaker_turn_accuracy IS NULL) OR ((speaker_turn_accuracy >= (0)::numeric) AND (speaker_turn_accuracy <= (1)::numeric)))),
    CONSTRAINT native_audio_reviews_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'passed'::text, 'failed'::text, 'manual_override'::text, 'cancelled'::text, 'stale'::text]))),
    CONSTRAINT native_audio_reviews_text_accuracy_check CHECK (((text_accuracy IS NULL) OR ((text_accuracy >= (0)::numeric) AND (text_accuracy <= (1)::numeric)))),
    CONSTRAINT native_audio_reviews_timing_accuracy_check CHECK (((timing_accuracy IS NULL) OR ((timing_accuracy >= (0)::numeric) AND (timing_accuracy <= (1)::numeric))))
);


--
-- Name: novel_chapters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.novel_chapters (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    novel_id uuid,
    chapter_index integer NOT NULL,
    title text,
    content_artifact_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    organization_id uuid,
    project_id uuid,
    source_id uuid,
    volume_title text,
    chapter_title text,
    content text,
    event_state text DEFAULT 'pending'::text NOT NULL,
    event_summary jsonb,
    error_message text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    volume_index integer,
    section_index integer
);


--
-- Name: novel_event_links; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.novel_event_links (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    source_event_id uuid NOT NULL,
    target_event_id uuid NOT NULL,
    link_type text NOT NULL,
    description text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: novel_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.novel_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    source_id uuid NOT NULL,
    chapter_id uuid,
    event_index integer NOT NULL,
    sequence_no integer NOT NULL,
    title text NOT NULL,
    summary text NOT NULL,
    event_type text,
    importance integer DEFAULT 3 NOT NULL,
    timeline_hint text,
    location_hint text,
    emotional_tone text,
    conflict text,
    outcome text,
    adaptation_hint text,
    characters jsonb DEFAULT '[]'::jsonb NOT NULL,
    scenes jsonb DEFAULT '[]'::jsonb NOT NULL,
    props jsonb DEFAULT '[]'::jsonb NOT NULL,
    keywords jsonb DEFAULT '[]'::jsonb NOT NULL,
    raw_excerpt text,
    review_status text DEFAULT 'pending'::text NOT NULL,
    manual_override boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    edited_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone
);


--
-- Name: novels; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.novels (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    title text NOT NULL,
    source_type text,
    raw_artifact_id uuid,
    clean_artifact_id uuid,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: organization_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.organization_members (
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT organization_members_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text, 'invited'::text])))
);


--
-- Name: organizations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.organizations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.permissions (
    permission_key text NOT NULL,
    description text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    name text NOT NULL,
    id uuid DEFAULT gen_random_uuid()
);


--
-- Name: project_exports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_exports (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    export_type text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    title text NOT NULL,
    format text NOT NULL,
    workflow_run_id uuid,
    artifact_id uuid,
    media_file_id uuid,
    storage_key text,
    byte_size bigint,
    content_hash text,
    request jsonb DEFAULT '{}'::jsonb NOT NULL,
    output jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_code text,
    error_message text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    CONSTRAINT project_exports_export_type_check CHECK ((export_type = ANY (ARRAY['final_video'::text, 'documents'::text, 'asset_package'::text, 'project_archive'::text]))),
    CONSTRAINT project_exports_format_check CHECK ((format = ANY (ARRAY['mp4'::text, 'json'::text, 'markdown'::text, 'zip'::text]))),
    CONSTRAINT project_exports_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text])))
);


--
-- Name: project_manual_bindings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_manual_bindings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    manual_kind text NOT NULL,
    prompt_version_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_manual_bindings_kind_check CHECK ((manual_kind = ANY (ARRAY['director'::text, 'visual'::text]))),
    CONSTRAINT project_manual_bindings_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text])))
);


--
-- Name: project_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_members (
    project_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_members_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text, 'invited'::text])))
);


--
-- Name: project_sources; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_sources (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    source_type text NOT NULL,
    title text NOT NULL,
    content text NOT NULL,
    content_format text DEFAULT 'plain_text'::text NOT NULL,
    original_file_name text,
    storage_key text,
    status text DEFAULT 'ready'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_sources_content_format_check CHECK ((content_format = ANY (ARRAY['plain_text'::text, 'markdown'::text]))),
    CONSTRAINT project_sources_source_type_check CHECK ((source_type = ANY (ARRAY['novel'::text, 'script'::text, 'brief'::text]))),
    CONSTRAINT project_sources_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'processing'::text, 'processed'::text, 'failed'::text, 'archived'::text])))
);


--
-- Name: project_timelines; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_timelines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    workflow_run_id uuid,
    title text DEFAULT '默认时间线'::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    aspect_ratio text DEFAULT '16:9'::text NOT NULL,
    resolution text DEFAULT '720p'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    edited_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone,
    manual_override boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    timeline_timebase bigint DEFAULT 90000 NOT NULL,
    fps_numerator integer DEFAULT 24 NOT NULL,
    fps_denominator integer DEFAULT 1 NOT NULL,
    CONSTRAINT project_timelines_frame_rate_exact_in_timebase CHECK ((((timeline_timebase * fps_denominator) % (fps_numerator)::bigint) = 0)),
    CONSTRAINT project_timelines_frame_rate_positive CHECK (((fps_numerator > 0) AND (fps_denominator > 0))),
    CONSTRAINT project_timelines_stale_state_check CHECK ((stale_state = ANY (ARRAY['fresh'::text, 'upstream_changed'::text, 'needs_regeneration'::text]))),
    CONSTRAINT project_timelines_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'active'::text, 'archived'::text]))),
    CONSTRAINT project_timelines_timebase_positive CHECK ((timeline_timebase > 0))
);


--
-- Name: projects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.projects (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    description text,
    project_type text,
    aspect_ratio text,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    content_type text,
    video_ratio text DEFAULT '16:9'::text NOT NULL,
    art_style text DEFAULT ''::text NOT NULL,
    director_manual text DEFAULT ''::text NOT NULL,
    visual_manual text DEFAULT ''::text NOT NULL,
    image_model_profile_key text DEFAULT 'image_generation_default'::text NOT NULL,
    video_model_profile_key text DEFAULT 'video_generation_default'::text NOT NULL,
    script_model_profile_key text DEFAULT 'script_agent_default'::text NOT NULL,
    image_quality text DEFAULT 'standard'::text NOT NULL,
    production_mode text DEFAULT 'silent_video'::text NOT NULL,
    active_final_video_version_id uuid,
    timeline_timebase bigint DEFAULT 90000 NOT NULL,
    fps_numerator integer DEFAULT 24 NOT NULL,
    fps_denominator integer DEFAULT 1 NOT NULL,
    audio_strategy text DEFAULT 'native_av'::text NOT NULL,
    audio_requirement text DEFAULT 'preferred'::text NOT NULL,
    tts_model_profile_key text DEFAULT 'tts_generation_default'::text NOT NULL,
    asr_model_profile_key text DEFAULT 'audio_transcription_default'::text NOT NULL,
    active_audio_mix_version_id uuid,
    audio_configuration_revision integer DEFAULT 1 NOT NULL,
    CONSTRAINT projects_audio_configuration_revision_positive CHECK ((audio_configuration_revision > 0)),
    CONSTRAINT projects_audio_requirement_check CHECK ((audio_requirement = ANY (ARRAY['preferred'::text, 'required'::text, 'disabled'::text]))),
    CONSTRAINT projects_audio_strategy_check CHECK ((audio_strategy = ANY (ARRAY['native_av'::text, 'hybrid'::text, 'tts_postdub'::text]))),
    CONSTRAINT projects_frame_rate_exact_in_timebase CHECK ((((timeline_timebase * fps_denominator) % (fps_numerator)::bigint) = 0)),
    CONSTRAINT projects_frame_rate_positive CHECK (((fps_numerator > 0) AND (fps_denominator > 0))),
    CONSTRAINT projects_timeline_timebase_positive CHECK ((timeline_timebase > 0))
);


--
-- Name: prompt_bindings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prompt_bindings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid,
    template_key text NOT NULL,
    prompt_version_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT prompt_bindings_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text])))
);


--
-- Name: prompt_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prompt_templates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid,
    template_key text NOT NULL,
    name text NOT NULL,
    purpose text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    description text,
    modality text NOT NULL,
    task_type text NOT NULL,
    scope text DEFAULT 'system'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    is_system boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT prompt_templates_scope_check CHECK ((scope = ANY (ARRAY['system'::text, 'organization'::text]))),
    CONSTRAINT prompt_templates_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text])))
);


--
-- Name: prompt_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prompt_versions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    prompt_template_id uuid NOT NULL,
    version_no integer NOT NULL,
    content text NOT NULL,
    variables_schema jsonb DEFAULT '{}'::jsonb NOT NULL,
    content_hash text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    template_id uuid NOT NULL,
    version integer NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    title text,
    content_format text DEFAULT 'text'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    activated_at timestamp with time zone,
    CONSTRAINT prompt_versions_content_format_check CHECK ((content_format = ANY (ARRAY['text'::text, 'markdown'::text]))),
    CONSTRAINT prompt_versions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'active'::text, 'archived'::text])))
);


--
-- Name: provider_accounts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_accounts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    connector_id uuid NOT NULL,
    name text NOT NULL,
    base_url text,
    auth_type text DEFAULT 'bearer'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provider_accounts_auth_type_check CHECK ((auth_type = ANY (ARRAY['none'::text, 'bearer'::text, 'api_key'::text, 'basic'::text]))),
    CONSTRAINT provider_accounts_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text, 'error'::text])))
);


--
-- Name: provider_async_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_async_tasks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider_call_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    provider_account_id uuid NOT NULL,
    provider_model_id uuid,
    external_task_id text,
    status text NOT NULL,
    poll_after timestamp with time zone,
    result_expires_at timestamp with time zone,
    raw_status jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_poll_at timestamp with time zone,
    finalized_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    project_id uuid,
    workflow_run_id uuid,
    node_run_id uuid,
    credential_id uuid,
    model_profile_id uuid,
    model_profile_binding_id uuid,
    model_profile_key text,
    task_type text DEFAULT 'video.generate'::text NOT NULL,
    execution_mode text DEFAULT 'async_polling'::text NOT NULL,
    input jsonb DEFAULT '{}'::jsonb NOT NULL,
    normalized_output jsonb,
    last_response_snapshot jsonb,
    error_code text,
    error_message text,
    poll_count integer DEFAULT 0 NOT NULL,
    next_poll_at timestamp with time zone,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    requested_duration_seconds numeric,
    actual_duration_seconds numeric,
    media_probe jsonb DEFAULT '{}'::jsonb NOT NULL,
    video_render_plan_id uuid,
    video_render_segment_id uuid,
    video_variant_key text,
    capability_snapshot_hash text,
    CONSTRAINT provider_async_tasks_actual_duration_positive CHECK (((actual_duration_seconds IS NULL) OR (actual_duration_seconds > (0)::numeric))),
    CONSTRAINT provider_async_tasks_requested_duration_positive CHECK (((requested_duration_seconds IS NULL) OR (requested_duration_seconds > (0)::numeric)))
);


--
-- Name: provider_call_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_call_logs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid,
    workflow_run_id uuid,
    node_run_id uuid,
    provider_account_id uuid NOT NULL,
    provider_model_id uuid,
    credential_id uuid,
    model_profile_id uuid,
    model_profile_binding_id uuid,
    model_profile_key text,
    prompt_version_id uuid,
    prompt_hash text,
    input_hash text,
    output_hash text,
    task_type text NOT NULL,
    execution_mode text DEFAULT 'sync'::text NOT NULL,
    status text NOT NULL,
    upstream_request_id text,
    external_task_id text,
    lease_id uuid,
    idempotency_key text,
    latency_ms integer,
    input_tokens integer,
    output_tokens integer,
    media_count integer,
    duration_seconds numeric,
    estimated_cost numeric(18,8),
    currency text DEFAULT 'USD'::text,
    error_code text,
    error_message text,
    upstream_status integer,
    upstream_error_code text,
    request_hash text,
    request_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    response_snapshot jsonb,
    normalized_output jsonb,
    artifact_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    media_file_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    requested_duration_seconds numeric,
    actual_duration_seconds numeric,
    media_probe jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT provider_call_logs_actual_duration_positive CHECK (((actual_duration_seconds IS NULL) OR (actual_duration_seconds > (0)::numeric))),
    CONSTRAINT provider_call_logs_execution_mode_check CHECK ((execution_mode = ANY (ARRAY['sync'::text, 'async'::text, 'stream'::text, 'async_create'::text, 'async_poll'::text]))),
    CONSTRAINT provider_call_logs_requested_duration_positive CHECK (((requested_duration_seconds IS NULL) OR (requested_duration_seconds > (0)::numeric))),
    CONSTRAINT provider_call_logs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text, 'skipped'::text, 'blocked'::text])))
);


--
-- Name: provider_catalog_entries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_catalog_entries (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider_key text NOT NULL,
    name text NOT NULL,
    display_name text NOT NULL,
    description text,
    provider_type text NOT NULL,
    category text NOT NULL,
    logo_key text,
    docs_url text,
    default_base_url text,
    default_auth_type text DEFAULT 'bearer'::text NOT NULL,
    connector_manifest jsonb DEFAULT '{}'::jsonb NOT NULL,
    model_templates jsonb DEFAULT '[]'::jsonb NOT NULL,
    supported_task_types jsonb DEFAULT '[]'::jsonb NOT NULL,
    setup_schema jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    is_official boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provider_catalog_entries_category_check CHECK ((category = ANY (ARRAY['text'::text, 'image'::text, 'video'::text, 'multimodal'::text]))),
    CONSTRAINT provider_catalog_entries_default_auth_type_check CHECK ((default_auth_type = ANY (ARRAY['none'::text, 'bearer'::text, 'api_key'::text, 'basic'::text]))),
    CONSTRAINT provider_catalog_entries_provider_type_check CHECK ((provider_type = ANY (ARRAY['openai_compatible'::text, 'declarative_manifest'::text, 'native'::text])))
);


--
-- Name: provider_circuit_states; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_circuit_states (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    provider_account_id uuid NOT NULL,
    provider_model_id uuid,
    task_type text NOT NULL,
    state text DEFAULT 'closed'::text NOT NULL,
    failure_count integer DEFAULT 0 NOT NULL,
    success_count integer DEFAULT 0 NOT NULL,
    opened_at timestamp with time zone,
    half_open_at timestamp with time zone,
    next_attempt_at timestamp with time zone,
    last_error_code text,
    last_error_message text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provider_circuit_states_state_check CHECK ((state = ANY (ARRAY['closed'::text, 'open'::text, 'half_open'::text])))
);


--
-- Name: provider_connectors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_connectors (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    connector_key text NOT NULL,
    name text NOT NULL,
    type text NOT NULL,
    is_official boolean DEFAULT false NOT NULL,
    manifest jsonb DEFAULT '{}'::jsonb NOT NULL,
    version text DEFAULT 'v1'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: provider_credentials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_credentials (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    provider_account_id uuid NOT NULL,
    credential_key text DEFAULT 'default'::text NOT NULL,
    credential_type text DEFAULT 'api_key'::text NOT NULL,
    secret_ref text,
    encrypted_payload bytea,
    masked_preview text,
    status text DEFAULT 'active'::text NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone,
    rotated_at timestamp with time zone,
    CONSTRAINT provider_credentials_status_check CHECK ((status = ANY (ARRAY['active'::text, 'rotated'::text, 'revoked'::text, 'expired'::text])))
);


--
-- Name: provider_endpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_endpoints (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider_account_id uuid NOT NULL,
    endpoint_key text NOT NULL,
    endpoint_type text NOT NULL,
    method text NOT NULL,
    path_template text NOT NULL,
    headers_template jsonb DEFAULT '{}'::jsonb NOT NULL,
    request_template jsonb DEFAULT '{}'::jsonb NOT NULL,
    response_mapping jsonb DEFAULT '{}'::jsonb NOT NULL,
    timeout_ms integer DEFAULT 120000 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: provider_leases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_leases (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    provider_account_id uuid NOT NULL,
    provider_model_id uuid,
    task_type text NOT NULL,
    workflow_run_id uuid,
    node_run_id uuid,
    provider_call_id uuid,
    acquired_by_service text DEFAULT 'provider-gateway'::text NOT NULL,
    status text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    released_at timestamp with time zone,
    lease_token text NOT NULL,
    CONSTRAINT provider_leases_status_check CHECK ((status = ANY (ARRAY['active'::text, 'released'::text, 'expired'::text, 'cancelled'::text])))
);


--
-- Name: provider_limit_policies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_limit_policies (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    provider_account_id uuid,
    provider_model_id uuid,
    task_type text NOT NULL,
    max_concurrency integer,
    requests_per_minute integer,
    requests_per_day integer,
    daily_budget numeric(18,8),
    monthly_budget numeric(18,8),
    currency text DEFAULT 'USD'::text NOT NULL,
    failure_threshold integer,
    failure_window_seconds integer,
    circuit_cooldown_seconds integer,
    enabled boolean DEFAULT true NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: provider_model_capabilities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_model_capabilities (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider_model_id uuid NOT NULL,
    task_types jsonb DEFAULT '[]'::jsonb NOT NULL,
    input_limits jsonb DEFAULT '{}'::jsonb NOT NULL,
    output_limits jsonb DEFAULT '{}'::jsonb NOT NULL,
    quality_tiers jsonb DEFAULT '[]'::jsonb NOT NULL,
    provider_options_schema jsonb DEFAULT '{}'::jsonb NOT NULL,
    pricing_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: provider_model_capability_presets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_model_capability_presets (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    preset_key text NOT NULL,
    display_name text NOT NULL,
    modality text NOT NULL,
    match_patterns jsonb DEFAULT '[]'::jsonb NOT NULL,
    task_types jsonb DEFAULT '[]'::jsonb NOT NULL,
    input_limits jsonb DEFAULT '{}'::jsonb NOT NULL,
    output_limits jsonb DEFAULT '{}'::jsonb NOT NULL,
    quality_tiers jsonb DEFAULT '[]'::jsonb NOT NULL,
    provider_options_schema jsonb DEFAULT '{}'::jsonb NOT NULL,
    pricing_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    priority integer DEFAULT 100 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provider_model_capability_presets_input_limits_check CHECK ((jsonb_typeof(input_limits) = 'object'::text)),
    CONSTRAINT provider_model_capability_presets_match_patterns_check CHECK ((jsonb_typeof(match_patterns) = 'array'::text)),
    CONSTRAINT provider_model_capability_presets_modality_check CHECK ((modality = ANY (ARRAY['text'::text, 'image'::text, 'video'::text, 'audio'::text, 'embedding'::text, 'multimodal'::text]))),
    CONSTRAINT provider_model_capability_presets_output_limits_check CHECK ((jsonb_typeof(output_limits) = 'object'::text)),
    CONSTRAINT provider_model_capability_presets_pricing_policy_check CHECK ((jsonb_typeof(pricing_policy) = 'object'::text)),
    CONSTRAINT provider_model_capability_presets_provider_options_schema_check CHECK ((jsonb_typeof(provider_options_schema) = 'object'::text)),
    CONSTRAINT provider_model_capability_presets_quality_tiers_check CHECK ((jsonb_typeof(quality_tiers) = 'array'::text)),
    CONSTRAINT provider_model_capability_presets_task_types_check CHECK ((jsonb_typeof(task_types) = 'array'::text))
);


--
-- Name: provider_models; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_models (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider_account_id uuid NOT NULL,
    model_key text NOT NULL,
    display_name text NOT NULL,
    modality text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provider_models_modality_check CHECK ((modality = ANY (ARRAY['text'::text, 'image'::text, 'video'::text, 'audio'::text, 'embedding'::text, 'multimodal'::text]))),
    CONSTRAINT provider_models_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text, 'deprecated'::text, 'error'::text])))
);


--
-- Name: provider_test_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provider_test_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    provider_account_id uuid NOT NULL,
    provider_model_id uuid,
    test_type text NOT NULL,
    status text NOT NULL,
    request_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    response_snapshot jsonb,
    normalized_output jsonb,
    error_code text,
    error_message text,
    latency_ms integer,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provider_test_runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'skipped'::text])))
);


--
-- Name: review_fixes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.review_fixes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    review_item_id uuid NOT NULL,
    target_entity_type text NOT NULL,
    target_entity_id uuid,
    status text DEFAULT 'draft'::text NOT NULL,
    fix_type text DEFAULT 'patch'::text NOT NULL,
    title text NOT NULL,
    explanation text NOT NULL,
    before_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    patch jsonb DEFAULT '{}'::jsonb NOT NULL,
    after_preview jsonb DEFAULT '{}'::jsonb NOT NULL,
    regenerate_request jsonb,
    prompt_version_id uuid,
    prompt_hash text,
    provider_call_id uuid,
    error_code text,
    error_message text,
    created_by uuid,
    applied_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    applied_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT review_fixes_fix_type_check CHECK ((fix_type = ANY (ARRAY['patch'::text, 'regenerate'::text, 'navigate'::text, 'note'::text]))),
    CONSTRAINT review_fixes_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'applied'::text, 'dismissed'::text, 'failed'::text])))
);


--
-- Name: review_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.review_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    review_run_id uuid,
    item_type text NOT NULL,
    category text NOT NULL,
    severity text DEFAULT 'medium'::text NOT NULL,
    title text NOT NULL,
    description text NOT NULL,
    suggestion text,
    entity_type text NOT NULL,
    entity_id uuid,
    related_entity_type text,
    related_entity_id uuid,
    status text DEFAULT 'open'::text NOT NULL,
    resolution_note text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    resolved_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    resolved_at timestamp with time zone,
    CONSTRAINT review_items_category_check CHECK ((category = ANY (ARRAY['script'::text, 'asset'::text, 'storyboard'::text, 'shot_asset'::text, 'shot_image'::text, 'shot_video'::text, 'timeline'::text, 'final_video'::text]))),
    CONSTRAINT review_items_entity_type_check CHECK ((entity_type = ANY (ARRAY['script_scene'::text, 'canonical_asset'::text, 'storyboard_shot'::text, 'shot_asset_requirement'::text, 'timeline_clip'::text, 'project_timeline'::text, 'final_video_version'::text, 'project'::text]))),
    CONSTRAINT review_items_item_type_check CHECK ((item_type = ANY (ARRAY['issue'::text, 'warning'::text, 'suggestion'::text]))),
    CONSTRAINT review_items_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT review_items_status_check CHECK ((status = ANY (ARRAY['open'::text, 'resolved'::text, 'ignored'::text])))
);


--
-- Name: review_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.review_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    workflow_run_id uuid,
    review_type text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    summary jsonb DEFAULT '{}'::jsonb NOT NULL,
    input jsonb DEFAULT '{}'::jsonb NOT NULL,
    output jsonb DEFAULT '{}'::jsonb NOT NULL,
    provider_call_id uuid,
    prompt_version_id uuid,
    prompt_hash text,
    error_code text,
    error_message text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    CONSTRAINT review_runs_review_type_check CHECK ((review_type = ANY (ARRAY['project'::text, 'script'::text, 'assets'::text, 'storyboard'::text, 'production'::text, 'timeline'::text, 'final_video'::text]))),
    CONSTRAINT review_runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text])))
);


--
-- Name: review_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.review_tasks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    workflow_run_id uuid,
    node_run_id uuid,
    status text DEFAULT 'pending'::text NOT NULL,
    review_type text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    assigned_to uuid,
    resolved_by uuid,
    resolved_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT review_tasks_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'cancelled'::text])))
);


--
-- Name: role_bindings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role_bindings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    role_id uuid NOT NULL,
    subject_type text NOT NULL,
    subject_user_id uuid,
    subject_team_id uuid,
    resource_type text NOT NULL,
    resource_organization_id uuid,
    resource_workspace_id uuid,
    resource_project_id uuid,
    created_by uuid,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT role_bindings_check CHECK ((((subject_type = 'user'::text) AND (subject_user_id IS NOT NULL) AND (subject_team_id IS NULL)) OR ((subject_type = 'team'::text) AND (subject_team_id IS NOT NULL) AND (subject_user_id IS NULL)))),
    CONSTRAINT role_bindings_check1 CHECK ((((resource_type = 'organization'::text) AND (resource_organization_id IS NOT NULL) AND (resource_workspace_id IS NULL) AND (resource_project_id IS NULL)) OR ((resource_type = 'workspace'::text) AND (resource_workspace_id IS NOT NULL) AND (resource_organization_id IS NULL) AND (resource_project_id IS NULL)) OR ((resource_type = 'project'::text) AND (resource_project_id IS NOT NULL) AND (resource_organization_id IS NULL) AND (resource_workspace_id IS NULL)))),
    CONSTRAINT role_bindings_resource_type_check CHECK ((resource_type = ANY (ARRAY['organization'::text, 'workspace'::text, 'project'::text]))),
    CONSTRAINT role_bindings_subject_type_check CHECK ((subject_type = ANY (ARRAY['user'::text, 'team'::text])))
);


--
-- Name: role_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role_permissions (
    role_id uuid NOT NULL,
    permission_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid,
    role_key text NOT NULL,
    name text NOT NULL,
    scope text NOT NULL,
    is_system boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    description text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT roles_scope_check CHECK ((scope = ANY (ARRAY['organization'::text, 'workspace'::text, 'project'::text])))
);


--
-- Name: scene_asset_links; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scene_asset_links (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_scene_id uuid NOT NULL,
    asset_id uuid NOT NULL,
    asset_role text,
    usage_note text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: script_asset_links; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.script_asset_links (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_id uuid NOT NULL,
    asset_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: script_episodes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.script_episodes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_id uuid NOT NULL,
    script_version_id uuid NOT NULL,
    source_id uuid,
    source_chapter_id uuid,
    episode_index integer NOT NULL,
    volume_index integer,
    section_index integer,
    volume_title text,
    episode_title text NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    content_format text DEFAULT 'markdown'::text NOT NULL,
    prompt_version_id uuid,
    prompt_hash text,
    provider_call_id uuid,
    review_status text DEFAULT 'pending'::text NOT NULL,
    manual_override boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    edited_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone,
    CONSTRAINT script_episodes_content_format_check CHECK ((content_format = ANY (ARRAY['plain_text'::text, 'markdown'::text]))),
    CONSTRAINT script_episodes_review_status_check CHECK ((review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'needs_edit'::text]))),
    CONSTRAINT script_episodes_stale_state_check CHECK ((stale_state = ANY (ARRAY['fresh'::text, 'upstream_changed'::text, 'needs_regeneration'::text])))
);


--
-- Name: script_scenes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.script_scenes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_id uuid NOT NULL,
    script_version_id uuid NOT NULL,
    scene_index integer NOT NULL,
    scene_no integer NOT NULL,
    title text NOT NULL,
    summary text,
    location text,
    time_of_day text,
    atmosphere text,
    characters jsonb DEFAULT '[]'::jsonb NOT NULL,
    scenes jsonb DEFAULT '[]'::jsonb NOT NULL,
    props jsonb DEFAULT '[]'::jsonb NOT NULL,
    action text,
    dialogue text,
    visual_goal text,
    emotional_tone text,
    conflict text,
    outcome text,
    source_event_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    content_format text DEFAULT 'markdown'::text NOT NULL,
    review_status text DEFAULT 'pending'::text NOT NULL,
    manual_override boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    edited_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone,
    deleted_at timestamp with time zone,
    script_episode_id uuid,
    CONSTRAINT script_scenes_content_format_check CHECK ((content_format = ANY (ARRAY['plain_text'::text, 'markdown'::text]))),
    CONSTRAINT script_scenes_review_status_check CHECK ((review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'needs_edit'::text]))),
    CONSTRAINT script_scenes_stale_state_check CHECK ((stale_state = ANY (ARRAY['fresh'::text, 'upstream_changed'::text, 'needs_regeneration'::text])))
);


--
-- Name: script_timing_analyses; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.script_timing_analyses (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_id uuid NOT NULL,
    script_version_id uuid NOT NULL,
    script_episode_id uuid NOT NULL,
    revision integer NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    estimated_duration_ticks bigint NOT NULL,
    minimum_duration_ticks bigint NOT NULL,
    target_duration_ticks bigint,
    timeline_timebase bigint DEFAULT 90000 NOT NULL,
    fps_numerator integer DEFAULT 24 NOT NULL,
    fps_denominator integer DEFAULT 1 NOT NULL,
    method_version text NOT NULL,
    prompt_version_id uuid,
    prompt_hash text,
    provider_call_id uuid,
    model_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT script_timing_analyses_check CHECK ((minimum_duration_ticks <= estimated_duration_ticks)),
    CONSTRAINT script_timing_analyses_check1 CHECK ((((timeline_timebase * fps_denominator) % (fps_numerator)::bigint) = 0)),
    CONSTRAINT script_timing_analyses_estimated_duration_ticks_check CHECK ((estimated_duration_ticks > 0)),
    CONSTRAINT script_timing_analyses_fps_denominator_check CHECK ((fps_denominator > 0)),
    CONSTRAINT script_timing_analyses_fps_numerator_check CHECK ((fps_numerator > 0)),
    CONSTRAINT script_timing_analyses_minimum_duration_ticks_check CHECK ((minimum_duration_ticks > 0)),
    CONSTRAINT script_timing_analyses_revision_check CHECK ((revision > 0)),
    CONSTRAINT script_timing_analyses_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'analyzing'::text, 'ready'::text, 'failed'::text, 'archived'::text]))),
    CONSTRAINT script_timing_analyses_target_duration_ticks_check CHECK (((target_duration_ticks IS NULL) OR (target_duration_ticks > 0))),
    CONSTRAINT script_timing_analyses_timeline_timebase_check CHECK ((timeline_timebase > 0))
);


--
-- Name: script_timing_units; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.script_timing_units (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    timing_analysis_id uuid NOT NULL,
    script_scene_id uuid,
    source_chapter_id uuid,
    unit_ordinal integer NOT NULL,
    unit_type text NOT NULL,
    track text NOT NULL,
    parallel_group text,
    speaker text,
    source_text text DEFAULT ''::text NOT NULL,
    delivery text,
    source_start_offset integer,
    source_end_offset integer,
    start_tick bigint NOT NULL,
    end_tick bigint NOT NULL,
    duration_ticks bigint GENERATED ALWAYS AS ((end_tick - start_tick)) STORED,
    min_duration_ticks bigint,
    max_duration_ticks bigint,
    duration_source text NOT NULL,
    confidence numeric(5,4),
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    source_tts_audio_clip_id uuid,
    CONSTRAINT script_timing_units_check CHECK ((end_tick > start_tick)),
    CONSTRAINT script_timing_units_check1 CHECK (((source_start_offset IS NULL) OR (source_end_offset IS NULL) OR (source_start_offset <= source_end_offset))),
    CONSTRAINT script_timing_units_check2 CHECK (((min_duration_ticks IS NULL) OR (max_duration_ticks IS NULL) OR (min_duration_ticks <= max_duration_ticks))),
    CONSTRAINT script_timing_units_confidence_check CHECK (((confidence IS NULL) OR ((confidence >= (0)::numeric) AND (confidence <= (1)::numeric)))),
    CONSTRAINT script_timing_units_duration_source_check CHECK ((duration_source = ANY (ARRAY['manual_locked'::text, 'tts_actual'::text, 'rule_estimated'::text, 'agent_estimated'::text]))),
    CONSTRAINT script_timing_units_max_duration_ticks_check CHECK (((max_duration_ticks IS NULL) OR (max_duration_ticks > 0))),
    CONSTRAINT script_timing_units_min_duration_ticks_check CHECK (((min_duration_ticks IS NULL) OR (min_duration_ticks > 0))),
    CONSTRAINT script_timing_units_source_end_offset_check CHECK (((source_end_offset IS NULL) OR (source_end_offset >= 0))),
    CONSTRAINT script_timing_units_source_start_offset_check CHECK (((source_start_offset IS NULL) OR (source_start_offset >= 0))),
    CONSTRAINT script_timing_units_start_tick_check CHECK ((start_tick >= 0)),
    CONSTRAINT script_timing_units_track_check CHECK ((track = ANY (ARRAY['audio'::text, 'visual'::text]))),
    CONSTRAINT script_timing_units_unit_ordinal_check CHECK ((unit_ordinal >= 0)),
    CONSTRAINT script_timing_units_unit_type_check CHECK ((unit_type = ANY (ARRAY['dialogue'::text, 'voiceover'::text, 'narration'::text, 'system'::text, 'action'::text, 'reaction'::text, 'establishing'::text, 'pause'::text, 'ambient_hold'::text, 'transition'::text])))
);


--
-- Name: script_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.script_versions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    script_id uuid NOT NULL,
    version_no integer NOT NULL,
    content_artifact_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    organization_id uuid,
    project_id uuid,
    version integer,
    content text,
    content_format text DEFAULT 'markdown'::text NOT NULL,
    source_type text,
    prompt_version_id uuid,
    prompt_hash text,
    status text DEFAULT 'active'::text NOT NULL,
    CONSTRAINT script_versions_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text])))
);


--
-- Name: scripts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scripts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    title text NOT NULL,
    current_version_id uuid,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_id uuid,
    status text DEFAULT 'draft'::text NOT NULL
);


--
-- Name: shot_asset_requirements; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.shot_asset_requirements (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    workflow_run_id uuid,
    storyboard_shot_id uuid NOT NULL,
    asset_id uuid NOT NULL,
    requirement_type text NOT NULL,
    role_in_shot text,
    costume text,
    pose text,
    expression text,
    action text,
    camera_relation text,
    scene_state text,
    prop_state text,
    prompt text,
    derived_artifact_id uuid,
    derived_media_file_id uuid,
    derived_storage_key text,
    status text DEFAULT 'pending'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    review_status text DEFAULT 'pending'::text NOT NULL,
    manual_override boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    edited_by uuid,
    edited_at timestamp with time zone,
    CONSTRAINT shot_asset_requirements_review_status_check CHECK ((review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'needs_edit'::text]))),
    CONSTRAINT shot_asset_requirements_stale_state_check CHECK ((stale_state = ANY (ARRAY['fresh'::text, 'upstream_changed'::text, 'needs_regeneration'::text]))),
    CONSTRAINT shot_asset_requirements_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'image_running'::text, 'image_succeeded'::text, 'image_failed'::text, 'skipped'::text])))
);


--
-- Name: storyboard_plan_reviews; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storyboard_plan_reviews (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    storyboard_plan_id uuid NOT NULL,
    revision integer NOT NULL,
    status text DEFAULT 'reviewing'::text NOT NULL,
    approved boolean DEFAULT false NOT NULL,
    issues jsonb DEFAULT '[]'::jsonb NOT NULL,
    corrections jsonb DEFAULT '[]'::jsonb NOT NULL,
    deterministic_report jsonb DEFAULT '{}'::jsonb NOT NULL,
    prompt_version_id uuid,
    prompt_hash text,
    provider_call_id uuid,
    model_id uuid,
    error_code text,
    error_message text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT storyboard_plan_reviews_corrections_check CHECK ((jsonb_typeof(corrections) = 'array'::text)),
    CONSTRAINT storyboard_plan_reviews_deterministic_report_check CHECK ((jsonb_typeof(deterministic_report) = 'object'::text)),
    CONSTRAINT storyboard_plan_reviews_issues_check CHECK ((jsonb_typeof(issues) = 'array'::text)),
    CONSTRAINT storyboard_plan_reviews_revision_check CHECK ((revision > 0)),
    CONSTRAINT storyboard_plan_reviews_status_check CHECK ((status = ANY (ARRAY['reviewing'::text, 'approved'::text, 'changes_requested'::text, 'failed'::text])))
);


--
-- Name: storyboard_plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storyboard_plans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_id uuid NOT NULL,
    script_version_id uuid NOT NULL,
    script_episode_id uuid NOT NULL,
    timing_analysis_id uuid NOT NULL,
    revision integer NOT NULL,
    status text DEFAULT 'planning'::text NOT NULL,
    pacing_profile jsonb DEFAULT '{}'::jsonb NOT NULL,
    target_duration_ticks bigint NOT NULL,
    estimated_shot_count integer DEFAULT 0 NOT NULL,
    actual_shot_count integer DEFAULT 0 NOT NULL,
    active boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    activated_at timestamp with time zone,
    CONSTRAINT storyboard_plans_actual_shot_count_check CHECK ((actual_shot_count >= 0)),
    CONSTRAINT storyboard_plans_check CHECK (((NOT active) OR (status = 'ready'::text))),
    CONSTRAINT storyboard_plans_estimated_shot_count_check CHECK ((estimated_shot_count >= 0)),
    CONSTRAINT storyboard_plans_revision_check CHECK ((revision > 0)),
    CONSTRAINT storyboard_plans_stale_state_check CHECK ((stale_state = ANY (ARRAY['fresh'::text, 'upstream_changed'::text, 'needs_regeneration'::text]))),
    CONSTRAINT storyboard_plans_status_check CHECK ((status = ANY (ARRAY['planning'::text, 'reviewing'::text, 'ready'::text, 'failed'::text, 'archived'::text]))),
    CONSTRAINT storyboard_plans_target_duration_ticks_check CHECK ((target_duration_ticks > 0))
);


--
-- Name: storyboard_scene_plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storyboard_scene_plans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    storyboard_plan_id uuid NOT NULL,
    blueprint_id uuid NOT NULL,
    script_scene_id uuid,
    scene_key text NOT NULL,
    scene_ordinal integer NOT NULL,
    dependency_group text,
    status text DEFAULT 'pending'::text NOT NULL,
    retry_generation integer DEFAULT 0 NOT NULL,
    start_tick bigint NOT NULL,
    end_tick bigint NOT NULL,
    shot_count integer DEFAULT 0 NOT NULL,
    planner_input jsonb DEFAULT '{}'::jsonb NOT NULL,
    planner_output jsonb DEFAULT '{}'::jsonb NOT NULL,
    reviewer_output jsonb DEFAULT '{}'::jsonb NOT NULL,
    entry_state jsonb DEFAULT '{}'::jsonb NOT NULL,
    exit_state jsonb DEFAULT '{}'::jsonb NOT NULL,
    prompt_version_id uuid,
    prompt_hash text,
    provider_call_id uuid,
    model_id uuid,
    error_code text,
    error_message text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT storyboard_scene_plans_check CHECK ((end_tick > start_tick)),
    CONSTRAINT storyboard_scene_plans_planner_input_check CHECK ((jsonb_typeof(planner_input) = 'object'::text)),
    CONSTRAINT storyboard_scene_plans_planner_output_check CHECK ((jsonb_typeof(planner_output) = 'object'::text)),
    CONSTRAINT storyboard_scene_plans_retry_generation_check CHECK ((retry_generation >= 0)),
    CONSTRAINT storyboard_scene_plans_reviewer_output_check CHECK ((jsonb_typeof(reviewer_output) = 'object'::text)),
    CONSTRAINT storyboard_scene_plans_scene_ordinal_check CHECK ((scene_ordinal >= 0)),
    CONSTRAINT storyboard_scene_plans_shot_count_check CHECK ((shot_count >= 0)),
    CONSTRAINT storyboard_scene_plans_start_tick_check CHECK ((start_tick >= 0)),
    CONSTRAINT storyboard_scene_plans_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'planning'::text, 'reviewing'::text, 'ready'::text, 'failed'::text, 'cancelled'::text])))
);


--
-- Name: storyboard_shot_continuity_frames; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storyboard_shot_continuity_frames (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    storyboard_shot_id uuid NOT NULL,
    source_video_artifact_id uuid NOT NULL,
    source_video_media_file_id uuid,
    frame_artifact_id uuid NOT NULL,
    frame_media_file_id uuid NOT NULL,
    storage_key text NOT NULL,
    frame_role text DEFAULT 'tail'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    frame_time_seconds numeric DEFAULT 0 NOT NULL,
    workflow_run_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT storyboard_shot_continuity_frames_frame_role_check CHECK ((frame_role = 'tail'::text)),
    CONSTRAINT storyboard_shot_continuity_frames_status_check CHECK ((status = ANY (ARRAY['active'::text, 'superseded'::text])))
);


--
-- Name: storyboard_shot_timing_spans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storyboard_shot_timing_spans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    storyboard_plan_id uuid NOT NULL,
    storyboard_shot_id uuid NOT NULL,
    timing_unit_id uuid NOT NULL,
    span_start_tick bigint NOT NULL,
    span_end_tick bigint NOT NULL,
    ordinal integer NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT storyboard_shot_timing_spans_check CHECK (((span_start_tick >= 0) AND (span_start_tick < span_end_tick))),
    CONSTRAINT storyboard_shot_timing_spans_ordinal_check CHECK ((ordinal >= 0))
);


--
-- Name: storyboard_shots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storyboard_shots (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    storyboard_id uuid,
    script_version_id uuid,
    shot_index integer NOT NULL,
    shot_size text,
    camera_move text,
    action text,
    dialogue text,
    asset_bindings jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    workflow_run_id uuid,
    storyboard_artifact_id uuid,
    shot_no integer,
    title text,
    visual text,
    camera text,
    motion text,
    mood text,
    image_prompt text,
    video_prompt text,
    image_artifact_id uuid,
    image_media_file_id uuid,
    image_storage_key text,
    video_artifact_id uuid,
    video_media_file_id uuid,
    video_storage_key text,
    video_provider_async_task_id uuid,
    video_external_task_id text,
    status text DEFAULT 'pending'::text NOT NULL,
    script_id uuid,
    storyboard_source text,
    review_status text DEFAULT 'pending'::text NOT NULL,
    manual_override boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    edited_by uuid,
    edited_at timestamp with time zone,
    script_scene_id uuid,
    deleted_at timestamp with time zone,
    image_status text DEFAULT 'not_started'::text NOT NULL,
    video_status text DEFAULT 'not_started'::text NOT NULL,
    image_error_code text,
    image_error_message text,
    video_error_code text,
    video_error_message text,
    image_started_at timestamp with time zone,
    image_completed_at timestamp with time zone,
    video_started_at timestamp with time zone,
    video_completed_at timestamp with time zone,
    image_workflow_run_id uuid,
    video_workflow_run_id uuid,
    script_episode_id uuid,
    episode_index integer,
    episode_shot_index integer,
    image_reference_mode text DEFAULT 'auto'::text NOT NULL,
    image_reference_keys text[] DEFAULT ARRAY[]::text[] NOT NULL,
    video_reference_mode text DEFAULT 'auto'::text NOT NULL,
    video_reference_keys text[] DEFAULT ARRAY[]::text[] NOT NULL,
    script_dialogue jsonb DEFAULT '[]'::jsonb NOT NULL,
    video_prompt_status text DEFAULT 'not_started'::text NOT NULL,
    video_prompt_error_code text,
    video_prompt_error_message text,
    video_prompt_workflow_run_id uuid,
    video_prompt_updated_at timestamp with time zone,
    image_prompt_status text DEFAULT 'not_started'::text NOT NULL,
    image_prompt_error_code text,
    image_prompt_error_message text,
    image_prompt_workflow_run_id uuid,
    image_prompt_updated_at timestamp with time zone,
    storyboard_plan_id uuid,
    start_tick bigint NOT NULL,
    end_tick bigint NOT NULL,
    duration_min_ticks bigint,
    duration_max_ticks bigint,
    duration_source text DEFAULT 'rule_estimated'::text NOT NULL,
    timing_confidence numeric(5,4),
    duration_locked boolean DEFAULT false NOT NULL,
    shot_group_id uuid,
    continuity_group_id uuid,
    one_take boolean DEFAULT false NOT NULL,
    timing_revision integer DEFAULT 1 NOT NULL,
    planned_duration_ticks bigint GENERATED ALWAYS AS ((end_tick - start_tick)) STORED,
    active_video_render_plan_id uuid,
    native_audio_status text DEFAULT 'not_requested'::text NOT NULL,
    production_readiness text DEFAULT 'blocked'::text NOT NULL,
    CONSTRAINT storyboard_shots_duration_bounds_valid CHECK ((((duration_min_ticks IS NULL) OR (duration_min_ticks > 0)) AND ((duration_max_ticks IS NULL) OR (duration_max_ticks > 0)) AND ((duration_min_ticks IS NULL) OR (duration_max_ticks IS NULL) OR (duration_min_ticks <= duration_max_ticks)))),
    CONSTRAINT storyboard_shots_duration_source_valid CHECK ((duration_source = ANY (ARRAY['manual_locked'::text, 'tts_actual'::text, 'rule_estimated'::text, 'agent_estimated'::text]))),
    CONSTRAINT storyboard_shots_episode_index_check CHECK (((episode_index IS NULL) OR (episode_index > 0))),
    CONSTRAINT storyboard_shots_episode_shot_index_check CHECK (((episode_shot_index IS NULL) OR (episode_shot_index >= 0))),
    CONSTRAINT storyboard_shots_image_prompt_status_check CHECK ((image_prompt_status = ANY (ARRAY['not_started'::text, 'queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text]))),
    CONSTRAINT storyboard_shots_image_reference_mode_check CHECK ((image_reference_mode = ANY (ARRAY['auto'::text, 'custom'::text, 'none'::text]))),
    CONSTRAINT storyboard_shots_native_audio_status_check CHECK ((native_audio_status = ANY (ARRAY['not_requested'::text, 'native_audio_unavailable'::text, 'audio_unverified'::text, 'audio_verified'::text, 'needs_audio_retry'::text]))),
    CONSTRAINT storyboard_shots_production_readiness_check CHECK ((production_readiness = ANY (ARRAY['ready'::text, 'preview_only'::text, 'partial'::text, 'blocked'::text]))),
    CONSTRAINT storyboard_shots_review_status_check CHECK ((review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'needs_edit'::text]))),
    CONSTRAINT storyboard_shots_script_dialogue_array_check CHECK ((jsonb_typeof(script_dialogue) = 'array'::text)),
    CONSTRAINT storyboard_shots_stale_state_check CHECK ((stale_state = ANY (ARRAY['fresh'::text, 'upstream_changed'::text, 'needs_regeneration'::text]))),
    CONSTRAINT storyboard_shots_tick_range_valid CHECK (((start_tick >= 0) AND (end_tick > start_tick))),
    CONSTRAINT storyboard_shots_timing_confidence_valid CHECK (((timing_confidence IS NULL) OR ((timing_confidence >= (0)::numeric) AND (timing_confidence <= (1)::numeric)))),
    CONSTRAINT storyboard_shots_timing_revision_positive CHECK ((timing_revision > 0)),
    CONSTRAINT storyboard_shots_video_prompt_status_check CHECK ((video_prompt_status = ANY (ARRAY['not_started'::text, 'queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text]))),
    CONSTRAINT storyboard_shots_video_reference_mode_check CHECK ((video_reference_mode = ANY (ARRAY['auto'::text, 'custom'::text, 'none'::text])))
);


--
-- Name: storyboards; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storyboards (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_id uuid,
    title text NOT NULL,
    current_version_no integer DEFAULT 1 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: team_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.team_members (
    team_id uuid NOT NULL,
    user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_by uuid
);


--
-- Name: teams; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.teams (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    description text,
    status text DEFAULT 'active'::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: timeline_clips; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.timeline_clips (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    timeline_id uuid NOT NULL,
    storyboard_shot_id uuid,
    video_artifact_id uuid,
    video_media_file_id uuid,
    clip_index integer NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    source_storage_key text,
    notes text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    manual_override boolean DEFAULT false NOT NULL,
    stale_state text DEFAULT 'fresh'::text NOT NULL,
    edited_by uuid,
    edited_at timestamp with time zone,
    start_tick bigint NOT NULL,
    end_tick bigint NOT NULL,
    source_duration_ticks bigint,
    trim_start_tick bigint DEFAULT 0 NOT NULL,
    trim_end_tick bigint,
    CONSTRAINT timeline_clips_stale_state_check CHECK ((stale_state = ANY (ARRAY['fresh'::text, 'upstream_changed'::text, 'needs_regeneration'::text]))),
    CONSTRAINT timeline_clips_tick_range_valid CHECK (((start_tick >= 0) AND (end_tick > start_tick))),
    CONSTRAINT timeline_clips_trim_range_valid CHECK (((trim_start_tick >= 0) AND ((trim_end_tick IS NULL) OR (trim_end_tick > trim_start_tick)) AND ((source_duration_ticks IS NULL) OR (source_duration_ticks > 0))))
);


--
-- Name: timing_calibration_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.timing_calibration_profiles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    revision integer NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    sample_count integer DEFAULT 0 NOT NULL,
    parameters jsonb DEFAULT '{}'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    audio_configuration_revision integer DEFAULT 1 NOT NULL,
    CONSTRAINT timing_calibration_profiles_audio_configuration_revision_positi CHECK ((audio_configuration_revision > 0)),
    CONSTRAINT timing_calibration_profiles_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT timing_calibration_profiles_parameters_check CHECK ((jsonb_typeof(parameters) = 'object'::text)),
    CONSTRAINT timing_calibration_profiles_revision_check CHECK ((revision > 0)),
    CONSTRAINT timing_calibration_profiles_sample_count_check CHECK ((sample_count >= 0)),
    CONSTRAINT timing_calibration_profiles_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text])))
);


--
-- Name: timing_calibration_samples; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.timing_calibration_samples (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_episode_id uuid,
    timing_unit_id uuid,
    storyboard_shot_id uuid,
    video_render_segment_id uuid,
    sample_kind text NOT NULL,
    sample_key text NOT NULL,
    source_kind text NOT NULL,
    expected_ticks bigint NOT NULL,
    actual_ticks bigint NOT NULL,
    timeline_timebase bigint NOT NULL,
    confidence numeric(5,4) DEFAULT 1 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    audio_configuration_revision integer DEFAULT 1 NOT NULL,
    CONSTRAINT timing_calibration_samples_actual_ticks_check CHECK ((actual_ticks > 0)),
    CONSTRAINT timing_calibration_samples_audio_configuration_revision_positiv CHECK ((audio_configuration_revision > 0)),
    CONSTRAINT timing_calibration_samples_confidence_check CHECK (((confidence >= (0)::numeric) AND (confidence <= (1)::numeric))),
    CONSTRAINT timing_calibration_samples_expected_ticks_check CHECK ((expected_ticks > 0)),
    CONSTRAINT timing_calibration_samples_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT timing_calibration_samples_sample_kind_check CHECK ((sample_kind = ANY (ARRAY['punctuation_pause'::text, 'dialogue_duration'::text, 'action_duration'::text, 'shot_pacing'::text]))),
    CONSTRAINT timing_calibration_samples_source_kind_check CHECK ((source_kind = ANY (ARRAY['tts_actual'::text, 'asr_alignment'::text, 'provider_media'::text, 'manual_review'::text]))),
    CONSTRAINT timing_calibration_samples_timeline_timebase_check CHECK ((timeline_timebase > 0))
);


--
-- Name: tts_audio_clips; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tts_audio_clips (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    script_episode_id uuid NOT NULL,
    timing_analysis_id uuid NOT NULL,
    timing_unit_id uuid NOT NULL,
    character_voice_profile_id uuid,
    workflow_run_id uuid,
    node_run_id uuid,
    model_profile_key text NOT NULL,
    provider_model_id uuid,
    provider_call_id uuid,
    source_text text NOT NULL,
    speaker text,
    language text DEFAULT 'zh-CN'::text NOT NULL,
    voice_key text NOT NULL,
    output_format text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    revision integer NOT NULL,
    active boolean DEFAULT false NOT NULL,
    artifact_id uuid,
    media_file_id uuid,
    storage_key text,
    mime_type text,
    byte_size bigint,
    sample_rate integer,
    sample_count bigint,
    channel_count integer,
    duration_ticks bigint,
    timeline_timebase bigint NOT NULL,
    error_code text,
    error_message text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    applied_timing_analysis_id uuid,
    audio_configuration_revision integer DEFAULT 1 NOT NULL,
    CONSTRAINT tts_audio_clips_audio_configuration_revision_positive CHECK ((audio_configuration_revision > 0)),
    CONSTRAINT tts_audio_clips_byte_size_check CHECK (((byte_size IS NULL) OR (byte_size >= 0))),
    CONSTRAINT tts_audio_clips_channel_count_check CHECK (((channel_count IS NULL) OR (channel_count > 0))),
    CONSTRAINT tts_audio_clips_check CHECK (((NOT active) OR (status = 'succeeded'::text))),
    CONSTRAINT tts_audio_clips_duration_ticks_check CHECK (((duration_ticks IS NULL) OR (duration_ticks > 0))),
    CONSTRAINT tts_audio_clips_metadata_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT tts_audio_clips_revision_check CHECK ((revision > 0)),
    CONSTRAINT tts_audio_clips_sample_count_check CHECK (((sample_count IS NULL) OR (sample_count > 0))),
    CONSTRAINT tts_audio_clips_sample_rate_check CHECK (((sample_rate IS NULL) OR (sample_rate > 0))),
    CONSTRAINT tts_audio_clips_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text, 'stale'::text]))),
    CONSTRAINT tts_audio_clips_timeline_timebase_check CHECK ((timeline_timebase > 0))
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email text NOT NULL,
    password_hash text,
    display_name text,
    avatar_url text,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT users_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text, 'pending'::text])))
);


--
-- Name: video_render_plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.video_render_plans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    storyboard_plan_id uuid,
    storyboard_shot_id uuid NOT NULL,
    workflow_run_id uuid,
    node_run_id uuid,
    model_profile_id uuid,
    model_profile_binding_id uuid,
    model_profile_key text,
    provider_account_id uuid NOT NULL,
    provider_model_id uuid NOT NULL,
    model_family text NOT NULL,
    variant_key text NOT NULL,
    capability_snapshot jsonb NOT NULL,
    capability_snapshot_hash text NOT NULL,
    fallback_candidates jsonb DEFAULT '[]'::jsonb NOT NULL,
    plan_key text NOT NULL,
    status text DEFAULT 'planned'::text NOT NULL,
    active boolean DEFAULT true NOT NULL,
    target_duration_ticks bigint NOT NULL,
    timeline_timebase bigint NOT NULL,
    fps_numerator integer NOT NULL,
    fps_denominator integer NOT NULL,
    task_type text NOT NULL,
    reference_mode text NOT NULL,
    aspect_ratio text NOT NULL,
    resolution text NOT NULL,
    audio_strategy text NOT NULL,
    audio_requirement text NOT NULL,
    native_audio_status text NOT NULL,
    production_readiness text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    output_artifact_id uuid,
    output_media_file_id uuid,
    output_storage_key text,
    audio_verified_by uuid,
    audio_verified_at timestamp with time zone,
    audio_verification_notes text,
    CONSTRAINT video_render_plans_audio_requirement_check CHECK ((audio_requirement = ANY (ARRAY['preferred'::text, 'required'::text, 'disabled'::text]))),
    CONSTRAINT video_render_plans_audio_strategy_check CHECK ((audio_strategy = ANY (ARRAY['native_av'::text, 'hybrid'::text, 'tts_postdub'::text]))),
    CONSTRAINT video_render_plans_capability_snapshot_check CHECK ((jsonb_typeof(capability_snapshot) = 'object'::text)),
    CONSTRAINT video_render_plans_check CHECK ((((timeline_timebase * fps_denominator) % (fps_numerator)::bigint) = 0)),
    CONSTRAINT video_render_plans_fallback_candidates_check CHECK ((jsonb_typeof(fallback_candidates) = 'array'::text)),
    CONSTRAINT video_render_plans_fps_denominator_check CHECK ((fps_denominator > 0)),
    CONSTRAINT video_render_plans_fps_numerator_check CHECK ((fps_numerator > 0)),
    CONSTRAINT video_render_plans_native_audio_status_check CHECK ((native_audio_status = ANY (ARRAY['not_requested'::text, 'native_audio_unavailable'::text, 'audio_unverified'::text, 'audio_verified'::text, 'needs_audio_retry'::text]))),
    CONSTRAINT video_render_plans_production_readiness_check CHECK ((production_readiness = ANY (ARRAY['ready'::text, 'preview_only'::text, 'partial'::text, 'blocked'::text]))),
    CONSTRAINT video_render_plans_status_check CHECK ((status = ANY (ARRAY['planned'::text, 'running'::text, 'partial_succeeded'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text, 'stale'::text, 'archived'::text, 'replan_required'::text]))),
    CONSTRAINT video_render_plans_target_duration_ticks_check CHECK ((target_duration_ticks > 0)),
    CONSTRAINT video_render_plans_timeline_timebase_check CHECK ((timeline_timebase > 0))
);


--
-- Name: video_render_segments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.video_render_segments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    video_render_plan_id uuid NOT NULL,
    storyboard_shot_id uuid NOT NULL,
    segment_index integer NOT NULL,
    planned_start_tick bigint NOT NULL,
    planned_end_tick bigint NOT NULL,
    planned_duration_ticks bigint GENERATED ALWAYS AS ((planned_end_tick - planned_start_tick)) STORED,
    requested_duration_seconds numeric NOT NULL,
    trim_end_tick bigint,
    continuity_mode text NOT NULL,
    status text DEFAULT 'planned'::text NOT NULL,
    retry_generation integer DEFAULT 0 NOT NULL,
    provider_async_task_id uuid,
    provider_call_id uuid,
    provider_model_id uuid,
    external_task_id text,
    artifact_id uuid,
    media_file_id uuid,
    storage_key text,
    prompt text,
    dialogue jsonb DEFAULT '[]'::jsonb NOT NULL,
    native_audio_requested boolean DEFAULT false NOT NULL,
    native_audio_detected boolean,
    audio_verification_status text DEFAULT 'not_requested'::text NOT NULL,
    production_readiness text DEFAULT 'blocked'::text NOT NULL,
    raw_av_artifact_id uuid,
    mezzanine_artifact_id uuid,
    extracted_audio_artifact_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_code text,
    error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    audio_verified_by uuid,
    audio_verified_at timestamp with time zone,
    audio_verification_notes text,
    CONSTRAINT video_render_segments_audio_verification_status_check CHECK ((audio_verification_status = ANY (ARRAY['not_requested'::text, 'native_audio_unavailable'::text, 'audio_unverified'::text, 'audio_verified'::text, 'needs_audio_retry'::text]))),
    CONSTRAINT video_render_segments_check CHECK ((planned_end_tick > planned_start_tick)),
    CONSTRAINT video_render_segments_planned_start_tick_check CHECK ((planned_start_tick >= 0)),
    CONSTRAINT video_render_segments_production_readiness_check CHECK ((production_readiness = ANY (ARRAY['ready'::text, 'preview_only'::text, 'partial'::text, 'blocked'::text]))),
    CONSTRAINT video_render_segments_requested_duration_seconds_check CHECK ((requested_duration_seconds > (0)::numeric)),
    CONSTRAINT video_render_segments_retry_generation_check CHECK ((retry_generation >= 0)),
    CONSTRAINT video_render_segments_segment_index_check CHECK ((segment_index >= 0)),
    CONSTRAINT video_render_segments_status_check CHECK ((status = ANY (ARRAY['planned'::text, 'queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text, 'stale'::text]))),
    CONSTRAINT video_render_segments_trim_end_tick_check CHECK (((trim_end_tick IS NULL) OR (trim_end_tick > 0)))
);


--
-- Name: workflow_node_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workflow_node_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    workflow_run_id uuid NOT NULL,
    node_key text NOT NULL,
    node_type text NOT NULL,
    status text NOT NULL,
    input jsonb DEFAULT '{}'::jsonb NOT NULL,
    output jsonb DEFAULT '{}'::jsonb NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    error_code text,
    error_message text,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_node_runs_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text, 'skipped'::text, 'waiting_review'::text])))
);


--
-- Name: workflow_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workflow_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    template_id uuid,
    temporal_workflow_id text NOT NULL,
    status text NOT NULL,
    input jsonb DEFAULT '{}'::jsonb NOT NULL,
    output jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_code text,
    error_message text,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    CONSTRAINT workflow_runs_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'queued'::text, 'running'::text, 'cancelling'::text, 'succeeded'::text, 'partial_succeeded'::text, 'failed'::text, 'cancelled'::text, 'skipped'::text, 'waiting_review'::text])))
);


--
-- Name: workflow_template_nodes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workflow_template_nodes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    template_id uuid NOT NULL,
    node_key text NOT NULL,
    node_type text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    depends_on jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: workflow_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workflow_templates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid,
    template_key text NOT NULL,
    name text NOT NULL,
    version text DEFAULT 'v1'::text NOT NULL,
    definition jsonb NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_templates_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text, 'draft'::text])))
);


--
-- Name: workspaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workspaces (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: adaptation_plans adaptation_plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.adaptation_plans
    ADD CONSTRAINT adaptation_plans_pkey PRIMARY KEY (id);


--
-- Name: agent_approvals agent_approvals_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_approvals
    ADD CONSTRAINT agent_approvals_pkey PRIMARY KEY (id);


--
-- Name: agent_messages agent_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_messages
    ADD CONSTRAINT agent_messages_pkey PRIMARY KEY (id);


--
-- Name: agent_runs agent_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_pkey PRIMARY KEY (id);


--
-- Name: agent_sessions agent_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_pkey PRIMARY KEY (id);


--
-- Name: agent_steps agent_steps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_steps
    ADD CONSTRAINT agent_steps_pkey PRIMARY KEY (id);


--
-- Name: agent_steps agent_steps_task_id_step_index_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_steps
    ADD CONSTRAINT agent_steps_task_id_step_index_key UNIQUE (task_id, step_index);


--
-- Name: agent_tasks agent_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_tasks
    ADD CONSTRAINT agent_tasks_pkey PRIMARY KEY (id);


--
-- Name: artifacts artifacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artifacts
    ADD CONSTRAINT artifacts_pkey PRIMARY KEY (id);


--
-- Name: asset_references asset_references_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_references
    ADD CONSTRAINT asset_references_pkey PRIMARY KEY (id);


--
-- Name: asset_relations asset_relations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_relations
    ADD CONSTRAINT asset_relations_pkey PRIMARY KEY (id);


--
-- Name: asset_versions asset_versions_asset_id_version_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_asset_id_version_key UNIQUE (asset_id, version);


--
-- Name: asset_versions asset_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_pkey PRIMARY KEY (id);


--
-- Name: assets assets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets
    ADD CONSTRAINT assets_pkey PRIMARY KEY (id);


--
-- Name: audio_mix_clips audio_mix_clips_audio_mix_version_id_ordinal_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_clips
    ADD CONSTRAINT audio_mix_clips_audio_mix_version_id_ordinal_key UNIQUE (audio_mix_version_id, ordinal);


--
-- Name: audio_mix_clips audio_mix_clips_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_clips
    ADD CONSTRAINT audio_mix_clips_pkey PRIMARY KEY (id);


--
-- Name: audio_mix_versions audio_mix_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_pkey PRIMARY KEY (id);


--
-- Name: audio_mix_versions audio_mix_versions_project_id_script_episode_id_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_project_id_script_episode_id_revision_key UNIQUE (project_id, script_episode_id, revision);


--
-- Name: audit_logs audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (id);


--
-- Name: auth_sessions auth_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_sessions
    ADD CONSTRAINT auth_sessions_pkey PRIMARY KEY (id);


--
-- Name: auth_sessions auth_sessions_refresh_token_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_sessions
    ADD CONSTRAINT auth_sessions_refresh_token_hash_key UNIQUE (refresh_token_hash);


--
-- Name: canonical_assets canonical_assets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_pkey PRIMARY KEY (id);


--
-- Name: canonical_assets canonical_assets_project_id_asset_type_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_project_id_asset_type_name_key UNIQUE (project_id, asset_type, name);


--
-- Name: character_voice_profiles character_voice_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.character_voice_profiles
    ADD CONSTRAINT character_voice_profiles_pkey PRIMARY KEY (id);


--
-- Name: cost_records cost_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_pkey PRIMARY KEY (id);


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_pkey PRIMARY KEY (id);


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_script_episode_id_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_script_episode_id_revision_key UNIQUE (script_episode_id, revision);


--
-- Name: event_outbox event_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event_outbox
    ADD CONSTRAINT event_outbox_pkey PRIMARY KEY (id);


--
-- Name: final_video_versions final_video_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_pkey PRIMARY KEY (id);


--
-- Name: final_video_versions final_video_versions_project_version_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_project_version_unique UNIQUE (project_id, version) DEFERRABLE;


--
-- Name: idempotency_keys idempotency_keys_organization_id_scope_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_organization_id_scope_key_key UNIQUE (organization_id, scope, key);


--
-- Name: idempotency_keys idempotency_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (id);


--
-- Name: media_files media_files_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_pkey PRIMARY KEY (id);


--
-- Name: media_variants media_variants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_variants
    ADD CONSTRAINT media_variants_pkey PRIMARY KEY (id);


--
-- Name: model_profile_bindings model_profile_bindings_model_profile_id_provider_model_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_profile_bindings
    ADD CONSTRAINT model_profile_bindings_model_profile_id_provider_model_id_key UNIQUE (model_profile_id, provider_model_id);


--
-- Name: model_profile_bindings model_profile_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_profile_bindings
    ADD CONSTRAINT model_profile_bindings_pkey PRIMARY KEY (id);


--
-- Name: model_profiles model_profiles_organization_id_profile_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_profiles
    ADD CONSTRAINT model_profiles_organization_id_profile_key_key UNIQUE (organization_id, profile_key);


--
-- Name: model_profiles model_profiles_organization_id_purpose_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_profiles
    ADD CONSTRAINT model_profiles_organization_id_purpose_key UNIQUE (organization_id, purpose);


--
-- Name: model_profiles model_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_profiles
    ADD CONSTRAINT model_profiles_pkey PRIMARY KEY (id);


--
-- Name: native_audio_reviews native_audio_reviews_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_pkey PRIMARY KEY (id);


--
-- Name: native_audio_reviews native_audio_reviews_video_render_segment_id_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_video_render_segment_id_revision_key UNIQUE (video_render_segment_id, revision);


--
-- Name: novel_chapters novel_chapters_novel_id_chapter_index_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_chapters
    ADD CONSTRAINT novel_chapters_novel_id_chapter_index_key UNIQUE (novel_id, chapter_index);


--
-- Name: novel_chapters novel_chapters_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_chapters
    ADD CONSTRAINT novel_chapters_pkey PRIMARY KEY (id);


--
-- Name: novel_event_links novel_event_links_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_event_links
    ADD CONSTRAINT novel_event_links_pkey PRIMARY KEY (id);


--
-- Name: novel_event_links novel_event_links_source_event_id_target_event_id_link_type_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_event_links
    ADD CONSTRAINT novel_event_links_source_event_id_target_event_id_link_type_key UNIQUE (source_event_id, target_event_id, link_type);


--
-- Name: novel_events novel_events_chapter_id_event_index_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_events
    ADD CONSTRAINT novel_events_chapter_id_event_index_key UNIQUE (chapter_id, event_index);


--
-- Name: novel_events novel_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_events
    ADD CONSTRAINT novel_events_pkey PRIMARY KEY (id);


--
-- Name: novels novels_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novels
    ADD CONSTRAINT novels_pkey PRIMARY KEY (id);


--
-- Name: organization_members organization_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_members
    ADD CONSTRAINT organization_members_pkey PRIMARY KEY (organization_id, user_id);


--
-- Name: organizations organizations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organizations
    ADD CONSTRAINT organizations_pkey PRIMARY KEY (id);


--
-- Name: organizations organizations_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organizations
    ADD CONSTRAINT organizations_slug_key UNIQUE (slug);


--
-- Name: permissions permissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.permissions
    ADD CONSTRAINT permissions_pkey PRIMARY KEY (permission_key);


--
-- Name: project_exports project_exports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_exports
    ADD CONSTRAINT project_exports_pkey PRIMARY KEY (id);


--
-- Name: project_manual_bindings project_manual_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_manual_bindings
    ADD CONSTRAINT project_manual_bindings_pkey PRIMARY KEY (id);


--
-- Name: project_members project_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT project_members_pkey PRIMARY KEY (project_id, user_id);


--
-- Name: project_sources project_sources_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_sources
    ADD CONSTRAINT project_sources_pkey PRIMARY KEY (id);


--
-- Name: project_timelines project_timelines_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_timelines
    ADD CONSTRAINT project_timelines_pkey PRIMARY KEY (id);


--
-- Name: projects projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);


--
-- Name: prompt_bindings prompt_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_bindings
    ADD CONSTRAINT prompt_bindings_pkey PRIMARY KEY (id);


--
-- Name: prompt_templates prompt_templates_organization_id_template_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_templates
    ADD CONSTRAINT prompt_templates_organization_id_template_key_key UNIQUE (organization_id, template_key);


--
-- Name: prompt_templates prompt_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_templates
    ADD CONSTRAINT prompt_templates_pkey PRIMARY KEY (id);


--
-- Name: prompt_versions prompt_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_versions
    ADD CONSTRAINT prompt_versions_pkey PRIMARY KEY (id);


--
-- Name: prompt_versions prompt_versions_prompt_template_id_version_no_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_versions
    ADD CONSTRAINT prompt_versions_prompt_template_id_version_no_key UNIQUE (prompt_template_id, version_no);


--
-- Name: provider_accounts provider_accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_accounts
    ADD CONSTRAINT provider_accounts_pkey PRIMARY KEY (id);


--
-- Name: provider_async_tasks provider_async_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_pkey PRIMARY KEY (id);


--
-- Name: provider_async_tasks provider_async_tasks_provider_account_id_external_task_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_provider_account_id_external_task_id_key UNIQUE (provider_account_id, external_task_id);


--
-- Name: provider_call_logs provider_call_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_pkey PRIMARY KEY (id);


--
-- Name: provider_catalog_entries provider_catalog_entries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_catalog_entries
    ADD CONSTRAINT provider_catalog_entries_pkey PRIMARY KEY (id);


--
-- Name: provider_catalog_entries provider_catalog_entries_provider_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_catalog_entries
    ADD CONSTRAINT provider_catalog_entries_provider_key_key UNIQUE (provider_key);


--
-- Name: provider_circuit_states provider_circuit_states_organization_id_provider_account_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_circuit_states
    ADD CONSTRAINT provider_circuit_states_organization_id_provider_account_id_key UNIQUE NULLS NOT DISTINCT (organization_id, provider_account_id, provider_model_id, task_type);


--
-- Name: provider_circuit_states provider_circuit_states_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_circuit_states
    ADD CONSTRAINT provider_circuit_states_pkey PRIMARY KEY (id);


--
-- Name: provider_connectors provider_connectors_connector_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_connectors
    ADD CONSTRAINT provider_connectors_connector_key_key UNIQUE (connector_key);


--
-- Name: provider_connectors provider_connectors_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_connectors
    ADD CONSTRAINT provider_connectors_pkey PRIMARY KEY (id);


--
-- Name: provider_credentials provider_credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_credentials
    ADD CONSTRAINT provider_credentials_pkey PRIMARY KEY (id);


--
-- Name: provider_endpoints provider_endpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_endpoints
    ADD CONSTRAINT provider_endpoints_pkey PRIMARY KEY (id);


--
-- Name: provider_endpoints provider_endpoints_provider_account_id_endpoint_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_endpoints
    ADD CONSTRAINT provider_endpoints_provider_account_id_endpoint_key_key UNIQUE (provider_account_id, endpoint_key);


--
-- Name: provider_leases provider_leases_lease_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_leases
    ADD CONSTRAINT provider_leases_lease_token_key UNIQUE (lease_token);


--
-- Name: provider_leases provider_leases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_leases
    ADD CONSTRAINT provider_leases_pkey PRIMARY KEY (id);


--
-- Name: provider_limit_policies provider_limit_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_limit_policies
    ADD CONSTRAINT provider_limit_policies_pkey PRIMARY KEY (id);


--
-- Name: provider_model_capabilities provider_model_capabilities_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_model_capabilities
    ADD CONSTRAINT provider_model_capabilities_pkey PRIMARY KEY (id);


--
-- Name: provider_model_capability_presets provider_model_capability_presets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_model_capability_presets
    ADD CONSTRAINT provider_model_capability_presets_pkey PRIMARY KEY (id);


--
-- Name: provider_model_capability_presets provider_model_capability_presets_preset_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_model_capability_presets
    ADD CONSTRAINT provider_model_capability_presets_preset_key_key UNIQUE (preset_key);


--
-- Name: provider_models provider_models_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_models
    ADD CONSTRAINT provider_models_pkey PRIMARY KEY (id);


--
-- Name: provider_models provider_models_provider_account_id_model_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_models
    ADD CONSTRAINT provider_models_provider_account_id_model_key_key UNIQUE (provider_account_id, model_key);


--
-- Name: provider_test_runs provider_test_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_test_runs
    ADD CONSTRAINT provider_test_runs_pkey PRIMARY KEY (id);


--
-- Name: review_fixes review_fixes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_fixes
    ADD CONSTRAINT review_fixes_pkey PRIMARY KEY (id);


--
-- Name: review_items review_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_items
    ADD CONSTRAINT review_items_pkey PRIMARY KEY (id);


--
-- Name: review_runs review_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_runs
    ADD CONSTRAINT review_runs_pkey PRIMARY KEY (id);


--
-- Name: review_tasks review_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_tasks
    ADD CONSTRAINT review_tasks_pkey PRIMARY KEY (id);


--
-- Name: role_bindings role_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_pkey PRIMARY KEY (id);


--
-- Name: role_permissions role_permissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_pkey PRIMARY KEY (role_id, permission_key);


--
-- Name: roles roles_organization_id_role_key_scope_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_organization_id_role_key_scope_key UNIQUE (organization_id, role_key, scope);


--
-- Name: roles roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);


--
-- Name: scene_asset_links scene_asset_links_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scene_asset_links
    ADD CONSTRAINT scene_asset_links_pkey PRIMARY KEY (id);


--
-- Name: scene_asset_links scene_asset_links_script_scene_id_asset_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scene_asset_links
    ADD CONSTRAINT scene_asset_links_script_scene_id_asset_id_key UNIQUE (script_scene_id, asset_id);


--
-- Name: script_asset_links script_asset_links_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_asset_links
    ADD CONSTRAINT script_asset_links_pkey PRIMARY KEY (id);


--
-- Name: script_asset_links script_asset_links_script_id_asset_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_asset_links
    ADD CONSTRAINT script_asset_links_script_id_asset_id_key UNIQUE (script_id, asset_id);


--
-- Name: script_episodes script_episodes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_pkey PRIMARY KEY (id);


--
-- Name: script_episodes script_episodes_script_version_id_episode_index_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_script_version_id_episode_index_key UNIQUE (script_version_id, episode_index);


--
-- Name: script_scenes script_scenes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_pkey PRIMARY KEY (id);


--
-- Name: script_scenes script_scenes_script_version_id_scene_index_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_script_version_id_scene_index_key UNIQUE (script_version_id, scene_index);


--
-- Name: script_timing_analyses script_timing_analyses_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_pkey PRIMARY KEY (id);


--
-- Name: script_timing_analyses script_timing_analyses_script_episode_id_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_script_episode_id_revision_key UNIQUE (script_episode_id, revision);


--
-- Name: script_timing_units script_timing_units_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_units
    ADD CONSTRAINT script_timing_units_pkey PRIMARY KEY (id);


--
-- Name: script_timing_units script_timing_units_timing_analysis_id_unit_ordinal_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_units
    ADD CONSTRAINT script_timing_units_timing_analysis_id_unit_ordinal_key UNIQUE (timing_analysis_id, unit_ordinal);


--
-- Name: script_versions script_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_pkey PRIMARY KEY (id);


--
-- Name: script_versions script_versions_script_id_version_no_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_script_id_version_no_key UNIQUE (script_id, version_no);


--
-- Name: scripts scripts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_pkey PRIMARY KEY (id);


--
-- Name: shot_asset_requirements shot_asset_requirements_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_pkey PRIMARY KEY (id);


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_pkey PRIMARY KEY (id);


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_storyboard_plan_id_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_storyboard_plan_id_revision_key UNIQUE (storyboard_plan_id, revision);


--
-- Name: storyboard_plans storyboard_plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_pkey PRIMARY KEY (id);


--
-- Name: storyboard_plans storyboard_plans_script_episode_id_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_script_episode_id_revision_key UNIQUE (script_episode_id, revision);


--
-- Name: storyboard_scene_plans storyboard_scene_plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_pkey PRIMARY KEY (id);


--
-- Name: storyboard_scene_plans storyboard_scene_plans_storyboard_plan_id_scene_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_storyboard_plan_id_scene_key_key UNIQUE (storyboard_plan_id, scene_key);


--
-- Name: storyboard_scene_plans storyboard_scene_plans_storyboard_plan_id_scene_ordinal_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_storyboard_plan_id_scene_ordinal_key UNIQUE (storyboard_plan_id, scene_ordinal);


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_fr_storyboard_shot_id_source_vid_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_fr_storyboard_shot_id_source_vid_key UNIQUE (storyboard_shot_id, source_video_artifact_id, frame_role);


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_pkey PRIMARY KEY (id);


--
-- Name: storyboard_shot_timing_spans storyboard_shot_timing_spans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_timing_spans
    ADD CONSTRAINT storyboard_shot_timing_spans_pkey PRIMARY KEY (id);


--
-- Name: storyboard_shot_timing_spans storyboard_shot_timing_spans_storyboard_plan_id_storyboard__key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_timing_spans
    ADD CONSTRAINT storyboard_shot_timing_spans_storyboard_plan_id_storyboard__key UNIQUE (storyboard_plan_id, storyboard_shot_id, timing_unit_id, span_start_tick, span_end_tick);


--
-- Name: storyboard_shot_timing_spans storyboard_shot_timing_spans_storyboard_plan_id_storyboard_key1; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_timing_spans
    ADD CONSTRAINT storyboard_shot_timing_spans_storyboard_plan_id_storyboard_key1 UNIQUE (storyboard_plan_id, storyboard_shot_id, ordinal);


--
-- Name: storyboard_shot_timing_spans storyboard_shot_timing_spans_storyboard_plan_id_timing_uni_excl; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_timing_spans
    ADD CONSTRAINT storyboard_shot_timing_spans_storyboard_plan_id_timing_uni_excl EXCLUDE USING gist (storyboard_plan_id WITH =, timing_unit_id WITH =, int8range(span_start_tick, span_end_tick, '[)'::text) WITH &&);


--
-- Name: storyboard_shots storyboard_shots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_pkey PRIMARY KEY (id);


--
-- Name: storyboard_shots storyboard_shots_storyboard_id_shot_index_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_storyboard_id_shot_index_key UNIQUE (storyboard_id, shot_index);


--
-- Name: storyboards storyboards_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboards
    ADD CONSTRAINT storyboards_pkey PRIMARY KEY (id);


--
-- Name: team_members team_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.team_members
    ADD CONSTRAINT team_members_pkey PRIMARY KEY (team_id, user_id);


--
-- Name: teams teams_organization_id_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_organization_id_slug_key UNIQUE (organization_id, slug);


--
-- Name: teams teams_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_pkey PRIMARY KEY (id);


--
-- Name: timeline_clips timeline_clips_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_pkey PRIMARY KEY (id);


--
-- Name: timeline_clips timeline_clips_timeline_index_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_timeline_index_unique UNIQUE (timeline_id, clip_index) DEFERRABLE;


--
-- Name: timing_calibration_profiles timing_calibration_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_profiles
    ADD CONSTRAINT timing_calibration_profiles_pkey PRIMARY KEY (id);


--
-- Name: timing_calibration_profiles timing_calibration_profiles_project_id_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_profiles
    ADD CONSTRAINT timing_calibration_profiles_project_id_revision_key UNIQUE (project_id, revision);


--
-- Name: timing_calibration_samples timing_calibration_samples_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_samples
    ADD CONSTRAINT timing_calibration_samples_pkey PRIMARY KEY (id);


--
-- Name: tts_audio_clips tts_audio_clips_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_pkey PRIMARY KEY (id);


--
-- Name: tts_audio_clips tts_audio_clips_timing_unit_id_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_timing_unit_id_revision_key UNIQUE (timing_unit_id, revision);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: video_render_plans video_render_plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_pkey PRIMARY KEY (id);


--
-- Name: video_render_plans video_render_plans_project_id_plan_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_project_id_plan_key_key UNIQUE (project_id, plan_key);


--
-- Name: video_render_segments video_render_segments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_pkey PRIMARY KEY (id);


--
-- Name: video_render_segments video_render_segments_video_render_plan_id_segment_index_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_video_render_plan_id_segment_index_key UNIQUE (video_render_plan_id, segment_index);


--
-- Name: workflow_node_runs workflow_node_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_node_runs
    ADD CONSTRAINT workflow_node_runs_pkey PRIMARY KEY (id);


--
-- Name: workflow_node_runs workflow_node_runs_workflow_run_id_node_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_node_runs
    ADD CONSTRAINT workflow_node_runs_workflow_run_id_node_key_key UNIQUE (workflow_run_id, node_key);


--
-- Name: workflow_runs workflow_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_runs
    ADD CONSTRAINT workflow_runs_pkey PRIMARY KEY (id);


--
-- Name: workflow_runs workflow_runs_temporal_workflow_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_runs
    ADD CONSTRAINT workflow_runs_temporal_workflow_id_key UNIQUE (temporal_workflow_id);


--
-- Name: workflow_template_nodes workflow_template_nodes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_template_nodes
    ADD CONSTRAINT workflow_template_nodes_pkey PRIMARY KEY (id);


--
-- Name: workflow_template_nodes workflow_template_nodes_template_id_node_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_template_nodes
    ADD CONSTRAINT workflow_template_nodes_template_id_node_key_key UNIQUE (template_id, node_key);


--
-- Name: workflow_templates workflow_templates_organization_id_template_key_version_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_templates
    ADD CONSTRAINT workflow_templates_organization_id_template_key_version_key UNIQUE (organization_id, template_key, version);


--
-- Name: workflow_templates workflow_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_templates
    ADD CONSTRAINT workflow_templates_pkey PRIMARY KEY (id);


--
-- Name: workspaces workspaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_pkey PRIMARY KEY (id);


--
-- Name: agent_approvals_step_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_approvals_step_idx ON public.agent_approvals USING btree (step_id) WHERE (step_id IS NOT NULL);


--
-- Name: agent_approvals_task_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_approvals_task_status_idx ON public.agent_approvals USING btree (task_id, status, created_at DESC);


--
-- Name: agent_messages_session_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_messages_session_idx ON public.agent_messages USING btree (session_id, created_at);


--
-- Name: agent_runs_project_type_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_runs_project_type_idx ON public.agent_runs USING btree (project_id, agent_type, task_type, created_at DESC);


--
-- Name: agent_runs_step_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_runs_step_idx ON public.agent_runs USING btree (step_id) WHERE (step_id IS NOT NULL);


--
-- Name: agent_runs_task_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_runs_task_idx ON public.agent_runs USING btree (task_id) WHERE (task_id IS NOT NULL);


--
-- Name: agent_sessions_project_type_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_sessions_project_type_idx ON public.agent_sessions USING btree (project_id, agent_type, created_at DESC);


--
-- Name: agent_steps_task_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_steps_task_status_idx ON public.agent_steps USING btree (task_id, status, step_index);


--
-- Name: agent_steps_tool_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_steps_tool_idx ON public.agent_steps USING btree (tool_name, status);


--
-- Name: agent_tasks_org_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_tasks_org_status_idx ON public.agent_tasks USING btree (organization_id, status, created_at DESC);


--
-- Name: agent_tasks_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_tasks_project_status_idx ON public.agent_tasks USING btree (project_id, status, created_at DESC);


--
-- Name: agent_tasks_session_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_tasks_session_idx ON public.agent_tasks USING btree (session_id, created_at DESC) WHERE (session_id IS NOT NULL);


--
-- Name: agent_tasks_temporal_workflow_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_tasks_temporal_workflow_idx ON public.agent_tasks USING btree (temporal_workflow_id) WHERE (temporal_workflow_id IS NOT NULL);


--
-- Name: artifacts_model_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX artifacts_model_id_idx ON public.artifacts USING btree (model_id);


--
-- Name: artifacts_node_run_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX artifacts_node_run_id_idx ON public.artifacts USING btree (node_run_id);


--
-- Name: artifacts_org_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX artifacts_org_project_idx ON public.artifacts USING btree (organization_id, project_id);


--
-- Name: artifacts_workflow_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX artifacts_workflow_id_idx ON public.artifacts USING btree (workflow_run_id);


--
-- Name: assets_project_type_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX assets_project_type_idx ON public.assets USING btree (project_id, asset_type, created_at DESC);


--
-- Name: audio_mix_clips_mix_track_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audio_mix_clips_mix_track_idx ON public.audio_mix_clips USING btree (audio_mix_version_id, track_kind, start_tick);


--
-- Name: audio_mix_versions_one_active_per_episode_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX audio_mix_versions_one_active_per_episode_idx ON public.audio_mix_versions USING btree (project_id, script_episode_id) WHERE ((active = true) AND (status = 'ready'::text));


--
-- Name: audio_mix_versions_project_configuration_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audio_mix_versions_project_configuration_idx ON public.audio_mix_versions USING btree (project_id, audio_configuration_revision, status, created_at DESC);


--
-- Name: audio_mix_versions_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audio_mix_versions_project_status_idx ON public.audio_mix_versions USING btree (project_id, status, created_at DESC);


--
-- Name: audit_logs_actor_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_logs_actor_user_id_idx ON public.audit_logs USING btree (actor_user_id);


--
-- Name: audit_logs_org_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_logs_org_created_idx ON public.audit_logs USING btree (organization_id, created_at DESC);


--
-- Name: auth_sessions_organization_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX auth_sessions_organization_id_idx ON public.auth_sessions USING btree (organization_id);


--
-- Name: auth_sessions_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX auth_sessions_user_id_idx ON public.auth_sessions USING btree (user_id);


--
-- Name: canonical_assets_project_review_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX canonical_assets_project_review_idx ON public.canonical_assets USING btree (project_id, review_status);


--
-- Name: canonical_assets_project_type_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX canonical_assets_project_type_idx ON public.canonical_assets USING btree (project_id, asset_type, created_at DESC);


--
-- Name: character_voice_profiles_one_default_per_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX character_voice_profiles_one_default_per_project_idx ON public.character_voice_profiles USING btree (project_id) WHERE ((status = 'active'::text) AND (is_default = true));


--
-- Name: character_voice_profiles_project_character_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX character_voice_profiles_project_character_active_idx ON public.character_voice_profiles USING btree (project_id, lower(character_name)) WHERE (status = 'active'::text);


--
-- Name: character_voice_profiles_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX character_voice_profiles_project_status_idx ON public.character_voice_profiles USING btree (project_id, status, updated_at DESC);


--
-- Name: cost_records_call_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX cost_records_call_id_idx ON public.cost_records USING btree (provider_call_id);


--
-- Name: cost_records_org_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX cost_records_org_created_idx ON public.cost_records USING btree (organization_id, created_at DESC);


--
-- Name: episode_continuity_blueprints_episode_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX episode_continuity_blueprints_episode_idx ON public.episode_continuity_blueprints USING btree (script_episode_id, revision DESC);


--
-- Name: episode_continuity_blueprints_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX episode_continuity_blueprints_project_status_idx ON public.episode_continuity_blueprints USING btree (project_id, status, created_at DESC);


--
-- Name: event_outbox_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX event_outbox_pending_idx ON public.event_outbox USING btree (next_attempt_at, created_at) WHERE (status = 'pending'::text);


--
-- Name: final_video_versions_project_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX final_video_versions_project_created_idx ON public.final_video_versions USING btree (project_id, created_at DESC);


--
-- Name: final_video_versions_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX final_video_versions_project_status_idx ON public.final_video_versions USING btree (project_id, status);


--
-- Name: idempotency_keys_org_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idempotency_keys_org_scope_idx ON public.idempotency_keys USING btree (organization_id, scope);


--
-- Name: idx_adaptation_plans_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_adaptation_plans_project ON public.adaptation_plans USING btree (project_id, created_at DESC);


--
-- Name: idx_adaptation_plans_review; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_adaptation_plans_review ON public.adaptation_plans USING btree (project_id, review_status);


--
-- Name: idx_adaptation_plans_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_adaptation_plans_source ON public.adaptation_plans USING btree (source_id, created_at DESC);


--
-- Name: idx_adaptation_plans_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_adaptation_plans_status ON public.adaptation_plans USING btree (project_id, status);


--
-- Name: idx_asset_references_asset; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_asset_references_asset ON public.asset_references USING btree (asset_id, created_at DESC);


--
-- Name: idx_asset_references_one_primary; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_asset_references_one_primary ON public.asset_references USING btree (asset_id) WHERE ((is_primary = true) AND (status = 'ready'::text));


--
-- Name: idx_asset_references_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_asset_references_project ON public.asset_references USING btree (project_id, created_at DESC);


--
-- Name: idx_canonical_assets_project_manual_override; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_canonical_assets_project_manual_override ON public.canonical_assets USING btree (project_id, manual_override);


--
-- Name: idx_canonical_assets_project_stale_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_canonical_assets_project_stale_state ON public.canonical_assets USING btree (project_id, stale_state);


--
-- Name: idx_canonical_assets_project_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_canonical_assets_project_status ON public.canonical_assets USING btree (project_id, status, asset_type, name);


--
-- Name: idx_cost_records_budget_window; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cost_records_budget_window ON public.cost_records USING btree (organization_id, provider_model_id, currency, created_at);


--
-- Name: idx_novel_event_links_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_novel_event_links_project ON public.novel_event_links USING btree (project_id);


--
-- Name: idx_novel_event_links_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_novel_event_links_source ON public.novel_event_links USING btree (source_event_id);


--
-- Name: idx_novel_event_links_target; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_novel_event_links_target ON public.novel_event_links USING btree (target_event_id);


--
-- Name: idx_novel_events_chapter; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_novel_events_chapter ON public.novel_events USING btree (chapter_id, event_index);


--
-- Name: idx_novel_events_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_novel_events_project ON public.novel_events USING btree (project_id, sequence_no);


--
-- Name: idx_novel_events_review; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_novel_events_review ON public.novel_events USING btree (project_id, review_status);


--
-- Name: idx_novel_events_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_novel_events_source ON public.novel_events USING btree (source_id, sequence_no);


--
-- Name: idx_project_exports_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_project_exports_project ON public.project_exports USING btree (project_id, created_at DESC);


--
-- Name: idx_project_exports_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_project_exports_status ON public.project_exports USING btree (project_id, status);


--
-- Name: idx_project_manual_bindings_one_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_project_manual_bindings_one_active ON public.project_manual_bindings USING btree (project_id, manual_kind) WHERE (status = 'active'::text);


--
-- Name: idx_project_manual_bindings_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_project_manual_bindings_project ON public.project_manual_bindings USING btree (project_id, manual_kind, status);


--
-- Name: idx_project_sources_project_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_project_sources_project_status ON public.project_sources USING btree (project_id, status, created_at DESC);


--
-- Name: idx_prompt_bindings_active_org; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_prompt_bindings_active_org ON public.prompt_bindings USING btree (organization_id, template_key) WHERE ((project_id IS NULL) AND (status = 'active'::text));


--
-- Name: idx_prompt_bindings_active_project; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_prompt_bindings_active_project ON public.prompt_bindings USING btree (project_id, template_key) WHERE ((project_id IS NOT NULL) AND (status = 'active'::text));


--
-- Name: idx_prompt_bindings_org_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prompt_bindings_org_key ON public.prompt_bindings USING btree (organization_id, template_key, status);


--
-- Name: idx_prompt_bindings_project_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prompt_bindings_project_key ON public.prompt_bindings USING btree (project_id, template_key, status);


--
-- Name: idx_prompt_templates_system_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_prompt_templates_system_key ON public.prompt_templates USING btree (template_key) WHERE (organization_id IS NULL);


--
-- Name: idx_prompt_versions_one_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_prompt_versions_one_active ON public.prompt_versions USING btree (template_id) WHERE (status = 'active'::text);


--
-- Name: idx_prompt_versions_template_version; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_prompt_versions_template_version ON public.prompt_versions USING btree (template_id, version);


--
-- Name: idx_provider_async_tasks_external; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_async_tasks_external ON public.provider_async_tasks USING btree (provider_account_id, external_task_id);


--
-- Name: idx_provider_async_tasks_next_poll; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_async_tasks_next_poll ON public.provider_async_tasks USING btree (status, next_poll_at);


--
-- Name: idx_provider_async_tasks_org_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_async_tasks_org_status ON public.provider_async_tasks USING btree (organization_id, status);


--
-- Name: idx_provider_call_logs_limit_window; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_call_logs_limit_window ON public.provider_call_logs USING btree (organization_id, provider_account_id, provider_model_id, task_type, created_at);


--
-- Name: idx_provider_circuit_states_account; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_circuit_states_account ON public.provider_circuit_states USING btree (provider_account_id);


--
-- Name: idx_provider_circuit_states_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_circuit_states_org ON public.provider_circuit_states USING btree (organization_id);


--
-- Name: idx_provider_circuit_states_task; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_circuit_states_task ON public.provider_circuit_states USING btree (organization_id, task_type, state);


--
-- Name: idx_provider_leases_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_leases_active ON public.provider_leases USING btree (organization_id, provider_account_id, provider_model_id, task_type, status, expires_at);


--
-- Name: idx_provider_leases_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_leases_expiry ON public.provider_leases USING btree (status, expires_at);


--
-- Name: idx_provider_limit_policies_account; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_limit_policies_account ON public.provider_limit_policies USING btree (provider_account_id);


--
-- Name: idx_provider_limit_policies_model; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_limit_policies_model ON public.provider_limit_policies USING btree (provider_model_id);


--
-- Name: idx_provider_limit_policies_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_limit_policies_org ON public.provider_limit_policies USING btree (organization_id);


--
-- Name: idx_provider_limit_policies_task; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provider_limit_policies_task ON public.provider_limit_policies USING btree (organization_id, task_type, enabled);


--
-- Name: idx_review_fixes_item; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_fixes_item ON public.review_fixes USING btree (review_item_id, created_at DESC);


--
-- Name: idx_review_fixes_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_fixes_project ON public.review_fixes USING btree (project_id, created_at DESC);


--
-- Name: idx_review_fixes_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_fixes_status ON public.review_fixes USING btree (project_id, status);


--
-- Name: idx_review_items_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_items_category ON public.review_items USING btree (project_id, category);


--
-- Name: idx_review_items_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_items_entity ON public.review_items USING btree (entity_type, entity_id);


--
-- Name: idx_review_items_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_items_project ON public.review_items USING btree (project_id, created_at DESC);


--
-- Name: idx_review_items_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_items_status ON public.review_items USING btree (project_id, status);


--
-- Name: idx_review_runs_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_runs_project ON public.review_runs USING btree (project_id, created_at DESC);


--
-- Name: idx_review_runs_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_review_runs_status ON public.review_runs USING btree (project_id, status);


--
-- Name: idx_scene_asset_links_asset; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scene_asset_links_asset ON public.scene_asset_links USING btree (asset_id);


--
-- Name: idx_scene_asset_links_scene; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scene_asset_links_scene ON public.scene_asset_links USING btree (script_scene_id);


--
-- Name: idx_script_episodes_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_episodes_project ON public.script_episodes USING btree (project_id, script_id, episode_index);


--
-- Name: idx_script_episodes_source_chapter; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_episodes_source_chapter ON public.script_episodes USING btree (project_id, source_chapter_id) WHERE (source_chapter_id IS NOT NULL);


--
-- Name: idx_script_episodes_version; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_episodes_version ON public.script_episodes USING btree (script_version_id, episode_index);


--
-- Name: idx_script_episodes_version_source_chapter_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_script_episodes_version_source_chapter_unique ON public.script_episodes USING btree (script_version_id, source_chapter_id) WHERE (source_chapter_id IS NOT NULL);


--
-- Name: idx_script_scenes_episode; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_scenes_episode ON public.script_scenes USING btree (script_episode_id) WHERE (script_episode_id IS NOT NULL);


--
-- Name: idx_script_scenes_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_scenes_project ON public.script_scenes USING btree (project_id, created_at DESC);


--
-- Name: idx_script_scenes_project_deleted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_scenes_project_deleted ON public.script_scenes USING btree (project_id, deleted_at, script_id, scene_index);


--
-- Name: idx_script_scenes_review; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_scenes_review ON public.script_scenes USING btree (project_id, review_status);


--
-- Name: idx_script_scenes_script; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_scenes_script ON public.script_scenes USING btree (script_id, scene_index);


--
-- Name: idx_script_scenes_version; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_scenes_version ON public.script_scenes USING btree (script_version_id, scene_index);


--
-- Name: idx_script_versions_project_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_script_versions_project_status ON public.script_versions USING btree (project_id, script_id, status, version DESC);


--
-- Name: idx_shot_asset_requirements_project_manual_override; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_shot_asset_requirements_project_manual_override ON public.shot_asset_requirements USING btree (project_id, manual_override);


--
-- Name: idx_shot_asset_requirements_project_stale_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_shot_asset_requirements_project_stale_state ON public.shot_asset_requirements USING btree (project_id, stale_state);


--
-- Name: idx_storyboard_shots_image_prompt_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_image_prompt_status ON public.storyboard_shots USING btree (project_id, image_prompt_status) WHERE (deleted_at IS NULL);


--
-- Name: idx_storyboard_shots_image_prompt_workflow; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_image_prompt_workflow ON public.storyboard_shots USING btree (image_prompt_workflow_run_id) WHERE (image_prompt_workflow_run_id IS NOT NULL);


--
-- Name: idx_storyboard_shots_image_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_image_status ON public.storyboard_shots USING btree (project_id, image_status) WHERE (deleted_at IS NULL);


--
-- Name: idx_storyboard_shots_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_project ON public.storyboard_shots USING btree (project_id, created_at DESC);


--
-- Name: idx_storyboard_shots_project_episode; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_project_episode ON public.storyboard_shots USING btree (project_id, episode_index, episode_shot_index) WHERE ((episode_index IS NOT NULL) AND (deleted_at IS NULL));


--
-- Name: idx_storyboard_shots_project_manual_override; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_project_manual_override ON public.storyboard_shots USING btree (project_id, manual_override);


--
-- Name: idx_storyboard_shots_project_scene; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_project_scene ON public.storyboard_shots USING btree (project_id, script_scene_id, shot_index) WHERE (deleted_at IS NULL);


--
-- Name: idx_storyboard_shots_project_stale_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_project_stale_state ON public.storyboard_shots USING btree (project_id, stale_state);


--
-- Name: idx_storyboard_shots_script_episode; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_script_episode ON public.storyboard_shots USING btree (script_episode_id, episode_shot_index) WHERE ((script_episode_id IS NOT NULL) AND (deleted_at IS NULL));


--
-- Name: idx_storyboard_shots_script_scene; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_script_scene ON public.storyboard_shots USING btree (script_scene_id);


--
-- Name: idx_storyboard_shots_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_status ON public.storyboard_shots USING btree (workflow_run_id, status);


--
-- Name: idx_storyboard_shots_video_prompt_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_video_prompt_status ON public.storyboard_shots USING btree (project_id, video_prompt_status) WHERE (deleted_at IS NULL);


--
-- Name: idx_storyboard_shots_video_prompt_workflow; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_video_prompt_workflow ON public.storyboard_shots USING btree (video_prompt_workflow_run_id) WHERE (video_prompt_workflow_run_id IS NOT NULL);


--
-- Name: idx_storyboard_shots_video_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_video_status ON public.storyboard_shots USING btree (project_id, video_status) WHERE (deleted_at IS NULL);


--
-- Name: idx_storyboard_shots_workflow; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storyboard_shots_workflow ON public.storyboard_shots USING btree (workflow_run_id, shot_index);


--
-- Name: media_files_artifact_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX media_files_artifact_idx ON public.media_files USING btree (artifact_id);


--
-- Name: media_files_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX media_files_project_idx ON public.media_files USING btree (project_id, created_at DESC);


--
-- Name: model_profile_bindings_model_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX model_profile_bindings_model_id_idx ON public.model_profile_bindings USING btree (provider_model_id);


--
-- Name: model_profile_bindings_profile_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX model_profile_bindings_profile_id_idx ON public.model_profile_bindings USING btree (model_profile_id);


--
-- Name: model_profiles_org_key_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX model_profiles_org_key_idx ON public.model_profiles USING btree (organization_id, profile_key);


--
-- Name: native_audio_reviews_plan_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX native_audio_reviews_plan_status_idx ON public.native_audio_reviews USING btree (video_render_plan_id, status, created_at DESC);


--
-- Name: native_audio_reviews_project_configuration_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX native_audio_reviews_project_configuration_idx ON public.native_audio_reviews USING btree (project_id, audio_configuration_revision, status, created_at DESC);


--
-- Name: novel_chapters_source_index_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX novel_chapters_source_index_unique ON public.novel_chapters USING btree (source_id, chapter_index) WHERE (source_id IS NOT NULL);


--
-- Name: novel_chapters_source_volume_section_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX novel_chapters_source_volume_section_idx ON public.novel_chapters USING btree (source_id, volume_index, section_index, chapter_index) WHERE (source_id IS NOT NULL);


--
-- Name: novels_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX novels_project_idx ON public.novels USING btree (project_id, created_at DESC);


--
-- Name: organization_members_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX organization_members_user_id_idx ON public.organization_members USING btree (user_id);


--
-- Name: permissions_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX permissions_id_unique ON public.permissions USING btree (id);


--
-- Name: project_members_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX project_members_user_id_idx ON public.project_members USING btree (user_id);


--
-- Name: project_sources_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX project_sources_project_idx ON public.project_sources USING btree (project_id, created_at DESC);


--
-- Name: project_timelines_project_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX project_timelines_project_created_idx ON public.project_timelines USING btree (project_id, created_at DESC);


--
-- Name: project_timelines_project_stale_state_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX project_timelines_project_stale_state_idx ON public.project_timelines USING btree (project_id, stale_state);


--
-- Name: project_timelines_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX project_timelines_project_status_idx ON public.project_timelines USING btree (project_id, status);


--
-- Name: projects_organization_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX projects_organization_id_idx ON public.projects USING btree (organization_id);


--
-- Name: projects_workspace_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX projects_workspace_id_idx ON public.projects USING btree (workspace_id);


--
-- Name: prompt_templates_org_key_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX prompt_templates_org_key_idx ON public.prompt_templates USING btree (organization_id, template_key);


--
-- Name: prompt_versions_template_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX prompt_versions_template_id_idx ON public.prompt_versions USING btree (prompt_template_id);


--
-- Name: provider_accounts_connector_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_accounts_connector_id_idx ON public.provider_accounts USING btree (connector_id);


--
-- Name: provider_accounts_organization_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_accounts_organization_status_idx ON public.provider_accounts USING btree (organization_id, status, created_at DESC);


--
-- Name: provider_async_tasks_call_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_async_tasks_call_id_idx ON public.provider_async_tasks USING btree (provider_call_id);


--
-- Name: provider_async_tasks_org_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_async_tasks_org_status_idx ON public.provider_async_tasks USING btree (organization_id, status);


--
-- Name: provider_async_tasks_render_segment_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_async_tasks_render_segment_idx ON public.provider_async_tasks USING btree (video_render_segment_id) WHERE (video_render_segment_id IS NOT NULL);


--
-- Name: provider_call_logs_account_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_call_logs_account_created_idx ON public.provider_call_logs USING btree (provider_account_id, created_at DESC);


--
-- Name: provider_call_logs_credential_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_call_logs_credential_id_idx ON public.provider_call_logs USING btree (credential_id);


--
-- Name: provider_call_logs_model_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_call_logs_model_created_idx ON public.provider_call_logs USING btree (provider_model_id, created_at DESC);


--
-- Name: provider_call_logs_org_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_call_logs_org_created_idx ON public.provider_call_logs USING btree (organization_id, created_at DESC);


--
-- Name: provider_call_logs_profile_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_call_logs_profile_id_idx ON public.provider_call_logs USING btree (model_profile_id);


--
-- Name: provider_call_logs_project_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_call_logs_project_id_idx ON public.provider_call_logs USING btree (project_id);


--
-- Name: provider_call_logs_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_call_logs_status_idx ON public.provider_call_logs USING btree (status);


--
-- Name: provider_catalog_entries_enabled_category_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_catalog_entries_enabled_category_idx ON public.provider_catalog_entries USING btree (enabled, category, display_name);


--
-- Name: provider_credentials_account_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_credentials_account_id_idx ON public.provider_credentials USING btree (provider_account_id);


--
-- Name: provider_credentials_active_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX provider_credentials_active_unique ON public.provider_credentials USING btree (provider_account_id, credential_key) WHERE (is_active = true);


--
-- Name: provider_credentials_organization_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_credentials_organization_id_idx ON public.provider_credentials USING btree (organization_id);


--
-- Name: provider_endpoints_account_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_endpoints_account_id_idx ON public.provider_endpoints USING btree (provider_account_id);


--
-- Name: provider_leases_org_model_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_leases_org_model_idx ON public.provider_leases USING btree (organization_id, provider_model_id);


--
-- Name: provider_leases_status_expires_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_leases_status_expires_idx ON public.provider_leases USING btree (status, expires_at);


--
-- Name: provider_model_capabilities_model_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_model_capabilities_model_id_idx ON public.provider_model_capabilities USING btree (provider_model_id);


--
-- Name: provider_model_capability_presets_enabled_priority_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_model_capability_presets_enabled_priority_idx ON public.provider_model_capability_presets USING btree (enabled, priority, preset_key);


--
-- Name: provider_models_account_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_models_account_status_idx ON public.provider_models USING btree (provider_account_id, status);


--
-- Name: provider_test_runs_account_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_test_runs_account_id_idx ON public.provider_test_runs USING btree (provider_account_id);


--
-- Name: provider_test_runs_model_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_test_runs_model_id_idx ON public.provider_test_runs USING btree (provider_model_id);


--
-- Name: provider_test_runs_org_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX provider_test_runs_org_created_idx ON public.provider_test_runs USING btree (organization_id, created_at DESC);


--
-- Name: review_tasks_org_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX review_tasks_org_status_idx ON public.review_tasks USING btree (organization_id, status);


--
-- Name: role_bindings_organization_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX role_bindings_organization_id_idx ON public.role_bindings USING btree (organization_id);


--
-- Name: role_bindings_resource_project_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX role_bindings_resource_project_id_idx ON public.role_bindings USING btree (resource_project_id);


--
-- Name: role_bindings_subject_team_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX role_bindings_subject_team_id_idx ON public.role_bindings USING btree (subject_team_id);


--
-- Name: role_bindings_subject_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX role_bindings_subject_user_id_idx ON public.role_bindings USING btree (subject_user_id);


--
-- Name: role_bindings_team_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX role_bindings_team_unique ON public.role_bindings USING btree (role_id, subject_team_id, resource_type, COALESCE(resource_organization_id, resource_workspace_id, resource_project_id)) WHERE (subject_type = 'team'::text);


--
-- Name: role_bindings_user_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX role_bindings_user_unique ON public.role_bindings USING btree (role_id, subject_user_id, resource_type, COALESCE(resource_organization_id, resource_workspace_id, resource_project_id)) WHERE (subject_type = 'user'::text);


--
-- Name: roles_system_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX roles_system_unique ON public.roles USING btree (role_key, scope) WHERE (organization_id IS NULL);


--
-- Name: script_asset_links_script_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX script_asset_links_script_idx ON public.script_asset_links USING btree (script_id);


--
-- Name: script_timing_analyses_episode_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX script_timing_analyses_episode_status_idx ON public.script_timing_analyses USING btree (script_episode_id, status, revision DESC);


--
-- Name: script_timing_analyses_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX script_timing_analyses_project_idx ON public.script_timing_analyses USING btree (project_id, created_at DESC);


--
-- Name: script_timing_units_analysis_range_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX script_timing_units_analysis_range_idx ON public.script_timing_units USING btree (timing_analysis_id, start_tick, end_tick);


--
-- Name: script_timing_units_scene_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX script_timing_units_scene_idx ON public.script_timing_units USING btree (script_scene_id, unit_ordinal);


--
-- Name: script_versions_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX script_versions_project_idx ON public.script_versions USING btree (project_id, script_id, version DESC);


--
-- Name: script_versions_script_version_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX script_versions_script_version_unique ON public.script_versions USING btree (script_id, version);


--
-- Name: scripts_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX scripts_project_idx ON public.scripts USING btree (project_id, created_at DESC);


--
-- Name: scripts_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX scripts_project_status_idx ON public.scripts USING btree (project_id, status, created_at DESC);


--
-- Name: shot_asset_requirements_asset_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shot_asset_requirements_asset_idx ON public.shot_asset_requirements USING btree (asset_id);


--
-- Name: shot_asset_requirements_project_review_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shot_asset_requirements_project_review_idx ON public.shot_asset_requirements USING btree (project_id, review_status);


--
-- Name: shot_asset_requirements_shot_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shot_asset_requirements_shot_idx ON public.shot_asset_requirements USING btree (storyboard_shot_id);


--
-- Name: storyboard_plan_reviews_plan_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_plan_reviews_plan_idx ON public.storyboard_plan_reviews USING btree (storyboard_plan_id, revision DESC);


--
-- Name: storyboard_plans_one_active_per_episode; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX storyboard_plans_one_active_per_episode ON public.storyboard_plans USING btree (script_episode_id) WHERE ((active = true) AND (status = 'ready'::text));


--
-- Name: storyboard_plans_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_plans_project_status_idx ON public.storyboard_plans USING btree (project_id, status, created_at DESC);


--
-- Name: storyboard_plans_timing_analysis_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_plans_timing_analysis_idx ON public.storyboard_plans USING btree (timing_analysis_id);


--
-- Name: storyboard_scene_plans_plan_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_scene_plans_plan_status_idx ON public.storyboard_scene_plans USING btree (storyboard_plan_id, status, scene_ordinal);


--
-- Name: storyboard_scene_plans_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_scene_plans_project_status_idx ON public.storyboard_scene_plans USING btree (project_id, status, updated_at DESC);


--
-- Name: storyboard_shot_continuity_frames_active_role_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX storyboard_shot_continuity_frames_active_role_idx ON public.storyboard_shot_continuity_frames USING btree (storyboard_shot_id, frame_role) WHERE (status = 'active'::text);


--
-- Name: storyboard_shot_continuity_frames_project_shot_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_shot_continuity_frames_project_shot_idx ON public.storyboard_shot_continuity_frames USING btree (project_id, storyboard_shot_id, created_at DESC);


--
-- Name: storyboard_shot_continuity_frames_source_video_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_shot_continuity_frames_source_video_idx ON public.storyboard_shot_continuity_frames USING btree (source_video_artifact_id);


--
-- Name: storyboard_shot_timing_spans_unit_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_shot_timing_spans_unit_idx ON public.storyboard_shot_timing_spans USING btree (timing_unit_id, span_start_tick, span_end_tick);


--
-- Name: storyboard_shots_episode_tick_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_shots_episode_tick_idx ON public.storyboard_shots USING btree (script_episode_id, start_tick, end_tick) WHERE (deleted_at IS NULL);


--
-- Name: storyboard_shots_legacy_workflow_index_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX storyboard_shots_legacy_workflow_index_unique ON public.storyboard_shots USING btree (workflow_run_id, shot_index) WHERE ((storyboard_plan_id IS NULL) AND (workflow_run_id IS NOT NULL) AND (deleted_at IS NULL));


--
-- Name: storyboard_shots_plan_order_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_shots_plan_order_idx ON public.storyboard_shots USING btree (storyboard_plan_id, start_tick, end_tick);


--
-- Name: storyboard_shots_plan_shot_index_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX storyboard_shots_plan_shot_index_unique ON public.storyboard_shots USING btree (storyboard_plan_id, shot_index) WHERE ((storyboard_plan_id IS NOT NULL) AND (deleted_at IS NULL));


--
-- Name: storyboard_shots_project_review_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_shots_project_review_idx ON public.storyboard_shots USING btree (project_id, review_status);


--
-- Name: storyboard_shots_script_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_shots_script_idx ON public.storyboard_shots USING btree (script_id, script_version_id);


--
-- Name: storyboard_shots_storyboard_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboard_shots_storyboard_idx ON public.storyboard_shots USING btree (storyboard_id, shot_index);


--
-- Name: storyboards_project_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX storyboards_project_idx ON public.storyboards USING btree (project_id, created_at DESC);


--
-- Name: team_members_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX team_members_user_id_idx ON public.team_members USING btree (user_id);


--
-- Name: teams_organization_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX teams_organization_id_idx ON public.teams USING btree (organization_id);


--
-- Name: timeline_clips_project_shot_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX timeline_clips_project_shot_idx ON public.timeline_clips USING btree (project_id, storyboard_shot_id);


--
-- Name: timeline_clips_project_stale_state_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX timeline_clips_project_stale_state_idx ON public.timeline_clips USING btree (project_id, stale_state);


--
-- Name: timeline_clips_timeline_order_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX timeline_clips_timeline_order_idx ON public.timeline_clips USING btree (timeline_id, clip_index);


--
-- Name: timing_calibration_profiles_one_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX timing_calibration_profiles_one_active_idx ON public.timing_calibration_profiles USING btree (project_id) WHERE (status = 'active'::text);


--
-- Name: timing_calibration_profiles_project_configuration_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX timing_calibration_profiles_project_configuration_idx ON public.timing_calibration_profiles USING btree (project_id, audio_configuration_revision, status, revision DESC);


--
-- Name: timing_calibration_samples_project_configuration_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX timing_calibration_samples_project_configuration_idx ON public.timing_calibration_samples USING btree (project_id, audio_configuration_revision, created_at DESC);


--
-- Name: timing_calibration_samples_project_kind_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX timing_calibration_samples_project_kind_idx ON public.timing_calibration_samples USING btree (project_id, sample_kind, created_at DESC);


--
-- Name: tts_audio_clips_episode_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tts_audio_clips_episode_status_idx ON public.tts_audio_clips USING btree (script_episode_id, status, created_at DESC);


--
-- Name: tts_audio_clips_one_active_per_unit_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX tts_audio_clips_one_active_per_unit_idx ON public.tts_audio_clips USING btree (timing_unit_id) WHERE ((active = true) AND (status = 'succeeded'::text));


--
-- Name: tts_audio_clips_project_configuration_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tts_audio_clips_project_configuration_idx ON public.tts_audio_clips USING btree (project_id, audio_configuration_revision, status, created_at DESC);


--
-- Name: tts_audio_clips_workflow_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX tts_audio_clips_workflow_idx ON public.tts_audio_clips USING btree (workflow_run_id, created_at DESC) WHERE (workflow_run_id IS NOT NULL);


--
-- Name: video_render_plans_one_active_per_shot; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX video_render_plans_one_active_per_shot ON public.video_render_plans USING btree (storyboard_shot_id) WHERE ((active = true) AND (status <> ALL (ARRAY['archived'::text, 'stale'::text, 'cancelled'::text])));


--
-- Name: video_render_plans_output_artifact_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX video_render_plans_output_artifact_idx ON public.video_render_plans USING btree (output_artifact_id) WHERE (output_artifact_id IS NOT NULL);


--
-- Name: video_render_plans_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX video_render_plans_project_status_idx ON public.video_render_plans USING btree (project_id, status, updated_at DESC);


--
-- Name: video_render_plans_shot_history_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX video_render_plans_shot_history_idx ON public.video_render_plans USING btree (storyboard_shot_id, created_at DESC);


--
-- Name: video_render_segments_plan_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX video_render_segments_plan_status_idx ON public.video_render_segments USING btree (video_render_plan_id, status, segment_index);


--
-- Name: video_render_segments_shot_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX video_render_segments_shot_idx ON public.video_render_segments USING btree (storyboard_shot_id, created_at DESC);


--
-- Name: workflow_node_runs_workflow_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workflow_node_runs_workflow_status_idx ON public.workflow_node_runs USING btree (workflow_run_id, status);


--
-- Name: workflow_runs_org_project_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workflow_runs_org_project_status_idx ON public.workflow_runs USING btree (organization_id, project_id, status);


--
-- Name: workflow_runs_template_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workflow_runs_template_id_idx ON public.workflow_runs USING btree (template_id);


--
-- Name: workflow_template_nodes_template_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workflow_template_nodes_template_id_idx ON public.workflow_template_nodes USING btree (template_id);


--
-- Name: workflow_templates_org_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workflow_templates_org_idx ON public.workflow_templates USING btree (organization_id);


--
-- Name: workspaces_organization_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workspaces_organization_id_idx ON public.workspaces USING btree (organization_id);


--
-- Name: adaptation_plans adaptation_plans_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER adaptation_plans_set_updated_at BEFORE UPDATE ON public.adaptation_plans FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: agent_approvals agent_approvals_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER agent_approvals_set_updated_at BEFORE UPDATE ON public.agent_approvals FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: agent_sessions agent_sessions_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER agent_sessions_set_updated_at BEFORE UPDATE ON public.agent_sessions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: agent_steps agent_steps_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER agent_steps_set_updated_at BEFORE UPDATE ON public.agent_steps FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: agent_tasks agent_tasks_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER agent_tasks_set_updated_at BEFORE UPDATE ON public.agent_tasks FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: asset_references asset_references_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER asset_references_set_updated_at BEFORE UPDATE ON public.asset_references FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: assets assets_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER assets_set_updated_at BEFORE UPDATE ON public.assets FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: canonical_assets canonical_assets_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER canonical_assets_set_updated_at BEFORE UPDATE ON public.canonical_assets FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: model_profiles model_profiles_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER model_profiles_set_updated_at BEFORE UPDATE ON public.model_profiles FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: novel_events novel_events_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER novel_events_set_updated_at BEFORE UPDATE ON public.novel_events FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: novels novels_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER novels_set_updated_at BEFORE UPDATE ON public.novels FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: project_manual_bindings project_manual_bindings_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER project_manual_bindings_set_updated_at BEFORE UPDATE ON public.project_manual_bindings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: project_sources project_sources_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER project_sources_set_updated_at BEFORE UPDATE ON public.project_sources FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: project_timelines project_timelines_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER project_timelines_set_updated_at BEFORE UPDATE ON public.project_timelines FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: projects projects_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER projects_set_updated_at BEFORE UPDATE ON public.projects FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: prompt_bindings prompt_bindings_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER prompt_bindings_set_updated_at BEFORE UPDATE ON public.prompt_bindings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: prompt_templates prompt_templates_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER prompt_templates_set_updated_at BEFORE UPDATE ON public.prompt_templates FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: provider_accounts provider_accounts_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER provider_accounts_set_updated_at BEFORE UPDATE ON public.provider_accounts FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: provider_async_tasks provider_async_tasks_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER provider_async_tasks_set_updated_at BEFORE UPDATE ON public.provider_async_tasks FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: provider_catalog_entries provider_catalog_entries_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER provider_catalog_entries_set_updated_at BEFORE UPDATE ON public.provider_catalog_entries FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: provider_endpoints provider_endpoints_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER provider_endpoints_set_updated_at BEFORE UPDATE ON public.provider_endpoints FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: provider_limit_policies provider_limit_policies_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER provider_limit_policies_set_updated_at BEFORE UPDATE ON public.provider_limit_policies FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: provider_model_capability_presets provider_model_capability_presets_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER provider_model_capability_presets_set_updated_at BEFORE UPDATE ON public.provider_model_capability_presets FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: provider_models provider_models_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER provider_models_set_updated_at BEFORE UPDATE ON public.provider_models FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: review_fixes review_fixes_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER review_fixes_set_updated_at BEFORE UPDATE ON public.review_fixes FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: review_items review_items_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER review_items_set_updated_at BEFORE UPDATE ON public.review_items FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: roles roles_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER roles_set_updated_at BEFORE UPDATE ON public.roles FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: script_episodes script_episodes_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER script_episodes_set_updated_at BEFORE UPDATE ON public.script_episodes FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: script_scenes script_scenes_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER script_scenes_set_updated_at BEFORE UPDATE ON public.script_scenes FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: scripts scripts_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER scripts_set_updated_at BEFORE UPDATE ON public.scripts FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: shot_asset_requirements shot_asset_requirements_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER shot_asset_requirements_set_updated_at BEFORE UPDATE ON public.shot_asset_requirements FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: storyboard_scene_plans storyboard_scene_plans_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER storyboard_scene_plans_set_updated_at BEFORE UPDATE ON public.storyboard_scene_plans FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER storyboard_shot_continuity_frames_set_updated_at BEFORE UPDATE ON public.storyboard_shot_continuity_frames FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: storyboard_shots storyboard_shots_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER storyboard_shots_set_updated_at BEFORE UPDATE ON public.storyboard_shots FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: storyboards storyboards_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER storyboards_set_updated_at BEFORE UPDATE ON public.storyboards FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: teams teams_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER teams_set_updated_at BEFORE UPDATE ON public.teams FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: timeline_clips timeline_clips_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER timeline_clips_set_updated_at BEFORE UPDATE ON public.timeline_clips FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: adaptation_plans adaptation_plans_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.adaptation_plans
    ADD CONSTRAINT adaptation_plans_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: adaptation_plans adaptation_plans_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.adaptation_plans
    ADD CONSTRAINT adaptation_plans_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: adaptation_plans adaptation_plans_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.adaptation_plans
    ADD CONSTRAINT adaptation_plans_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: adaptation_plans adaptation_plans_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.adaptation_plans
    ADD CONSTRAINT adaptation_plans_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: adaptation_plans adaptation_plans_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.adaptation_plans
    ADD CONSTRAINT adaptation_plans_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: adaptation_plans adaptation_plans_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.adaptation_plans
    ADD CONSTRAINT adaptation_plans_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE SET NULL;


--
-- Name: adaptation_plans adaptation_plans_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.adaptation_plans
    ADD CONSTRAINT adaptation_plans_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.project_sources(id) ON DELETE SET NULL;


--
-- Name: agent_approvals agent_approvals_decided_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_approvals
    ADD CONSTRAINT agent_approvals_decided_by_fkey FOREIGN KEY (decided_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: agent_approvals agent_approvals_step_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_approvals
    ADD CONSTRAINT agent_approvals_step_id_fkey FOREIGN KEY (step_id) REFERENCES public.agent_steps(id) ON DELETE CASCADE;


--
-- Name: agent_approvals agent_approvals_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_approvals
    ADD CONSTRAINT agent_approvals_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.agent_tasks(id) ON DELETE CASCADE;


--
-- Name: agent_messages agent_messages_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_messages
    ADD CONSTRAINT agent_messages_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: agent_messages agent_messages_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_messages
    ADD CONSTRAINT agent_messages_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: agent_messages agent_messages_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_messages
    ADD CONSTRAINT agent_messages_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.agent_sessions(id) ON DELETE CASCADE;


--
-- Name: agent_runs agent_runs_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: agent_runs agent_runs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: agent_runs agent_runs_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: agent_runs agent_runs_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: agent_runs agent_runs_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: agent_runs agent_runs_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.agent_sessions(id) ON DELETE SET NULL;


--
-- Name: agent_runs agent_runs_step_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_step_id_fkey FOREIGN KEY (step_id) REFERENCES public.agent_steps(id) ON DELETE SET NULL;


--
-- Name: agent_runs agent_runs_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT agent_runs_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.agent_tasks(id) ON DELETE SET NULL;


--
-- Name: agent_sessions agent_sessions_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: agent_sessions agent_sessions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: agent_sessions agent_sessions_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: agent_steps agent_steps_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_steps
    ADD CONSTRAINT agent_steps_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.agent_tasks(id) ON DELETE CASCADE;


--
-- Name: agent_tasks agent_tasks_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_tasks
    ADD CONSTRAINT agent_tasks_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: agent_tasks agent_tasks_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_tasks
    ADD CONSTRAINT agent_tasks_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: agent_tasks agent_tasks_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_tasks
    ADD CONSTRAINT agent_tasks_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: agent_tasks agent_tasks_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_tasks
    ADD CONSTRAINT agent_tasks_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.agent_sessions(id) ON DELETE SET NULL;


--
-- Name: artifacts artifacts_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artifacts
    ADD CONSTRAINT artifacts_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: artifacts artifacts_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artifacts
    ADD CONSTRAINT artifacts_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: artifacts artifacts_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artifacts
    ADD CONSTRAINT artifacts_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: artifacts artifacts_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artifacts
    ADD CONSTRAINT artifacts_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: artifacts artifacts_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.artifacts
    ADD CONSTRAINT artifacts_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: asset_references asset_references_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_references
    ADD CONSTRAINT asset_references_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: asset_references asset_references_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_references
    ADD CONSTRAINT asset_references_asset_id_fkey FOREIGN KEY (asset_id) REFERENCES public.canonical_assets(id) ON DELETE CASCADE;


--
-- Name: asset_references asset_references_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_references
    ADD CONSTRAINT asset_references_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: asset_references asset_references_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_references
    ADD CONSTRAINT asset_references_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: asset_references asset_references_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_references
    ADD CONSTRAINT asset_references_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: asset_references asset_references_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_references
    ADD CONSTRAINT asset_references_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: asset_references asset_references_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_references
    ADD CONSTRAINT asset_references_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: asset_relations asset_relations_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_relations
    ADD CONSTRAINT asset_relations_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: asset_relations asset_relations_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_relations
    ADD CONSTRAINT asset_relations_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: asset_relations asset_relations_source_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_relations
    ADD CONSTRAINT asset_relations_source_asset_id_fkey FOREIGN KEY (source_asset_id) REFERENCES public.assets(id) ON DELETE CASCADE;


--
-- Name: asset_relations asset_relations_target_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_relations
    ADD CONSTRAINT asset_relations_target_asset_id_fkey FOREIGN KEY (target_asset_id) REFERENCES public.assets(id) ON DELETE CASCADE;


--
-- Name: asset_versions asset_versions_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_asset_id_fkey FOREIGN KEY (asset_id) REFERENCES public.canonical_assets(id) ON DELETE CASCADE;


--
-- Name: asset_versions asset_versions_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: asset_versions asset_versions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: asset_versions asset_versions_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: asset_versions asset_versions_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: asset_versions asset_versions_reference_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_reference_artifact_id_fkey FOREIGN KEY (reference_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: asset_versions asset_versions_reference_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.asset_versions
    ADD CONSTRAINT asset_versions_reference_media_file_id_fkey FOREIGN KEY (reference_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: assets assets_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets
    ADD CONSTRAINT assets_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: assets assets_current_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets
    ADD CONSTRAINT assets_current_artifact_id_fkey FOREIGN KEY (current_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: assets assets_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets
    ADD CONSTRAINT assets_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: assets assets_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assets
    ADD CONSTRAINT assets_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: audio_mix_clips audio_mix_clips_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_clips
    ADD CONSTRAINT audio_mix_clips_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: audio_mix_clips audio_mix_clips_audio_mix_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_clips
    ADD CONSTRAINT audio_mix_clips_audio_mix_version_id_fkey FOREIGN KEY (audio_mix_version_id) REFERENCES public.audio_mix_versions(id) ON DELETE CASCADE;


--
-- Name: audio_mix_clips audio_mix_clips_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_clips
    ADD CONSTRAINT audio_mix_clips_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: audio_mix_clips audio_mix_clips_timing_unit_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_clips
    ADD CONSTRAINT audio_mix_clips_timing_unit_id_fkey FOREIGN KEY (timing_unit_id) REFERENCES public.script_timing_units(id) ON DELETE SET NULL;


--
-- Name: audio_mix_clips audio_mix_clips_tts_audio_clip_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_clips
    ADD CONSTRAINT audio_mix_clips_tts_audio_clip_id_fkey FOREIGN KEY (tts_audio_clip_id) REFERENCES public.tts_audio_clips(id) ON DELETE SET NULL;


--
-- Name: audio_mix_clips audio_mix_clips_video_render_segment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_clips
    ADD CONSTRAINT audio_mix_clips_video_render_segment_id_fkey FOREIGN KEY (video_render_segment_id) REFERENCES public.video_render_segments(id) ON DELETE SET NULL;


--
-- Name: audio_mix_versions audio_mix_versions_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: audio_mix_versions audio_mix_versions_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: audio_mix_versions audio_mix_versions_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: audio_mix_versions audio_mix_versions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: audio_mix_versions audio_mix_versions_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: audio_mix_versions audio_mix_versions_script_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_script_episode_id_fkey FOREIGN KEY (script_episode_id) REFERENCES public.script_episodes(id) ON DELETE CASCADE;


--
-- Name: audio_mix_versions audio_mix_versions_storyboard_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_storyboard_plan_id_fkey FOREIGN KEY (storyboard_plan_id) REFERENCES public.storyboard_plans(id) ON DELETE SET NULL;


--
-- Name: audio_mix_versions audio_mix_versions_timing_analysis_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_timing_analysis_id_fkey FOREIGN KEY (timing_analysis_id) REFERENCES public.script_timing_analyses(id) ON DELETE SET NULL;


--
-- Name: audio_mix_versions audio_mix_versions_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audio_mix_versions
    ADD CONSTRAINT audio_mix_versions_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: audit_logs audit_logs_actor_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_actor_user_id_fkey FOREIGN KEY (actor_user_id) REFERENCES public.users(id);


--
-- Name: audit_logs audit_logs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: auth_sessions auth_sessions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_sessions
    ADD CONSTRAINT auth_sessions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: auth_sessions auth_sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_sessions
    ADD CONSTRAINT auth_sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: canonical_assets canonical_assets_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: canonical_assets canonical_assets_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: canonical_assets canonical_assets_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: canonical_assets canonical_assets_primary_reference_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_primary_reference_artifact_id_fkey FOREIGN KEY (primary_reference_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: canonical_assets canonical_assets_primary_reference_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_primary_reference_media_file_id_fkey FOREIGN KEY (primary_reference_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: canonical_assets canonical_assets_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: canonical_assets canonical_assets_reference_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_reference_artifact_id_fkey FOREIGN KEY (reference_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: canonical_assets canonical_assets_reference_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.canonical_assets
    ADD CONSTRAINT canonical_assets_reference_media_file_id_fkey FOREIGN KEY (reference_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: character_voice_profiles character_voice_profiles_canonical_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.character_voice_profiles
    ADD CONSTRAINT character_voice_profiles_canonical_asset_id_fkey FOREIGN KEY (canonical_asset_id) REFERENCES public.canonical_assets(id) ON DELETE SET NULL;


--
-- Name: character_voice_profiles character_voice_profiles_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.character_voice_profiles
    ADD CONSTRAINT character_voice_profiles_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: character_voice_profiles character_voice_profiles_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.character_voice_profiles
    ADD CONSTRAINT character_voice_profiles_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: character_voice_profiles character_voice_profiles_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.character_voice_profiles
    ADD CONSTRAINT character_voice_profiles_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: character_voice_profiles character_voice_profiles_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.character_voice_profiles
    ADD CONSTRAINT character_voice_profiles_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: character_voice_profiles character_voice_profiles_reference_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.character_voice_profiles
    ADD CONSTRAINT character_voice_profiles_reference_artifact_id_fkey FOREIGN KEY (reference_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: character_voice_profiles character_voice_profiles_reference_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.character_voice_profiles
    ADD CONSTRAINT character_voice_profiles_reference_media_file_id_fkey FOREIGN KEY (reference_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: cost_records cost_records_credential_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_credential_id_fkey FOREIGN KEY (credential_id) REFERENCES public.provider_credentials(id);


--
-- Name: cost_records cost_records_model_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_model_profile_id_fkey FOREIGN KEY (model_profile_id) REFERENCES public.model_profiles(id);


--
-- Name: cost_records cost_records_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: cost_records cost_records_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: cost_records cost_records_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE SET NULL;


--
-- Name: cost_records cost_records_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: cost_records cost_records_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id);


--
-- Name: cost_records cost_records_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_records
    ADD CONSTRAINT cost_records_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_script_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_script_episode_id_fkey FOREIGN KEY (script_episode_id) REFERENCES public.script_episodes(id) ON DELETE CASCADE;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_script_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_script_version_id_fkey FOREIGN KEY (script_version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;


--
-- Name: episode_continuity_blueprints episode_continuity_blueprints_timing_analysis_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.episode_continuity_blueprints
    ADD CONSTRAINT episode_continuity_blueprints_timing_analysis_id_fkey FOREIGN KEY (timing_analysis_id) REFERENCES public.script_timing_analyses(id) ON DELETE CASCADE;


--
-- Name: event_outbox event_outbox_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event_outbox
    ADD CONSTRAINT event_outbox_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: event_outbox event_outbox_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.event_outbox
    ADD CONSTRAINT event_outbox_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: final_video_versions final_video_versions_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: final_video_versions final_video_versions_audio_mix_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_audio_mix_version_id_fkey FOREIGN KEY (audio_mix_version_id) REFERENCES public.audio_mix_versions(id) ON DELETE SET NULL;


--
-- Name: final_video_versions final_video_versions_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: final_video_versions final_video_versions_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: final_video_versions final_video_versions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: final_video_versions final_video_versions_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: final_video_versions final_video_versions_timeline_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_timeline_id_fkey FOREIGN KEY (timeline_id) REFERENCES public.project_timelines(id) ON DELETE CASCADE;


--
-- Name: final_video_versions final_video_versions_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.final_video_versions
    ADD CONSTRAINT final_video_versions_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: idempotency_keys idempotency_keys_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: media_files media_files_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: media_files media_files_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: media_files media_files_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: media_files media_files_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE SET NULL;


--
-- Name: media_variants media_variants_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_variants
    ADD CONSTRAINT media_variants_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE CASCADE;


--
-- Name: model_profile_bindings model_profile_bindings_model_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_profile_bindings
    ADD CONSTRAINT model_profile_bindings_model_profile_id_fkey FOREIGN KEY (model_profile_id) REFERENCES public.model_profiles(id) ON DELETE CASCADE;


--
-- Name: model_profile_bindings model_profile_bindings_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_profile_bindings
    ADD CONSTRAINT model_profile_bindings_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id) ON DELETE CASCADE;


--
-- Name: model_profiles model_profiles_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_profiles
    ADD CONSTRAINT model_profiles_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: native_audio_reviews native_audio_reviews_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: native_audio_reviews native_audio_reviews_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: native_audio_reviews native_audio_reviews_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: native_audio_reviews native_audio_reviews_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: native_audio_reviews native_audio_reviews_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: native_audio_reviews native_audio_reviews_reviewed_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_reviewed_by_fkey FOREIGN KEY (reviewed_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: native_audio_reviews native_audio_reviews_video_render_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_video_render_plan_id_fkey FOREIGN KEY (video_render_plan_id) REFERENCES public.video_render_plans(id) ON DELETE CASCADE;


--
-- Name: native_audio_reviews native_audio_reviews_video_render_segment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_video_render_segment_id_fkey FOREIGN KEY (video_render_segment_id) REFERENCES public.video_render_segments(id) ON DELETE CASCADE;


--
-- Name: native_audio_reviews native_audio_reviews_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: novel_chapters novel_chapters_content_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_chapters
    ADD CONSTRAINT novel_chapters_content_artifact_id_fkey FOREIGN KEY (content_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: novel_chapters novel_chapters_novel_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_chapters
    ADD CONSTRAINT novel_chapters_novel_id_fkey FOREIGN KEY (novel_id) REFERENCES public.novels(id) ON DELETE CASCADE;


--
-- Name: novel_chapters novel_chapters_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_chapters
    ADD CONSTRAINT novel_chapters_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: novel_chapters novel_chapters_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_chapters
    ADD CONSTRAINT novel_chapters_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: novel_chapters novel_chapters_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_chapters
    ADD CONSTRAINT novel_chapters_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.project_sources(id) ON DELETE CASCADE;


--
-- Name: novel_event_links novel_event_links_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_event_links
    ADD CONSTRAINT novel_event_links_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: novel_event_links novel_event_links_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_event_links
    ADD CONSTRAINT novel_event_links_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: novel_event_links novel_event_links_source_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_event_links
    ADD CONSTRAINT novel_event_links_source_event_id_fkey FOREIGN KEY (source_event_id) REFERENCES public.novel_events(id) ON DELETE CASCADE;


--
-- Name: novel_event_links novel_event_links_target_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_event_links
    ADD CONSTRAINT novel_event_links_target_event_id_fkey FOREIGN KEY (target_event_id) REFERENCES public.novel_events(id) ON DELETE CASCADE;


--
-- Name: novel_events novel_events_chapter_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_events
    ADD CONSTRAINT novel_events_chapter_id_fkey FOREIGN KEY (chapter_id) REFERENCES public.novel_chapters(id) ON DELETE CASCADE;


--
-- Name: novel_events novel_events_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_events
    ADD CONSTRAINT novel_events_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: novel_events novel_events_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_events
    ADD CONSTRAINT novel_events_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: novel_events novel_events_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_events
    ADD CONSTRAINT novel_events_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: novel_events novel_events_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_events
    ADD CONSTRAINT novel_events_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: novel_events novel_events_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novel_events
    ADD CONSTRAINT novel_events_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.project_sources(id) ON DELETE CASCADE;


--
-- Name: novels novels_clean_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novels
    ADD CONSTRAINT novels_clean_artifact_id_fkey FOREIGN KEY (clean_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: novels novels_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novels
    ADD CONSTRAINT novels_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: novels novels_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novels
    ADD CONSTRAINT novels_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: novels novels_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novels
    ADD CONSTRAINT novels_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: novels novels_raw_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.novels
    ADD CONSTRAINT novels_raw_artifact_id_fkey FOREIGN KEY (raw_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: organization_members organization_members_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_members
    ADD CONSTRAINT organization_members_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: organization_members organization_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_members
    ADD CONSTRAINT organization_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: project_exports project_exports_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_exports
    ADD CONSTRAINT project_exports_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: project_exports project_exports_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_exports
    ADD CONSTRAINT project_exports_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: project_exports project_exports_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_exports
    ADD CONSTRAINT project_exports_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: project_exports project_exports_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_exports
    ADD CONSTRAINT project_exports_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: project_exports project_exports_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_exports
    ADD CONSTRAINT project_exports_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: project_exports project_exports_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_exports
    ADD CONSTRAINT project_exports_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: project_manual_bindings project_manual_bindings_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_manual_bindings
    ADD CONSTRAINT project_manual_bindings_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: project_manual_bindings project_manual_bindings_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_manual_bindings
    ADD CONSTRAINT project_manual_bindings_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: project_manual_bindings project_manual_bindings_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_manual_bindings
    ADD CONSTRAINT project_manual_bindings_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: project_manual_bindings project_manual_bindings_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_manual_bindings
    ADD CONSTRAINT project_manual_bindings_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE RESTRICT;


--
-- Name: project_members project_members_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT project_members_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: project_members project_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT project_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: project_sources project_sources_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_sources
    ADD CONSTRAINT project_sources_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: project_sources project_sources_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_sources
    ADD CONSTRAINT project_sources_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: project_sources project_sources_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_sources
    ADD CONSTRAINT project_sources_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: project_timelines project_timelines_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_timelines
    ADD CONSTRAINT project_timelines_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: project_timelines project_timelines_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_timelines
    ADD CONSTRAINT project_timelines_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: project_timelines project_timelines_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_timelines
    ADD CONSTRAINT project_timelines_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: project_timelines project_timelines_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_timelines
    ADD CONSTRAINT project_timelines_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: project_timelines project_timelines_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_timelines
    ADD CONSTRAINT project_timelines_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: projects projects_active_audio_mix_version_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_active_audio_mix_version_fk FOREIGN KEY (active_audio_mix_version_id) REFERENCES public.audio_mix_versions(id) ON DELETE SET NULL;


--
-- Name: projects projects_active_final_video_version_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_active_final_video_version_fk FOREIGN KEY (active_final_video_version_id) REFERENCES public.final_video_versions(id) ON DELETE SET NULL;


--
-- Name: projects projects_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: projects projects_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: projects projects_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;


--
-- Name: prompt_bindings prompt_bindings_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_bindings
    ADD CONSTRAINT prompt_bindings_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: prompt_bindings prompt_bindings_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_bindings
    ADD CONSTRAINT prompt_bindings_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: prompt_bindings prompt_bindings_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_bindings
    ADD CONSTRAINT prompt_bindings_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: prompt_bindings prompt_bindings_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_bindings
    ADD CONSTRAINT prompt_bindings_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE RESTRICT;


--
-- Name: prompt_templates prompt_templates_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_templates
    ADD CONSTRAINT prompt_templates_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: prompt_templates prompt_templates_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_templates
    ADD CONSTRAINT prompt_templates_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: prompt_versions prompt_versions_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_versions
    ADD CONSTRAINT prompt_versions_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: prompt_versions prompt_versions_prompt_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_versions
    ADD CONSTRAINT prompt_versions_prompt_template_id_fkey FOREIGN KEY (prompt_template_id) REFERENCES public.prompt_templates(id) ON DELETE CASCADE;


--
-- Name: prompt_versions prompt_versions_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_versions
    ADD CONSTRAINT prompt_versions_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.prompt_templates(id) ON DELETE CASCADE;


--
-- Name: provider_accounts provider_accounts_connector_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_accounts
    ADD CONSTRAINT provider_accounts_connector_id_fkey FOREIGN KEY (connector_id) REFERENCES public.provider_connectors(id);


--
-- Name: provider_accounts provider_accounts_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_accounts
    ADD CONSTRAINT provider_accounts_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: provider_accounts provider_accounts_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_accounts
    ADD CONSTRAINT provider_accounts_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: provider_async_tasks provider_async_tasks_credential_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_credential_id_fkey FOREIGN KEY (credential_id) REFERENCES public.provider_credentials(id);


--
-- Name: provider_async_tasks provider_async_tasks_model_profile_binding_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_model_profile_binding_id_fkey FOREIGN KEY (model_profile_binding_id) REFERENCES public.model_profile_bindings(id) ON DELETE SET NULL;


--
-- Name: provider_async_tasks provider_async_tasks_model_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_model_profile_id_fkey FOREIGN KEY (model_profile_id) REFERENCES public.model_profiles(id);


--
-- Name: provider_async_tasks provider_async_tasks_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: provider_async_tasks provider_async_tasks_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: provider_async_tasks provider_async_tasks_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE SET NULL;


--
-- Name: provider_async_tasks provider_async_tasks_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id);


--
-- Name: provider_async_tasks provider_async_tasks_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE CASCADE;


--
-- Name: provider_async_tasks provider_async_tasks_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id);


--
-- Name: provider_async_tasks provider_async_tasks_video_render_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_video_render_plan_id_fkey FOREIGN KEY (video_render_plan_id) REFERENCES public.video_render_plans(id) ON DELETE SET NULL;


--
-- Name: provider_async_tasks provider_async_tasks_video_render_segment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_video_render_segment_id_fkey FOREIGN KEY (video_render_segment_id) REFERENCES public.video_render_segments(id) ON DELETE SET NULL;


--
-- Name: provider_async_tasks provider_async_tasks_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: provider_call_logs provider_call_logs_credential_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_credential_id_fkey FOREIGN KEY (credential_id) REFERENCES public.provider_credentials(id);


--
-- Name: provider_call_logs provider_call_logs_model_profile_binding_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_model_profile_binding_id_fkey FOREIGN KEY (model_profile_binding_id) REFERENCES public.model_profile_bindings(id) ON DELETE SET NULL;


--
-- Name: provider_call_logs provider_call_logs_model_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_model_profile_id_fkey FOREIGN KEY (model_profile_id) REFERENCES public.model_profiles(id);


--
-- Name: provider_call_logs provider_call_logs_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: provider_call_logs provider_call_logs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: provider_call_logs provider_call_logs_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE SET NULL;


--
-- Name: provider_call_logs provider_call_logs_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id);


--
-- Name: provider_call_logs provider_call_logs_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id);


--
-- Name: provider_call_logs provider_call_logs_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id);


--
-- Name: provider_call_logs provider_call_logs_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_call_logs
    ADD CONSTRAINT provider_call_logs_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: provider_circuit_states provider_circuit_states_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_circuit_states
    ADD CONSTRAINT provider_circuit_states_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: provider_circuit_states provider_circuit_states_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_circuit_states
    ADD CONSTRAINT provider_circuit_states_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id) ON DELETE CASCADE;


--
-- Name: provider_circuit_states provider_circuit_states_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_circuit_states
    ADD CONSTRAINT provider_circuit_states_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: provider_credentials provider_credentials_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_credentials
    ADD CONSTRAINT provider_credentials_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: provider_credentials provider_credentials_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_credentials
    ADD CONSTRAINT provider_credentials_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: provider_credentials provider_credentials_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_credentials
    ADD CONSTRAINT provider_credentials_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id) ON DELETE CASCADE;


--
-- Name: provider_endpoints provider_endpoints_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_endpoints
    ADD CONSTRAINT provider_endpoints_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id) ON DELETE CASCADE;


--
-- Name: provider_leases provider_leases_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_leases
    ADD CONSTRAINT provider_leases_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: provider_leases provider_leases_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_leases
    ADD CONSTRAINT provider_leases_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: provider_leases provider_leases_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_leases
    ADD CONSTRAINT provider_leases_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id);


--
-- Name: provider_leases provider_leases_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_leases
    ADD CONSTRAINT provider_leases_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: provider_leases provider_leases_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_leases
    ADD CONSTRAINT provider_leases_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id);


--
-- Name: provider_leases provider_leases_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_leases
    ADD CONSTRAINT provider_leases_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: provider_limit_policies provider_limit_policies_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_limit_policies
    ADD CONSTRAINT provider_limit_policies_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: provider_limit_policies provider_limit_policies_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_limit_policies
    ADD CONSTRAINT provider_limit_policies_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: provider_limit_policies provider_limit_policies_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_limit_policies
    ADD CONSTRAINT provider_limit_policies_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id) ON DELETE CASCADE;


--
-- Name: provider_limit_policies provider_limit_policies_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_limit_policies
    ADD CONSTRAINT provider_limit_policies_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id) ON DELETE CASCADE;


--
-- Name: provider_model_capabilities provider_model_capabilities_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_model_capabilities
    ADD CONSTRAINT provider_model_capabilities_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id) ON DELETE CASCADE;


--
-- Name: provider_models provider_models_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_models
    ADD CONSTRAINT provider_models_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id) ON DELETE CASCADE;


--
-- Name: provider_test_runs provider_test_runs_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_test_runs
    ADD CONSTRAINT provider_test_runs_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: provider_test_runs provider_test_runs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_test_runs
    ADD CONSTRAINT provider_test_runs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: provider_test_runs provider_test_runs_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_test_runs
    ADD CONSTRAINT provider_test_runs_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id) ON DELETE CASCADE;


--
-- Name: provider_test_runs provider_test_runs_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provider_test_runs
    ADD CONSTRAINT provider_test_runs_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: review_fixes review_fixes_applied_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_fixes
    ADD CONSTRAINT review_fixes_applied_by_fkey FOREIGN KEY (applied_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: review_fixes review_fixes_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_fixes
    ADD CONSTRAINT review_fixes_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: review_fixes review_fixes_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_fixes
    ADD CONSTRAINT review_fixes_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: review_fixes review_fixes_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_fixes
    ADD CONSTRAINT review_fixes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: review_fixes review_fixes_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_fixes
    ADD CONSTRAINT review_fixes_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: review_fixes review_fixes_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_fixes
    ADD CONSTRAINT review_fixes_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: review_fixes review_fixes_review_item_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_fixes
    ADD CONSTRAINT review_fixes_review_item_id_fkey FOREIGN KEY (review_item_id) REFERENCES public.review_items(id) ON DELETE CASCADE;


--
-- Name: review_items review_items_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_items
    ADD CONSTRAINT review_items_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: review_items review_items_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_items
    ADD CONSTRAINT review_items_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: review_items review_items_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_items
    ADD CONSTRAINT review_items_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: review_items review_items_resolved_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_items
    ADD CONSTRAINT review_items_resolved_by_fkey FOREIGN KEY (resolved_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: review_items review_items_review_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_items
    ADD CONSTRAINT review_items_review_run_id_fkey FOREIGN KEY (review_run_id) REFERENCES public.review_runs(id) ON DELETE SET NULL;


--
-- Name: review_runs review_runs_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_runs
    ADD CONSTRAINT review_runs_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: review_runs review_runs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_runs
    ADD CONSTRAINT review_runs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: review_runs review_runs_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_runs
    ADD CONSTRAINT review_runs_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: review_runs review_runs_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_runs
    ADD CONSTRAINT review_runs_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: review_runs review_runs_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_runs
    ADD CONSTRAINT review_runs_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: review_runs review_runs_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_runs
    ADD CONSTRAINT review_runs_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: review_tasks review_tasks_assigned_to_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_tasks
    ADD CONSTRAINT review_tasks_assigned_to_fkey FOREIGN KEY (assigned_to) REFERENCES public.users(id);


--
-- Name: review_tasks review_tasks_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_tasks
    ADD CONSTRAINT review_tasks_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: review_tasks review_tasks_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_tasks
    ADD CONSTRAINT review_tasks_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: review_tasks review_tasks_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_tasks
    ADD CONSTRAINT review_tasks_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: review_tasks review_tasks_resolved_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_tasks
    ADD CONSTRAINT review_tasks_resolved_by_fkey FOREIGN KEY (resolved_by) REFERENCES public.users(id);


--
-- Name: review_tasks review_tasks_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.review_tasks
    ADD CONSTRAINT review_tasks_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: role_bindings role_bindings_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: role_bindings role_bindings_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: role_bindings role_bindings_resource_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_resource_organization_id_fkey FOREIGN KEY (resource_organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: role_bindings role_bindings_resource_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_resource_project_id_fkey FOREIGN KEY (resource_project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: role_bindings role_bindings_resource_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_resource_workspace_id_fkey FOREIGN KEY (resource_workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;


--
-- Name: role_bindings role_bindings_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


--
-- Name: role_bindings role_bindings_subject_team_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_subject_team_id_fkey FOREIGN KEY (subject_team_id) REFERENCES public.teams(id) ON DELETE CASCADE;


--
-- Name: role_bindings role_bindings_subject_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_bindings
    ADD CONSTRAINT role_bindings_subject_user_id_fkey FOREIGN KEY (subject_user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: role_permissions role_permissions_permission_key_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_permission_key_fkey FOREIGN KEY (permission_key) REFERENCES public.permissions(permission_key) ON DELETE CASCADE;


--
-- Name: role_permissions role_permissions_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


--
-- Name: roles roles_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: scene_asset_links scene_asset_links_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scene_asset_links
    ADD CONSTRAINT scene_asset_links_asset_id_fkey FOREIGN KEY (asset_id) REFERENCES public.canonical_assets(id) ON DELETE CASCADE;


--
-- Name: scene_asset_links scene_asset_links_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scene_asset_links
    ADD CONSTRAINT scene_asset_links_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: scene_asset_links scene_asset_links_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scene_asset_links
    ADD CONSTRAINT scene_asset_links_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: scene_asset_links scene_asset_links_script_scene_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scene_asset_links
    ADD CONSTRAINT scene_asset_links_script_scene_id_fkey FOREIGN KEY (script_scene_id) REFERENCES public.script_scenes(id) ON DELETE CASCADE;


--
-- Name: script_asset_links script_asset_links_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_asset_links
    ADD CONSTRAINT script_asset_links_asset_id_fkey FOREIGN KEY (asset_id) REFERENCES public.canonical_assets(id) ON DELETE CASCADE;


--
-- Name: script_asset_links script_asset_links_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_asset_links
    ADD CONSTRAINT script_asset_links_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: script_asset_links script_asset_links_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_asset_links
    ADD CONSTRAINT script_asset_links_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: script_asset_links script_asset_links_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_asset_links
    ADD CONSTRAINT script_asset_links_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;


--
-- Name: script_episodes script_episodes_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: script_episodes script_episodes_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: script_episodes script_episodes_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: script_episodes script_episodes_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: script_episodes script_episodes_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: script_episodes script_episodes_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: script_episodes script_episodes_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;


--
-- Name: script_episodes script_episodes_script_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_script_version_id_fkey FOREIGN KEY (script_version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;


--
-- Name: script_episodes script_episodes_source_chapter_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_source_chapter_id_fkey FOREIGN KEY (source_chapter_id) REFERENCES public.novel_chapters(id) ON DELETE SET NULL;


--
-- Name: script_episodes script_episodes_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_episodes
    ADD CONSTRAINT script_episodes_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.project_sources(id) ON DELETE SET NULL;


--
-- Name: script_scenes script_scenes_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: script_scenes script_scenes_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: script_scenes script_scenes_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: script_scenes script_scenes_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: script_scenes script_scenes_script_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_script_episode_id_fkey FOREIGN KEY (script_episode_id) REFERENCES public.script_episodes(id) ON DELETE SET NULL;


--
-- Name: script_scenes script_scenes_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;


--
-- Name: script_scenes script_scenes_script_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_scenes
    ADD CONSTRAINT script_scenes_script_version_id_fkey FOREIGN KEY (script_version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;


--
-- Name: script_timing_analyses script_timing_analyses_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: script_timing_analyses script_timing_analyses_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: script_timing_analyses script_timing_analyses_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: script_timing_analyses script_timing_analyses_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: script_timing_analyses script_timing_analyses_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: script_timing_analyses script_timing_analyses_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: script_timing_analyses script_timing_analyses_script_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_script_episode_id_fkey FOREIGN KEY (script_episode_id) REFERENCES public.script_episodes(id) ON DELETE CASCADE;


--
-- Name: script_timing_analyses script_timing_analyses_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;


--
-- Name: script_timing_analyses script_timing_analyses_script_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_analyses
    ADD CONSTRAINT script_timing_analyses_script_version_id_fkey FOREIGN KEY (script_version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;


--
-- Name: script_timing_units script_timing_units_script_scene_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_units
    ADD CONSTRAINT script_timing_units_script_scene_id_fkey FOREIGN KEY (script_scene_id) REFERENCES public.script_scenes(id) ON DELETE SET NULL;


--
-- Name: script_timing_units script_timing_units_source_chapter_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_units
    ADD CONSTRAINT script_timing_units_source_chapter_id_fkey FOREIGN KEY (source_chapter_id) REFERENCES public.novel_chapters(id) ON DELETE SET NULL;


--
-- Name: script_timing_units script_timing_units_source_tts_audio_clip_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_units
    ADD CONSTRAINT script_timing_units_source_tts_audio_clip_id_fkey FOREIGN KEY (source_tts_audio_clip_id) REFERENCES public.tts_audio_clips(id) ON DELETE SET NULL;


--
-- Name: script_timing_units script_timing_units_timing_analysis_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_timing_units
    ADD CONSTRAINT script_timing_units_timing_analysis_id_fkey FOREIGN KEY (timing_analysis_id) REFERENCES public.script_timing_analyses(id) ON DELETE CASCADE;


--
-- Name: script_versions script_versions_content_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_content_artifact_id_fkey FOREIGN KEY (content_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: script_versions script_versions_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: script_versions script_versions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: script_versions script_versions_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: script_versions script_versions_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: script_versions script_versions_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.script_versions
    ADD CONSTRAINT script_versions_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;


--
-- Name: scripts scripts_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: scripts scripts_current_version_id_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_current_version_id_fk FOREIGN KEY (current_version_id) REFERENCES public.script_versions(id) ON DELETE SET NULL;


--
-- Name: scripts scripts_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: scripts scripts_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: scripts scripts_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scripts
    ADD CONSTRAINT scripts_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.project_sources(id) ON DELETE SET NULL;


--
-- Name: shot_asset_requirements shot_asset_requirements_asset_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_asset_id_fkey FOREIGN KEY (asset_id) REFERENCES public.canonical_assets(id) ON DELETE CASCADE;


--
-- Name: shot_asset_requirements shot_asset_requirements_derived_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_derived_artifact_id_fkey FOREIGN KEY (derived_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: shot_asset_requirements shot_asset_requirements_derived_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_derived_media_file_id_fkey FOREIGN KEY (derived_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: shot_asset_requirements shot_asset_requirements_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: shot_asset_requirements shot_asset_requirements_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: shot_asset_requirements shot_asset_requirements_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: shot_asset_requirements shot_asset_requirements_storyboard_shot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_storyboard_shot_id_fkey FOREIGN KEY (storyboard_shot_id) REFERENCES public.storyboard_shots(id) ON DELETE CASCADE;


--
-- Name: shot_asset_requirements shot_asset_requirements_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE CASCADE;


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: storyboard_plan_reviews storyboard_plan_reviews_storyboard_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plan_reviews
    ADD CONSTRAINT storyboard_plan_reviews_storyboard_plan_id_fkey FOREIGN KEY (storyboard_plan_id) REFERENCES public.storyboard_plans(id) ON DELETE CASCADE;


--
-- Name: storyboard_plans storyboard_plans_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: storyboard_plans storyboard_plans_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: storyboard_plans storyboard_plans_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: storyboard_plans storyboard_plans_script_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_script_episode_id_fkey FOREIGN KEY (script_episode_id) REFERENCES public.script_episodes(id) ON DELETE CASCADE;


--
-- Name: storyboard_plans storyboard_plans_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE CASCADE;


--
-- Name: storyboard_plans storyboard_plans_script_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_script_version_id_fkey FOREIGN KEY (script_version_id) REFERENCES public.script_versions(id) ON DELETE CASCADE;


--
-- Name: storyboard_plans storyboard_plans_timing_analysis_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_plans
    ADD CONSTRAINT storyboard_plans_timing_analysis_id_fkey FOREIGN KEY (timing_analysis_id) REFERENCES public.script_timing_analyses(id) ON DELETE RESTRICT;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_blueprint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_blueprint_id_fkey FOREIGN KEY (blueprint_id) REFERENCES public.episode_continuity_blueprints(id) ON DELETE CASCADE;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_prompt_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_prompt_version_id_fkey FOREIGN KEY (prompt_version_id) REFERENCES public.prompt_versions(id) ON DELETE SET NULL;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_script_scene_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_script_scene_id_fkey FOREIGN KEY (script_scene_id) REFERENCES public.script_scenes(id) ON DELETE SET NULL;


--
-- Name: storyboard_scene_plans storyboard_scene_plans_storyboard_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_scene_plans
    ADD CONSTRAINT storyboard_scene_plans_storyboard_plan_id_fkey FOREIGN KEY (storyboard_plan_id) REFERENCES public.storyboard_plans(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_fram_source_video_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_fram_source_video_media_file_id_fkey FOREIGN KEY (source_video_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_frame_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_frame_artifact_id_fkey FOREIGN KEY (frame_artifact_id) REFERENCES public.artifacts(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_frame_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_frame_media_file_id_fkey FOREIGN KEY (frame_media_file_id) REFERENCES public.media_files(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_source_video_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_source_video_artifact_id_fkey FOREIGN KEY (source_video_artifact_id) REFERENCES public.artifacts(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_storyboard_shot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_storyboard_shot_id_fkey FOREIGN KEY (storyboard_shot_id) REFERENCES public.storyboard_shots(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_continuity_frames storyboard_shot_continuity_frames_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_continuity_frames
    ADD CONSTRAINT storyboard_shot_continuity_frames_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: storyboard_shot_timing_spans storyboard_shot_timing_spans_storyboard_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_timing_spans
    ADD CONSTRAINT storyboard_shot_timing_spans_storyboard_plan_id_fkey FOREIGN KEY (storyboard_plan_id) REFERENCES public.storyboard_plans(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_timing_spans storyboard_shot_timing_spans_storyboard_shot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_timing_spans
    ADD CONSTRAINT storyboard_shot_timing_spans_storyboard_shot_id_fkey FOREIGN KEY (storyboard_shot_id) REFERENCES public.storyboard_shots(id) ON DELETE CASCADE;


--
-- Name: storyboard_shot_timing_spans storyboard_shot_timing_spans_timing_unit_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shot_timing_spans
    ADD CONSTRAINT storyboard_shot_timing_spans_timing_unit_id_fkey FOREIGN KEY (timing_unit_id) REFERENCES public.script_timing_units(id) ON DELETE CASCADE;


--
-- Name: storyboard_shots storyboard_shots_active_video_render_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_active_video_render_plan_id_fkey FOREIGN KEY (active_video_render_plan_id) REFERENCES public.video_render_plans(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_image_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_image_artifact_id_fkey FOREIGN KEY (image_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_image_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_image_media_file_id_fkey FOREIGN KEY (image_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_image_prompt_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_image_prompt_workflow_run_id_fkey FOREIGN KEY (image_prompt_workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_image_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_image_workflow_run_id_fkey FOREIGN KEY (image_workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: storyboard_shots storyboard_shots_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: storyboard_shots storyboard_shots_script_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_script_episode_id_fkey FOREIGN KEY (script_episode_id) REFERENCES public.script_episodes(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_script_scene_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_script_scene_id_fkey FOREIGN KEY (script_scene_id) REFERENCES public.script_scenes(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_script_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_script_version_id_fkey FOREIGN KEY (script_version_id) REFERENCES public.script_versions(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_storyboard_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_storyboard_artifact_id_fkey FOREIGN KEY (storyboard_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_storyboard_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_storyboard_id_fkey FOREIGN KEY (storyboard_id) REFERENCES public.storyboards(id) ON DELETE CASCADE;


--
-- Name: storyboard_shots storyboard_shots_storyboard_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_storyboard_plan_id_fkey FOREIGN KEY (storyboard_plan_id) REFERENCES public.storyboard_plans(id) ON DELETE CASCADE;


--
-- Name: storyboard_shots storyboard_shots_video_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_video_artifact_id_fkey FOREIGN KEY (video_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_video_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_video_media_file_id_fkey FOREIGN KEY (video_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_video_prompt_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_video_prompt_workflow_run_id_fkey FOREIGN KEY (video_prompt_workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_video_provider_async_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_video_provider_async_task_id_fkey FOREIGN KEY (video_provider_async_task_id) REFERENCES public.provider_async_tasks(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_video_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_video_workflow_run_id_fkey FOREIGN KEY (video_workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: storyboard_shots storyboard_shots_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboard_shots
    ADD CONSTRAINT storyboard_shots_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE CASCADE;


--
-- Name: storyboards storyboards_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboards
    ADD CONSTRAINT storyboards_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: storyboards storyboards_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboards
    ADD CONSTRAINT storyboards_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: storyboards storyboards_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboards
    ADD CONSTRAINT storyboards_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: storyboards storyboards_script_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storyboards
    ADD CONSTRAINT storyboards_script_id_fkey FOREIGN KEY (script_id) REFERENCES public.scripts(id) ON DELETE SET NULL;


--
-- Name: team_members team_members_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.team_members
    ADD CONSTRAINT team_members_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: team_members team_members_team_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.team_members
    ADD CONSTRAINT team_members_team_id_fkey FOREIGN KEY (team_id) REFERENCES public.teams(id) ON DELETE CASCADE;


--
-- Name: team_members team_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.team_members
    ADD CONSTRAINT team_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: teams teams_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: teams teams_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: timeline_clips timeline_clips_edited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_edited_by_fkey FOREIGN KEY (edited_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: timeline_clips timeline_clips_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: timeline_clips timeline_clips_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: timeline_clips timeline_clips_storyboard_shot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_storyboard_shot_id_fkey FOREIGN KEY (storyboard_shot_id) REFERENCES public.storyboard_shots(id) ON DELETE SET NULL;


--
-- Name: timeline_clips timeline_clips_timeline_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_timeline_id_fkey FOREIGN KEY (timeline_id) REFERENCES public.project_timelines(id) ON DELETE CASCADE;


--
-- Name: timeline_clips timeline_clips_video_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_video_artifact_id_fkey FOREIGN KEY (video_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: timeline_clips timeline_clips_video_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timeline_clips
    ADD CONSTRAINT timeline_clips_video_media_file_id_fkey FOREIGN KEY (video_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: timing_calibration_profiles timing_calibration_profiles_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_profiles
    ADD CONSTRAINT timing_calibration_profiles_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: timing_calibration_profiles timing_calibration_profiles_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_profiles
    ADD CONSTRAINT timing_calibration_profiles_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: timing_calibration_samples timing_calibration_samples_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_samples
    ADD CONSTRAINT timing_calibration_samples_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: timing_calibration_samples timing_calibration_samples_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_samples
    ADD CONSTRAINT timing_calibration_samples_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: timing_calibration_samples timing_calibration_samples_script_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_samples
    ADD CONSTRAINT timing_calibration_samples_script_episode_id_fkey FOREIGN KEY (script_episode_id) REFERENCES public.script_episodes(id) ON DELETE CASCADE;


--
-- Name: timing_calibration_samples timing_calibration_samples_storyboard_shot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_samples
    ADD CONSTRAINT timing_calibration_samples_storyboard_shot_id_fkey FOREIGN KEY (storyboard_shot_id) REFERENCES public.storyboard_shots(id) ON DELETE SET NULL;


--
-- Name: timing_calibration_samples timing_calibration_samples_timing_unit_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_samples
    ADD CONSTRAINT timing_calibration_samples_timing_unit_id_fkey FOREIGN KEY (timing_unit_id) REFERENCES public.script_timing_units(id) ON DELETE SET NULL;


--
-- Name: timing_calibration_samples timing_calibration_samples_video_render_segment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timing_calibration_samples
    ADD CONSTRAINT timing_calibration_samples_video_render_segment_id_fkey FOREIGN KEY (video_render_segment_id) REFERENCES public.video_render_segments(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_applied_timing_analysis_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_applied_timing_analysis_id_fkey FOREIGN KEY (applied_timing_analysis_id) REFERENCES public.script_timing_analyses(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_character_voice_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_character_voice_profile_id_fkey FOREIGN KEY (character_voice_profile_id) REFERENCES public.character_voice_profiles(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: tts_audio_clips tts_audio_clips_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: tts_audio_clips tts_audio_clips_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id) ON DELETE SET NULL;


--
-- Name: tts_audio_clips tts_audio_clips_script_episode_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_script_episode_id_fkey FOREIGN KEY (script_episode_id) REFERENCES public.script_episodes(id) ON DELETE CASCADE;


--
-- Name: tts_audio_clips tts_audio_clips_timing_analysis_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_timing_analysis_id_fkey FOREIGN KEY (timing_analysis_id) REFERENCES public.script_timing_analyses(id) ON DELETE CASCADE;


--
-- Name: tts_audio_clips tts_audio_clips_timing_unit_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_timing_unit_id_fkey FOREIGN KEY (timing_unit_id) REFERENCES public.script_timing_units(id) ON DELETE CASCADE;


--
-- Name: tts_audio_clips tts_audio_clips_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tts_audio_clips
    ADD CONSTRAINT tts_audio_clips_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: video_render_plans video_render_plans_audio_verified_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_audio_verified_by_fkey FOREIGN KEY (audio_verified_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: video_render_plans video_render_plans_model_profile_binding_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_model_profile_binding_id_fkey FOREIGN KEY (model_profile_binding_id) REFERENCES public.model_profile_bindings(id) ON DELETE SET NULL;


--
-- Name: video_render_plans video_render_plans_model_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_model_profile_id_fkey FOREIGN KEY (model_profile_id) REFERENCES public.model_profiles(id) ON DELETE SET NULL;


--
-- Name: video_render_plans video_render_plans_node_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_node_run_id_fkey FOREIGN KEY (node_run_id) REFERENCES public.workflow_node_runs(id) ON DELETE SET NULL;


--
-- Name: video_render_plans video_render_plans_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: video_render_plans video_render_plans_output_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_output_artifact_id_fkey FOREIGN KEY (output_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: video_render_plans video_render_plans_output_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_output_media_file_id_fkey FOREIGN KEY (output_media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: video_render_plans video_render_plans_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: video_render_plans video_render_plans_provider_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES public.provider_accounts(id);


--
-- Name: video_render_plans video_render_plans_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id);


--
-- Name: video_render_plans video_render_plans_storyboard_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_storyboard_plan_id_fkey FOREIGN KEY (storyboard_plan_id) REFERENCES public.storyboard_plans(id) ON DELETE CASCADE;


--
-- Name: video_render_plans video_render_plans_storyboard_shot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_storyboard_shot_id_fkey FOREIGN KEY (storyboard_shot_id) REFERENCES public.storyboard_shots(id) ON DELETE CASCADE;


--
-- Name: video_render_plans video_render_plans_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_plans
    ADD CONSTRAINT video_render_plans_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_audio_verified_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_audio_verified_by_fkey FOREIGN KEY (audio_verified_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_extracted_audio_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_extracted_audio_artifact_id_fkey FOREIGN KEY (extracted_audio_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_media_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_media_file_id_fkey FOREIGN KEY (media_file_id) REFERENCES public.media_files(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_mezzanine_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_mezzanine_artifact_id_fkey FOREIGN KEY (mezzanine_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: video_render_segments video_render_segments_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: video_render_segments video_render_segments_provider_async_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_provider_async_task_id_fkey FOREIGN KEY (provider_async_task_id) REFERENCES public.provider_async_tasks(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_provider_call_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_provider_call_id_fkey FOREIGN KEY (provider_call_id) REFERENCES public.provider_call_logs(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_provider_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_provider_model_id_fkey FOREIGN KEY (provider_model_id) REFERENCES public.provider_models(id);


--
-- Name: video_render_segments video_render_segments_raw_av_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_raw_av_artifact_id_fkey FOREIGN KEY (raw_av_artifact_id) REFERENCES public.artifacts(id) ON DELETE SET NULL;


--
-- Name: video_render_segments video_render_segments_storyboard_shot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_storyboard_shot_id_fkey FOREIGN KEY (storyboard_shot_id) REFERENCES public.storyboard_shots(id) ON DELETE CASCADE;


--
-- Name: video_render_segments video_render_segments_video_render_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.video_render_segments
    ADD CONSTRAINT video_render_segments_video_render_plan_id_fkey FOREIGN KEY (video_render_plan_id) REFERENCES public.video_render_plans(id) ON DELETE CASCADE;


--
-- Name: workflow_node_runs workflow_node_runs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_node_runs
    ADD CONSTRAINT workflow_node_runs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: workflow_node_runs workflow_node_runs_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_node_runs
    ADD CONSTRAINT workflow_node_runs_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: workflow_node_runs workflow_node_runs_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_node_runs
    ADD CONSTRAINT workflow_node_runs_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE CASCADE;


--
-- Name: workflow_runs workflow_runs_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_runs
    ADD CONSTRAINT workflow_runs_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: workflow_runs workflow_runs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_runs
    ADD CONSTRAINT workflow_runs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: workflow_runs workflow_runs_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_runs
    ADD CONSTRAINT workflow_runs_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: workflow_runs workflow_runs_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_runs
    ADD CONSTRAINT workflow_runs_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.workflow_templates(id) ON DELETE SET NULL;


--
-- Name: workflow_template_nodes workflow_template_nodes_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_template_nodes
    ADD CONSTRAINT workflow_template_nodes_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.workflow_templates(id) ON DELETE CASCADE;


--
-- Name: workflow_templates workflow_templates_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_templates
    ADD CONSTRAINT workflow_templates_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: workflow_templates workflow_templates_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_templates
    ADD CONSTRAINT workflow_templates_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- Name: workspaces workspaces_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--
-- Restore the application schema for migrations that run on this connection.
SELECT pg_catalog.set_config('search_path', 'public', false);

-- +goose Down
-- The Goose ledger lives in cineweave_migrations, outside public.
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public AUTHORIZATION pg_database_owner;
GRANT USAGE, CREATE ON SCHEMA public TO pg_database_owner;
GRANT USAGE ON SCHEMA public TO PUBLIC;
COMMENT ON SCHEMA public IS 'standard public schema';
