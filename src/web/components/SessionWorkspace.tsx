/** @module SessionWorkspace @description Provides accessible tabs for the human-facing views of one session. */
import React,{useState} from "react";
import type {Session,Turn} from "../types";
import {ActivityMix} from "./ActivityMix";
import {Collaboration} from "./Collaboration";
import {Findings} from "./Findings";
import {SessionContext} from "./SessionContext";
import {Timeline} from "./Timeline";
import {ToolUsage} from "./ToolUsage";
import {Transcript} from "./Transcript";

type SessionTab="conversation"|"timeline"|"tools"|"overview";
const tabs:{id:SessionTab;label:string;icon:string}[]=[
  {id:"conversation",label:"Conversation",icon:"◌"},
  {id:"timeline",label:"Timeline",icon:"⌁"},
  {id:"tools",label:"Tools & findings",icon:"◇"},
  {id:"overview",label:"Overview",icon:"◎"},
];

export function SessionWorkspace({session,onTurn}:{session:Session;onTurn:(turn:Turn)=>void}){
  const[active,setActive]=useState<SessionTab>("conversation");
  const metrics=session.metrics!;
  const moveTab=(event:React.KeyboardEvent<HTMLButtonElement>,index:number)=>{
    const direction=event.key==="ArrowRight"?1:event.key==="ArrowLeft"?-1:0;
    if(!direction&&event.key!=="Home"&&event.key!=="End")return;
    event.preventDefault();
    const next=event.key==="Home"?0:event.key==="End"?tabs.length-1:(index+direction+tabs.length)%tabs.length;
    setActive(tabs[next].id);
    event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  };
  return <section className="sessionWorkspace" aria-label="Selected session workspace">
    <nav className="sessionTabs" role="tablist" aria-label="Session views">
      {tabs.map((tab,index)=><button key={tab.id} id={`session-tab-${tab.id}`} role="tab" aria-selected={active===tab.id} aria-controls={`session-panel-${tab.id}`} tabIndex={active===tab.id?0:-1} onClick={()=>setActive(tab.id)} onKeyDown={event=>moveTab(event,index)}><span aria-hidden="true">{tab.icon}</span>{tab.label}{tab.id==="conversation"&&<small>{metrics.turn_count}</small>}{tab.id==="timeline"&&<small>{(session.events?.length||0)+(session.tool_calls?.length||0)+metrics.turn_count}</small>}{tab.id==="tools"&&<small>{metrics.tool_calls+metrics.finding_count}</small>}</button>)}
    </nav>
    <div className="sessionTabPanel" id={`session-panel-${active}`} role="tabpanel" aria-labelledby={`session-tab-${active}`}>
      {active==="conversation"&&<Transcript session={session}/>}
      {active==="timeline"&&<Timeline session={session} onTurn={onTurn}/>}
      {active==="tools"&&<div className="sessionSplitPanel"><ToolUsage session={session}/><Findings findings={session.findings||[]}/></div>}
      {active==="overview"&&<div className="overviewScroll scrollViewport"><div className="overviewGrid"><SessionContext session={session}/><ActivityMix explorationRatio={metrics.exploration_ratio}/><Collaboration sessionId={session.id}/></div></div>}
    </div>
  </section>;
}
