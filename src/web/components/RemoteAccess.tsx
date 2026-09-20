/** @module RemoteAccess @description Gives the owner explicit pairing, OAuth, audit, and kill-switch controls. */
import React,{useCallback,useEffect,useState} from "react";
import {api} from "../api";
import type {RemotePairing,RemoteStatus} from "../types";

export function RemoteAccess(){
  const[status,setStatus]=useState<RemoteStatus>();
  const[pairing,setPairing]=useState<RemotePairing>();
  const[token,setToken]=useState("");
  const[principal,setPrincipal]=useState("");
  const[error,setError]=useState("");
  const refresh=useCallback(()=>api.remoteStatus().then(setStatus).catch(value=>setError(String(value))),[]);
  useEffect(()=>{refresh();const timer=window.setInterval(refresh,3000);return()=>window.clearInterval(timer)},[refresh]);
  const create=()=>api.createPairing().then(value=>{setPairing(value);setToken("");return refresh()}).catch(value=>setError(String(value)));
  const approve=(code:string)=>api.approvePairing(code).then(value=>{setToken(value.token);return refresh()}).catch(value=>setError(String(value)));
  const deny=(code:string)=>api.denyPairing(code).then(refresh).catch(value=>setError(String(value)));
  const disconnect=(connectionId:string,grantId:string)=>api.disconnect(connectionId).then(()=>api.revokeGrant(grantId)).then(refresh).catch(value=>setError(String(value)));
  const approveOAuth=()=>api.approveOAuthPrincipal(principal).then(()=>{setPrincipal("");return refresh()}).catch(value=>setError(String(value)));
  const oauthGrants=status?.oauth_grants||[];
  return <main className="remoteView">
    <header><span className="eyebrow">OWNER CONTROL</span><h1>Remote access</h1><p>Pair reviewers or approve a ChatGPT identity with explicit capabilities. Local authorization and the kill switch always win.</p></header>
    {error&&<div className="warningBanner">{error}</div>}
    <section className="card oauthCard">
      <div className="oauthIntro">
        <div className={`oauthState ${status?.oauth_configured?"ready":"pending"}`}>{status?.oauth_configured?"Origin verifier ready":"Origin config needed"}</div>
        <div><h3>ChatGPT MCP</h3><p>Cloudflare Managed OAuth must authenticate the person; this device independently approves which identity can read SessionSift.</p></div><code>/mcp</code>
      </div>
      <div className="oauthForm"><label>Approved Cloudflare identity<input aria-label="OAuth identity email" type="email" value={principal} placeholder="you@example.com" onChange={event=>setPrincipal(event.target.value)}/></label><button className="primaryButton" disabled={!principal.trim()} onClick={approveOAuth}>Approve ChatGPT access</button></div>
      {oauthGrants.length?<div className="oauthGrants">{oauthGrants.map(grant=><div className="remoteRow" key={grant.id}><div><b>{grant.principal||grant.reviewer}</b><small>{grant.operations.length} read capabilities · expires {new Date(grant.expires_at).toLocaleString()}</small></div><button aria-label={`Revoke ${grant.principal||grant.reviewer}`} onClick={()=>api.revokeGrant(grant.id).then(refresh).catch(value=>setError(String(value)))}>Revoke</button></div>)}</div>:<p className="empty compactEmpty">No ChatGPT identity is approved.</p>}
    </section>
    <section className="card remoteHero"><div><h3>Share SessionSift</h3><p>Codes are single-use and expire after ten minutes.</p></div><button className="primaryButton" onClick={create}>Create pairing code</button>{pairing&&<div className="pairingCode"><b>{pairing.code}</b><small>Expires {new Date(pairing.expires_at).toLocaleTimeString()}</small></div>}{token&&<div className="grantToken"><b>Approved 30-minute grant</b><code>{token}</code><small>Share this scoped credential with the approved reviewer. It is shown once.</small></div>}</section>
    <div className="remoteGrid">
      <section className="card"><div className="sectionTitle"><h3>Approval requests</h3><span className="pill">{status?.pending_pairings.length||0}</span></div>{status?.pending_pairings.map(value=><div className="remoteRow" key={value.code}><div><b>{value.reviewer}</b><small>{value.requested?.join(" · ")}</small></div><div className="rowActions"><button onClick={()=>deny(value.code)}>Deny</button><button onClick={()=>approve(value.code)}>Allow 30m</button></div></div>)}{!status?.pending_pairings.length&&<p className="empty">No approval is waiting.</p>}</section>
      <section className="card"><div className="sectionTitle"><h3>Connections</h3><span className="pill">kill switch</span></div>{status?.connections.map(value=><div className="remoteRow" key={value.id}><div><b>{value.reviewer}</b><small>{value.active?`Connected ${new Date(value.connected_at).toLocaleString()}`:"Disconnected"}</small></div>{value.active&&<button onClick={()=>disconnect(value.id,value.grant_id)}>Disconnect & revoke</button>}</div>)}{!status?.connections.length&&<p className="empty">No remote connections.</p>}</section>
    </div>
    <section className="card auditPanel"><div className="sectionTitle"><h3>Remote audit</h3><span className="pill">metadata only</span></div>{status?.audit.slice().reverse().map((value,index)=><div className="auditRow" key={`${value.timestamp}-${index}`}><time>{new Date(value.timestamp).toLocaleString()}</time><b>{value.client_id||"unknown"}</b><code>{value.operation}</code><span className={value.allowed?"allowed":"denied"}>{value.status}</span></div>)}{!status?.audit.length&&<p className="empty">No remote operations yet.</p>}</section>
  </main>
}
