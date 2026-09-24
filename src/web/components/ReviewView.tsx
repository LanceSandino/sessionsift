/** @module ReviewView @description The review lens: an explicit reviewer decision and the session's notes, kept beside the transcript rather than in it. */
import React,{useState} from "react";
import {shortDateTime} from "../format";
import {currentDecision,decisionLabel,newestFirst} from "../reviewState";
import type {Session} from "../types";
import type {Collaboration} from "../useCollaboration";
import {Icon} from "./Icon";
import {NoteList} from "./NoteList";

const choices=[{status:"reviewed",label:"Mark reviewed",icon:"circle-check" as const,fallback:"Marked reviewed."},{status:"changes_requested",label:"Needs follow-up",icon:"note" as const,fallback:"Follow-up requested."}];

export function ReviewView({session,collaboration,onToast}:{session:Session;collaboration:Collaboration;onToast:(text:string)=>void}){
  const[summary,setSummary]=useState("");
  const[busy,setBusy]=useState(false);
  const decision=currentDecision(collaboration.reviews);
  const metrics=session.metrics;
  const record=async(status:string,fallback:string)=>{
    setBusy(true);
    try{await collaboration.recordDecision(status,summary.trim()||fallback);setSummary("");onToast("Decision recorded on this device. The transcript is unchanged.")}
    catch{/* The hook reports the error inline. */}
    finally{setBusy(false)}
  };
  const history=newestFirst(collaboration.reviews);
  return <div className="content-page" aria-label="Review">
    <div className="page-intro"><h2>A review, not another chat.</h2><p>Your annotations and decisions sit alongside the original session. They never rewrite it, and nothing here is sent to the source agent.</p></div>
    <section className="review-decision" aria-label="Review decision">
      <div className="row between"><h3>{decision.kind==="pending"?"Awaiting your review":decision.label}</h3><span className={`badge ${decision.kind==="reviewed"?"good":decision.kind==="followup"?"warn":""}`}>{decision.kind==="pending"?"No decision":decision.label}</span></div>
      <p>{session.turns?.length||0} prompts, {metrics?.tool_calls??session.tool_calls?.length??0} tool calls{metrics?.failed_tool_calls?`, ${metrics.failed_tool_calls} failed`:""}. Review the evidence before recording a decision. “Reviewed” is your judgment, not proof the work is correct.</p>
      <label className="field-label" htmlFor="decision-note">Decision note <span className="muted">(optional)</span></label>
      <textarea id="decision-note" className="field" maxLength={4000} value={summary} placeholder="What did you check, and what still needs attention?" onChange={event=>setSummary(event.target.value)}/>
      <div className="row">{choices.map(choice=><button type="button" key={choice.status} className={`button ${decision.review?.status===choice.status?"primary":""}`} disabled={busy} onClick={()=>record(choice.status,choice.fallback)}><Icon name={choice.icon}/>{choice.label}</button>)}</div>
      {collaboration.error&&<p className="form-error" role="alert">{collaboration.error}</p>}
      <p className="fine-print muted">Decisions are appended on this device; the newest one is the session’s current state.</p>
    </section>
    {history.length>0&&<><div className="section-head"><h3>Decision history</h3><small>{history.length} recorded</small></div>
      <div className="decision-list">{history.map(review=><article className="note-card" key={review.id}><div className="row"><span className="avatar" aria-hidden="true">{review.reviewer.slice(0,1).toUpperCase()}</span><strong>{review.reviewer}</strong><span className="muted">· {decisionLabel(review.status)}</span><small className="muted note-time">{shortDateTime(review.created_at)}</small></div><p>{review.summary}</p></article>)}</div></>}
    <div className="section-head"><h3>Review notes</h3><small>{collaboration.annotations.length} annotation{collaboration.annotations.length===1?"":"s"}</small></div>
    <NoteList annotations={collaboration.annotations} turns={session.turns||[]}/>
  </div>;
}
