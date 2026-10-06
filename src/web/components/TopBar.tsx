/** @module TopBar @description Breadcrumbs, command search, focus and inspector toggles, and the one-conversation support action. */
import React from "react";
import type {Page} from "../navigation";
import {Icon} from "./Icon";

const pageNames:Record<Page,string>={sessions:"Sessions",analytics:"Analytics",compare:"Compare",optimize:"Optimize",connections:"Connections",diagnostics:"Diagnostics",changelog:"Changelog"};

export type SupportIndicator="none"|"waiting"|"connected";

type Props={
  page:Page;
  place?:string;
  focus:boolean;
  inspectorOpen:boolean;
  support:SupportIndicator;
  canShare:boolean;
  onMenu:()=>void;
  onCommand:()=>void;
  onFocus:()=>void;
  onInspector:()=>void;
  onSupport:()=>void;
};

export function TopBar({page,place,focus,inspectorOpen,support,canShare,onMenu,onCommand,onFocus,onInspector,onSupport}:Props){
  const sessions=page==="sessions";
  return <header className="topbar">
    <button type="button" className="icon-btn mobile-menu" aria-label="Open session library" onClick={onMenu}><Icon name="menu"/></button>
    <div className="breadcrumbs">
      <Icon name="folder"/><span className="desktop-meta">Local library</span><span className="slash desktop-meta" aria-hidden="true">/</span><span className="current">{sessions?place||"Sessions":pageNames[page]}</span>
    </div>
    <div className="top-actions">
      <button type="button" className="search-trigger" aria-label="Open command palette" title="Find anything · Ctrl K" onClick={onCommand}><Icon name="search"/><kbd>Ctrl K</kbd></button>
      {sessions&&<span className="sep" aria-hidden="true"/>}
      {sessions&&<button type="button" className={`icon-btn ${focus?"on":""}`} aria-label={focus?"Exit focus mode":"Enter focus mode"} aria-pressed={focus} title={focus?"Exit focus mode · Ctrl Shift F":"Focus mode · Ctrl Shift F"} onClick={onFocus}><Icon name="focus"/></button>}
      {sessions&&<button type="button" className={`icon-btn ${inspectorOpen?"on":""}`} aria-label={inspectorOpen?"Hide inspector":"Show inspector"} aria-pressed={inspectorOpen} title="Toggle inspector" onClick={onInspector}><Icon name="panel"/></button>}
      {sessions&&canShare&&<button type="button" className={`button support-button ${support!=="none"?"live":""}`} aria-label={support==="connected"?"Support: supporter connected":support==="waiting"?"Support: code waiting":"Support"} onClick={onSupport}>{support!=="none"?<span className="dot" aria-hidden="true"/>:<Icon name="users"/>}<span className="support-label">{support==="connected"?"Connected":support==="waiting"?"Code active":"Support"}</span></button>}
    </div>
  </header>;
}
