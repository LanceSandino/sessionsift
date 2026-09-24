/** @module SessionNavigator @description The session library: search, source filters, sort order and recency-grouped conversations from every source. */
import React,{useMemo,useState} from "react";
import type {Page} from "../navigation";
import {clockTime,shortDate,shortDateTime} from "../format";
import type {Session} from "../types";
import {libraryGroups,libraryTime,recencyLabel} from "../libraryGroups";
import {sessionPlace,sourceInfo,sources} from "../sources";
import {destinations} from "./AppRail";
import {Icon} from "./Icon";
import {filterSessions} from "./sessionFilters";
import {sortOrders,type SessionSortOrder} from "./sessionSort";

const chips=[{id:"",label:"All"},{id:"claude_code",label:"Claude Code"},{id:"codex",label:"Codex"},{id:"chat",label:"Chats"}];

type Props={
  sessions:Session[];
  selected?:string;
  page:Page;
  now:number;
  searchRef:React.RefObject<HTMLInputElement|null>;
  onSelect:(id:string)=>void;
  onPage:(page:Page)=>void;
  onImport:()=>void;
  onClose:()=>void;
};

export function SessionNavigator({sessions,selected,page,now,searchRef,onSelect,onPage,onImport,onClose}:Props){
  const[query,setQuery]=useState("");
  const[source,setSource]=useState("");
  const[repo,setRepo]=useState("");
  const[model,setModel]=useState("");
  const[date,setDate]=useState("");
  const[highTokens,setHighTokens]=useState(false);
  const[failures,setFailures]=useState(false);
  const[findings,setFindings]=useState(false);
  const[internal,setInternal]=useState(false);
  const[filtersOpen,setFiltersOpen]=useState(false);
  const[order,setOrder]=useState<SessionSortOrder>("updated_desc");
  const repos=useMemo(()=>[...new Set(sessions.map(s=>s.repo_display_name).filter(Boolean))].sort() as string[],[sessions]);
  const models=useMemo(()=>[...new Set(sessions.map(s=>s.model).filter(Boolean))].sort() as string[],[sessions]);
  const filtered=useMemo(()=>filterSessions(sessions,{query,source,repo,model,date,highTokens,failures,findings,internal}),[sessions,query,source,repo,model,date,highTokens,failures,findings,internal]);
  const groups=useMemo(()=>libraryGroups(filtered,order,now),[filtered,order,now]);
  const created=order==="started_desc"||order==="started_asc";
  // Rows active today or yesterday show a clock time; older rows carry it in the tooltip.
  const stamp=(value:string)=>["Today","Yesterday"].includes(recencyLabel(value,now))?clockTime(value):shortDate(value);
  const extraFilters=[repo,model,date,highTokens,failures,findings,internal,source&&!chips.some(chip=>chip.id===source)].filter(Boolean).length;
  return <aside className="sidebar" aria-label="Session library">
    <div className="workspace-select">
      <div className="grow"><div className="wordmark">Session<span>Sift</span></div><div className="workspace-meta">Local workspace <span className="muted">/</span> This device</div></div>
      <button type="button" className="icon-btn view-close" aria-label="Close session library" onClick={onClose}><Icon name="x"/></button>
    </div>
    <nav className="mobile-nav" aria-label="Workspace pages">
      {[...destinations,{page:"diagnostics" as Page,label:"Diagnostics",icon:"health" as const,href:"/diagnostics"}].map(item=><button type="button" key={item.page} className={page===item.page?"active":""} aria-current={page===item.page?"page":undefined} onClick={()=>onPage(item.page)}><Icon name={item.icon}/>{item.label}</button>)}
    </nav>
    <div className="library-head">
      <div className="row between"><span className="eyebrow">Sessions <span className="mono muted" aria-label={`${filtered.length} shown`}>{String(filtered.length).padStart(2,"0")}</span></span><span className="row library-tools"><select className="sort-select" aria-label="Sort conversations" value={order} onChange={event=>setOrder(event.target.value as SessionSortOrder)}>{sortOrders.map(item=><option key={item.id} value={item.id}>{item.label}</option>)}</select><button type="button" className="icon-btn" aria-label="Import conversations" title="Import conversations" onClick={onImport}><Icon name="plus"/></button></span></div>
      <label className="search-field"><Icon name="search"/><input ref={searchRef} type="search" aria-label="Search sessions" placeholder="Find a session…" value={query} onChange={event=>setQuery(event.target.value)}/><kbd>/</kbd></label>
      <div className="filter-row" role="group" aria-label="Filter by source">
        {chips.map(chip=><button type="button" key={chip.id||"all"} className={`filter ${source===chip.id?"active":""}`} aria-pressed={source===chip.id} onClick={()=>setSource(chip.id)}>{chip.label}</button>)}
        <button type="button" className="filter more" aria-expanded={filtersOpen} aria-controls="library-filters" onClick={()=>setFiltersOpen(value=>!value)}><Icon name="filter"/>{filtersOpen?"Hide filters":"Filters"}{extraFilters>0&&<b>{extraFilters}</b>}</button>
      </div>
      {filtersOpen&&<div id="library-filters" className="library-filters" data-testid="session-filters">
        <select className="field" aria-label="Source filter" value={source} onChange={event=>setSource(event.target.value)}><option value="">All sources</option>{sources.map(item=><option key={item.id} value={item.id}>{item.label}</option>)}<option value="chat">All chats</option></select>
        <select className="field" aria-label="Repository filter" value={repo} onChange={event=>setRepo(event.target.value)}><option value="">All repositories</option>{repos.map(value=><option key={value}>{value}</option>)}</select>
        <select className="field" aria-label="Model filter" value={model} onChange={event=>setModel(event.target.value)}><option value="">All models</option>{models.map(value=><option key={value}>{value}</option>)}</select>
        <input className="field" aria-label="Date filter" type="date" value={date} onChange={event=>setDate(event.target.value)}/>
        <div className="filter-checks">
          <label><input type="checkbox" checked={highTokens} onChange={event=>setHighTokens(event.target.checked)}/>100k+ tokens</label>
          <label><input type="checkbox" checked={failures} onChange={event=>setFailures(event.target.checked)}/>Failures</label>
          <label><input type="checkbox" checked={findings} onChange={event=>setFindings(event.target.checked)}/>Findings</label>
          <label><input type="checkbox" checked={internal} onChange={event=>setInternal(event.target.checked)}/>Internal reviews</label>
        </div>
      </div>}
    </div>
    <div className="session-list" role="list" aria-label="Sessions">
      {groups.map(group=><React.Fragment key={group.label}>
        <div className="list-label" role="presentation">{group.label}</div>
        {group.sessions.map(session=>{const info=sourceInfo(session);const active=session.id===selected;const prompts=session.metrics?.turn_count??session.turns?.length??0;const time=libraryTime(session,order);return <div role="listitem" key={session.id}><button type="button" className={`session-item ${active?"active":""}`} aria-current={active?"true":undefined} onClick={()=>onSelect(session.id)}>
          <span className={`source-icon ${info.tone}`} title={info.label} aria-hidden="true">{info.badge}</span>
          <span className="grow"><strong>{session.title}</strong><span className="session-meta"><span>{sessionPlace(session)}</span><span aria-hidden="true">·</span><span>{prompts} prompt{prompts===1?"":"s"}</span><span aria-hidden="true">·</span><time dateTime={time} title={`${created?"Created":"Updated"} ${shortDateTime(time)}`}>{stamp(time)}</time>{session.session_kind==="internal_review"&&<span className="internal-tag">internal review</span>}{(session.metrics?.failed_tool_calls||0)>0&&<span className="internal-tag">{session.metrics?.failed_tool_calls} failed</span>}</span></span>
          {active&&<span className="current-dot" aria-hidden="true"/>}
        </button></div>})}
      </React.Fragment>)}
      {filtered.length===0&&<div className="library-empty">{sessions.length?<>No sessions match.<br/>Try another source or clear a filter.</>:<>No conversations imported yet.<br/>Codex and Claude Code sessions appear here automatically.</>}</div>}
    </div>
    <div className="sidebar-bottom">
      <button type="button" className="button" onClick={onImport}><Icon name="import"/>Import conversations</button>
      <div className="storage-note"><Icon name="lock"/><span>Transcript storage stays on your device.</span></div>
    </div>
  </aside>;
}
