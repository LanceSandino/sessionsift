/** @module RemoteAccess @description Owner controls for MCP identities, local pairing, live connections with a kill switch, and the metadata-only audit. */
import React,{useCallback,useEffect,useState} from "react";
import {api} from "../api";
import type {RemotePairing,RemoteStatus} from "../types";
import {Icon} from "./Icon";

export function RemoteAccess(){
  const[status,setStatus]=useState<RemoteStatus>();
  const[pairing,setPairing]=useState<RemotePairing>();
  const[token,setToken]=useState("");
  const[principal,setPrincipal]=useState("");
  const[detailed,setDetailed]=useState(false);
  const[error,setError]=useState("");
  const report=(value:unknown)=>setError(value instanceof Error?value.message:String(value));
  const refresh=useCallback(()=>api.remoteStatus().then(setStatus).catch(report),[]);
  useEffect(()=>{refresh();const timer=window.setInterval(refresh,3000);return()=>window.clearInterval(timer)},[refresh]);
  const create=()=>api.createPairing().then(value=>{setPairing(value);setToken("");return refresh()}).catch(report);
  const approve=(code:string)=>api.approvePairing(code).then(value=>{setToken(value.token);return refresh()}).catch(report);
  const deny=(code:string)=>api.denyPairing(code).then(refresh).catch(report);
  const disconnect=(connectionId:string,grantId:string)=>api.disconnect(connectionId).then(()=>api.revokeGrant(grantId)).then(refresh).catch(report);
  const approveOAuth=(event:React.FormEvent)=>{event.preventDefault();api.approveOAuthPrincipal(principal,detailed).then(()=>{setPrincipal("");setDetailed(false);return refresh()}).catch(report)};
  const oauthGrants=status?.oauth_grants||[];
  return <>
    {error&&<div className="evidence-banner" role="alert">{error}</div>}
    <section className="device-card" aria-label="ChatGPT and Claude MCP">
      <div className="row between"><h3><Icon name="shield"/>ChatGPT and Claude MCP</h3><span className={`badge ${status?.oauth_configured?"good":"warn"}`}>{status?.oauth_configured?"Origin verifier ready":"Origin config needed"}</span></div>
      <p>Cloudflare Managed OAuth authenticates the person at <code>/mcp</code>; this device independently approves which identity can read SessionSift. A support grant never becomes an MCP route.</p>
      <form className="form-grid" onSubmit={approveOAuth}><label>Approved Cloudflare identity<input className="field" aria-label="OAuth identity email" type="email" value={principal} placeholder="you@example.com" onChange={event=>setPrincipal(event.target.value)}/></label><label><input type="checkbox" checked={detailed} onChange={event=>setDetailed(event.target.checked)}/> Also allow session bodies, timelines, findings, comparisons, and prompt/tool excerpts</label><small>{detailed?"Detailed access is explicitly enabled for this identity.":"Default access is limited to bounded lists and aggregate analysis."}</small><button type="submit" className="button primary" disabled={!principal.trim()}>Approve MCP access</button></form>
      {oauthGrants.length?oauthGrants.map(grant=><div className="list-row" key={grant.id}><div><b>{grant.principal||grant.reviewer}</b><small>{grant.operations.length} read capabilities · expires {new Date(grant.expires_at).toLocaleString()}</small></div><button type="button" className="button danger" aria-label={`Revoke ${grant.principal||grant.reviewer}`} onClick={()=>api.revokeGrant(grant.id).then(refresh).catch(report)}>Revoke</button></div>):<p className="list-empty">No MCP identity is approved.</p>}
    </section>
    <section className="device-card" aria-label="Local pairing">
      <div className="row between"><h3><Icon name="users"/>Manual pairing (advanced)</h3><button type="button" className="button" onClick={create}>Create pairing code</button></div>
      <p>Email invitations and support codes are the normal way to share one session. Use pairing only when a reviewer on this network or tunnel needs the manual Reviewer Portal flow. Codes are single-use and expire after ten minutes; you approve each claim, and the kill switch always wins.</p>
      {pairing&&<div role="status"><div className="code-display">{pairing.code}</div><p className="card-copy">The reviewer enters this in the Reviewer Portal · expires {new Date(pairing.expires_at).toLocaleTimeString()}.</p></div>}
      {token&&<div className="support-state connected" role="status"><strong>Approved 30-minute grant</strong><code className="secret-box">{token}</code><small>Send this to the same reviewer after approval. It is held only in their browser memory and shown once here.</small></div>}
      <div className="two-up">
        <div><h4 className="panel-subhead">Approval requests <span className="muted">{status?.pending_pairings.length||0}</span></h4>{status?.pending_pairings.map(value=><div className="list-row" key={value.code}><div><b>{value.reviewer}</b><small>{value.requested?.join(" · ")}</small></div><div className="row"><button type="button" className="button" onClick={()=>deny(value.code)}>Deny</button><button type="button" className="button primary" onClick={()=>approve(value.code)}>Allow 30m</button></div></div>)}{!status?.pending_pairings.length&&<p className="list-empty">No approval is waiting.</p>}</div>
        <div><h4 className="panel-subhead">Connections <span className="muted">kill switch</span></h4>{status?.connections.map(value=><div className="list-row" key={value.id}><div><b>{value.reviewer}</b><small>{value.active?`Connected ${new Date(value.connected_at).toLocaleString()}`:"Disconnected"}</small></div>{value.active&&<button type="button" className="button danger" onClick={()=>disconnect(value.id,value.grant_id)}>Disconnect & revoke</button>}</div>)}{!status?.connections.length&&<p className="list-empty">No remote connections.</p>}</div>
      </div>
    </section>
    <section className="device-card" aria-label="Remote audit">
      <div className="row between"><h3><Icon name="clock"/>Remote audit</h3><span className="badge">metadata only</span></div>
      {status?.audit.slice().reverse().slice(0,50).map((value,index)=><div className="audit-row" key={`${value.timestamp}-${index}`}><time>{new Date(value.timestamp).toLocaleString()}</time><b>{value.client_id||"unknown"}</b><code>{value.operation}</code><span className={`badge ${value.allowed?"good":"bad"}`}>{value.status}</span></div>)}
      {(status?.audit.length||0)>50&&<p className="list-empty">Showing the latest 50 of {status!.audit.length} recorded operations.</p>}
      {!status?.audit.length&&<p className="list-empty">No remote operations yet.</p>}
    </section>
  </>;
}
