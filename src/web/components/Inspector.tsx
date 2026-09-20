/** @module Inspector @description Composes the fixed-height, tabbed workspace for one session. */
import React,{useState} from "react";
import {compact,duration,percent} from "../format";
import type {Session,Turn} from "../types";
import {MetricCard} from "./MetricCard";
import {SessionWorkspace} from "./SessionWorkspace";
import {TurnDetail} from "./TurnDetail";
import {usageDeltaLabel} from "../usageMetrics";

export function Inspector({session,onCompare}:{session:Session;onCompare:()=>void}){
  const[selectedTurn,setSelectedTurn]=useState<Turn>();
  const[showMetrics,setShowMetrics]=useState(false);
  const metrics=session.metrics!;
  const source=session.source==="chatgpt"?"ChatGPT":"Codex";
  return <main className="inspector">
    <header className="inspectorHeader"><div><span className="eyebrow">{source.toUpperCase()} / {session.repo_display_name||"conversation"}</span><h1>{session.title}</h1><p>{source} · {session.model||"model unavailable"} · {duration(metrics.duration_ms)} active · {duration(metrics.elapsed_duration_ms)} elapsed · {metrics.tool_calls} tool calls · {metrics.mcp_calls} MCP</p></div><div className="inspectorActions"><button className="secondaryButton" aria-expanded={showMetrics} onClick={()=>setShowMetrics(!showMetrics)}>{showMetrics?"Hide metrics":"Metrics"}</button><button className="primaryButton" onClick={onCompare}>Compare</button></div></header>
    {session.warnings?.length?<div className="warningBanner">Parser retained this session with {session.warnings.length} warning{session.warnings.length===1?"":"s"}.</div>:null}
    {showMetrics&&<div className="metricGrid"><MetricCard label="Active work" value={duration(metrics.duration_ms)} sub="deduplicated prompt time"/><MetricCard label="Elapsed span" value={duration(metrics.elapsed_duration_ms)} sub="first prompt to last reply"/><MetricCard label="Observed account usage" value={metrics.usage_measured_turns?usageDeltaLabel(metrics.usage_delta_percent):"Unavailable"} sub={metrics.usage_measured_turns?`${metrics.usage_measured_turns} measured prompts · not exact attribution`:"No rate snapshots retained"}/><MetricCard label="Input tokens" value={compact(metrics.input_tokens)} sub={`${metrics.turn_count} turns`}/><MetricCard label="Cache reuse" value={percent(metrics.cache_ratio)} sub={`${compact(metrics.cached_input_tokens)} cached`} tone={metrics.cache_ratio<.25?"warn":undefined}/><MetricCard label="Output tokens" value={compact(metrics.output_tokens)} sub={`${compact(metrics.reasoning_tokens)} reasoning`}/><MetricCard label="Tool calls" value={String(metrics.tool_calls)} sub={`${metrics.unique_tools} unique · ${metrics.mcp_calls} MCP`}/><MetricCard label="Failed calls" value={String(metrics.failed_tool_calls)} sub={`${metrics.finding_count} findings`} tone={metrics.failed_tool_calls?"danger":undefined}/></div>}
    <SessionWorkspace session={session} onTurn={setSelectedTurn}/>
    {selectedTurn&&<TurnDetail turn={selectedTurn} session={session} onClose={()=>setSelectedTurn(undefined)}/>}
  </main>;
}
