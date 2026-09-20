/** @module SessionList @description Filters and selects imported Codex and ChatGPT conversations. */
import React,{useMemo,useState} from "react";
import {compact,duration} from "../format";
import type {Session} from "../types";
import {filterSessions} from "./sessionFilters";

export function SessionList({sessions,selected,onSelect}:{sessions:Session[];selected?:string;onSelect:(id:string)=>void}){
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
  const[mobileBrowserOpen,setMobileBrowserOpen]=useState(false);
  const repos=useMemo(()=>[...new Set(sessions.map(s=>s.repo_display_name).filter(Boolean))] as string[],[sessions]);
  const models=useMemo(()=>[...new Set(sessions.map(s=>s.model).filter(Boolean))] as string[],[sessions]);
  const filtered=useMemo(()=>filterSessions(sessions,{query,source,repo,model,date,highTokens,failures,findings,internal}),[sessions,query,source,repo,model,date,highTokens,failures,findings,internal]);
  const activeFilterCount=[source,repo,model,date,highTokens,failures,findings,internal].filter(Boolean).length;
  const current=sessions.find(session=>session.id===selected);
  const select=(id:string)=>{onSelect(id);setMobileBrowserOpen(false)};
  return <aside className="sessionRail" aria-label="Conversation browser">
    <header className="railTitle"><div><span>Workspace</span><h2>Conversations</h2></div><span className="railCount">{filtered.length}</span></header>
    <button className="mobileSessionTrigger" type="button" aria-controls="session-browser" aria-expanded={mobileBrowserOpen} aria-label={`Browse conversations${current?`. Current: ${current.title}`:""}`} onClick={()=>setMobileBrowserOpen(true)}>
      <span className="sessionSourceIcon" aria-hidden="true">{current?.source==="chatgpt"?"✦":"◈"}</span>
      <span><small>Current conversation</small><b>{current?.title||"Choose a conversation"}</b></span>
      <span className="mobileSessionMeta">{current?.source==="chatgpt"?"ChatGPT":current?.repo_display_name||"Codex"}<i aria-hidden="true">⌄</i></span>
    </button>
    <div id="session-browser" className={`sessionBrowser mobileSessionDialog ${mobileBrowserOpen?"open":""}`} role={mobileBrowserOpen?"dialog":undefined} aria-modal={mobileBrowserOpen?true:undefined} aria-label={mobileBrowserOpen?"Choose a conversation":undefined} onKeyDown={event=>{if(event.key==="Escape")setMobileBrowserOpen(false)}}>
      <header className="mobileBrowserHeader"><div><small>Workspace</small><h2>Conversations</h2></div><button type="button" aria-label="Close conversation picker" onClick={()=>setMobileBrowserOpen(false)}>×</button></header>
      <div className="railControls">
        <label className="search"><span aria-hidden="true">⌕</span><input aria-label="Search sessions" value={query} onChange={e=>setQuery(e.target.value)} placeholder="Search conversations…"/></label>
        <button className="filterToggle" type="button" aria-controls="session-filters" aria-expanded={filtersOpen} onClick={()=>setFiltersOpen(value=>!value)}>{filtersOpen?"Hide filters":"Show filters"}{activeFilterCount>0&&<b>{activeFilterCount}</b>}</button>
      </div>
      <div className="sourceTabs" role="group" aria-label="Conversation source"><button className={!source?"active":""} onClick={()=>setSource("")}>All</button><button className={source==="codex"?"active":""} onClick={()=>setSource("codex")}>Codex</button><button className={source==="chatgpt"?"active":""} onClick={()=>setSource("chatgpt")}>ChatGPT</button></div>
      <div id="session-filters" data-testid="session-filters" className={`sessionFilters ${filtersOpen?"open":""}`}>
        <select aria-label="Source filter" value={source} onChange={e=>setSource(e.target.value)}><option value="">Codex + ChatGPT</option><option value="codex">Codex</option><option value="chatgpt">ChatGPT</option></select>
        <select aria-label="Repository filter" value={repo} onChange={e=>setRepo(e.target.value)}><option value="">All repositories</option>{repos.map(value=><option key={value}>{value}</option>)}</select>
        <select aria-label="Model filter" value={model} onChange={e=>setModel(e.target.value)}><option value="">All models</option>{models.map(value=><option key={value}>{value}</option>)}</select>
        <input aria-label="Date filter" type="date" value={date} onChange={e=>setDate(e.target.value)}/>
        <div className="filterChecks"><label><input type="checkbox" checked={highTokens} onChange={e=>setHighTokens(e.target.checked)}/>100k+ tokens</label><label><input type="checkbox" checked={failures} onChange={e=>setFailures(e.target.checked)}/>Failures</label><label><input type="checkbox" checked={findings} onChange={e=>setFindings(e.target.checked)}/>Findings</label><label><input type="checkbox" checked={internal} onChange={e=>setInternal(e.target.checked)}/>Internal reviews</label></div>
      </div>
      <div className="railHeader"><span>Recent conversations</span><small>{filtered.length} · newest first</small></div>
      <div className="sessionItems" aria-label="Filtered conversations">{filtered.map(session=><button key={session.id} className={`sessionItem ${selected===session.id?"active":""}`} aria-pressed={selected===session.id} onClick={()=>select(session.id)}><span className="sessionItemTop"><span className="sessionSourceIcon" aria-hidden="true">{session.source==="chatgpt"?"✦":"◈"}</span><span className="sessionTitle">{session.title}</span></span><span className="sessionMeta">{session.source==="chatgpt"?"ChatGPT":session.repo_display_name||"Codex"}<i>·</i>{duration(session.metrics?.duration_ms||0)}</span><span className="sessionStats">{session.session_kind==="internal_review"&&<span>internal</span>}<span>{compact(session.metrics?.input_tokens||0)} input</span><span>{session.metrics?.mcp_calls||0} MCP</span><span>{session.metrics?.finding_count||0} findings</span></span></button>)}{filtered.length===0&&<div className="railEmpty"><span>⌕</span><b>No conversations match</b><small>Try clearing one of the active filters.</small></div>}</div>
    </div>
  </aside>;
}
