/** @module Transcript @description Renders the redacted conversation with prompt checkpoint navigation. */
import {RichContent} from "./RichContent";
import React,{useEffect,useRef,useState} from "react";
import type {Session} from "../types";
import {scrollToCheckpoint,useLatestScroll} from "../useLatestScroll";
import {latestWindow} from "./timelineWindow";
import {sourceInfo} from "../sources";

export function Transcript({session}:{session:Session}){
  const viewport=useRef<HTMLDivElement>(null);
  const turns=session.turns||[];
  const[limit,setLimit]=useState(3);
  const{shown,remaining}=latestWindow(turns,limit);
  const nextBatch=Math.min(5,remaining);
  const assistant=sourceInfo(session).assistant;
  useLatestScroll(viewport,session.id);
  useEffect(()=>setLimit(3),[session.id]);
  const jump=(id:string)=>{if(id)scrollToCheckpoint(id,viewport.current);else if(viewport.current)viewport.current.scrollTop=viewport.current.scrollHeight};
  return <section className="card transcriptCard"><div className="sectionTitle"><div><h3>Conversation</h3><small>Redacted local transcript{remaining>0?` · latest ${shown.length} of ${turns.length}`:""}</small></div><span className="pill">{turns.length} prompts</span></div><div className="jumpBar"><label className="jumpControl"><span>Jump to</span><select aria-label="Jump to transcript prompt" defaultValue="" onChange={event=>jump(event.target.value)}><option value="">Latest prompt</option>{shown.map(turn=><option key={turn.id} value={`transcript-${session.id}-${turn.id}`}>Prompt {turn.sequence} · {new Date(turn.started_at).toLocaleTimeString()}</option>)}</select></label><button className="secondaryButton latestButton" onClick={()=>jump("")}>Latest message</button></div><div ref={viewport} className="transcriptScroll scrollViewport" aria-label="Chat transcript entries" data-start-position="latest">{remaining>0&&<button className="loadMore" onClick={()=>setLimit(value=>Math.min(turns.length,value+nextBatch))}>Show {nextBatch} earlier prompts</button>}{shown.map(turn=><article className="transcriptTurn" id={`transcript-${session.id}-${turn.id}`} key={turn.id}><div className="promptDivider"><span>Prompt {turn.sequence}</span><time>{new Date(turn.started_at).toLocaleString()}</time></div><div className="chatBubbleRow user" aria-label={`You message for prompt ${turn.sequence}`}><div className="chatAvatar" aria-hidden="true">Y</div><div className="chatBubble"><span>You</span><RichContent text={turn.user_prompt_redacted||"Prompt content unavailable."}/></div></div>{turn.assistant_summary_redacted&&<div className="chatBubbleRow assistant" aria-label={`${assistant} reply for prompt ${turn.sequence}`}><div className="chatAvatar" aria-hidden="true">{assistant.slice(0,1)}</div><div className="chatBubble"><span>{assistant}</span><RichContent text={turn.assistant_summary_redacted}/></div></div>}</article>)}{turns.length===0&&<p className="empty">No transcript turns were observed.</p>}</div></section>
}
