/** @module EvidenceInspector @description The contextual inspector beside the conversation, with Context, Evidence and Notes tabs sharing one selection. */
import React,{useEffect,useRef} from "react";
import type {Session,Annotation} from "../types";
import type {Evidence,InspectorTab,WorkspaceActions} from "../workspace";
import {Icon} from "./Icon";
import {InspectorContext} from "./InspectorContext";
import {InspectorEvidence} from "./InspectorEvidence";
import {NoteList} from "./NoteList";

const tabs:{id:InspectorTab;label:string}[]=[{id:"context",label:"Context"},{id:"evidence",label:"Evidence"},{id:"notes",label:"Notes"}];

type Props={
  session:Session;
  tab:InspectorTab;
  evidence?:Evidence;
  annotations:Annotation[];
  actions:WorkspaceActions;
  onTab:(tab:InspectorTab)=>void;
  onClose:()=>void;
  onCompare:()=>void;
};

export function EvidenceInspector({session,tab,evidence,annotations,actions,onTab,onClose,onCompare}:Props){
  const content=useRef<HTMLDivElement>(null);
  useEffect(()=>{if(content.current)content.current.scrollTop=0},[evidence,tab,session.id]);
  const move=(event:React.KeyboardEvent<HTMLButtonElement>,index:number)=>{
    const step=event.key==="ArrowRight"?1:event.key==="ArrowLeft"?-1:0;
    if(!step&&event.key!=="Home"&&event.key!=="End")return;
    event.preventDefault();
    const next=event.key==="Home"?0:event.key==="End"?tabs.length-1:(index+step+tabs.length)%tabs.length;
    onTab(tabs[next].id);
    event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  };
  return <aside className="inspector" id="inspector" aria-label="Session inspector">
    <div className="inspector-head"><span className="eyebrow">Session inspector</span><button type="button" className="icon-btn" aria-label="Close inspector" onClick={onClose}><Icon name="x"/></button></div>
    <div className="inspector-tabs" role="tablist" aria-label="Inspector tabs">
      {tabs.map((item,index)=><button type="button" key={item.id} id={`inspector-tab-${item.id}`} role="tab" aria-selected={tab===item.id} aria-controls="inspector-panel" tabIndex={tab===item.id?0:-1} className={tab===item.id?"active":""} onClick={()=>onTab(item.id)} onKeyDown={event=>move(event,index)}>{item.label}{item.id==="notes"&&` ${annotations.length}`}</button>)}
    </div>
    <div ref={content} className="inspector-content" id="inspector-panel" role="tabpanel" aria-labelledby={`inspector-tab-${tab}`} tabIndex={0}>
      {tab==="context"&&<InspectorContext session={session} actions={actions} onCompare={onCompare}/>}
      {tab==="evidence"&&<InspectorEvidence session={session} evidence={evidence} canAnnotate actions={actions}/>}
      {tab==="notes"&&<><NoteList annotations={annotations} turns={session.turns||[]}/><button type="button" className="button notes-add" onClick={()=>actions.noteOn()}><Icon name="note"/>Add a note</button></>}
    </div>
    <div className="inspector-footer"><button type="button" className="button" onClick={()=>actions.setView("review")}><Icon name="circle-check"/>Review this session<span className="push"><Icon name="arrow"/></span></button></div>
  </aside>;
}
