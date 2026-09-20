/** @module Sidebar @description Renders primary local profiler navigation. */
import React,{useState} from "react";
export type View="sessions"|"compare"|"analytics"|"diagnostics"|"remote"|"review";

const destinations=[
  {id:"sessions",icon:"◉",label:"Conversations",mobileLabel:"Sessions",href:"/",secondary:false},
  {id:"compare",icon:"⇄",label:"Compare",mobileLabel:"Compare",href:"/compare",secondary:false},
  {id:"analytics",icon:"▥",label:"Analytics",mobileLabel:"Analytics",href:"/analytics",secondary:false},
  {id:"remote",icon:"⌁",label:"Remote access",mobileLabel:"Access",href:"/remote-access",secondary:false},
  {id:"review",icon:"◎",label:"Reviewer portal",mobileLabel:"Review",href:"/remote-review",secondary:true},
  {id:"diagnostics",icon:"◇",label:"Diagnostics",mobileLabel:"Diagnostics",href:"/diagnostics",secondary:true},
] as const;

export function Sidebar({view,onView,codexCount,chatCount}:{view:View;onView:(view:View)=>void;codexCount:number;chatCount:number}){
  const[moreOpen,setMoreOpen]=useState(false);
  const navigate=(event:React.MouseEvent<HTMLAnchorElement>,next:View)=>{event.preventDefault();setMoreOpen(false);onView(next)};
  return <aside className="sidebar" aria-label="Application navigation">
    <div className="brand"><span className="brandMark" aria-hidden="true">◈</span><b>SessionSift</b><small>session intelligence</small></div>
    <nav aria-label="Primary navigation">
      {destinations.map(destination=><a key={destination.id} href={destination.href} className={`${view===destination.id?"active":""} ${destination.secondary?"secondaryDestination":"primaryDestination"}`} aria-label={destination.label} aria-current={view===destination.id?"page":undefined} title={destination.label} onClick={event=>navigate(event,destination.id)}><span className="navIcon" aria-hidden="true">{destination.icon}</span><span className="navLabel desktopNavLabel">{destination.label}</span><span className="navLabel mobileNavLabel">{destination.mobileLabel}</span></a>)}
      <button className={`mobileMoreButton ${view==="review"||view==="diagnostics"?"active":""}`} type="button" aria-label="More destinations" aria-expanded={moreOpen} onClick={()=>setMoreOpen(value=>!value)}><span className="navIcon" aria-hidden="true">•••</span><span className="navLabel">More</span></button>
    </nav>
    {moreOpen&&<div className="mobileMoreMenu" role="menu" aria-label="More destinations">{destinations.filter(destination=>destination.secondary).map(destination=><a key={destination.id} role="menuitem" aria-label={destination.label} href={destination.href} aria-current={view===destination.id?"page":undefined} onClick={event=>navigate(event,destination.id)}><span aria-hidden="true">{destination.icon}</span><span><b>{destination.label}</b><small>{destination.id==="review"?"Open the scoped reviewer workspace":"Check collection and parser health"}</small></span></a>)}</div>}
    <div className="providerSummary">
      <div className="providerLabel">CONNECTED SOURCES</div>
      <div className="provider"><span className="statusDot"/>Codex <strong>{codexCount}</strong></div>
      <div className="provider"><span className={`statusDot ${chatCount?"":"idle"}`}/>ChatGPT <strong>{chatCount}</strong></div>
    </div>
    <div className="privacy"><span aria-hidden="true">⌂</span><div><b>Local authority</b><small>Remote access stays owner-controlled.</small></div></div>
  </aside>;
}
