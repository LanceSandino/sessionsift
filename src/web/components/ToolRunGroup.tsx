/** @module ToolRunGroup @description One prompt's tool activity as an expandable group whose rows open their evidence. */
import React,{useState} from "react";
import type {ToolCall} from "../types";
import {callLabel,callStatus,runOutcome,transportLabel} from "../sessionEvidence";
import type {Evidence} from "../workspace";
import {Icon} from "./Icon";

const rowLimit=25;

type Props={calls:ToolCall[];open:boolean;selected?:Evidence;onToggle:(open:boolean)=>void;onSelect:(evidence:Evidence)=>void};

export function ToolRunGroup({calls,open,selected,onToggle,onSelect}:Props){
  const[all,setAll]=useState(false);
  const outcome=runOutcome(calls);
  const kinds=Object.entries(calls.reduce<Record<string,number>>((counts,call)=>{const key=call.tool_category||"other";counts[key]=(counts[key]||0)+1;return counts},{})).sort((a,b)=>b[1]-a[1]).slice(0,3);
  const shown=all?calls:calls.slice(0,rowLimit);
  // The summary toggles explicitly so the open state stays controlled by the parent.
  return <details className="execution" open={open}>
    <summary onClick={event=>{event.preventDefault();onToggle(!open)}}><Icon name="tool"/><strong>{calls.length} tool call{calls.length===1?"":"s"}</strong><span className="run-kinds">· {kinds.map(([kind,count])=>`${count} ${kind}`).join(" · ")}</span>{outcome.failed>0&&<span className="summary-warn">{outcome.failed} failed{outcome.recovered?" → later calls succeeded":""}</span>}<span className="chevron"><Icon name="chevron"/></span></summary>
    {open&&<div className="execution-list">
      {shown.map(call=>{const status=callStatus(call);const external=call.tool_transport==="mcp"||call.tool_transport==="app";const isSelected=selected?.kind==="tool"&&selected.id===call.id;return <button type="button" key={call.id} className={`exec-row ${call.success?"":"failed"} ${isSelected?"selected":""}`} aria-label={`Inspect ${call.tool_name}: ${callLabel(call)} (${status})`} onClick={()=>onSelect({kind:"tool",id:call.id})}>
        <Icon name={call.success?"check":"warning"}/><span className="tool-name">{call.tool_server&&external?call.tool_server:call.tool_name}</span><span className="exec-label">{callLabel(call)}</span><span className="exec-status">{external&&call.success?transportLabel(call):status}</span><Icon name="chevron"/>
      </button>})}
      {calls.length>shown.length&&<button type="button" className="exec-more" onClick={()=>setAll(true)}>Show all {calls.length} calls</button>}
    </div>}
  </details>;
}
