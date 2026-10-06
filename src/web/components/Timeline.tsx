/** @module Timeline @description The prompt-centred work journey: each checkpoint summarizes its activity and lists bounded, selectable evidence. */
import React,{useEffect,useMemo,useState} from "react";
import {absoluteTime,clockTime,excerpt,twoDigits} from "../format";
import {callLabel,callStatus,phaseOf,transportLabel} from "../sessionEvidence";
import type {Session,ToolCall} from "../types";
import {observedUsageDelta,usageDeltaLabel} from "../usageMetrics";
import {scrollToCheckpoint} from "../useLatestScroll";
import type {Evidence,WorkspaceActions} from "../workspace";
import {Icon,type IconName} from "./Icon";
import {ActivityBadges} from "./ActivityBadges";
import {buildTimelineJourney,journeyEntries,type JourneyCheckpoint,type JourneyFilter} from "./timelineJourney";
import {latestWindow} from "./timelineWindow";

const entryCap=8;
const phaseIcons:Record<string,IconName>={explore:"search",edit:"file",validate:"circle-check",run:"terminal",external:"link"};

type Props={session:Session;selected?:Evidence;viewport:React.RefObject<HTMLElement|null>;actions:Pick<WorkspaceActions,"selectEvidence">};

export function timelineID(sessionID:string,turnID:string){return `timeline-${sessionID}-${turnID}`}

export function Timeline({session,selected,viewport,actions}:Props){
  const journey=useMemo(()=>buildTimelineJourney(session),[session]);
  const[limit,setLimit]=useState(8);
  const[filter,setFilter]=useState<JourneyFilter>("all");
  const[expanded,setExpanded]=useState<Set<string>>(()=>new Set());
  useEffect(()=>{setLimit(8);setExpanded(new Set())},[session.id]);
  const{shown,remaining}=latestWindow(journey,limit);
  const failedTotal=journey.reduce((sum,checkpoint)=>sum+checkpoint.failedCount,0);
  const jumpTo=(id:string)=>{
    const index=journey.findIndex(checkpoint=>checkpoint.id===id);
    if(index<0)return;
    if(journey.length-index>limit)setLimit(journey.length-index);
    requestAnimationFrame(()=>scrollToCheckpoint(timelineID(session.id,id),viewport.current));
  };
  return <div className="content-page" aria-label="Timeline">
    <div className="page-intro"><h2>The work, in order.</h2><p>Prompt checkpoints, tool activity and recorded events. Started {absoluteTime(session.started_at)}.</p></div>
    <div className="page-toolbar">
      <div className="filter-row" role="group" aria-label="Timeline filter">
        <button type="button" className={`filter ${filter==="all"?"active":""}`} aria-pressed={filter==="all"} onClick={()=>setFilter("all")}>All activity</button>
        <button type="button" className={`filter ${filter==="failed"?"active":""}`} aria-pressed={filter==="failed"} onClick={()=>setFilter("failed")}>Failed calls <span className="mono">{failedTotal}</span></button>
      </div>
      {journey.length>1&&<select className="jump-select" aria-label="Jump to timeline prompt" value="" onChange={event=>{if(event.target.value)jumpTo(event.target.value)}}><option value="">Jump to prompt…</option>{journey.map(checkpoint=><option key={checkpoint.id} value={checkpoint.id}>Prompt {checkpoint.sequence} · {clockTime(checkpoint.startedAt)}</option>)}</select>}
    </div>
    {remaining>0&&<button type="button" className="load-more" onClick={()=>setLimit(journey.length)}>Show {remaining} earlier checkpoints</button>}
    {shown.map(checkpoint=><Checkpoint key={checkpoint.id} sessionID={session.id} checkpoint={checkpoint} filter={filter} expanded={expanded.has(checkpoint.id)} selected={selected} onExpand={()=>setExpanded(value=>new Set(value).add(checkpoint.id))} onSelect={actions.selectEvidence}/>)}
    {journey.length===0&&<p className="empty">No prompt checkpoints available.</p>}
  </div>;
}

