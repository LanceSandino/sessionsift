/** @module SupportShare @description Generates a one-session support code on this machine and tracks the code, the connection and its end. */
import React,{useCallback,useEffect,useState} from "react";
import {api} from "../api";
import {activeSupportFor,countdown,liveStatus,supportHeadline} from "../supportState";
import type {BridgeStatus,SupportOffer,SupportRole,SupportSession} from "../types";
import {Icon} from "./Icon";

const roles:{id:SupportRole;name:string;description:string}[]=[
  {id:"view",name:"View",description:"Read the session"},
  {id:"comment",name:"Comment",description:"Read + add notes"},
  {id:"review",name:"Review",description:"Notes + decisions"},
];

type Props={sessionId:string;sessionTitle:string;summary?:React.ReactNode;onOpenConnections?:()=>void};

// Codes this page issued, by support ID. Memory only: a reload, or any other view, never sees them.
const issuedCodes=new Map<string,string>();

export function SupportShare({sessionId,sessionTitle,summary,onOpenConnections}:Props){
  const[bridge,setBridge]=useState<BridgeStatus>();
  const[sessions,setSessions]=useState<SupportSession[]>([]);
  const[offer,setOffer]=useState<SupportOffer>();
  const[role,setRole]=useState<SupportRole>("view");
  const[minutes,setMinutes]=useState(60);
  const[now,setNow]=useState(()=>Date.now());
  const[busy,setBusy]=useState(false);
  const[copied,setCopied]=useState(false);
  const[error,setError]=useState("");
  const report=(value:unknown)=>setError(value instanceof Error?value.message:String(value));
  const refresh=useCallback(()=>Promise.all([api.bridge(),api.supportSessions()]).then(([status,values])=>{setBridge(status);setSessions(values)}).catch(report),[]);
  useEffect(()=>{refresh();const poll=window.setInterval(refresh,3000);const tick=window.setInterval(()=>setNow(Date.now()),1000);return()=>{window.clearInterval(poll);window.clearInterval(tick)}},[refresh]);
  const active=activeSupportFor(sessions,sessionId,now);
  const code=active?(offer&&offer.support.id===active.id?offer.code:issuedCodes.get(active.id)||""):"";
  const start=async(event:React.FormEvent)=>{event.preventDefault();setBusy(true);setError("");try{const value=await api.startSupport(sessionId,role,minutes);issuedCodes.set(value.support.id,value.code);setOffer(value);await refresh()}catch(value){report(value)}finally{setBusy(false)}};
  const end=async(id:string)=>{setBusy(true);setError("");try{await api.endSupport(id);issuedCodes.delete(id);setOffer(undefined);await refresh()}catch(value){report(value)}finally{setBusy(false)}};
  const copy=async()=>{if(!code)return;try{await navigator.clipboard.writeText(code);setCopied(true)}catch{report(new Error("Clipboard unavailable. Select and copy the displayed code."))}};
  const status=active?liveStatus(active,now):undefined;
  const entry=bridge?.url?`${bridge.url}/support`:"the bridge’s /support page";
  return <section aria-label="Remote support">
    {!bridge&&!error&&<p className="dialog-copy">Checking this device’s bridge connection…</p>}
    {bridge&&!bridge.connected&&<>
      <p className="dialog-copy">Connect this device to a SessionSift bridge in Connections to generate support codes. The bridge relays this one conversation; nothing else on this machine becomes reachable.</p>
      {onOpenConnections&&<div className="dialog-footer"><button type="button" className="button primary" onClick={onOpenConnections}>Open Connections <Icon name="arrow"/></button></div>}
    </>}
    {bridge?.connected&&!active&&<>
      <p className="dialog-copy">A short-lived code gives a helper access to this session only. Your library, device and other conversations stay out of their view.</p>
      <div className="dialog-session">{summary}<div><strong>{sessionTitle}</strong><small>One session · live while access lasts</small></div></div>
      <form onSubmit={start}>
        <fieldset className="permission-options"><legend className="field-label">What can the helper do?</legend>
          {roles.map(item=><label key={item.id}><input type="radio" name="support-role" value={item.id} checked={role===item.id} onChange={()=>setRole(item.id)}/><strong>{item.name}</strong><small>{item.description}</small></label>)}
        </fieldset>
        <label className="field-label" htmlFor="support-duration">Access duration after redemption</label>
        <select id="support-duration" className="field" aria-label="Support duration" value={minutes} onChange={event=>setMinutes(Number(event.target.value))}><option value={15}>15 minutes</option><option value={60}>1 hour</option><option value={240}>4 hours</option><option value={480}>8 hours</option></select>
        <div className="privacy-line"><Icon name="lock"/><span>The code works once and expires after 10 minutes. Your helper signs in and enters it at <code>{entry}</code>.</span></div>
        <div className="dialog-footer"><button type="submit" className="button primary" disabled={busy}>{busy?"Generating…":"Generate support code"}<Icon name="arrow"/></button></div>
      </form>
    </>}
    {active&&status==="waiting"&&<>
      {code?<><div className="support-code" aria-label="Support code">{code}</div>
        <div className="code-expiry"><span>Expires in <strong>{countdown(active.code_expires_at,now)}</strong> · single use</span><button type="button" className="text-button" onClick={copy}><Icon name="copy"/>{copied?"Copied":"Copy"}</button></div></>
        :<p className="dialog-copy">The code was shown when it was created. End this code and generate a new one if you need it again. It expires in {countdown(active.code_expires_at,now)}.</p>}
      <div className="dialog-session"><Icon name="users"/><div><strong>{supportHeadline(active,now)}</strong><small>{active.role} access · {active.access_minutes} minutes after redemption · one conversation</small></div></div>
      <div className="privacy-line"><Icon name="shield"/><span>Your helper enters it at <code>{entry}</code>. The code appears only in the view that created it; cancelling takes effect on this device first.</span></div>
      <div className="dialog-footer"><button type="button" className="button danger" disabled={busy} onClick={()=>end(active.id)}>Cancel code</button></div>
    </>}
    {active&&status==="connected"&&<>
      <div className="support-state connected" role="status"><strong>{supportHeadline(active,now)}</strong><small>{active.role} access · ends in {countdown(active.expires_at,now)}{active.last_activity_at?` · last request ${new Date(active.last_activity_at).toLocaleTimeString()}`:""}</small></div>
      <div className="privacy-line"><Icon name="shield"/><span>They see only this conversation and the controls their access allows. Disconnecting revokes access on this device first.</span></div>
      <div className="dialog-footer"><button type="button" className="button danger" disabled={busy} onClick={()=>end(active.id)}>Disconnect supporter</button></div>
    </>}
    {error&&<p className="form-error" role="alert">{error}</p>}
  </section>;
}
