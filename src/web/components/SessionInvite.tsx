/** @module SessionInvite @description Creates a hosted, email-bound invitation for exactly one session. */
import React,{useEffect,useState} from "react";
import {api} from "../api";
import type {HostedInvitation,Session} from "../types";
import {SharingPreview} from "./SharingPreview";

export function SessionInvite({sessionId,sessionTitle,session}:{sessionId:string;sessionTitle:string;session?:Session}){
  const[reviewed,setReviewed]=useState(false);
  const[email,setEmail]=useState("");
  const[role,setRole]=useState<"view"|"comment"|"review">("comment");
  const[hours,setHours]=useState(24);
  const[invitation,setInvitation]=useState<HostedInvitation>();
  const[invitations,setInvitations]=useState<HostedInvitation[]>([]);
  const[busy,setBusy]=useState(false);
  const[revoking,setRevoking]=useState("");
  const[error,setError]=useState("");
  useEffect(()=>{let current=true;api.invitations().then(values=>{if(current)setInvitations(values.filter(value=>value.session_id===sessionId))}).catch(value=>{if(current)setError(value instanceof Error?value.message:String(value))});return()=>{current=false}},[sessionId]);
  const create=async()=>{setBusy(true);setError("");try{const value=await api.createInvitation({invitee:email,session_id:sessionId,role,duration_hours:hours});setInvitation(value);setInvitations(previous=>[value,...previous.filter(item=>item.id!==value.id)])}catch(value){setError(value instanceof Error?value.message:String(value))}finally{setBusy(false)}};
  const copy=async()=>{if(invitation?.accept_url)await navigator.clipboard.writeText(invitation.accept_url)};
  const revoke=async(id:string)=>{setRevoking(id);setError("");try{await api.revokeInvitation(id);setInvitations(previous=>previous.map(value=>value.id===id?{...value,revoked_at:new Date().toISOString()}:value))}catch(value){setError(value instanceof Error?value.message:String(value))}finally{setRevoking("")}};
  return <section className="card inviteCard">
    <div className="sectionTitle"><div><h3>Invite to this session</h3><small>Email-bound · one session only</small></div><span className="pill">Hosted</span></div>
    <p>Share <b>{sessionTitle}</b> without exposing your session list, repository, shell, or other private data.</p>
    {session&&<><SharingPreview session={session}/><label className="shareConfirm"><input type="checkbox" checked={reviewed} onChange={event=>setReviewed(event.target.checked)}/>I reviewed the content and understand that new session content is also shared.</label></>}
    <div className="inviteForm">
      <label>Email<input type="email" aria-label="Invitee email" value={email} onChange={event=>setEmail(event.target.value)} placeholder="reviewer@example.com"/></label>
      <label>Access<select aria-label="Invitation role" value={role} onChange={event=>setRole(event.target.value as typeof role)}><option value="view">View</option><option value="comment">Comment</option><option value="review">Review</option></select></label>
      <label>Expires<select aria-label="Invitation expiry" value={hours} onChange={event=>setHours(Number(event.target.value))}><option value={1}>1 hour</option><option value={24}>24 hours</option><option value={168}>7 days</option></select></label>
      <button className="primaryButton" disabled={busy||!email.includes("@")||!!session&&!reviewed} onClick={create}>{busy?"Creating…":"Create private link"}</button>
    </div>
    {error&&<small className="dangerText">{error}</small>}
    {invitation?.accept_url&&<div className="inviteResult"><div><b>Link ready for {invitation.invitee}</b><small>Single-use acceptance; access ends {new Date(invitation.expires_at).toLocaleString()}.</small></div><button onClick={copy}>Copy link</button><code>{invitation.accept_url}</code></div>}
    <div className="inviteList" aria-label="Invitations for this session">
      {invitations.length===0?<small>No invitations for this session.</small>:invitations.map(value=>{const active=!value.revoked_at&&new Date(value.expires_at)>new Date();return <div className="inviteRow" key={value.id}><div><b>{value.invitee}</b><small>{value.last_viewed_at?`Last requested ${new Date(value.last_viewed_at).toLocaleString()} · `:""}{value.role} · {value.accepted_at?"accepted":"not accepted"} · {active?`expires ${new Date(value.expires_at).toLocaleString()}`:value.revoked_at?"revoked":"expired"}</small></div>{active&&<button aria-label={`Revoke invitation for ${value.invitee}`} disabled={revoking===value.id} onClick={()=>revoke(value.id)}>{revoking===value.id?"Revoking…":"Revoke"}</button>}</div>})}
    </div>
  </section>;
}
