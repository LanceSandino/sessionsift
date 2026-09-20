/** @module SessionWorkspace @description Provides accessible tabs for the human-facing views of one session. */
import React,{useEffect,useState} from "react";
import type {Session,Turn} from "../types";
import {ActivityMix} from "./ActivityMix";
import {Collaboration} from "./Collaboration";
import {Findings} from "./Findings";
import {SessionContext} from "./SessionContext";
import {SessionInvite} from "./SessionInvite";
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

export const SessionWorkspace=React.memo(function SessionWorkspace({session,onTurn}:{session:Session;onTurn:(turn:Turn)=>void}){
  const[active,setActive]=useState<SessionTab>("conversation");
  const[expanded,setExpanded]=useState(false);
  const[split,setSplit]=useState(()=>typeof window!=="undefined"&&window.innerWidth>=1180);
  const metrics=session.metrics!;
  const activeTab=tabs.find(tab=>tab.id===active)!;
  useEffect(()=>{
    if(!expanded)return;
    const close=(event:KeyboardEvent)=>{if(event.key==="Escape")setExpanded(false)};
    window.addEventListener("keydown",close);
    return()=>window.removeEventListener("keydown",close);
  },[expanded]);
  const moveTab=(event:React.KeyboardEvent<HTMLButtonElement>,index:number)=>{
    const direction=event.key==="ArrowRight"?1:event.key==="ArrowLeft"?-1:0;
    if(!direction&&event.key!=="Home"&&event.key!=="End")return;
    event.preventDefault();
    const next=event.key==="Home"?0:event.key==="End"?tabs.length-1:(index+direction+tabs.length)%tabs.length;
    setActive(tabs[next].id);
    event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  };
  const panel=(tab:SessionTab)=>tab==="conversation"?<Transcript session={session}/>:tab==="timeline"?<Timeline session={session} onTurn={onTurn} expanded={expanded}/>:tab==="tools"?<div className="sessionSplitPanel"><ToolUsage session={session}/><Findings findings={session.findings||[]}/></div>:<div className="overviewScroll scrollViewport" aria-label="Session overview entries"><div className="overviewGrid"><SessionContext session={session}/><ActivityMix explorationRatio={metrics.exploration_ratio}/><SessionInvite key={session.id} session={session} sessionId={session.id} sessionTitle={session.title}/><Collaboration sessionId={session.id}/></div></div>;
  const secondary=active==="conversation"?"timeline":active;
  return <section className={`sessionWorkspace ${expanded?"expanded":""} ${split?"split":""}`} data-active={active} aria-label="Selected session workspace">
    <header className="workspaceHeader">
      <nav className="sessionTabs" role="tablist" aria-label="Session views">
        {tabs.map((tab,index)=><button key={tab.id} id={`session-tab-${tab.id}`} role="tab" aria-selected={active===tab.id} aria-controls={`session-panel-${tab.id}`} tabIndex={active===tab.id?0:-1} onClick={()=>setActive(tab.id)} onKeyDown={event=>moveTab(event,index)}><span aria-hidden="true">{tab.icon}</span>{tab.label}{tab.id==="conversation"&&<small>{metrics.turn_count}</small>}{tab.id==="timeline"&&<small>{metrics.turn_count}</small>}{tab.id==="tools"&&<small>{metrics.tool_calls+metrics.finding_count}</small>}</button>)}
      </nav>
      <div className="workspaceActions"><button className="workspaceModeButton" type="button" aria-label={split?"Use focused control center":"Use split control center"} aria-pressed={split} onClick={()=>setSplit(value=>!value)}><span aria-hidden="true">{split?"▣":"▥"}</span><b>{split?"Focus":"Split"}</b></button><button className="workspaceExpandButton" type="button" aria-label={expanded?`Exit expanded ${activeTab.label} view`:`Expand ${activeTab.label} view`} aria-pressed={expanded} onClick={()=>setExpanded(value=>!value)}><span aria-hidden="true">{expanded?"↙":"⛶"}</span><b>{expanded?"Restore":"Expand"}</b></button></div>
    </header>
    <div className="sessionTabPanel" id={`session-panel-${active}`} role="tabpanel" aria-labelledby={`session-tab-${active}`}>
      {split?<div className="controlCenterGrid"><section className="controlCenterPane controlCenterPrimary" aria-label="Conversation control pane">{panel("conversation")}</section><section className="controlCenterPane controlCenterSecondary" aria-label={`${tabs.find(tab=>tab.id===secondary)!.label} control pane`}>{panel(secondary)}</section></div>:panel(active)}
    </div>
  </section>;
});
