/** @module Analytics @description Privacy-safe aggregate patterns across the local library; no conversation text, arguments or results. */
import React from "react";
import {compact,duration,percent,plural} from "../format";
import {sources} from "../sources";
import type {AnalyticsBreakdown,AnalyticsReport} from "../types";
import {Icon} from "./Icon";

const sourceLabels:Record<string,string>=Object.fromEntries(sources.map(source=>[source.id,source.label]));

function Bars({title,items,total,label}:{title:string;items:AnalyticsBreakdown[];total:number;label:(name:string)=>string}){
  const maximum=Math.max(1,...items.map(item=>item.sessions));
  return <section className="aggregate-panel"><div className="row between"><h3>{title}</h3><span className="small muted">{total} total</span></div>
    {items.length?items.map(item=><div className="bar-row" key={item.name}><div className="row between"><span>{label(item.name)}</span><span className="mono">{item.sessions} <span className="muted small">{item.sessions===1?"session":"sessions"} · {compact(item.input_tokens)} input · {item.tool_calls} tools</span></span></div><div className="bar"><span style={{width:`${Math.max(4,item.sessions/maximum*100)}%`}}/></div></div>):<p className="empty">No observed data yet.</p>}
  </section>;
}

export function Analytics({report}:{report:AnalyticsReport}){
  const {summary}=report;
  const inventory=report.inventory||{stored_records:summary.sessions,included_sessions:summary.sessions,internal_review_records:0,telemetry_only_records:0,parser_warning_sessions:0,parser_warnings:0,skipped_record_warnings:0};
  const dailyMax=Math.max(1,...report.daily.map(day=>day.input_tokens));
  return <div className="content-page" aria-label="Analytics">
    <div className="page-intro"><span className="eyebrow">Across your local library</span><h2>The pattern, not the transcript.</h2><p>{plural(summary.sessions,"session")} from {report.sources.length} source{report.sources.length===1?"":"s"}. Aggregates only—no conversation text, tool arguments or tool results enter this report.</p></div>
    <section className="aggregate-panel analytics-inventory" aria-label="Analytics coverage"><h3>What these totals include</h3><p>{inventory.included_sessions.toLocaleString()} analyzed conversations from {inventory.stored_records.toLocaleString()} stored records. {inventory.internal_review_records.toLocaleString()} internal auto-review{inventory.internal_review_records===1?"":"s"} and {inventory.telemetry_only_records.toLocaleString()} telemetry-only placeholder{inventory.telemetry_only_records===1?"":"s"} are intentionally excluded.</p><p className="small muted">{inventory.parser_warnings.toLocaleString()} parser notice{inventory.parser_warnings===1?"":"s"} across {inventory.parser_warning_sessions.toLocaleString()} session{inventory.parser_warning_sessions===1?"":"s"}; {inventory.skipped_record_warnings.toLocaleString()} may include skipped records. Other notices are non-fatal compatibility or informational conditions; source transcripts remain untouched.</p></section>
    <div className="summary-metrics" aria-label="Analytics summary">
      <div className="metric"><span className="label">Sessions</span><strong>{summary.sessions}</strong><small>{summary.turns} prompts</small></div>
      <div className="metric"><span className="label">Active work</span><strong>{duration(summary.duration_ms)}</strong><small>overlapping prompt time counted once</small></div>
      <div className="metric"><span className="label">Input tokens</span><strong>{compact(summary.input_tokens)}</strong><small>{compact(summary.output_tokens)} output</small></div>
      <div className="metric"><span className="label">Cache reuse</span><strong>{percent(summary.cache_ratio)}</strong><small>{compact(summary.cached_input_tokens)} cached</small></div>
      <div className="metric"><span className="label">Tool calls</span><strong>{summary.tool_calls.toLocaleString()}</strong><small>{summary.mcp_calls} MCP · {summary.app_tool_calls} app</small></div>
      <div className={`metric ${summary.failed_tool_calls?"alert":""}`}><span className="label">Failed calls</span><strong>{summary.failed_tool_calls}</strong><small>{summary.findings} findings</small></div>
    </div>
    <div className="aggregate-grid">
      <Bars title="Sessions by source" items={report.sources} total={summary.sessions} label={name=>sourceLabels[name]||name}/>
      <section className="aggregate-panel"><h3>A useful signal, not a score.</h3><p>Exploration ratios, failed calls and cache reuse describe observed activity. They are not automatic judgments about the quality of a session.</p>
        <div className="row between panel-subhead"><h3>Activity by start date</h3><span className="small muted">{report.daily.length} days</span></div>
        {report.daily.length?<div className="trend" aria-label="Input tokens by start date">{report.daily.map(day=><div className="trend-day" key={day.date} title={`${day.date}: ${plural(day.sessions,"session")} · ${compact(day.input_tokens)} input tokens`}><span style={{height:`${Math.max(4,day.input_tokens/dailyMax*100)}%`}}/><small>{day.date.slice(5)}</small></div>)}</div>:<p className="empty">Trends appear after conversations are imported.</p>}
      </section>
    </div>
    <div className="aggregate-grid">
      <Bars title="Models" items={report.models} total={summary.sessions} label={name=>name}/>
      <Bars title="Repositories" items={report.repositories} total={summary.sessions} label={name=>name}/>
    </div>
    <div className="section-head"><h3>Tools used</h3><small>observed</small></div>
    {report.tools.length?<div className="table-wrap"><table><thead><tr><th>Tool</th><th>Transport</th><th className="num">Calls</th><th className="num">Failed</th></tr></thead><tbody>{report.tools.map(tool=><tr key={`${tool.transport}-${tool.server||""}-${tool.name}`}><td><span className="mono">{tool.name}</span>{tool.server&&<span className="cell-sub">{tool.server}</span>}</td><td><span className="badge">{tool.transport}</span></td><td className="num">{tool.calls.toLocaleString()}</td><td className="num">{tool.failed||"—"}</td></tr>)}</tbody></table></div>:<p className="empty">No tools observed yet.</p>}
    <div className="section-head"><h3>Recommendations</h3><small>deterministic</small></div>
    {report.recommendations.length?report.recommendations.map(item=><article className="recommendation" key={item.id}><Icon name="spark"/><div><b>{item.title}</b><p>{item.summary}</p><small>{item.confidence} · {item.evidence.join(" · ")}</small></div></article>):<p className="empty">No recurring pattern currently crosses a recommendation threshold.</p>}
    {report.findings.length>0&&<><div className="section-head"><h3>Finding patterns</h3><small>aggregate only</small></div>
      <div className="table-wrap"><table><thead><tr><th>Pattern</th><th>Confidence</th><th className="num">Findings</th><th className="num">Sessions</th></tr></thead><tbody>{report.findings.map(item=><tr key={`${item.type}-${item.severity}-${item.confidence}`}><td>{item.type.replaceAll("_"," ")}</td><td><span className="badge">{item.confidence}</span></td><td className="num">{item.count}</td><td className="num">{item.sessions}</td></tr>)}</tbody></table></div></>}
  </div>;
}