function Checkpoint({sessionID,checkpoint,filter,expanded,selected,onExpand,onSelect}:{sessionID:string;checkpoint:JourneyCheckpoint;filter:JourneyFilter;expanded:boolean;selected?:Evidence;onExpand:()=>void;onSelect:(evidence:Evidence)=>void}){
  const{shown,hidden}=journeyEntries(checkpoint,filter,expanded?Number.MAX_SAFE_INTEGER:entryCap);
  const categories=Object.entries(checkpoint.categories).sort((a,b)=>b[1]-a[1]).slice(0,4);
  const usage=observedUsageDelta(checkpoint.turn);
  return <section className={`timeline-group ${checkpoint.failedCount?"has-failure":""}`} id={timelineID(sessionID,checkpoint.id)}>
    <button type="button" className="timeline-heading" aria-label={`Open prompt ${checkpoint.sequence}: ${excerpt(checkpoint.prompt,120)}`} onClick={()=>onSelect({kind:"turn",id:checkpoint.id})}><span className="number">{twoDigits(checkpoint.sequence)}</span><h3>{excerpt(checkpoint.prompt,110)||"Redacted prompt"}</h3><time dateTime={checkpoint.startedAt}>{clockTime(checkpoint.startedAt)}</time></button>
    <div className="timeline-summary">
      <span>{checkpoint.turn.model||"model unavailable"} · {checkpoint.turn.reasoning_effort?`${checkpoint.turn.reasoning_effort} effort`:"effort unreported"}</span>
      <span>{checkpoint.toolCount.toLocaleString()} tool calls</span>
      {categories.map(([category,count])=><span key={category}>{count.toLocaleString()} {category}</span>)}
      {checkpoint.mcpCount>0&&<span>{checkpoint.mcpCount.toLocaleString()} MCP</span>}
      {checkpoint.failedCount>0&&<span className="warn-text">{checkpoint.failedCount} failed</span>}
      {usage!==undefined&&<span title="Observed account-wide rate-meter movement; concurrent chats may contribute.">{usageDeltaLabel(usage)} usage</span>}
    </div>
    {shown.map(entry=>entry.kind==="call"?<CallRow key={entry.call.id} call={entry.call} selected={selected?.kind==="tool"&&selected.id===entry.call.id} onSelect={onSelect}/>:entry.note.compaction?<div className="compact-boundary" key={entry.note.id}><Icon name="compact"/>{entry.note.kind.toLowerCase()} · recorded boundary</div>:<div className="timeline-event note" key={entry.note.id}><time>{clockTime(entry.at)}</time><Icon name={entry.note.kind==="STEER / QUEUE"?"steer":"spark"}/><div className="grow"><strong>{excerpt(entry.note.text,140)}</strong><small>{entry.note.kind}</small></div></div>)}
    {hidden>0&&<button type="button" className="timeline-more" onClick={onExpand}>Show all {(checkpoint.calls.length+checkpoint.notes.length).toLocaleString()} events</button>}
    {filter==="failed"&&shown.length===0&&<div className="timeline-none">No failed calls in this checkpoint.</div>}
    {filter==="all"&&shown.length===0&&<div className="timeline-none">No tool activity or events were recorded for this prompt.</div>}
  </section>;
}

function CallRow({call,selected,onSelect}:{call:ToolCall;selected:boolean;onSelect:(evidence:Evidence)=>void}){
  const status=callStatus(call);
  return <button type="button" className={`timeline-event ${call.success?"":"failed"}`} aria-current={selected?"true":undefined} onClick={()=>onSelect({kind:"tool",id:call.id})}>
    <time>{clockTime(call.timestamp)}</time><Icon name={call.success?phaseIcons[phaseOf(call)||""]||"tool":"warning"}/>
    <div className="grow"><strong>{callLabel(call)}</strong><small>{call.tool_name} · {transportLabel(call)}{call.tool_server?` · ${call.tool_server}`:""}</small><ActivityBadges call={call} compact/></div>
    <span className={`badge ${status==="Failed"?"warn":status==="Passed"?"good":""}`}>{status}</span>
  </button>;
}
