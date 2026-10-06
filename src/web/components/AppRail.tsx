/** @module AppRail @description The narrow application rail: workspace destinations above, health and theme utilities below. */
import React from "react";
import type {Page} from "../navigation";
import type {Theme} from "../theme";
import {Icon,type IconName} from "./Icon";
import {LayeredSiftMark} from "./LayeredSiftMark";

export const destinations:readonly {page:Page;label:string;icon:IconName;href:string;hint?:string}[]=[
  {page:"sessions",label:"Sessions",icon:"chat",href:"/"},
  {page:"analytics",label:"Analytics",icon:"chart",href:"/analytics"},
  {page:"compare",label:"Compare",icon:"compare",href:"/compare"},
  {page:"optimize",label:"Optimize",icon:"spark",href:"/optimize",hint:"Optimize · turn recommendations into measured experiments"},
  {page:"connections",label:"Connections",icon:"link",href:"/connections",hint:"Connections · bridge, support codes, MCP approvals, manual pairing and revocation"},
];

export function AppRail({page,theme,onPage,onTheme}:{page:Page;theme:Theme;onPage:(page:Page)=>void;onTheme:()=>void}){
  const go=(event:React.MouseEvent<HTMLAnchorElement>,next:Page)=>{event.preventDefault();onPage(next)};
  return <nav className="rail" aria-label="Application">
    <div className="brand-mark" title="SessionSift"><LayeredSiftMark/></div>
    <div className="rail-nav">
      {destinations.map(item=><a key={item.page} href={item.href} className={`icon-btn ${page===item.page?"active":""}`} aria-label={item.label} title={item.hint||item.label} aria-current={page===item.page?"page":undefined} onClick={event=>go(event,item.page)}><Icon name={item.icon}/></a>)}
    </div>
    <div className="rail-bottom">
      <a href="/changelog" className={`icon-btn ${page==="changelog"?"active":""}`} aria-label="Changelog" title="Changelog · semantic release history" aria-current={page==="changelog"?"page":undefined} onClick={event=>go(event,"changelog")}><Icon name="file"/></a>
      <a href="/diagnostics" className={`icon-btn ${page==="diagnostics"?"active":""}`} aria-label="Diagnostics" title="Diagnostics · capture activity and available data" aria-current={page==="diagnostics"?"page":undefined} onClick={event=>go(event,"diagnostics")}><Icon name="health"/></a>
      <button type="button" className="icon-btn" aria-label={theme==="dark"?"Use light theme":"Use dark theme"} title="Toggle theme" onClick={onTheme}><Icon name={theme==="dark"?"sun":"moon"}/></button>
      <span className="avatar" title="Local owner · this device"><Icon name="user"/></span>
    </div>
  </nav>;
}
