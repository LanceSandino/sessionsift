/** @module Transcript @description The conversation lens: prompt checkpoints, chat turns, tool runs, reported validation and review notes, latest first. */
import React,{useEffect,useLayoutEffect,useMemo,useRef,useState} from "react";
import {clockTime,shortDateTime,span,twoDigits} from "../format";
import {noteTarget} from "../reviewState";
import {callLabel,callsByTurn,lastValidation,workPhases} from "../sessionEvidence";
import {sourceInfo} from "../sources";
import type {Annotation,Session} from "../types";
import {scrollToCheckpoint} from "../useLatestScroll";
import type {Evidence,WorkspaceActions} from "../workspace";
import {Icon} from "./Icon";
import {RichContent} from "./RichContent";
import {ToolRunGroup} from "./ToolRunGroup";
import {latestWindow} from "./timelineWindow";

export type JumpRequest={turnID:string;nonce:number};

type Props={
  session:Session;
  annotations:Annotation[];
  selected?:Evidence;
  viewport:React.RefObject<HTMLElement|null>;
  jump?:JumpRequest;
  openRuns:Record<string,boolean>;
  onRunToggle:(turnID:string,open:boolean)=>void;
  actions:Pick<WorkspaceActions,"selectEvidence">;
};

export function checkpointID(sessionID:string,turnID:string){return `checkpoint-${sessionID}-${turnID}`}

