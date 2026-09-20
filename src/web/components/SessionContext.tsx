/** @module SessionContext @description Displays observed client and execution metadata without guessing unavailable device details. */
import React from "react";
import {absoluteTime} from "../format";
import type {Session} from "../types";
import {modelSettingPeriods,usageDeltaLabel} from "../usageMetrics";

export function SessionContext({session}:{session:Session}){
  const settings=modelSettingPeriods(session.turns||[]);
  const values=[
    ["Started",absoluteTime(session.started_at)],
    ["Client / device",session.originator],
    ["Surface",session.surface],
    ["Work type",session.session_kind?.replaceAll("_"," ")],
    ["Run location",session.runtime_location?`${session.runtime_location} (derived)`:undefined],
    ["Thread source",session.thread_source],
    ["Approvals",session.approval_mode],
    ["Sandbox",session.sandbox_policy],
    ["Network",session.network_access],
  ].filter((entry):entry is [string,string]=>Boolean(entry[1]));
  return <section className="card sessionContext"><div className="sectionTitle"><h3>Session context</h3><span className="pill">observed</span></div><dl>{values.map(([label,value])=><div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>{session.title_source==="renamed"&&<p className="titleSource">Title renamed in Codex</p>}{settings.length>0&&<div className="modelHistory"><div className="modelHistoryTitle"><h4>Model & reasoning history</h4><small>{settings.length===1?"No setting change observed":`${settings.length-1} setting change${settings.length===2?"":"s"} observed`}</small></div>{settings.map(period=><div className="modelHistoryRow" key={`${period.key}-${period.firstSequence}`}><span className="modelMarker" aria-hidden="true"/><div><b>{period.model}</b><small>{period.effort} reasoning · prompt{period.firstSequence===period.lastSequence?` ${period.firstSequence}`:`s ${period.firstSequence}–${period.lastSequence}`}</small></div><time dateTime={period.startedAt}>{absoluteTime(period.startedAt)}</time>{period.measuredTurns>0?<em title="Observed change in the account-wide rate meter; other concurrent chats may contribute.">{usageDeltaLabel(period.usageDelta)} observed</em>:<em>usage unavailable</em>}</div>)}<p className="usageNote">Usage is the observed change in the account-wide rate meter while these prompts ran. Concurrent chats can contribute, so it is an estimate—not exact billing or causal attribution.</p></div>}</section>;
}
