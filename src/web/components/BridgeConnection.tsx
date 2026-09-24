/** @module BridgeConnection @description Connects this device to a bridge, links it to an owner with a code it shows, and lists its support sessions. */
import React,{useCallback,useEffect,useState} from "react";
import {api} from "../api";
import {countdown,liveStatus,supportHeadline} from "../supportState";
import type {BridgeStatus,LinkOffer,SupportSession} from "../types";
import {Icon} from "./Icon";

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
  const connect=async(event:React.FormEvent)=>{event.preventDefault();setBusy(true);setError("");try{setBridge(await api.connectBridge(url,name))}catch(value){report(value)}finally{setBusy(false)}};
  const startLink=async()=>{setBusy(true);setError("");try{setLink(await api.startLink())}catch(value){report(value)}finally{setBusy(false)}};
  const end=async(id:string)=>{setError("");try{await api.endSupport(id);await refresh()}catch(value){report(value)}};
  const live=support.filter(value=>["waiting","connected"].includes(liveStatus(value,now)));
  return <section className="device-card" aria-label="Bridge connection">
    <div className="row between"><h3><Icon name="panel"/>This device</h3><span className={`badge ${bridge?.connected?"good":""}`}>{bridge?.connected?"Connected":"Not connected"}</span></div>
    <p>The local agent owns every transcript. A bridge relays one shared conversation at a time; no account is needed to hand out support codes.</p>
    <div className="data-row"><span className="key">Device ID</span><span className="value mono">{bridge?.device_id||"Loading…"}</span></div>
    {bridge?.connected?<>
      <div className="data-row"><span className="key">Bridge</span><span className="value">{bridge.url}</span></div>
      <div className="data-row"><span className="key">Device name</span><span className="value">{bridge.name||"This device"}</span></div>
      <div className="data-row"><span className="key">Account</span><span className="value">{bridge.owner||"Not linked"}</span></div>
    </>:<form className="form-grid" onSubmit={connect}>
      <label>Bridge URL<input className="field" aria-label="Bridge URL" type="url" value={url} placeholder="https://sessionsift.example.com" onChange={event=>setUrl(event.target.value)}/></label>
      <label>Device name<input className="field" aria-label="Device name" value={name} placeholder="Optional" onChange={event=>setName(event.target.value)}/></label>
      <button type="submit" className="button primary" disabled={busy||!url.trim()}>{busy?"Connecting…":"Connect this device"}</button>
    </form>}
    {bridge?.connected&&!bridge.owner&&<div className="panel-subhead">
      {link?<div role="status"><div className="code-display" aria-label="Device link code">{link.code}</div><p className="card-copy">Enter this under <b>Add a device</b> at <code>{link.link_url}</code> · expires in {countdown(link.expires_at,now)}. Account linking is separate from support access.</p></div>
        :<><p className="card-copy">Link this device to your account to use ChatGPT or Claude MCP, email invitations and the hosted session list.</p><button type="button" className="button" disabled={busy} onClick={startLink}><Icon name="link"/>Link to account</button></>}
    </div>}
    {bridge?.connected&&<div className="panel-subhead" aria-label="Support sessions">
      <h4>Support sessions</h4>
      {live.length===0?<p className="list-empty">No active support codes. Open a conversation and choose <b>Support</b>.</p>:live.map(value=><div className="list-row" key={value.id}><div><b>{value.session_title||value.session_id}</b><small>{supportHeadline(value,now)} · {value.role}</small></div><button type="button" className="button danger" onClick={()=>end(value.id)}>{liveStatus(value,now)==="connected"?"Disconnect":"Cancel"}</button></div>)}
    </div>}
    {error&&<p className="form-error" role="alert">{error}</p>}
  </section>;
}
