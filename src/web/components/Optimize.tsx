/** @module Optimize @description Runs a local recommendation-to-experiment loop with observed deltas and privacy-safe reports. */
import React,{useCallback,useEffect,useMemo,useState} from "react";
import {api} from "../api";
import {compact} from "../format";
import {formatOptimizationDelta,measurementUpdate,recommendationStates} from "../optimization";
import type {OptimizationExperiment,OptimizationOverview,RecommendationState,Session} from "../types";
import {Icon} from "./Icon";

export function Optimize({sessions}:{sessions:Session[]}){
  const[overview,setOverview]=useState<OptimizationOverview>();
  const[error,setError]=useState("");
  const[busy,setBusy]=useState("");
  const[title,setTitle]=useState("");
  const[hypothesis,setHypothesis]=useState("");
  const[baseline,setBaseline]=useState(sessions[0]?.id||"");
  const[recommendation,setRecommendation]=useState("");
  const[followups,setFollowups]=useState<Record<string,string>>({});
  const load=useCallback(()=>api.optimization().then(value=>{setOverview(value);setError("")}).catch(value=>setError(String(value))),[]);

  useEffect(()=>{void load();void api.recordAdoption("optimize_viewed").catch(()=>undefined)},[load]);
  useEffect(()=>{if(!baseline&&sessions[0])setBaseline(sessions[0].id)},[baseline,sessions]);

  const sessionOptions=useMemo(()=>sessions.map(session=><option key={session.id} value={session.id}>{session.title}</option>),[sessions]);
  const create=async(event:React.FormEvent)=>{
    event.preventDefault();
    if(!title.trim()||!baseline)return;
    setBusy("create");setError("");
    try{
      await api.createExperiment({title:title.trim(),hypothesis:hypothesis.trim()||undefined,recommendation_id:recommendation||undefined,baseline_session_id:baseline});
      setTitle("");setHypothesis("");setRecommendation("");await load();
    }catch(value){setError(String(value))}finally{setBusy("")}
  };
  const setState=async(id:string,state:RecommendationState)=>{
    setBusy(`recommendation:${id}`);setError("");
    try{await api.setRecommendation(id,state);setOverview(value=>value&&({...value,recommendations:value.recommendations.map(item=>item.id===id?{...item,state}:item)}))}catch(value){setError(String(value))}finally{setBusy("")}
  };
  const measure=async(experiment:OptimizationExperiment)=>{
    const followup=followups[experiment.id]||"";
    if(!followup)return;
    setBusy(`experiment:${experiment.id}`);setError("");
    try{await api.updateExperiment(measurementUpdate(experiment,followup));await load()}catch(value){setError(String(value))}finally{setBusy("")}
  };

  if(error&&!overview)return <div className="content-page optimize-page"><div className="evidence-banner" role="alert">{error}</div></div>;
  if(!overview)return <div className="content-page optimize-page"><p className="empty">Preparing your local optimization loop…</p></div>;
  const {summary,adoption}=overview;
  return <div className="content-page optimize-page" aria-label="Optimize">
    <div className="page-intro optimize-intro"><div><span className="eyebrow">Personal optimization loop</span><h2>Turn patterns into a measurable change.</h2><p>Choose a recommendation, try it in a later session, then compare only observed metrics. SessionSift never assigns a productivity score or declares a winner.</p></div><div className="report-actions" aria-label="Report exports"><a className="button" href="/api/v1/optimization/report.md" download><Icon name="file"/>Download Markdown</a><a className="button" href="/api/v1/optimization/report.json" download><Icon name="code"/>Download JSON</a></div></div>
    {error&&<div className="evidence-banner" role="alert">{error}</div>}
    <div className="optimization-shell">
      <section className="optimization-main">
        <div className="loop-steps" aria-label="Optimization workflow"><div className="active"><span>1</span><b>Choose</b><small>a pattern</small></div><Icon name="arrow"/><div><span>2</span><b>Try</b><small>one change</small></div><Icon name="arrow"/><div><span>3</span><b>Measure</b><small>observed deltas</small></div></div>
        <div className="section-head"><h3>Recommendations</h3><small>deterministic · owner controlled</small></div>
        {overview.recommendations.length?<div className="recommendation-grid">{overview.recommendations.map(item=><article className="recommendation optimization-recommendation" key={item.id}><Icon name="spark"/><div className="grow"><div className="row between"><b>{item.title}</b><span className={`status-pill ${item.state}`}>{item.state}</span></div><p>{item.summary}</p><small>{item.confidence} · {item.evidence.join(" · ")}</small><label className="compact-field">State for {item.title}<select className="field" aria-label={`State for ${item.title}`} value={item.state} disabled={busy===`recommendation:${item.id}`} onChange={event=>void setState(item.id,event.target.value as RecommendationState)}>{recommendationStates.map(state=><option value={state} key={state}>{state[0].toUpperCase()+state.slice(1)}</option>)}</select></label></div></article>)}</div>:<p className="empty">No recurring pattern currently crosses a recommendation threshold. You can still start a manual experiment.</p>}
        <div className="section-head"><h3>Experiments</h3><small>{summary.running_experiments} running · {summary.measured_experiments} measured</small></div>
        {overview.experiments.length?<div className="experiment-list">{overview.experiments.map(experiment=><article className="experiment-card" key={experiment.id}><div className="experiment-head"><div><span className="eyebrow">{experiment.status}</span><h4>{experiment.title}</h4>{experiment.hypothesis&&<p>{experiment.hypothesis}</p>}</div><span className={`status-pill ${experiment.status}`}>{experiment.status}</span></div><div className="experiment-route"><div><small>Baseline</small><b>{experiment.baseline_title||"Session unavailable"}</b></div><Icon name="arrow"/><div><small>Follow-up</small><b>{experiment.followup_title||"Waiting for another session"}</b></div></div>{experiment.measurement?<><div className="delta-grid" aria-label={`Observed deltas for ${experiment.title}`}>{experiment.measurement.deltas.map(delta=><div className="delta" key={delta.metric}><small>{delta.metric}</small><strong className={delta.delta>0?"up":delta.delta<0?"down":""}>{formatOptimizationDelta(delta.delta,delta.unit)}</strong><span>{compact(delta.baseline)} → {compact(delta.followup)}</span></div>)}</div><p className="fine-print"><span className="badge">{experiment.measurement.confidence}</span> Raw follow-up minus baseline; direction is context, not a judgment.</p></>:<div className="measure-row"><label>Follow-up for {experiment.title}<select className="field" aria-label={`Follow-up for ${experiment.title}`} value={followups[experiment.id]||""} onChange={event=>setFollowups(value=>({...value,[experiment.id]:event.target.value}))}><option value="">Choose a later session</option>{sessions.filter(session=>session.id!==experiment.baseline_session_id).map(session=><option key={session.id} value={session.id}>{session.title}</option>)}</select></label><button className="button primary" type="button" disabled={!followups[experiment.id]||busy===`experiment:${experiment.id}`} aria-label={`Measure ${experiment.title}`} onClick={()=>void measure(experiment)}>Measure</button></div>}</article>)}</div>:<p className="empty">No experiments yet.</p>}
      </section>
      <aside className="optimization-side">
        <form className="optimization-form" onSubmit={create}><span className="eyebrow">Next change</span><h3>Start an experiment</h3><p>Anchor one change to a baseline now; attach a follow-up session when the next comparable attempt is complete.</p><label>Experiment title<input className="field" aria-label="Experiment title" value={title} maxLength={160} onChange={event=>setTitle(event.target.value)} placeholder="e.g. Validate before refactoring"/></label><label>Hypothesis<textarea className="field" aria-label="Hypothesis" value={hypothesis} maxLength={500} onChange={event=>setHypothesis(event.target.value)} placeholder="What observable change do you expect?"/></label><label>Baseline session<select className="field" aria-label="Baseline session" value={baseline} onChange={event=>setBaseline(event.target.value)}><option value="">Choose a session</option>{sessionOptions}</select></label><label>Recommendation (optional)<select className="field" value={recommendation} onChange={event=>setRecommendation(event.target.value)}><option value="">Manual experiment</option>{overview.recommendations.map(item=><option key={item.id} value={item.id}>{item.title}</option>)}</select></label><button className="button primary" type="submit" disabled={!title.trim()||!baseline||busy==="create"}><Icon name="plus"/>Start experiment</button></form>
        <section className="weekly-card"><span className="eyebrow">Last 7 days</span><h3>Weekly report</h3><div className="weekly-grid"><div><strong>{summary.sessions}</strong><small>sessions</small></div><div><strong>{summary.turns}</strong><small>prompts</small></div><div><strong>{summary.tool_calls}</strong><small>tool calls</small></div><div><strong>{summary.failed_tool_calls}</strong><small>failed</small></div></div><p>{summary.findings} findings observed. Reports are generated on demand from current local aggregates.</p></section>
        <section className="weekly-card"><span className="eyebrow">Local adoption</span><h3>Is the loop being used?</h3><div className="weekly-grid"><div><strong>{adoption.active_days}</strong><small>active days</small></div><div><strong>{adoption.experiments_created}</strong><small>experiments</small></div><div><strong>{adoption.recommendations_changed}</strong><small>decisions</small></div><div><strong>{adoption.reports_exported}</strong><small>exports</small></div></div><p>No telemetry is sent anywhere. These counters have no transcript, session, or actor dimension.</p></section>
        <details className="privacy-card"><summary><Icon name="shield"/>Privacy boundary</summary>{overview.privacy.map(statement=><p key={statement}>{statement}</p>)}</details>
      </aside>
    </div>
  </div>;
}
