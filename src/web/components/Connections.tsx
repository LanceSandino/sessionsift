/** @module Connections @description The connection workspace: this device and its bridge, helping someone else, MCP identities, local pairing and the reviewer portal. */
import React,{useEffect,useState} from "react";
import {api} from "../api";
import {BridgeConnection} from "./BridgeConnection";
import {CloudSync} from "./CloudSync";
import {Icon} from "./Icon";
import {RemoteAccess} from "./RemoteAccess";
import {RemoteReview} from "./RemoteReview";

function hostOf(url:string){try{return new URL(url).host}catch{return url}}

export function Connections({showRemote:controlledRemote,onShowRemote}:{showRemote?:boolean;onShowRemote?:(show:boolean)=>void}={}){
  const[localRemote,setLocalRemote]=useState(false);
  const showRemote=controlledRemote??localRemote;
  const setShowRemote=onShowRemote??setLocalRemote;
  const[bridgeURL,setBridgeURL]=useState<string>();
  useEffect(()=>{
    if(!showRemote)return;
    let active=true;
    const load=()=>api.bridge().then(status=>{if(active)setBridgeURL(status.connected?status.url:undefined)}).catch(()=>{});
    load();const timer=window.setInterval(load,5000);
    return()=>{active=false;window.clearInterval(timer)};
  },[showRemote]);
  return <div className="content-page" aria-label="Connections">
    <div className="page-intro"><span className="eyebrow">Local MCP</span><h2>Local tools. No account required.</h2><p>Use SessionSift’s ten read-only tools from Codex or Claude Code on this computer. No login, hosted bridge or cloud sync is required.</p></div>
    <section className="device-card" aria-label="Local MCP setup"><h3>Connect a local coding client</h3><p>Run <code>sessionsift setup mcp</code> to print reviewable configuration. For optional guided setup, preview <code>sessionsift setup mcp --client codex --preview</code> or use <code>--client claude-code</code>. Changes require explicit <code>--apply --yes</code>; discovery uses <code>--verify</code>, and removal uses <code>--undo --yes</code>.</p><p>MCP lets your selected client read the local library. Review what you import; local read-only access is not a guarantee of secret-free content.</p></section>
    <section className="device-card"><label><input type="checkbox" checked={showRemote} onChange={event=>setShowRemote(event.target.checked)}/> Show experimental remote features</label><p>Remote support, hosted MCP and encrypted cloud sync are not offered in the initial local-only release. These advanced controls require a separately configured bridge and explicit grants. Showing controls does not connect or upload data.</p></section>
    {showRemote&&<>
    <div className="page-intro"><span className="eyebrow">Connection workspace</span><h2>Share the session. Not the machine.</h2><p>Device administration lives here; one-conversation support lives inside each conversation. Local authorization and the kill switch always win.</p></div>
    <section className="device-card access-guide" aria-labelledby="access-guide-title">
      <h3 id="access-guide-title"><Icon name="shield"/>Choose the access method</h3>
      <p>Use the narrowest option that fits. Each one is owner-approved and revocable here.</p>
      <ol>
        <li><b>Support code or email invitation</b><span><strong>Recommended for people.</strong> Open a conversation and choose Support: a support code connects one signed-in helper to that session; an email invitation sends a private link bound to one address.</span></li>
        <li><b>ChatGPT or Claude MCP</b><span>Approve an exact identity below so it can use SessionSift’s read-only tools through <code>/mcp</code>.</span></li>
        <li><b>Manual pairing</b><span>An advanced, testing-oriented flow for the Reviewer Portal. It creates a short-lived capability grant rather than an invitation.</span></li>
      </ol>
    </section>
    <BridgeConnection/>
    <CloudSync/>
    <section className="device-card" aria-label="Help someone with one session">
      <h3><Icon name="users"/>Help someone with one session</h3>
      <p>When someone shares a support code with you, sign in to their bridge and enter it on its support page. You will see only the one conversation it was created for, with the controls their grant allows.</p>
      {bridgeURL?<a className="button primary" href={`${bridgeURL}/support`} target="_blank" rel="noopener noreferrer"><Icon name="external"/>Open {hostOf(bridgeURL)}/support</a>:<p className="card-copy">Open <code>/support</code> on the bridge the other person uses. Their code works once and expires ten minutes after they create it.</p>}
    </section>
    <RemoteAccess/>
    <RemoteReview/>
    </>}
  </div>;
}
