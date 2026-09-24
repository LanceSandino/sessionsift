/** @module BridgeConnection @description Connects this device to a bridge, links it to an owner with a code it shows, and lists support sessions. */
import React,{useCallback,useEffect,useState} from "react";
import {api} from "../api";
import {countdown,liveStatus,supportHeadline} from "../supportState";
import type {BridgeStatus,LinkOffer,SupportSession} from "../types";

export function BridgeConnection(){
  const[bridge,setBridge]=useState<BridgeStatus>();
  const[support,setSupport]=useState<SupportSession[]>([]);
  const[url,setUrl]=useState("");
  const[name,setName]=useState("");
  const[link,setLink]=useState<LinkOffer>();
  const[now,setNow]=useState(()=>Date.now());
  const[busy,setBusy]=useState(false);
  const[error,setError]=useState("");
  const report=(value:unknown)=>setError(value instanceof Error?value.message:String(value));
  const refresh=useCallback(async()=>{try{const[status,values]=await Promise.all([api.bridge(),api.supportSessions()]);setBridge(status);setSupport(values)}catch(value){report(value)}},[]);
  useEffect(()=>{refresh();const poll=window.setInterval(refresh,3000);const tick=window.setInterval(()=>setNow(Date.now()),1000);return()=>{window.clearInterval(poll);window.clearInterval(tick)}},[refresh]);
  useEffect(()=>{if(!link||bridge?.owner)return;const timer=window.setInterval(()=>{api.refreshBridge().then(status=>{setBridge(status);if(status.owner)setLink(undefined)}).catch(report)},2500);return()=>window.clearInterval(timer)},[link,bridge?.owner]);
  const connect=async()=>{setBusy(true);setError("");try{setBridge(await api.connectBridge(url,name))}catch(value){report(value)}finally{setBusy(false)}};
  const startLink=async()=>{setBusy(true);setError("");try{setLink(await api.startLink())}catch(value){report(value)}finally{setBusy(false)}};
  const end=async(id:string)=>{setError("");try{await api.endSupport(id);await refresh()}catch(value){report(value)}};
  const live=support.filter(value=>["waiting","connected"].includes(liveStatus(value,now)));
  return <section className="card bridgeCard" aria-label="Bridge connection">
    <div className="sectionTitle"><div><h3>This device</h3><small>{bridge?.device_id||"Loading…"}</small></div><span className={`pill ${bridge?.connected?"ready":""}`}>{bridge?.connected?"Connected":"Not connected"}</span></div>
    {bridge?.connected?<div className="bridgeFacts">
      <div><small>Bridge</small><b>{bridge.url}</b></div>
      <div><small>Name</small><b>{bridge.name||"This device"}</b></div>
      <div><small>Owner</small><b>{bridge.owner||"Not linked"}</b></div>
    </div>:<div className="bridgeForm">
      <p>Connect once to a SessionSift bridge. No account is needed: this device can then generate support codes that connect one person to one conversation.</p>
      <label>Bridge URL<input aria-label="Bridge URL" type="url" value={url} placeholder="https://sessionsift.example.com" onChange={event=>setUrl(event.target.value)}/></label>
      <label>Device name<input aria-label="Device name" value={name} placeholder="Optional" onChange={event=>setName(event.target.value)}/></label>
      <button className="primaryButton" disabled={busy||!url.trim()} onClick={connect}>{busy?"Connecting…":"Connect this device"}</button>
    </div>}
    {bridge?.connected&&!bridge.owner&&<div className="linkBlock">
      {link?<div className="supportCode" role="status"><b aria-label="Device link code">{link.code}</b><small>Enter this under <b>Add a device</b> at <code>{link.link_url}</code> · expires in {countdown(link.expires_at,now)}</small></div>
        :<><p>Link this device to your account to use ChatGPT or Claude MCP, email invitations, and the hosted session list.</p><button disabled={busy} onClick={startLink}>Link to account</button></>}
    </div>}
    {bridge?.connected&&<div className="supportList" aria-label="Support sessions">
      <h4>Support sessions</h4>
      {live.length===0?<p className="empty compactEmpty">No active support codes. Open a conversation and choose <b>Generate support code</b>.</p>:live.map(value=><div className="remoteRow" key={value.id}><div><b>{value.session_title||value.session_id}</b><small>{supportHeadline(value,now)} · {value.role}</small></div><button onClick={()=>end(value.id)}>{liveStatus(value,now)==="connected"?"Disconnect":"Cancel"}</button></div>)}
    </div>}
    {error&&<small className="dangerText">{error}</small>}
  </section>;
}
