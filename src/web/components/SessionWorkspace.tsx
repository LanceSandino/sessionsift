/** @module SessionWorkspace @description Composes one session: header, view tabs, the active lens with its review composer, and the contextual inspector. */
import React,{useCallback,useEffect,useMemo,useRef,useState} from "react";
import type {SessionView} from "../navigation";
import {currentDecision} from "../reviewState";
import {sourceInfo} from "../sources";
import type {Session} from "../types";
import type {Collaboration} from "../useCollaboration";
import {useLatestScroll} from "../useLatestScroll";
import type {Evidence,InspectorTab,WorkspaceActions} from "../workspace";
import {EvidenceInspector} from "./EvidenceInspector";
import {Icon,type IconName} from "./Icon";
import {ReviewComposer} from "./ReviewComposer";
import {ReviewView} from "./ReviewView";
import {SessionHeader} from "./SessionHeader";
import {SourceView} from "./SourceView";
import {Timeline} from "./Timeline";
import {ToolsView} from "./ToolsView";
import {Transcript,type JumpRequest} from "./Transcript";

const views:{id:SessionView;label:string;icon:IconName}[]=[
  {id:"conversation",label:"Conversation",icon:"chat"},
  {id:"timeline",label:"Timeline",icon:"timeline"},
  {id:"tools",label:"Tools",icon:"tool"},
  {id:"review",label:"Review",icon:"note"},
];

type Props={
  session:Session;
  view:SessionView;
  collaboration:Collaboration;
  onView:(view:SessionView)=>void;
  /** Called when evidence is selected so a hidden or overlay inspector can appear. */
  onRevealInspector:()=>void;
  /** Closes a narrow-screen overlay inspector; a docked inspector stays. */
  onDismissOverlay:()=>void;
  onCloseInspector:()=>void;
  onCompare:()=>void;
  onToast:(text:string)=>void;
};

export function SessionWorkspace({session,view,collaboration,onView,onRevealInspector,onDismissOverlay,onCloseInspector,onCompare,onToast}:Props){
  const scroll=useRef<HTMLDivElement>(null);
  const composer=useRef<HTMLTextAreaElement>(null);
  const memory=useMemo(()=>new Map<string,number>(),[]);
  const[source,setSource]=useState(false);
  const[tab,setTab]=useState<InspectorTab>("context");
  const[evidence,setEvidence]=useState<Evidence>();
  const[jump,setJump]=useState<JumpRequest>();
  const[openRuns,setOpenRuns]=useState<Record<string,boolean>>({});
  const[draft,setDraft]=useState("");
  const[target,setTarget]=useState("");
  const pendingFocus=useRef(false);
  useEffect(()=>{setSource(false);setTab("context");setEvidence(undefined);setJump(undefined);setOpenRuns({});setDraft("");setTarget("");memory.clear()},[session.id,memory]);
  useLatestScroll(scroll,`${session.id}:${source?"source":view}`,{memory,start:source||view==="tools"||view==="review"?"top":"latest"});
  useEffect(()=>{if(pendingFocus.current){pendingFocus.current=false;composer.current?.focus()}});
  const actions:WorkspaceActions=useMemo(()=>({
    selectEvidence:(next:Evidence)=>{setEvidence(next);setTab("evidence");onRevealInspector()},
    jumpToTurn:(turnID:string)=>{setSource(false);onView("conversation");setJump({turnID,nonce:Date.now()})},
    noteOn:(turnID?:string)=>{setTarget(turnID||"");setSource(false);if(view!=="conversation"&&view!=="review")onView("conversation");pendingFocus.current=true;onDismissOverlay()},
    setView:(next:SessionView)=>{setSource(false);onView(next)},
  }),[onRevealInspector,onDismissOverlay,onView,view]);
  const addNote=useCallback(async(body:string,turnID?:string)=>{await collaboration.addNote(body,turnID);onToast("Review note added on this device. Nothing was sent to the source agent.")},[collaboration,onToast]);
  const counts:Record<SessionView,number>={conversation:session.turns?.length||0,timeline:session.turns?.length||0,tools:session.tool_calls?.length||0,review:collaboration.annotations.length+collaboration.reviews.length};
  const move=(event:React.KeyboardEvent<HTMLButtonElement>,index:number)=>{
    const step=event.key==="ArrowRight"?1:event.key==="ArrowLeft"?-1:0;
    if(!step&&event.key!=="Home"&&event.key!=="End")return;
    event.preventDefault();
    const next=event.key==="Home"?0:event.key==="End"?views.length-1:(index+step+views.length)%views.length;
    actions.setView(views[next].id);
    event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  };
  const active=source?undefined:view;
  const sourceLabel=sourceInfo(session).label;
  return <>
    <SessionHeader session={session} decision={currentDecision(collaboration.reviews)}/>
    <div className="view-tabs">
      <div className="view-tablist" role="tablist" aria-label="Session views">
        {views.map((item,index)=><button type="button" key={item.id} id={`view-tab-${item.id}`} role="tab" aria-selected={active===item.id} aria-controls="session-panel" tabIndex={view===item.id?0:-1} className={`view-tab ${active===item.id?"active":""}`} onClick={()=>actions.setView(item.id)} onKeyDown={event=>move(event,index)}><Icon name={item.icon}/>{item.label}<span className="tab-count">{counts[item.id].toLocaleString()}</span></button>)}
      </div>
      <div className="view-controls"><button type="button" className={`icon-btn ${source?"on":""}`} aria-label={source?"Hide normalized record":"Show normalized record"} aria-pressed={source} title="Normalized record (JSON)" onClick={()=>setSource(value=>!value)}><Icon name="code"/></button></div>
    </div>
    <div className="workbench">
      <section className="main-pane" aria-label="Session content">
        <div ref={scroll} className="main-scroll" id="session-panel" role="tabpanel" aria-labelledby={active?`view-tab-${active}`:undefined} aria-label={active?undefined:"Normalized record"} tabIndex={0}>
          {source?<SourceView session={session}/>
            :view==="conversation"?<Transcript session={session} annotations={collaboration.annotations} selected={evidence} viewport={scroll} jump={jump} openRuns={openRuns} onRunToggle={(turnID,open)=>setOpenRuns(value=>({...value,[turnID]:open}))} actions={actions}/>
            :view==="timeline"?<Timeline session={session} selected={evidence} viewport={scroll} actions={actions}/>
            :view==="tools"?<ToolsView session={session} selected={evidence} actions={actions}/>
            :<ReviewView session={session} collaboration={collaboration} onToast={onToast}/>}
        </div>
        {!source&&(view==="conversation"||view==="review")&&<ReviewComposer turns={session.turns||[]} sourceLabel={sourceLabel} target={target} draft={draft} textarea={composer} onTarget={setTarget} onDraft={setDraft} onSubmit={addNote}/>}
      </section>
      <EvidenceInspector session={session} tab={tab} evidence={evidence} annotations={collaboration.annotations} actions={actions} onTab={setTab} onClose={onCloseInspector} onCompare={onCompare}/>
    </div>
  </>;
}
