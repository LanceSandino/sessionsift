/** @module Sidebar @description Renders primary local profiler navigation. */
import React,{useEffect,useRef,useState} from "react";
import {LayeredSiftMark} from "./LayeredSiftMark";
import {sources,type SourceId} from "../sources";
export type View="sessions"|"compare"|"analytics"|"diagnostics"|"remote"|"review";

const destinations=[
  {id:"sessions",icon:"◉",label:"Conversations",mobileLabel:"Sessions",href:"/",secondary:false},
  {id:"compare",icon:"⇄",label:"Compare",mobileLabel:"Compare",href:"/compare",secondary:false},
  {id:"analytics",icon:"▥",label:"Analytics",mobileLabel:"Analytics",href:"/analytics",secondary:false},
  {id:"remote",icon:"⌁",label:"Remote access",mobileLabel:"Access",href:"/remote-access",secondary:false},
  {id:"review",icon:"◎",label:"Reviewer portal",mobileLabel:"Review",href:"/remote-review",secondary:true},
  {id:"diagnostics",icon:"◇",label:"Diagnostics",mobileLabel:"Diagnostics",href:"/diagnostics",secondary:true},
] as const;

export function Sidebar({view,onView,counts}:{view:View;onView:(view:View)=>void;counts:Record<SourceId,number>}){
  const[moreOpen,setMoreOpen]=useState(false);
  const moreButton=useRef<HTMLButtonElement>(null);
  const moreMenu=useRef<HTMLDivElement>(null);
  const navigate=(event:React.MouseEvent<HTMLAnchorElement>,next:View)=>{event.preventDefault();setMoreOpen(false);onView(next)};
  useEffect(()=>{
    if(!moreOpen)return;
    const dismiss=(event:PointerEvent)=>{const target=event.target as Node;if(!moreButton.current?.contains(target)&&!moreMenu.current?.contains(target))setMoreOpen(false)};
    const escape=(event:KeyboardEvent)=>{if(event.key==="Escape"){setMoreOpen(false);moreButton.current?.focus()}};
    window.addEventListener("pointerdown",dismiss);
    window.addEventListener("keydown",escape);
    return()=>{window.removeEventListener("pointerdown",dismiss);window.removeEventListener("keydown",escape)};
  },[moreOpen]);
  return <aside className="sidebar" aria-label="Application navigation">
    <div className="brand"><span className="brandMark"><LayeredSiftMark/></span><b>Session<span>Sift</span></b><small>clarity from coding sessions</small></div>
    <nav aria-label="Primary navigation">
      {destinations.map(destination=><a key={destination.id} href={destination.href} className={`${view===destination.id?"active":""} ${destination.secondary?"secondaryDestination":"primaryDestination"}`} aria-label={destination.label} aria-current={view===destination.id?"page":undefined} title={destination.label} onClick={event=>navigate(event,destination.id)}><span className="navIcon" aria-hidden="true">{destination.icon}</span><span className="navLabel desktopNavLabel">{destination.label}</span><span className="navLabel mobileNavLabel">{destination.mobileLabel}</span></a>)}
      <button ref={moreButton} className={`mobileMoreButton ${view==="review"||view==="diagnostics"?"active":""}`} type="button" aria-label="More destinations" aria-haspopup="menu" aria-controls="mobile-more-menu" aria-expanded={moreOpen} onClick={()=>setMoreOpen(value=>!value)}><span className="navIcon" aria-hidden="true">•••</span><span className="navLabel">More</span></button>
    </nav>
    {moreOpen&&<div ref={moreMenu} id="mobile-more-menu" className="mobileMoreMenu" role="menu" aria-label="More destinations">{destinations.filter(destination=>destination.secondary).map(destination=><a key={destination.id} role="menuitem" aria-label={destination.label} href={destination.href} aria-current={view===destination.id?"page":undefined} onClick={event=>navigate(event,destination.id)}><span aria-hidden="true">{destination.icon}</span><span><b>{destination.label}</b><small>{destination.id==="review"?"Open the scoped reviewer workspace":"Check collection and parser health"}</small></span></a>)}</div>}
    <div className="providerSummary">
      <div className="providerLabel">CONNECTED SOURCES</div>
      {sources.map(source=><div className="provider" key={source.id}><span className={`statusDot ${counts[source.id]?"":"idle"}`}/>{source.label} <strong>{counts[source.id]}</strong></div>)}
    </div>
    <div className="privacy"><span aria-hidden="true">⌂</span><div><b>Local authority</b><small>Remote access stays owner-controlled.</small></div></div>
  </aside>;
}
