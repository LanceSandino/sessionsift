/** @module SupportShare @description Generates a one-session support code on this machine and tracks the connection. */
import React,{useCallback,useEffect,useState} from "react";
import {api} from "../api";
import {activeSupportFor,countdown,liveStatus,supportHeadline} from "../supportState";
import type {BridgeStatus,SupportOffer,SupportRole,SupportSession} from "../types";

export function SupportShare({sessionId,sessionTitle}:{sessionId:string;sessionTitle:string}){
  const[bridge,setBridge]=useState<BridgeStatus>();
  const[sessions,setSessions]=useState<SupportSession[]>([]);
  const[offer,setOffer]=useState<SupportOffer>();
  const[role,setRole]=useState<SupportRole>("view");
  const[minutes,setMinutes]=useState(60);
  const[now,setNow]=useState(()=>Date.now());
  const[busy,setBusy]=useState(false);
  const[error,setError]=useState("");
  const refresh=useCallback(()=>Promise.all([api.bridge(),api.supportSessions()]).then(([status,values])=>{setBridge(status);setSessions(values)}).catch(value=>setError(value instanceof Error?value.message:String(value))),[]);
  useEffect(()=>{refresh();const poll=window.setInterval(refresh,3000);const tick=window.setInterval(()=>setNow(Date.now()),1000);return()=>{window.clearInterval(poll);window.clearInterval(tick)}},[refresh]);
  const active=activeSupportFor(sessions,sessionId,now);
  const code=offer&&active&&offer.support.id===active.id?offer.code:"";
  const start=async()=>{setBusy(true);setError("");try{const value=await api.startSupport(sessionId,role,minutes);setOffer(value);await refresh()}catch(value){setError(value instanceof Error?value.message:String(value))}finally{setBusy(false)}};
  const end=async(id:string)=>{setBusy(true);setError("");try{await api.endSupport(id);setOffer(undefined);await refresh()}catch(value){setError(value instanceof Error?value.message:String(value))}finally{setBusy(false)}};
  const copy=async()=>{if(code)await navigator.clipboard?.writeText(code)};
  const status=active?liveStatus(active,now):undefined;
  return <section className="card supportCard" aria-label="Remote support">
    <div className="sectionTitle"><div><h3>Remote support</h3><small>Share this one conversation with a code</small></div><span className="pill">{bridge?.connected?"Bridge ready":"Offline"}</span></div>
    {!bridge?.connected&&<p>Connect this device to a SessionSift bridge in <a href="/remote-access">Remote access</a> to generate support codes.</p>}
    {bridge?.connected&&!active&&<>
      <p>Your supporter enters the code at <code>{bridge.url}/support</code> and sees only <b>{sessionTitle}</b>. Nothing else on this machine is reachable.</p>
      <div className="supportForm">
        <label>Access<select aria-label="Support access" value={role} onChange={event=>setRole(event.target.value as SupportRole)}><option value="view">View only</option><option value="comment">View and comment</option><option value="review">View, comment and review</option></select></label>
        <label>Lasts<select aria-label="Support duration" value={minutes} onChange={event=>setMinutes(Number(event.target.value))}><option value={15}>15 minutes</option><option value={60}>1 hour</option><option value={240}>4 hours</option><option value={480}>8 hours</option></select></label>
        <button className="primaryButton" disabled={busy} onClick={start}>{busy?"Generating…":"Generate support code"}</button>
      </div>
    </>}
    {active&&<div className={`supportActive ${status}`} role="status">
      {status==="waiting"&&<>{code?<div className="supportCode"><b aria-label="Support code">{code}</b><button onClick={copy}>Copy</button></div>:<p className="supportCodeHidden">The code was shown when it was created. End this code and generate a new one if you need it again.</p>}<small>Expires in {countdown(active.code_expires_at,now)} · works once · enter at <code>{bridge?.url}/support</code></small></>}
      <strong>{supportHeadline(active,now)}</strong>
      {status==="connected"&&<small>{active.role} access · ends in {countdown(active.expires_at,now)}{active.last_activity_at?` · last request ${new Date(active.last_activity_at).toLocaleTimeString()}`:""}</small>}
      <button className="dangerButton" disabled={busy} onClick={()=>end(active.id)}>{status==="connected"?"Disconnect supporter":"Cancel code"}</button>
    </div>}
    {error&&<small className="dangerText">{error}</small>}
  </section>;
}
