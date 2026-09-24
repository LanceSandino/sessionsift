/** @module ToolsView @description The tools lens: built-in, MCP and app activity kept distinct, every call selectable, and deterministic findings. */
import React,{useEffect,useMemo,useState} from "react";
import {percent,twoDigits} from "../format";
import {callLabel,callStatus,callsByTurn,explorationCounts,transportLabel} from "../sessionEvidence";
import type {Session,ToolCall} from "../types";
import type {Evidence,WorkspaceActions} from "../workspace";
import {FindingList} from "./FindingList";
import {latestWindow} from "./timelineWindow";

type Group={key:string;name:string;transport:string;server?:string;calls:number;failed:number;latestID:string;latestAt:string};
const pageSize=60;

export function ToolsView({session,selected,actions}:{session:Session;selected?:Evidence;actions:Pick<WorkspaceActions,"selectEvidence">}){
  const calls=useMemo(()=>(session.tool_calls||[]).slice().sort((a,b)=>a.timestamp.localeCompare(b.timestamp)),[session.tool_calls]);
  const promptOf=useMemo(()=>{const index=new Map<string,number>();const sequence=new Map((session.turns||[]).map(turn=>[turn.id,turn.sequence]));for(const[turnID,values]of callsByTurn(session))for(const call of values)index.set(call.id,sequence.get(turnID)||0);return index},[session]);
  const groups=useMemo(()=>groupCalls(calls),[calls]);
  const[limit,setLimit]=useState(pageSize);
  useEffect(()=>setLimit(pageSize),[session.id]);
  const{shown,remaining}=latestWindow(calls,limit);
  const builtin=calls.filter(call=>(call.tool_transport||"builtin")==="builtin");
  const external=calls.filter(call=>call.tool_transport==="mcp"||call.tool_transport==="app");
  const servers=[...new Set(external.map(call=>call.tool_server).filter(Boolean))];
  const failed=calls.filter(call=>!call.success).length;
  const exploration=explorationCounts(calls);
  return <div className="content-page" aria-label="Tools">
    <div className="page-intro"><h2>Every action has a source.</h2><p>Built-in, MCP and app activity, kept distinct. Select a call to inspect its redacted arguments and result.</p></div>
    <div className="summary-metrics">
      <div className="metric"><span className="label">Built-in calls</span><strong>{builtin.length.toLocaleString()}</strong><small>{new Set(builtin.map(call=>call.tool_name)).size} unique tools</small></div>
      <div className="metric"><span className="label">MCP & app calls</span><strong>{external.length.toLocaleString()}</strong><small>{servers.length?servers.slice(0,3).join(" · "):"none observed"}</small></div>
      <div className={`metric ${failed?"alert":""}`}><span className="label">Failed calls</span><strong>{failed.toLocaleString()}</strong><small>kept as evidence</small></div>
      <div className="metric"><span className="label">Exploration</span><strong>{calls.length?percent(exploration.explore/exploration.total).replace("%",""):"—"}{calls.length>0&&<span className="unit">%</span>}</strong><small>{exploration.explore} reads / searches of {exploration.total}</small></div>
    </div>
    {failed>0&&<div className="evidence-banner">Failed calls are preserved as evidence. A later passing result does not erase them or prove every issue is resolved.</div>}
    <div className="section-head"><h3>Tools used</h3><small>{groups.length} unique</small></div>
    {groups.length?<div className="table-wrap"><table><thead><tr><th>Tool</th><th>Type</th><th className="num">Calls</th><th className="num">Failed</th></tr></thead><tbody>
      {groups.map(group=><tr key={group.key} className="clickable" tabIndex={0} aria-label={`Inspect the latest ${group.name} call`} onClick={()=>actions.selectEvidence({kind:"tool",id:group.latestID})} onKeyDown={event=>activate(event,()=>actions.selectEvidence({kind:"tool",id:group.latestID}))}><td><span className="mono">{group.server&&group.transport!=="builtin"?`${group.server} ${group.transport.toUpperCase()}`:group.name}</span>{group.server&&group.transport!=="builtin"&&<span className="cell-sub">{group.name}</span>}</td><td><span className="badge">{group.transport==="mcp"?"MCP":group.transport==="app"?"App":"Built-in"}</span></td><td className="num">{group.calls.toLocaleString()}</td><td className="num">{group.failed||"—"}</td></tr>)}
    </tbody></table></div>:<p className="empty">No tool calls were observed.</p>}
    {calls.length>0&&<>
      <div className="section-head"><h3>Calls</h3><small>{remaining>0?`Latest ${shown.length} of ${calls.length.toLocaleString()}`:`${calls.length.toLocaleString()} in order`}</small></div>
      {remaining>0&&<button type="button" className="load-more" onClick={()=>setLimit(value=>value+pageSize*4)}>Show {Math.min(remaining,pageSize*4)} earlier calls</button>}
      <div className="table-wrap"><table><thead><tr><th>Tool / action</th><th>Type</th><th>Prompt</th><th>Result</th></tr></thead><tbody>
        {shown.map(call=><CallRow key={call.id} call={call} prompt={promptOf.get(call.id)||0} selected={selected?.kind==="tool"&&selected.id===call.id} onSelect={()=>actions.selectEvidence({kind:"tool",id:call.id})}/>)}
      </tbody></table></div>
    </>}
    <div className="section-head"><h3>What SessionSift noticed</h3><small>{(session.findings||[]).length} findings · deterministic</small></div>
    <FindingList findings={session.findings||[]} onSelect={actions.selectEvidence}/>
  </div>;
}

function CallRow({call,prompt,selected,onSelect}:{call:ToolCall;prompt:number;selected:boolean;onSelect:()=>void}){
  const status=callStatus(call);
  return <tr className="clickable" tabIndex={0} aria-current={selected?"true":undefined} aria-label={`Inspect ${call.tool_name}: ${callLabel(call)}`} onClick={onSelect} onKeyDown={event=>activate(event,onSelect)}>
    <td><span className="mono">{call.tool_name}</span><span className="cell-sub">{callLabel(call)}</span></td>
    <td><span className="badge">{transportLabel(call)}</span></td>
    <td className="mono">{prompt?twoDigits(prompt):"—"}</td>
    <td><span className={`badge ${status==="Failed"?"warn":status==="Passed"?"good":""}`}>{status}{call.exit_code!==undefined&&!call.success?` · exit ${call.exit_code}`:""}</span></td>
  </tr>;
}

function activate(event:React.KeyboardEvent,run:()=>void){
  if(event.key==="Enter"||event.key===" "){event.preventDefault();run()}
}

export function groupCalls(calls:ToolCall[]):Group[]{
  const groups=new Map<string,Group>();
  for(const call of calls){
    const transport=call.tool_transport||"builtin";
    const key=`${transport}:${call.tool_server||""}:${call.tool_name}`;
    const group=groups.get(key)||{key,name:call.tool_name,transport,server:call.tool_server,calls:0,failed:0,latestID:call.id,latestAt:call.timestamp};
    group.calls++;
    if(!call.success)group.failed++;
    if(call.timestamp>=group.latestAt){group.latestAt=call.timestamp;group.latestID=call.id}
    groups.set(key,group);
  }
  return [...groups.values()].sort((a,b)=>b.calls-a.calls||a.name.localeCompare(b.name));
}
