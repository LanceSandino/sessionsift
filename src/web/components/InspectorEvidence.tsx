/** @module InspectorEvidence @description The inspector's evidence tab: one selected call, prompt, file or finding with its provenance and redacted record. */
import React,{useMemo} from "react";
import {absoluteTime,compact,excerpt,percent,span,twoDigits} from "../format";
import {callLabel,callStatus,callsByTurn,transportLabel} from "../sessionEvidence";
import type {Session,ToolCall} from "../types";
import {observedUsageDelta,usageDeltaLabel} from "../usageMetrics";
import type {Evidence,WorkspaceActions} from "../workspace";
import {Icon} from "./Icon";
import {RichContent} from "./RichContent";

type Actions=Pick<WorkspaceActions,"selectEvidence"|"jumpToTurn"|"noteOn">;
const callListLimit=30;

export function InspectorEvidence({session,evidence,canAnnotate,actions}:{session:Session;evidence?:Evidence;canAnnotate:boolean;actions:Actions}){
  const runs=useMemo(()=>callsByTurn(session),[session]);
  const turnOf=useMemo(()=>{const index=new Map<string,string>();for(const[turnID,calls]of runs)for(const call of calls)index.set(call.id,turnID);return index},[runs]);
  const calls=session.tool_calls||[];
  if(!evidence)return <div className="empty"><Icon name="tool"/><br/>Select a tool call, file, prompt or finding.<br/><span className="small">Its evidence opens here without leaving the conversation.</span></div>;
  if(evidence.kind==="tool"){
    const call=calls.find(value=>value.id===evidence.id);
    if(!call)return <Missing/>;
    const turnID=turnOf.get(call.id);
    const turn=session.turns?.find(value=>value.id===turnID);
    const status=callStatus(call);
    const ordered=calls.slice().sort((a,b)=>a.timestamp.localeCompare(b.timestamp));
    return <>
      <section className="inspector-section">
        <div className="row between"><span className="badge">{transportLabel(call)} tool</span><span className={`badge ${status==="Failed"?"warn":status==="Passed"?"good":""}`}>{status}</span></div>
        <h3 className="tool-title">{call.tool_name}</h3>
        <p className="description">{callLabel(call)}</p>
        {turn&&<div className="data-row"><span className="key">Prompt checkpoint</span><button type="button" className="text-button" onClick={()=>actions.jumpToTurn(turn.id)}>{twoDigits(turn.sequence)} <Icon name="arrow"/></button></div>}
        <div className="data-row"><span className="key">Category</span><span className="value">{call.tool_category||"other"}</span></div>
        {call.tool_server&&<div className="data-row"><span className="key">Server</span><span className="value">{call.tool_server}</span></div>}
        {call.target_path_display&&<div className="data-row"><span className="key">Target</span><button type="button" className="text-button mono" onClick={()=>actions.selectEvidence({kind:"file",path:call.target_path_display!})}>{call.target_path_display}</button></div>}
        {call.exit_code!==undefined&&<div className="data-row"><span className="key">Exit code</span><span className="value mono">{call.exit_code}</span></div>}
        <div className="data-row"><span className="key">Recorded</span><span className="value">{absoluteTime(call.timestamp)}</span></div>
        <div className="data-row"><span className="key">Tool call</span><span className="value mono">{ordered.indexOf(call)+1} / {ordered.length}</span></div>
      </section>
      <section className="inspector-section"><div className="inspector-title"><span>Arguments</span><span className="muted">Redacted record</span></div>{call.arguments_redacted||call.command_signature?<div className="evidence-block"><RichContent text={call.arguments_redacted||call.command_signature||""} preformatted/></div>:<p className="evidence-empty">No arguments were retained for this call.</p>}</section>
      <section className="inspector-section"><div className="inspector-title"><span>Result</span><span className="muted">Redacted preview</span></div>{call.result_preview_redacted?<div className="evidence-block"><RichContent text={call.result_preview_redacted} preformatted/></div>:<p className="evidence-empty">No result preview was retained for this call.</p>}
        {!call.success&&<div className="evidence-banner">This failure stays in the record. A later passing call does not erase it or prove the issue is resolved.</div>}
      </section>
      {canAnnotate&&<button type="button" className="button" onClick={()=>actions.noteOn(turnID)}><Icon name="note"/>Add a review note</button>}
    </>;
  }
  if(evidence.kind==="turn"){
    const turn=session.turns?.find(value=>value.id===evidence.id);
    if(!turn)return <Missing/>;
    const turnCalls=runs.get(turn.id)||[];
    const files=[...new Set(turnCalls.map(call=>call.target_path_display).filter(Boolean))] as string[];
    const findings=(session.findings||[]).filter(finding=>finding.turn_id===turn.id);
    const usage=observedUsageDelta(turn);
    return <>
      <section className="inspector-section">
        <div className="row between"><span className="badge">Prompt {turn.sequence}</span><span className="badge">{span(turn.started_at,turn.ended_at)} active</span></div>
        <h3>{excerpt(turn.user_prompt_redacted,140)||"Redacted prompt"}</h3>
        <button type="button" className="text-button" onClick={()=>actions.jumpToTurn(turn.id)}>Open in conversation <Icon name="arrow"/></button>
        <div className="data-row"><span className="key">Started</span><span className="value">{absoluteTime(turn.started_at)}</span></div>
        <div className="data-row"><span className="key">Model</span><span className="value">{turn.model||session.model||"Unavailable"}</span></div>
        <div className="data-row"><span className="key">Reasoning effort</span><span className="value">{turn.reasoning_effort||"Unreported"}</span></div>
        <div className="turn-metrics">
          <div><span>Input</span><b>{compact(turn.input_tokens)}</b></div>
          <div><span>Cached</span><b>{compact(turn.cached_input_tokens)} · {percent(turn.input_tokens?turn.cached_input_tokens/turn.input_tokens:0)}</b></div>
          <div><span>Output</span><b>{compact(turn.output_tokens)}</b></div>
          <div><span>Reasoning</span><b>{compact(turn.reasoning_tokens)}</b></div>
          <div><span>Observed account usage</span><b>{usage===undefined?"Unavailable":usageDeltaLabel(usage)}</b></div>
          <div><span>Tool calls</span><b>{turnCalls.length} · {turnCalls.filter(call=>!call.success).length} failed</b></div>
        </div>
        {usage!==undefined&&<p className="description small">Account-wide meter movement during this prompt; concurrent chats may contribute. Window: {turn.rate_limit_window_minutes?`${turn.rate_limit_window_minutes} minutes`:"unavailable"}.</p>}
      </section>
      {files.length>0&&<section className="inspector-section"><div className="inspector-title"><span>Files touched</span><span className="muted">{files.length}</span></div>{files.slice(0,12).map(path=><button type="button" className="file-row" key={path} onClick={()=>actions.selectEvidence({kind:"file",path})}><Icon name="file"/><span><strong>{path}</strong></span><Icon name="chevron"/></button>)}</section>}
      <section className="inspector-section"><div className="inspector-title"><span>Tool sequence</span><span className="muted">{turnCalls.length}</span></div><CallList calls={turnCalls} onSelect={actions.selectEvidence}/>{!turnCalls.length&&<p className="evidence-empty">No tools were associated with this prompt.</p>}</section>
      {findings.length>0&&<section className="inspector-section"><div className="inspector-title"><span>Findings</span><span className="muted">{findings.length}</span></div>{findings.map(finding=><button type="button" className="file-row" key={finding.id} onClick={()=>actions.selectEvidence({kind:"finding",id:finding.id})}><Icon name="spark"/><span><strong>{finding.title}</strong><small>{finding.confidence} · {finding.summary}</small></span><Icon name="chevron"/></button>)}</section>}
      {canAnnotate&&<button type="button" className="button" onClick={()=>actions.noteOn(turn.id)}><Icon name="note"/>Add a note on this prompt</button>}
    </>;
  }
  if(evidence.kind==="file"){
    const fileCalls=calls.filter(call=>call.target_path_display===evidence.path).sort((a,b)=>a.timestamp.localeCompare(b.timestamp));
    if(!fileCalls.length)return <Missing/>;
    const edited=fileCalls.some(call=>call.tool_category==="edit");
    return <>
      <section className="inspector-section">
        <div className="row between"><span className="badge">Referenced file</span><span className={`badge ${edited?"good":""}`}>{edited?"Edited":"Read or referenced"}</span></div>
        <h3 className="mono">{evidence.path}</h3>
        <p className="description">{fileCalls.length} recorded call{fileCalls.length===1?"":"s"} touched this path.</p>
        <div className="evidence-banner neutral">SessionSift keeps redacted call previews, not file contents or diffs. Open a call to see what was recorded.</div>
      </section>
      <section className="inspector-section"><div className="inspector-title"><span>Activity</span><span className="muted">{fileCalls.length}</span></div><CallList calls={fileCalls} onSelect={actions.selectEvidence}/></section>
    </>;
  }
  const finding=session.findings?.find(value=>value.id===evidence.id);
  if(!finding)return <Missing/>;
  const turn=session.turns?.find(value=>value.id===finding.turn_id);
  return <section className="inspector-section">
    <div className="row between"><span className="badge">{finding.severity}</span><span className={`badge ${finding.confidence==="observed"?"good":finding.confidence==="possible"?"warn":""}`}>{finding.confidence}</span></div>
    <h3>{finding.title}</h3>
    <p className="description">{finding.summary}</p>
    <div className="data-row"><span className="key">Type</span><span className="value">{finding.finding_type.replaceAll("_"," ")}</span></div>
    {turn&&<div className="data-row"><span className="key">Prompt checkpoint</span><button type="button" className="text-button" onClick={()=>actions.selectEvidence({kind:"turn",id:turn.id})}>{twoDigits(turn.sequence)} <Icon name="arrow"/></button></div>}
    <div className="inspector-title spaced"><span>Evidence</span><span className="muted">{finding.evidence.length}</span></div>
    {finding.evidence.map(id=>{const call=calls.find(value=>value.id===id);return call?<button type="button" className="file-row" key={id} aria-label={`${call.tool_name}: ${callLabel(call)}`} onClick={()=>actions.selectEvidence({kind:"tool",id})}><Icon name="tool"/><span><strong>{call.tool_name}</strong><small>{callLabel(call)}</small></span><Icon name="chevron"/></button>:<div className="data-row" key={id}><span className="key">Record</span><span className="value mono">{id}</span></div>})}
    {!finding.evidence.length&&<p className="evidence-empty">Derived from session metrics.</p>}
    {Object.keys(finding.metric||{}).length>0&&<div className="evidence-block"><RichContent text={JSON.stringify(finding.metric)}/></div>}
  </section>;
}

function CallList({calls,onSelect}:{calls:ToolCall[];onSelect:(evidence:Evidence)=>void}){
  return <>
    {calls.slice(0,callListLimit).map(call=><button type="button" className={`file-row ${call.success?"":"warn-row"}`} key={call.id} aria-label={`${call.tool_name}: ${callLabel(call)} (${callStatus(call)})`} onClick={()=>onSelect({kind:"tool",id:call.id})}><Icon name={call.success?"check":"warning"}/><span><strong>{call.tool_name}</strong><small>{callLabel(call)} · {callStatus(call)}</small></span><Icon name="chevron"/></button>)}
    {calls.length>callListLimit&&<p className="small muted">{calls.length-callListLimit} more calls are listed in the Tools view.</p>}
  </>;
}

function Missing(){
  return <div className="empty"><Icon name="warning"/><br/>This evidence is no longer in the session.<br/><span className="small">It may have been re-imported under a new identity.</span></div>;
}
