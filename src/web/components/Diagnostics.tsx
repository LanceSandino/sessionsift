/** @module Diagnostics @description Explains whether capture is arriving, what the local agent discovered, and which evidence SessionSift can present. */
import React from "react";
import {compact,shortDateTime} from "../format";
import {sourceOf} from "../sources";
import type {DiagnosticsStatus,Session} from "../types";
import {DiagnosticExport} from "./DiagnosticExport";
import {Icon} from "./Icon";

type Check={label:string;value:string|number;detail:string;state:"ok"|"optional"|"attention"};

const received=(value?:string)=>value?`last received ${shortDateTime(value)}`:"no events received yet";

/** One live collector: active within the last 15 minutes, idle after, checking until the first probe returns. */
function collector(label:string,active:boolean|undefined,events:number|undefined,latest:string|undefined,noun:string,status?:DiagnosticsStatus,statusError?:string):Check{
  if(!status)return {label,value:statusError?"Unavailable":"Checking…",detail:statusError||"Reading capture activity…",state:statusError?"attention":"optional"};
  return {label,value:active?"Active now":"Idle",detail:`${compact(events||0)} ${noun} · ${received(latest)}`,state:active?"ok":"optional"};
}

export function Diagnostics({sessions,status,statusError}:{sessions:Session[];status?:DiagnosticsStatus;statusError?:string}){
  const warnings=sessions.reduce((sum,session)=>sum+(session.warnings?.length||0),0);
  const internal=sessions.filter(session=>session.session_kind==="internal_review").length;
  const visible=sessions.filter(session=>session.session_kind!=="internal_review");
  const bySource=(id:string)=>visible.filter(session=>sourceOf(session)===id);
  const codex=bySource("codex");
  const archived=codex.filter(session=>session.archived).length;
  const tools=visible.reduce((sum,session)=>sum+(session.metrics?.tool_calls||0),0);
  const mcp=visible.reduce((sum,session)=>sum+(session.metrics?.mcp_calls||0),0);
  const tokens=visible.reduce((sum,session)=>sum+(session.metrics?.input_tokens||0),0);
  const contexts=new Set(visible.flatMap(session=>[session.model,session.repo_display_name].filter(Boolean))).size;
  const live:Check[]=[
    collector("OpenTelemetry logs",status?.otel_active,status?.otel_events,status?.latest_otel_event_at,"structured telemetry events",status,statusError),
    collector("Codex lifecycle hooks",status?.hook_active,status?.hook_events,status?.latest_hook_event_at,"hook events",status,statusError),
    collector("Claude Code hooks",status?.claude_hook_active,status?.claude_hook_events,status?.latest_claude_hook_event_at,"hook events",status,statusError),
    {label:"Collector endpoint",value:status?.collector_running?"Online":statusError?"Unavailable":"Checking…",detail:"OTel log data enriches tokens, cache, latency and lifecycle evidence. Metrics batches are validated and acknowledged; raw metric series are not retained.",state:status?.collector_running?"ok":statusError?"attention":"optional"},
  ];
  const discovered:Check[]=[
    {label:"Current Codex sessions",value:codex.length-archived,detail:"Developer sessions from CODEX_HOME/sessions",state:"ok"},
    {label:"Archived Codex sessions",value:archived,detail:"Imported from CODEX_HOME/archived_sessions",state:"ok"},
    {label:"Claude Code sessions",value:bySource("claude_code").length,detail:"Transcripts from CLAUDE_CONFIG_DIR/projects, subagents kept separate",state:"ok"},
    {label:"ChatGPT conversations",value:bySource("chatgpt").length,detail:"Import an export with sessionsift import-chatgpt",state:bySource("chatgpt").length?"ok":"optional"},
    {label:"Claude.ai conversations",value:bySource("claude").length,detail:"Import an export with sessionsift import-claude",state:bySource("claude").length?"ok":"optional"},
    {label:"Internal reviews",value:internal,detail:"Guardian and auto-review work retained for audit, hidden from the library by default",state:"ok"},
    {label:"Parser warnings",value:warnings,detail:"Unsupported records are preserved instead of silently discarded",state:warnings?"attention":"ok"},
  ];
  const coverage=[
    {title:"Conversation",body:"Prompts, assistant summaries, checkpoints, steering or queued messages, and exact start and end times."},
    {title:"Model & environment",body:"Model and reasoning changes, repository and branch, surface, local or remote runtime, sandbox, approvals and network mode."},
    {title:"Usage",body:`${compact(tokens)} observed input tokens, cached, output and reasoning tokens, active work, elapsed span, and qualified rate-meter movement.`},
    {title:"Tools",body:`${compact(tools)} calls including ${compact(mcp)} MCP calls: transports, servers, success or failure, timing, files and bounded redacted previews.`},
    {title:"Findings",body:"Deterministic large-output, repetition, exploration, validation, correction and compaction signals with evidence references."},
    {title:"Coverage",body:`${contexts} observed model and repository contexts, plus cross-session source, model, repository, tool and activity trends.`},
  ];
  const rows=(checks:Check[])=>checks.map(check=><div className={`health-row ${check.state}`} key={check.label}><Icon name={check.state==="ok"?"circle-check":check.state==="attention"?"warning":"clock"}/><div><b>{check.label}</b><small>{check.detail}</small></div><strong className={typeof check.value==="string"?"text-value":undefined}>{check.value}</strong></div>);
  return <div className="content-page" aria-label="Diagnostics">
    <div className="page-intro"><span className="eyebrow">Collection &amp; data health</span><h2>What SessionSift can see.</h2><p>Use this page when data looks missing. Live capture answers “is it arriving?”, discovery shows what the local agent found, and coverage explains what SessionSift can derive. This page never displays credential values.</p></div>
    <section aria-labelledby="live-capture"><div className="section-head"><h3 id="live-capture">Live capture</h3><small>Metadata-only counts and recency</small></div>{rows(live)}</section>
    <section aria-labelledby="discovered"><div className="section-head"><h3 id="discovered">Discovered on this device</h3><small>{visible.length} conversations</small></div>{rows(discovered)}</section>
    <section aria-labelledby="coverage-title"><div className="section-head"><h3 id="coverage-title">What SessionSift can show</h3><small>Observed, redacted data already on this device</small></div><div className="coverage-grid">{coverage.map(item=><article key={item.title}><b>{item.title}</b><p>{item.body}</p></article>)}</div></section>
    <DiagnosticExport sessions={sessions}/>
    <section className="device-card"><h3><Icon name="shield"/>Privacy boundary</h3><p>SessionSift binds to <code>127.0.0.1</code>, hashes repository roots, redacts common secrets before storage, and never reads <code>~/.codex/auth.json</code>. Telemetry prompt logging is disabled.</p></section>
  </div>;
}
