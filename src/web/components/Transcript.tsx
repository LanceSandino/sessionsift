/** @module Transcript @description Renders the redacted conversation with prompt checkpoint navigation. */
import React,{useRef} from "react";
import type {Session} from "../types";
import {scrollToCheckpoint,useLatestScroll} from "../useLatestScroll";

export function Transcript({session}:{session:Session}){
  const viewport=useRef<HTMLDivElement>(null);
  const turns=session.turns||[];
  const assistant=session.source==="chatgpt"?"ChatGPT":"Codex";
  useLatestScroll(viewport,session.id);
  return <section className="card transcriptCard"><div className="sectionTitle"><div><h3>Conversation</h3><small>Redacted local transcript</small></div><span className="pill">{turns.length} prompts</span></div><nav className="checkpointNav" aria-label="Transcript prompt checkpoints">{turns.map(turn=><button key={turn.id} onClick={()=>scrollToCheckpoint(`transcript-${session.id}-${turn.id}`)}>Prompt {turn.sequence}</button>)}</nav><div ref={viewport} className="transcriptScroll scrollViewport" aria-label="Chat transcript entries" data-start-position="latest">{turns.map(turn=><article className="transcriptTurn" id={`transcript-${session.id}-${turn.id}`} key={turn.id}><div className="promptDivider"><span>Prompt {turn.sequence}</span><time>{new Date(turn.started_at).toLocaleString()}</time></div><div className="chatBubbleRow user" aria-label={`You message for prompt ${turn.sequence}`}><div className="chatAvatar" aria-hidden="true">Y</div><div className="chatBubble"><span>You</span><p>{turn.user_prompt_redacted||"Prompt content unavailable."}</p></div></div>{turn.assistant_summary_redacted&&<div className="chatBubbleRow assistant" aria-label={`${assistant} reply for prompt ${turn.sequence}`}><div className="chatAvatar" aria-hidden="true">{assistant.slice(0,1)}</div><div className="chatBubble"><span>{assistant}</span><p>{turn.assistant_summary_redacted}</p></div></div>}</article>)}{turns.length===0&&<p className="empty">No transcript turns were observed.</p>}</div></section>
}
