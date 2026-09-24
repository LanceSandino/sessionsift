/** @module InspectorContext @description The inspector's context tab: reported outcome, derived work sequence, counts, usage, artifacts, findings, model settings and execution context. */
import React,{useMemo} from "react";
import {absoluteTime,compact,duration,excerpt,percent} from "../format";
import {explorationCounts,referencedFiles,workPhases} from "../sessionEvidence";
import {sourceInfo} from "../sources";
import type {Session} from "../types";
import {modelSettingPeriods,usageDeltaLabel} from "../usageMetrics";
import type {WorkspaceActions} from "../workspace";
import {FindingList} from "./FindingList";
import {Icon} from "./Icon";

const fileLimit=8;

export function InspectorContext({session,actions,onCompare}:{session:Session;actions:Pick<WorkspaceActions,"selectEvidence"|"jumpToTurn">;onCompare:()=>void}){
  const calls=session.tool_calls||[];
  const turns=useMemo(()=>(session.turns||[]).slice().sort((a,b)=>a.started_at.localeCompare(b.started_at)),[session.turns]);
  const phases=useMemo(()=>workPhases(calls),[calls]);
  const files=useMemo(()=>referencedFiles(calls),[calls]);
  const settings=useMemo(()=>modelSettingPeriods(turns),[turns]);
  const latest=[...turns].reverse().find(turn=>turn.assistant_summary_redacted);
  const metrics=session.metrics;
  const failed=metrics?.failed_tool_calls??calls.filter(call=>!call.success).length;
  const exploration=explorationCounts(calls);
  const ratio=metrics?.exploration_ratio??(exploration.total?exploration.explore/exploration.total:0);
  const info=sourceInfo(session);
  const context=[
    ["Started",absoluteTime(session.started_at)],
    ["Client / device",session.originator],
    ["Surface",session.surface],
    ["Work type",session.session_kind?.replaceAll("_"," ")],
    ["Run location",session.runtime_location?`${session.runtime_location} (derived)`:undefined],
    ["Thread source",session.thread_source],
    ["Branch",session.git_branch],
    ["Approvals",session.approval_mode],
    ["Sandbox",session.sandbox_policy],
    ["Network",session.network_access],
  ].filter((entry):entry is [string,string]=>Boolean(entry[1]));
  return <>
    <section className="inspector-section">
      <span className="eyebrow">Latest reported outcome</span>
      {latest?<><h3>{excerpt(latest.assistant_summary_redacted,120)}</h3><p className="description">Reported by {info.assistant} in prompt {latest.sequence}. SessionSift did not re-run or verify it.</p><button type="button" className="text-button" onClick={()=>actions.jumpToTurn(latest.id)}>Read in conversation <Icon name="arrow"/></button></>:<p className="description">No reply text was recorded for this session.</p>}
      {phases.length>0&&<div className="journey" aria-label="Work sequence">{phases.map(phase=><button type="button" key={phase.id} className={`journey-step ${phase.failed?"has-failure":""}`} onClick={()=>actions.selectEvidence({kind:"tool",id:phase.firstCallId})}><span className="step-mark"><Icon name={phase.failed?"warning":"check"}/></span><span><strong>{phase.label}</strong><small>{phase.count.toLocaleString()} call{phase.count===1?"":"s"}{phase.failed?` · ${phase.failed} failed`:""} · derived from tool categories</small></span></button>)}</div>}
    </section>
    <section className="inspector-section">
      <div className="stats-mini"><div><strong>{turns.length}</strong><span>prompts</span></div><div><strong>{(metrics?.tool_calls??calls.length).toLocaleString()}</strong><span>tool calls</span></div><div><strong className={failed?"warn-text":""}>{failed}</strong><span>failed calls</span></div></div>
      <div className="row between small"><span>Exploration ratio</span><strong>{percent(ratio)}</strong></div>
      <div className="ratio-track"><div className="ratio-fill" style={{width:`${Math.round(ratio*100)}%`}}/></div>
      <div className="row between ratio-caption"><span>{exploration.explore} reads / searches of {exploration.total} calls</span><span>Derived</span></div>
    </section>
    {metrics&&<section className="inspector-section">
      <div className="inspector-title"><span>Usage</span><span className="muted">Observed</span></div>
      <div className="data-row"><span className="key">Active work</span><span className="value">{duration(metrics.duration_ms)}</span></div>
      <div className="data-row"><span className="key">Elapsed span</span><span className="value">{duration(metrics.elapsed_duration_ms)}</span></div>
      <div className="data-row"><span className="key">Input tokens</span><span className="value">{compact(metrics.input_tokens)} · {percent(metrics.cache_ratio)} cached</span></div>
      <div className="data-row"><span className="key">Output tokens</span><span className="value">{compact(metrics.output_tokens)} · {compact(metrics.reasoning_tokens)} reasoning</span></div>
      <div className="data-row"><span className="key">Observed account usage</span><span className="value">{metrics.usage_measured_turns?usageDeltaLabel(metrics.usage_delta_percent):"Unavailable"}</span></div>
      {metrics.usage_measured_turns>0&&<p className="description small">Account-wide rate-meter movement across {metrics.usage_measured_turns} measured prompt{metrics.usage_measured_turns===1?"":"s"}; concurrent chats can contribute, so it is not exact attribution.</p>}
    </section>}
    {files.length>0&&<section className="inspector-section">
      <div className="inspector-title"><span>Referenced artifacts</span><span className="muted">{files.length}</span></div>
      {files.slice(0,fileLimit).map(file=><button type="button" className="file-row" key={file.path} onClick={()=>actions.selectEvidence({kind:"file",path:file.path})}><Icon name="file"/><span><strong>{file.path}</strong><small>{file.edited?"Edited":"Referenced"} · {file.count} call{file.count===1?"":"s"}</small></span><Icon name="chevron"/></button>)}
      {files.length>fileLimit&&<p className="small muted">{files.length-fileLimit} more files appear in tool evidence.</p>}
    </section>}
    {(session.findings||[]).length>0&&<section className="inspector-section">
      <div className="inspector-title"><span>What SessionSift noticed</span><span className="muted">{session.findings!.length}</span></div>
      <FindingList findings={session.findings!} limit={3} onSelect={actions.selectEvidence}/>
    </section>}
    {settings.length>0&&<section className="inspector-section">
      <div className="inspector-title"><span>Model & effort</span><span className="muted">{settings.length===1?"No change observed":`${settings.length-1} change${settings.length===2?"":"s"} observed`}</span></div>
      {settings.map(period=><div className="data-row" key={`${period.key}-${period.firstSequence}`}><span className="key">Prompt{period.firstSequence===period.lastSequence?` ${period.firstSequence}`:`s ${period.firstSequence}–${period.lastSequence}`}</span><span className="value">{period.model} <span className="badge">{period.effort}</span>{period.measuredTurns>0&&<><br/><span className="muted">{usageDeltaLabel(period.usageDelta)} observed</span></>}</span></div>)}
    </section>}
    {context.length>0&&<section className="inspector-section">
      <div className="inspector-title"><span>Execution context</span><span className="muted">Observed</span></div>
      {context.map(([key,value])=><div className="data-row" key={key}><span className="key">{key}</span><span className="value">{value}</span></div>)}
      {session.title_source==="renamed"&&<p className="description small">Title renamed in Codex.</p>}
    </section>}
    <section className="inspector-section"><button type="button" className="text-button" onClick={onCompare}>Compare with another session <Icon name="arrow"/></button></section>
  </>;
}
