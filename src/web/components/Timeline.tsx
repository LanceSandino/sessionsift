/** @module Timeline @description Presents a prompt-centered work journey with summarized evidence and expanded navigation. */
import React,{useEffect,useMemo,useRef,useState} from "react";
import {absoluteTime,duration} from "../format";
import type {Session,Turn} from "../types";
import {scrollToCheckpoint,useLatestScroll} from "../useLatestScroll";
import {buildTimelineJourney,type JourneyCheckpoint} from "./timelineJourney";
import {latestWindow} from "./timelineWindow";
import {observedUsageDelta,usageDeltaLabel} from "../usageMetrics";

export function Timeline({session,onTurn,expanded=false}:{session:Session;onTurn:(turn:Turn)=>void;expanded?:boolean}){
  const[limit,setLimit]=useState(8);
  const viewport=useRef<HTMLDivElement>(null);
  const journey=useMemo(()=>buildTimelineJourney(session),[session]);
  const{shown,remaining}=latestWindow(journey,limit);
  useLatestScroll(viewport,session.id);
  useEffect(()=>setLimit(8),[session.id]);

  const jumpTo=(id:string)=>{
    if(!id)return;
    scrollToCheckpoint(`timeline-checkpoint-${session.id}-${id}`,viewport.current);
  };

  return <section className={`card timelineCard ${expanded?"expandedTimeline":""}`}>
    <div className="sectionTitle timelineHeading"><div><h3>Work journey</h3><small className="timelineStart">Started {absoluteTime(session.started_at)}</small></div><span className="pill">{journey.length} prompts</span></div>
    <div className="timelineToolbar">
      <label className="jumpControl"><span>Jump to prompt</span><select aria-label="Jump to timeline prompt" defaultValue="" onChange={event=>jumpTo(event.target.value)}><option value="">Choose a visible prompt</option>{shown.map(checkpoint=><option key={checkpoint.id} value={checkpoint.id}>Prompt {checkpoint.sequence} · {clockTime(checkpoint.startedAt)}</option>)}</select></label>
      <button className="secondaryButton latestButton" onClick={()=>{if(viewport.current)viewport.current.scrollTop=viewport.current.scrollHeight}}>Latest checkpoint</button>
      <div className="timelineSummary" aria-label="Timeline evidence summary"><span><b>{journey.length}</b> prompt checkpoints</span><span><b>{(session.tool_calls||[]).length.toLocaleString()}</b> tool calls summarized</span><span><b>{(session.tool_calls||[]).filter(call=>!call.success).length}</b> failed</span></div>
    </div>
    <div className="timelineJourneyLayout">
      {expanded&&<aside className="timelineNavigator" aria-label="Timeline checkpoints"><header><b>Prompt index</b><small>{clockTime(session.started_at)} – {clockTime(session.ended_at)}</small></header><div>{shown.map(checkpoint=><button key={checkpoint.id} type="button" onClick={()=>jumpTo(checkpoint.id)}><span>{checkpoint.sequence}</span><span><b>{clockTime(checkpoint.startedAt)}</b><small>{checkpoint.prompt}</small></span></button>)}</div></aside>}
      <div ref={viewport} className="timeline timelineScroll scrollViewport" aria-label="Conversation timeline entries" data-start-position="latest">
        {remaining>0&&<button className="loadMore" onClick={()=>setLimit(journey.length)}>Show {remaining} earlier checkpoints</button>}
        {shown.map(checkpoint=><Checkpoint key={checkpoint.id} checkpoint={checkpoint} sessionID={session.id} expanded={expanded} onTurn={onTurn}/>)}
        {shown.length===0&&<p className="empty">No prompt checkpoints available.</p>}
      </div>
    </div>
  </section>;
}

function Checkpoint({checkpoint,sessionID,expanded,onTurn}:{checkpoint:JourneyCheckpoint;sessionID:string;expanded:boolean;onTurn:(turn:Turn)=>void}){
  const categories=Object.entries(checkpoint.categories).sort((a,b)=>b[1]-a[1]);
  const notes=expanded?checkpoint.notes:checkpoint.notes.slice(0,2);
  const usageDelta=observedUsageDelta(checkpoint.turn);
  return <article id={`timeline-checkpoint-${sessionID}-${checkpoint.id}`} className={`journeyCheckpoint ${checkpoint.failedCount?"hasFailure":""}`}>
    <div className="journeyRail" aria-hidden="true"><span>{checkpoint.sequence}</span></div>
    <button className="journeyPrompt" type="button" aria-label={`Open prompt ${checkpoint.sequence}: ${checkpoint.prompt}`} onClick={()=>onTurn(checkpoint.turn)}>
      <header><span className="journeyLabel">Prompt {checkpoint.sequence}</span><time dateTime={checkpoint.startedAt} title={absoluteTime(checkpoint.startedAt)}>{clockTime(checkpoint.startedAt)}</time><small>{duration(Math.max(0,new Date(checkpoint.endedAt).getTime()-new Date(checkpoint.startedAt).getTime()))}</small></header>
      <p>{checkpoint.prompt}</p>
      <div className="journeyStats"><span className="settingStat">{checkpoint.turn.model||"model unavailable"} · {checkpoint.turn.reasoning_effort||"effort unavailable"}</span>{usageDelta!==undefined&&<span className="usageStat" title="Observed account-wide rate-meter movement; concurrent chats may contribute.">{usageDeltaLabel(usageDelta)} usage</span>}<span className="primaryStat">{checkpoint.toolCount.toLocaleString()} tool calls</span>{categories.slice(0,expanded?6:3).map(([category,count])=><span key={category}>{count.toLocaleString()} {category}</span>)}{checkpoint.mcpCount>0&&<span>{checkpoint.mcpCount.toLocaleString()} MCP</span>}{checkpoint.failedCount>0&&<span className="failureStat">{checkpoint.failedCount} failed</span>}</div>
      {notes.length>0&&<div className="journeyNotes">{notes.map(note=><span key={note.id}><b>{note.kind}</b>{note.text}</span>)}{!expanded&&checkpoint.notes.length>notes.length&&<small>+{checkpoint.notes.length-notes.length} more notable events</small>}</div>}
    </button>
  </article>;
}

function clockTime(value:string){
  const date=new Date(value);
  if(Number.isNaN(date.getTime()))return "Unknown time";
  return new Intl.DateTimeFormat(undefined,{hour:"numeric",minute:"2-digit",second:"2-digit"}).format(date);
}