export function Transcript({session,annotations,selected,viewport,jump,openRuns,onRunToggle,actions}:Props){
  const turns=useMemo(()=>(session.turns||[]).slice().sort((a,b)=>a.started_at.localeCompare(b.started_at)),[session.turns]);
  const runs=useMemo(()=>callsByTurn(session),[session]);
  const phases=useMemo(()=>workPhases(session.tool_calls||[]),[session.tool_calls]);
  const[limit,setLimit]=useState(3);
  const pending=useRef<string|undefined>(undefined);
  const{shown,remaining}=latestWindow(turns,limit);
  const nextBatch=Math.min(5,remaining);
  const info=sourceInfo(session);
  useEffect(()=>setLimit(3),[session.id]);
  // Earlier prompts mount first when a jump targets history outside the window.
  const jumpTo=(turnID:string)=>{
    const index=turns.findIndex(turn=>turn.id===turnID);
    if(index<0)return;
    const needed=turns.length-index;
    if(needed<=limit){scrollToCheckpoint(checkpointID(session.id,turnID),viewport.current);return}
    pending.current=turnID;
    setLimit(needed);
  };
  useEffect(()=>{if(jump)jumpTo(jump.turnID)},[jump]);
  useLayoutEffect(()=>{
    if(!pending.current)return;
    const target=pending.current;pending.current=undefined;
    scrollToCheckpoint(checkpointID(session.id,target),viewport.current);
  });
  const toLatest=()=>{const node=viewport.current;if(!node)return;if(typeof node.scrollTo==="function")node.scrollTo({top:node.scrollHeight,behavior:"smooth"});else node.scrollTop=node.scrollHeight};
  const sessionNotes=annotations.filter(note=>!note.turn_id||!turns.some(turn=>turn.id===note.turn_id));
  return <article className="transcript" aria-label="Conversation transcript">
    <div className="transcript-head">
      <span className="row"><Icon name="lock"/>{info.chat?"Imported export":"Recorded transcript"}<span>· Read-only</span></span>
      {turns.length>1&&<span className="row">
        <select className="jump-select" aria-label="Jump to prompt" value="" onChange={event=>{if(event.target.value)jumpTo(event.target.value)}}><option value="">Jump to prompt…</option>{turns.map(turn=><option key={turn.id} value={turn.id}>Prompt {turn.sequence} · {clockTime(turn.started_at)}</option>)}</select>
        <button type="button" className="text-button" onClick={toLatest}>Latest checkpoint <Icon name="arrow"/></button>
      </span>}
    </div>
    {phases.length>0&&<nav className="phase-nav" aria-label="Work sequence">
      {phases.map((phase,index)=><button type="button" key={phase.id} className={`phase ${phase.failed?"has-failure":""}`} title={`${phase.count} ${phase.label.toLowerCase()} call${phase.count===1?"":"s"}${phase.failed?`, ${phase.failed} failed`:""} · opens the first one`} onClick={()=>actions.selectEvidence({kind:"tool",id:phase.firstCallId})}><span className="phase-dot">{index+1}</span>{phase.label}<span className="phase-count">{phase.count}</span></button>)}
    </nav>}
    {remaining>0&&<button type="button" className="load-more" onClick={()=>setLimit(value=>Math.min(turns.length,value+nextBatch))}>Show {nextBatch} earlier prompt{nextBatch===1?"":"s"}</button>}
    {shown.map((turn,index)=>{
      const calls=runs.get(turn.id)||[];
      const validation=lastValidation(calls);
      const edited=[...new Set(calls.filter(call=>call.tool_category==="edit"&&call.target_path_display).map(call=>call.target_path_display!))];
      const steering=(session.events||[]).filter(event=>event.turn_id===turn.id&&event.event_subtype==="steering_or_queued");
      const compactions=(session.events||[]).filter(event=>event.turn_id===turn.id&&event.event_type==="compaction");
      const notes=annotations.filter(note=>note.turn_id===turn.id);
      const first=index===0||shown[index-1].started_at.slice(0,10)!==turn.started_at.slice(0,10);
      return <section key={turn.id} aria-label={`Prompt ${turn.sequence}`}>
        <div className="checkpoint" id={checkpointID(session.id,turn.id)}>
          <button type="button" className="number" aria-label={`Inspect prompt ${turn.sequence}`} onClick={()=>actions.selectEvidence({kind:"turn",id:turn.id})}>{twoDigits(turn.sequence)}</button>
          <span className="checkpoint-meta">Prompt {turn.sequence}{calls.length>0?` · ${calls.length} tool call${calls.length===1?"":"s"}`:""}</span>
          <time dateTime={turn.started_at}>{first?shortDateTime(turn.started_at):clockTime(turn.started_at)}</time>
        </div>
        <div className="prompt" aria-label={`You message for prompt ${turn.sequence}`}><div className="speaker">You</div><div className="user-message"><RichContent text={turn.user_prompt_redacted||"Prompt content unavailable."}/></div></div>
        {steering.map(event=><div className="prompt steer" key={event.id} aria-label="Steering or queued message"><div className="speaker">You · steering or queued</div><div className="user-message"><RichContent text={String(event.payload_redacted?.text||"Message content unavailable.")}/></div></div>)}
        <section className="assistant" aria-label={`${info.assistant} reply for prompt ${turn.sequence}`}>
          <div className="assistant-label"><span className={`assistant-mark ${info.tone}`} aria-hidden="true">{info.icon}</span><strong>{info.assistant}</strong><span className="model">{turn.model||session.model||"Model unavailable"} · {turn.reasoning_effort?`${turn.reasoning_effort} effort`:"effort unreported"}</span><time>{span(turn.started_at,turn.ended_at)} active</time></div>
          <div className="assistant-body">
            {calls.length>0&&<ToolRunGroup calls={calls} open={Boolean(openRuns[turn.id])} selected={selected} onToggle={open=>onRunToggle(turn.id,open)} onSelect={actions.selectEvidence}/>}
            {turn.assistant_summary_redacted?<RichContent text={turn.assistant_summary_redacted}/>:<p className="no-reply">No reply text was recorded for this prompt.</p>}
            {validation&&<div className={`result-strip ${validation.success?"":"failed"}`}><Icon name={validation.success?"circle-check":"warning"}/><div><strong>{validation.success?"Validation passed":"Validation failed"} · {callLabel(validation)}</strong><small>Last validation call in this prompt · reported in the transcript, not rerun here</small></div><button type="button" className="text-button" onClick={()=>actions.selectEvidence({kind:"tool",id:validation.id})}>Inspect run <Icon name="arrow"/></button></div>}
            {edited.length>0&&<div className="artifact-grid">{edited.slice(0,6).map(path=><button type="button" className="artifact" key={path} onClick={()=>actions.selectEvidence({kind:"file",path})}><Icon name="file"/><span><strong>{path}</strong><small>Edited · inspect activity</small></span></button>)}</div>}
            {edited.length>6&&<p className="no-reply">{edited.length-6} more edited files are listed in the inspector.</p>}
          </div>
        </section>
        {compactions.map(event=><div className="compact-boundary" key={event.id}><Icon name="compact"/>Context compacted · recorded transcript boundary</div>)}
        {notes.map(note=><Note key={note.id} note={note} target={`Prompt ${turn.sequence}`}/>)}
      </section>;
    })}
    {sessionNotes.map(note=><Note key={note.id} note={note} target={noteTarget(note,turns)}/>)}
    {turns.length===0&&<p className="transcript-empty">No transcript turns were observed.</p>}
  </article>;
}

function Note({note,target}:{note:Annotation;target:string}){
  return <div className="annotation-block"><div className="row between"><span className="eyebrow">Review note · {target}</span><small>{note.author} · {shortDateTime(note.created_at)}</small></div><p>{note.body}</p></div>;
}
