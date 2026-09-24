/** @module RemoteReview @description Lets an approved reviewer inspect another workstation's sessions through a scoped, memory-only grant. */
import React,{useState} from "react";
import {api} from "../api";
import {shortDateTime} from "../format";
import {sourceInfo} from "../sources";
import type {Session} from "../types";
import {Icon} from "./Icon";
import {RichContent} from "./RichContent";

type SessionPage={sessions:Session[];total:number;returned:number;offset:number;has_more:boolean;next_cursor?:number};

export function RemoteReview(){
  const[code,setCode]=useState("");const[name,setName]=useState("");const[token,setToken]=useState("");const[sessions,setSessions]=useState<Session[]>([]);const[selected,setSelected]=useState<Session>();const[message,setMessage]=useState("");const[error,setError]=useState("");
  const claim=(event:React.FormEvent)=>{event.preventDefault();api.claimPairing(code,name).then(()=>{setMessage("Request sent. The workstation owner must approve it and share the one-time grant token.");setError("")}).catch(value=>setError(String(value)))};
  const load=async(event:React.FormEvent)=>{event.preventDefault();try{let cursor=0;const all:Session[]=[];do{const response=await api.remoteProtocol<SessionPage>(token,"session.list",{cursor,limit:100});all.push(...response.data.sessions);if(!response.data.has_more)break;cursor=response.data.next_cursor||0}while(true);setSessions(all);setSelected(undefined);setError("")}catch(value){setError(String(value))}};
  const inspect=(summary:Session)=>api.remoteProtocol<Session>(token,"session.get",{session_id:summary.id}).then(response=>setSelected(response.data)).catch(value=>setError(String(value)));
  const assistant=sourceInfo(selected).assistant;
  return <section className="device-card" aria-label="Reviewer portal">
    <h3><Icon name="users"/>Reviewer portal</h3>
    <p>This portal is for a reviewer using a manual pairing code and an owner-approved token; it is not the normal invitation experience. If you received an email invitation link, open that link directly. If you were given a support code, enter it on the bridge’s support page above. The workstation stays private, and the grant token lives only in this page’s memory.</p>
    {error&&<div className="evidence-banner" role="alert">{error}</div>}
    <div className="two-up">
      <form onSubmit={claim}><h4 className="panel-subhead">1. Request access</h4><div className="form-grid"><label>Pairing code<input className="field" value={code} onChange={event=>setCode(event.target.value)} placeholder="123-456"/></label><label>Reviewer name<input className="field" value={name} onChange={event=>setName(event.target.value)} placeholder="Your name"/></label></div><button type="submit" className="button primary">Send request</button>{message&&<p className="card-copy">{message}</p>}</form>
      <form onSubmit={load}><h4 className="panel-subhead">2. Use the approved grant</h4><div className="form-grid"><label>Scoped token<input className="field" type="password" value={token} onChange={event=>setToken(event.target.value)} placeholder="Paste the one-time token"/></label></div><button type="submit" className="button primary">Load approved sessions</button></form>
    </div>
    {sessions.length>0&&<div className="reviewer-sessions" aria-label="Approved sessions">{sessions.map(session=><button type="button" key={session.id} onClick={()=>inspect(session)}><b>{session.title}</b><small>{shortDateTime(session.started_at)} · {sourceInfo(session).label}</small></button>)}</div>}
    {selected&&<div className="reviewer-detail"><header><h3>{selected.title}</h3><p className="small muted">{shortDateTime(selected.started_at)} · {selected.runtime_location||"local"} · {selected.surface||"unknown surface"}</p></header>
      <div className="transcript">{(selected.turns||[]).map(turn=><section key={turn.id} aria-label={`Prompt ${turn.sequence}`}><div className="checkpoint"><span className="number">{String(turn.sequence).padStart(2,"0")}</span><span>Prompt {turn.sequence}</span></div><div className="prompt"><div className="speaker">Owner</div><div className="user-message"><RichContent text={turn.user_prompt_redacted||"Prompt content unavailable."}/></div></div>{turn.assistant_summary_redacted&&<section className="assistant"><div className="assistant-label"><strong>{assistant}</strong></div><div className="assistant-body"><RichContent text={turn.assistant_summary_redacted}/></div></section>}</section>)}{!selected.turns?.length&&<p className="transcript-empty">No transcript turns were shared.</p>}</div>
    </div>}
  </section>;
}
