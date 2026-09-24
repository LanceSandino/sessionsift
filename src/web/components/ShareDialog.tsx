/** @module ShareDialog @description The single-purpose share dialog for the open conversation: a device-generated support code, or an email invitation. */
import React,{useState} from "react";
import {sourceInfo} from "../sources";
import type {Session} from "../types";
import {Dialog} from "./Dialog";
import {SessionInvite} from "./SessionInvite";
import {SupportShare} from "./SupportShare";

export function ShareDialog({open,session,onClose,onOpenConnections}:{open:boolean;session?:Session;onClose:()=>void;onOpenConnections:()=>void}){
  const[mode,setMode]=useState<"code"|"invite">("code");
  const info=sourceInfo(session);
  return <Dialog open={open&&Boolean(session)} title="Share this conversation" wide={mode==="invite"} onClose={onClose}>
    {session&&<>
      <div className="dialog-tabs" role="group" aria-label="Sharing method"><button type="button" className={mode==="code"?"active":""} aria-pressed={mode==="code"} onClick={()=>setMode("code")}>Support code</button><button type="button" className={mode==="invite"?"active":""} aria-pressed={mode==="invite"} onClick={()=>setMode("invite")}>Email invitation</button></div>
      {mode==="code"?<SupportShare sessionId={session.id} sessionTitle={session.title} summary={<span className={`source-icon ${info.tone}`} aria-hidden="true">{info.badge}</span>} onOpenConnections={onOpenConnections}/>
        :<SessionInvite session={session} sessionId={session.id} sessionTitle={session.title}/>}
    </>}
  </Dialog>;
}
