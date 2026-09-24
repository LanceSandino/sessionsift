/** @module Compare @description Compares observable metrics of two sessions without assigning a winner. */
import React,{useEffect,useState} from "react";
import {api} from "../api";
import {compact,duration,percent} from "../format";
import type {ComparePayload,Metrics,Session} from "../types";

const rows:[string,(m:Metrics)=>string][]=[["Input tokens",m=>compact(m.input_tokens)],["Cache reuse",m=>percent(m.cache_ratio)],["Tool calls",m=>String(m.tool_calls)],["Repeated findings",m=>String(m.finding_count)],["Failed calls",m=>String(m.failed_tool_calls)],["First edit",m=>m.time_to_first_edit_ms?duration(m.time_to_first_edit_ms):"Not observed"],["First validation",m=>m.time_to_first_validation_ms?duration(m.time_to_first_validation_ms):"Not observed"],["Active work",m=>duration(m.duration_ms)],["Elapsed span",m=>duration(m.elapsed_duration_ms)],["Observed account usage",m=>m.usage_measured_turns?`+${m.usage_delta_percent.toFixed(1)} points`:"Unavailable"]];

export function Compare({sessions,initial}:{sessions:Session[];initial?:string}){
  const[a,setA]=useState(initial||sessions[0]?.id||"");
  const[b,setB]=useState(sessions.find(item=>item.id!==(initial||sessions[0]?.id))?.id||"");
  const[data,setData]=useState<ComparePayload>();
  const[error,setError]=useState("");
  useEffect(()=>{
    if(!a||!b||a===b){setData(undefined);return}
    let active=true;
    api.compare(a,b).then(value=>{if(active){setData(value);setError("")}}).catch(value=>{if(active)setError(String(value))});
    return()=>{active=false};
  },[a,b]);
  return <div className="content-page" aria-label="Compare">
    <div className="page-intro"><span className="eyebrow">Evidence, not a score</span><h2>Compare two attempts.</h2><p>See observable differences between two sessions. SessionSift does not declare a winner.</p></div>
    <div className="compare-pickers">
      <label>Session A<select className="field" value={a} onChange={event=>setA(event.target.value)}>{sessions.map(session=><option key={session.id} value={session.id}>{session.title}</option>)}</select></label>
      <span aria-hidden="true">⇄</span>
      <label>Session B<select className="field" value={b} onChange={event=>setB(event.target.value)}>{sessions.map(session=><option key={session.id} value={session.id}>{session.title}</option>)}</select></label>
    </div>
    {error&&<div className="evidence-banner" role="alert">{error}</div>}
    {data?<>
      <div className="table-wrap"><table><thead><tr><th>Observed metric</th><th>A</th><th>B</th></tr></thead><tbody>{rows.map(([label,format])=><tr key={label}><td>{label}</td><td className="mono">{format(data.metrics_a)}</td><td className="mono">{format(data.metrics_b)}</td></tr>)}</tbody></table></div>
      <div className="section-head"><h3>Observable differences</h3><small>derived</small></div>
      {data.differences.map((value,index)=><div className="difference" key={value}><span>{String(index+1).padStart(2,"0")}</span><p>{value}</p></div>)}
      {!data.differences.length&&<p className="empty">No observable differences crossed a reporting threshold.</p>}
    </>:<p className="empty">Select two different sessions to compare.</p>}
  </div>;
}
